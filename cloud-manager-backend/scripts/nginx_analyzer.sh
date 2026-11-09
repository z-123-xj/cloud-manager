#!/bin/bash

export LC_ALL=C

# 目录可通过环境变量覆盖，便于在测试机上用合成日志验证
CURRENT_LOG_DIR="${NGINX_CURRENT_LOG_DIR:-/usr/local/nginx/logs}"
BACKUP_LOG_DIR="${NGINX_BACKUP_LOG_DIR:-/data/backup/nginx_log}"
# 切割脚本在备份原始日志时，同步把日志解析成分析所需的 7 列 TSV 压缩放这里
PREPARED_LOG_DIR="${NGINX_PREPARED_LOG_DIR:-${BACKUP_LOG_DIR}/analysis}"
NGINX_CONF_DIRS="${NGINX_CONF_DIRS:-/etc/nginx /usr/local/nginx/conf /opt/nginx/conf /usr/local/etc/nginx /etc/nginx/conf.d}"

# 预生成 TSV 首行的版本标记，配合 log_format 指纹一起决定能否直接复用
PREPARED_MAGIC="#nginx-prepared-v1"

# 两种调用方式：
#   1) 平台在线分析：9 个参数（见下方）
#   2) 切割脚本预生成：--prepare <原始日志> <log_format模板|auto>
DOMAINS_STR=""
START_TIME=""
END_TIME=""
STATUS=""
URL_KEYWORD=""
IP_FILTER=""
START_DATE=""
END_DATE=""
FORMAT_TEMPLATE=""
MODE="analyze"
PREP_IN=""
if [ "${1:-}" = "--prepare" ]; then
    MODE="prepare"
    PREP_IN="${2:-}"
    FORMAT_TEMPLATE="${3:-auto}"
else
    DOMAINS_STR="${1:-}"
    START_TIME="${2:-}"
    END_TIME="${3:-}"
    STATUS="${4:-}"
    URL_KEYWORD="${5:-}"
    IP_FILTER="${6:-}"
    START_DATE="${7:-}"
    END_DATE="${8:-}"
    # 第 9 个参数：nginx log_format 模板。空或 auto = 脚本自行从 nginx 配置探测；
    # 平台会把探查到的模板传进来，保证「预览的格式」和「实际分析用的格式」是同一个。
    FORMAT_TEMPLATE="${9:-}"
fi

TASK_ID=$(date +%s%N)
TEMP_BASE="${NGINX_TEMP_BASE:-/var/tmp/nginx_analyzer}"
TEMP_DIR="${TEMP_BASE}/${TASK_ID}"
TOP_N=10
mkdir -p "$TEMP_DIR" || { echo '{"error":"无法创建临时目录"}'; exit 1; }
cleanup() { rm -rf "$TEMP_DIR"; }
trap cleanup EXIT

# ================= 0. 探测 Nginx log_format =================
# 输出一行模板：优先取 access_log 实际引用的格式名，其次取含 $request 的格式。
# nginx -T 能展开 include，优先使用；失败再扫常见的配置目录。
FORMAT_AWK="${TEMP_DIR}/nginx_log_format.awk"
cat > "$FORMAT_AWK" <<'AWK'
BEGIN {
    default_combined = "$remote_addr - $remote_user [$time_local] \"$request\" $status $body_bytes_sent \"$http_referer\" \"$http_user_agent\""
}

# 拼接 log_format 里所有被引号包住的片段（nginx 允许跨行写多个字符串）
function quoted_parts(s,   out, rest, chunk) {
    out = ""
    rest = s
    while (match(rest, /'[^']*'|"[^"]*"/)) {
        chunk = substr(rest, RSTART + 1, RLENGTH - 2)
        out = out chunk
        rest = substr(rest, RSTART + RLENGTH)
    }
    return out
}

function process(d,   r, name, tpl, n, parts) {
    r = d
    if (r ~ /^[ \t]*log_format[ \t]/) {
        sub(/^[ \t]*log_format[ \t]+/, "", r)
        # 跳过可选的 escape=json / escape=default
        while (r ~ /^[A-Za-z_]+=[^ \t]+[ \t]+/) sub(/^[A-Za-z_]+=[^ \t]+[ \t]+/, "", r)
        if (!match(r, /^[^ \t;]+/)) return
        name = substr(r, 1, RLENGTH)
        r = substr(r, RLENGTH + 1)
        tpl = quoted_parts(r)
        if (tpl != "") formats[name] = tpl
    } else if (r ~ /^[ \t]*access_log[ \t]/) {
        sub(/^[ \t]*access_log[ \t]+/, "", r)
        sub(/;[ \t]*$/, "", r)
        n = split(r, parts, /[ \t]+/)
        if (n >= 1 && parts[1] == "off") return
        if (n >= 2 && parts[2] !~ /^\// && parts[2] !~ /^(buffer=|gzip|flush=|if=)/) used[parts[2]] = 1
        else used["combined"] = 1
    }
}

/^[ \t]*#/ { next }
inblock {
    buf = buf " " $0
    if ($0 ~ /;/) { process(buf); buf = ""; inblock = 0 }
    next
}
/^[ \t]*(log_format|access_log)[ \t]/ {
    buf = $0
    if ($0 ~ /;/) { process(buf); buf = "" } else { inblock = 1 }
    next
}
END {
    if ("combined" in used && !("combined" in formats)) formats["combined"] = default_combined
    picked = ""
    if ("main" in used && "main" in formats) picked = "main"
    else if ("combined" in used && "combined" in formats) picked = "combined"
    else for (n in used) if (n in formats && formats[n] ~ /\$request/) { picked = n; break }
    if (picked == "") for (n in formats) if (formats[n] ~ /\$request/) { picked = n; break }
    if (picked == "") for (n in formats) { picked = n; break }
    if (picked != "") print formats[picked]
}
AWK

detect_log_format() {
    if command -v nginx >/dev/null 2>&1; then
        local conf
        conf=$(nginx -T 2>/dev/null) || conf=""
        if [ -n "$conf" ]; then
            echo "$conf" | awk -f "$FORMAT_AWK" 2>/dev/null | head -1
            return
        fi
    fi
    # shellcheck disable=SC2086
    find $NGINX_CONF_DIRS -name '*.conf' -type f 2>/dev/null | head -200 \
        | xargs -r awk -f "$FORMAT_AWK" 2>/dev/null | head -1
}

if [ -n "${NGINX_LOG_FORMAT:-}" ]; then
    FORMAT_TEMPLATE="$NGINX_LOG_FORMAT"
    FORMAT_SOURCE="env"
elif [ -z "$FORMAT_TEMPLATE" ] || [ "$FORMAT_TEMPLATE" == "auto" ]; then
    FORMAT_TEMPLATE="$(detect_log_format)"
    FORMAT_SOURCE="nginx_conf"
else
    FORMAT_SOURCE="platform"
fi
if [ -z "$FORMAT_TEMPLATE" ]; then
    FORMAT_SOURCE="legacy"
fi
echo "[nginx_analyzer] log_format source=$FORMAT_SOURCE template=$FORMAT_TEMPLATE" >&2

# 预生成数据与本次分析必须使用同一份 log_format；指纹不一致时回退原始备份重解析。
# 目标机没有 md5sum 时统一退化为 nohash，闭环仍可正常工作。
if command -v md5sum >/dev/null 2>&1; then
    FORMAT_SUM=$(printf '%s' "$FORMAT_TEMPLATE" | md5sum | awk '{print $1}')
else
    FORMAT_SUM="nohash"
fi

# ================= 1. 解析文件列表（预生成优先 + 原始备份兜底） =================
# RAW_FILES ：原始 .log / .gz，需按 log_format 完整解析
#              （当天日志 + 平台上线前没跑过新切割脚本的历史备份）
# PREP_FILES：切割脚本预生成的 .tsv.gz，已是分析页所需的 7 列 TSV，只需再过滤一次
declare -a RAW_FILES=()
declare -a PREP_FILES=()
declare -A RAW_SEEN=()
declare -A PREP_SEEN=()

read_log() {
    if [[ "$1" == *.gz ]]; then zcat "$1" 2>/dev/null; else cat "$1" 2>/dev/null; fi
}

# === 超级剥离器：把日期、.gz、.log 全部剥干净 ===
get_domain_name() {
    local fname=$(basename "$1")
    echo "$fname" | sed -E 's/\.gz$//; s/\.[0-9]{8}$//; s/\.(access|error)\.log$//; s/\.log$//'
}

# 原始备份 -> 预生成文件：access.log.20261108.gz -> analysis/access.log.20261108.tsv.gz
prepared_path() {
    local base=$(basename "$1")
    echo "${PREPARED_LOG_DIR}/${base%.gz}.tsv.gz"
}

# 只解压首行校验「版本 + log_format 指纹」；旧格式 / 坏文件一律回退原始备份
prepared_ok() {
    local h
    h=$(gzip -cd "$1" 2>/dev/null | head -1)
    [ "$h" = "${PREPARED_MAGIC} ${FORMAT_SUM}" ]
}

collect_prepared() {
    local p
    for p in $1; do
        [ -f "$p" ] || continue
        if [ -z "${PREP_SEEN[$p]:-}" ] && prepared_ok "$p"; then
            PREP_SEEN[$p]=1
            PREP_FILES+=("$p")
        fi
    done
}

collect_raw() {
    local f p
    for f in $1; do
        [ -f "$f" ] || continue
        p=$(prepared_path "$f")
        # 已有可用预生成数据的原始备份直接跳过，避免重复统计
        [ -n "${PREP_SEEN[$p]:-}" ] && continue
        [ -n "${RAW_SEEN[$f]:-}" ] && continue
        RAW_SEEN[$f]=1
        RAW_FILES+=("$f")
    done
}

if [ "$MODE" = "analyze" ]; then
    if [ -z "$DOMAINS_STR" ]; then
        DOMAINS_STR="access,api.testphp8.com,back.testphp8.com,cdn.testphp8.com,wss.testphp8.com"
    fi

    # 核心逻辑：判断日期范围（只查今天时只读当前日志，其余都要读切割备份）
    TODAY=$(date +%Y%m%d)
    USE_BACKUP=0
    if [ -n "$START_DATE" ] && [ -n "$END_DATE" ]; then
        if [[ "$START_DATE" == "$TODAY" && "$END_DATE" == "$TODAY" ]]; then
            USE_BACKUP=0
        else
            USE_BACKUP=1
        fi
    else
        USE_BACKUP=1 # 没传日期：全量读取备份
    fi

    IFS=',' read -ra DOMAIN_ARRAY <<< "$DOMAINS_STR"
    for domain in "${DOMAIN_ARRAY[@]}"; do
        # 当前日志永远是原始格式（当天尚未切割），走完整解析
        collect_raw "${CURRENT_LOG_DIR}/*${domain}*.log ${CURRENT_LOG_DIR}/*${domain}*.log.*"

        if [ "$USE_BACKUP" -eq 1 ]; then
            if [ -n "$START_DATE" ] && [ -n "$END_DATE" ]; then
                curr_date="$START_DATE"
                while [[ "$curr_date" < "$END_DATE" || "$curr_date" == "$END_DATE" ]]; do
                    collect_prepared "${PREPARED_LOG_DIR}/*${domain}*.${curr_date}.tsv.gz"
                    collect_raw "${BACKUP_LOG_DIR}/*${domain}*.${curr_date}.gz"
                    curr_date=$(date -d "$curr_date + 1 day" +%Y%m%d)
                done
            else
                collect_prepared "${PREPARED_LOG_DIR}/*${domain}*.tsv.gz"
                collect_raw "${BACKUP_LOG_DIR}/*${domain}*.gz"
            fi
        fi
    done

    if [ ${#RAW_FILES[@]} -eq 0 ] && [ ${#PREP_FILES[@]} -eq 0 ]; then
        echo '{"code":0, "msg":"error", "error":"找不到匹配的日志文件"}'
        exit 0
    fi
    echo "[nginx_analyzer] 预生成 ${#PREP_FILES[@]} 份 / 原始兜底 ${#RAW_FILES[@]} 份 (USE_BACKUP=$USE_BACKUP)" >&2
fi

# ================= 2. 提取起止小时 =================
ST_HOUR=""
EN_HOUR=""
if [ -n "$START_TIME" ]; then
    ST_HOUR=$(echo "$START_TIME" | awk -F':' '{print $2}')
fi
if [ -n "$END_TIME" ]; then
    EN_HOUR=$(echo "$END_TIME" | awk -F':' '{print $2}')
fi

# ================= 3. 按 log_format 模板提取字段 =================
# 模板写入文件而不是用 -v 传参，避免 $ / " / \ 被 shell 或 awk 二次解释。
FORMAT_FILE="${TEMP_DIR}/format.tpl"
printf '%s\n' "$FORMAT_TEMPLATE" > "$FORMAT_FILE"

EXTRACT_AWK="${TEMP_DIR}/extract.awk"
cat > "$EXTRACT_AWK" <<'AWK'
BEGIN {
    fmt = ""
    while ((getline ln < fmtfile) > 0) fmt = fmt ln
    close(fmtfile)
    split("Jan Feb Mar Apr May Jun Jul Aug Sep Oct Nov Dec", monname, " ")
    nt = (fmt == "" ? 0 : compile_format(fmt))
}

# 把 log_format 模板编译成 token：L=字面量，V=变量名
function compile_format(t,   s, p, v, n) {
    n = 0
    s = t
    while (length(s) > 0) {
        p = index(s, "$")
        if (p == 0) { if (s != "") tk[++n] = "L" s; break }
        if (p > 1) tk[++n] = "L" substr(s, 1, p - 1)
        s = substr(s, p + 1)
        if (!match(s, /^[A-Za-z0-9_]+/)) { tk[++n] = "L$"; continue }
        v = substr(s, 1, RLENGTH)
        s = substr(s, RLENGTH + 1)
        tk[++n] = "V" v
    }
    return n
}

# 按 token 顺序消费一行日志，结果放进全局数组 f[变量名]
function parse_line(line,   g, i, j, pos, tag, body, idx, val, nextlit, rest, sp, k) {
    pos = 1
    for (i = 1; i <= nt; i++) {
        tag = substr(tk[i], 1, 1)
        body = substr(tk[i], 2)
        if (tag == "L") {
            if (body == "") continue
            if (substr(line, pos, length(body)) != body) return 0
            pos += length(body)
            continue
        }
        nextlit = ""
        for (j = i + 1; j <= nt; j++) {
            if (substr(tk[j], 1, 1) == "L") {
                if (substr(tk[j], 2) != "") { nextlit = substr(tk[j], 2); break }
            } else break
        }
        if (nextlit != "") {
            idx = index(substr(line, pos), nextlit)
            if (idx < 1) return 0
            val = substr(line, pos, idx - 1)
            pos += idx - 1
        } else {
            rest = substr(line, pos)
            sp = index(rest, " ")
            if (sp > 0) { val = substr(rest, 1, sp - 1); pos += sp - 1 }
            else { val = rest; pos = length(line) + 1 }
        }
        g[body] = val
    }
    for (k in g) f[k] = g[k]
    return 1
}

function pick(g, spec,   keys, n, i) {
    n = split(spec, keys, " ")
    for (i = 1; i <= n; i++)
        if (keys[i] in g && g[keys[i]] != "" && g[keys[i]] != "-") return g[keys[i]]
    return ""
}

# 返回 "DD/Mon/YYYY:HH"，兼容 $time_local 与 $time_iso8601
function hour_of(t,   p, n, y, m, d, hh) {
    if (t == "") return ""
    if (t ~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T/) {
        y = substr(t, 1, 4)
        m = substr(t, 6, 2) + 0
        d = substr(t, 9, 2)
        hh = substr(t, 12, 2)
        if (m >= 1 && m <= 12 && hh ~ /^[0-9][0-9]$/) return d "/" monname[m] "/" y ":" hh
        return ""
    }
    n = split(t, p, ":")
    if (n >= 2 && p[2] ~ /^[0-9][0-9]?$/) return p[1] ":" p[2]
    return ""
}

{
    for (k in f) delete f[k]
    if (length($0) < 5) next

    if (nt > 0) {
        if (!parse_line($0)) next

        ip = pick(f, "remote_addr http_x_forwarded_for x_forwarded_for")
        if (ip ~ /,/) { split(ip, ip_parts, ","); ip = ip_parts[1] }
        gsub(/^[ \t]+|[ \t]+$/, "", ip)

        time_str = pick(f, "time_local time_iso8601")
        hour_str = hour_of(time_str)
        log_hour = (hour_str == "" ? -1 : substr(hour_str, length(hour_str) - 1, 2) + 0)

        req = pick(f, "request")
        method = "-"; url = "-"
        if (req != "") {
            split(req, r_parts, " ")
            method = r_parts[1]
            url = r_parts[2]
        } else {
            method = pick(f, "request_method")
            url = pick(f, "request_uri uri")
        }

        status = pick(f, "status")
        bytes = pick(f, "body_bytes_sent bytes_sent")

        # 自定义格式带 $host 时优先用真实域名，否则沿用文件名推导的 domain
        host = pick(f, "host http_host server_name")
        if (host ~ /^[A-Za-z0-9._-]+$/ && length(host) < 128) domain = host
    } else {
        # 兜底：读不到 nginx 配置时沿用旧版「按位置 + 固定正则」的解析
        if ($0 !~ /\[/) next
        ip = $1
        match($0, /\[[^]]+\]/)
        time_str = substr($0, RSTART + 1, 20)
        split(time_str, t_parts, ":")
        log_hour = t_parts[2] + 0
        hour_str = t_parts[1] ":" t_parts[2]

        method = "-"; url = "-"
        if (match($0, /"[A-Z]+ [^ ]+/)) {
            req = substr($0, RSTART + 1, RLENGTH - 1)
            split(req, r_parts, " ")
            method = r_parts[1]
            url = r_parts[2]
        }
        match($0, /" [0-9]{3} [0-9]+/)
        status_bytes = substr($0, RSTART + 2, RLENGTH - 2)
        split(status_bytes, sb_arr, " ")
        status = sb_arr[1]
        bytes = sb_arr[2]
    }

    if (bytes == "" || bytes !~ /^[0-9]+$/) bytes = 0
    if (status !~ /^[0-9][0-9][0-9]$/) status = "-"
    if (log_hour < 0 || log_hour > 23) next
    if (status !~ /^[0-9][0-9][0-9]$/) next
    if (url == "" || url == "-") next
    if (st != "" && log_hour < st) next
    if (en != "" && log_hour > en) next
    if (f_status != "" && status != f_status) next
    if (f_url != "" && url !~ f_url) next
    if (f_ip != "" && ip != f_ip) next

    # HTTP 方法只保留合法动词：SMB / 二进制探测请求的 method 会是 \x00... 一长串，
    # 既污染 method 分布，也容易把非法转义带进最终 JSON
    if (method !~ /^[A-Za-z]+$/ || length(method) > 16) method = "-"

    full_url = domain " " url
    print ip"\t"full_url"\t"bytes"\t"status"\t"method"\t"hour_str"\t"domain
}
AWK

# ================= 3a. 预生成模式（切割脚本在 gzip 之前调用） =================
# 输出格式：#nginx-prepared-v1 <log_format指纹> 首行 + 7 列 TSV。
# 不做任何条件过滤，过滤留到在线分析时做，保证一份预生成数据可服务任意查询条件。
if [ "$MODE" = "prepare" ]; then
    if [ -z "$PREP_IN" ] || [ ! -f "$PREP_IN" ]; then
        echo "[nginx_analyzer] --prepare 找不到输入日志: $PREP_IN" >&2
        exit 1
    fi
    printf '%s %s\n' "$PREPARED_MAGIC" "$FORMAT_SUM"
    read_log "$PREP_IN" | awk \
        -v fmtfile="$FORMAT_FILE" \
        -v domain="$(get_domain_name "$PREP_IN")" \
        -v st="" -v en="" -v f_status="" -v f_url="" -v f_ip="" \
        -f "$EXTRACT_AWK"
    exit 0
fi

# ================= 3b. 预生成 TSV 的二级过滤 =================
# 数据已经是 7 列 TSV，只需补上时间 / 状态码 / URL / IP 条件，跳过逐行解析。
FILTER_AWK="${TEMP_DIR}/filter_prepared.awk"
cat > "$FILTER_AWK" <<'AWK'
BEGIN { FS = "\t" }
/^#nginx-prepared-/ { next }
NF < 7 { next }
{
    ip = $1; full_url = $2; bytes = $3; status = $4; method = $5; hour_str = $6; domain = $7
    if (bytes !~ /^[0-9]+$/) bytes = 0
    if (status !~ /^[0-9][0-9][0-9]$/) next
    log_hour = (length(hour_str) >= 2 ? substr(hour_str, length(hour_str) - 1, 2) + 0 : -1)
    if (log_hour < 0 || log_hour > 23) next
    # 第 2 列是 "domain url"，过滤 URL 时取空格后的路径，与原始解析口径一致
    sp = index(full_url, " ")
    url = (sp > 0 ? substr(full_url, sp + 1) : full_url)
    if (st != "" && log_hour < st) next
    if (en != "" && log_hour > en) next
    if (f_status != "" && status != f_status) next
    if (f_url != "" && url !~ f_url) next
    if (f_ip != "" && ip != f_ip) next
    print ip"\t"full_url"\t"bytes"\t"status"\t"method"\t"hour_str"\t"domain
}
AWK

ALL_FILTERED="${TEMP_DIR}/all_filtered.log"
> "$ALL_FILTERED"

# 原始日志（当天 + 历史兜底）：完整解析 + 过滤
for f in "${RAW_FILES[@]}"; do
    dname=$(get_domain_name "$f")
    read_log "$f" | awk \
        -v fmtfile="$FORMAT_FILE" \
        -v domain="$dname" \
        -v st="$ST_HOUR" \
        -v en="$EN_HOUR" \
        -v f_status="$STATUS" \
        -v f_url="$URL_KEYWORD" \
        -v f_ip="$IP_FILTER" \
        -f "$EXTRACT_AWK" >> "$ALL_FILTERED" 2>/dev/null
done

# 预生成数据：只做条件过滤，不再重复解压解析原始日志
for p in "${PREP_FILES[@]}"; do
    read_log "$p" | awk \
        -v st="$ST_HOUR" \
        -v en="$EN_HOUR" \
        -v f_status="$STATUS" \
        -v f_url="$URL_KEYWORD" \
        -v f_ip="$IP_FILTER" \
        -f "$FILTER_AWK" >> "$ALL_FILTERED" 2>/dev/null
done

if [ ! -s "$ALL_FILTERED" ]; then
    echo '{"code":0, "msg":"success", "data": {"domain_stats":[], "global_stats": {"hourly_trend":[],"top_urls_hits":[],"top_urls_traffic":[],"top_ips":[],"status_distribution":[],"method_distribution":[]}}}'
    exit 0
fi

# ================= 4. 全局统计 =================
# 日志里的 URL / UA / 域名可能带反斜杠或双引号（如扫描器的 \vendor\... 路径），
# 直接拼进 JSON 会产生非法转义，后端解析时报
# invalid character 'v' in string escape code，因此所有字符串值统一转义。
# 用 sprintf("%c") 构造反斜杠与双引号，避开 awk 源码的多层转义陷阱。
JSON_ESCAPE_AWK='function jstr(s,   out, i, c, bs, q) {
    bs = sprintf("%c", 92)
    q = sprintf("%c", 34)
    out = ""
    for (i = 1; i <= length(s); i++) {
        c = substr(s, i, 1)
        if (c == bs || c == q) out = out bs c
        else if (c < " ") out = out " "
        else out = out c
    }
    return out
}'

GLOBAL_HOURLY=$(awk -F'\t' '{print $6}' "$ALL_FILTERED" | sort | uniq -c | sort -k2 | awk "$JSON_ESCAPE_AWK"'{print "{\"hour\":\"" jstr($2) "\", \"count\":"$1"}"}' | paste -sd, -)
GLOBAL_URL_HITS=$(awk -F'\t' '{print $2}' "$ALL_FILTERED" | sort | uniq -c | sort -nr | head -n $TOP_N | awk "$JSON_ESCAPE_AWK"'{print "{\"url\":\"" jstr(substr($0, index($0,$2))) "\", \"count\":"$1"}"}' | paste -sd, -)
GLOBAL_URL_TRAFFIC=$(awk -F'\t' '{traffic[$2]+=$3} END {for(u in traffic) print traffic[u], u}' "$ALL_FILTERED" | sort -nr | head -n $TOP_N | awk "$JSON_ESCAPE_AWK"'{print "{\"url\":\"" jstr(substr($0, index($0,$2))) "\", \"traffic\":"$1"}"}' | paste -sd, -)
GLOBAL_IPS=$(awk -F'\t' '{print $1}' "$ALL_FILTERED" | sort | uniq -c | sort -nr | head -n $TOP_N | awk "$JSON_ESCAPE_AWK"'{print "{\"ip\":\"" jstr($2) "\", \"count\":"$1"}"}' | paste -sd, -)
GLOBAL_STATUS=$(awk -F'\t' '{print $4}' "$ALL_FILTERED" | sort | uniq -c | sort -nr | awk "$JSON_ESCAPE_AWK"'{print "{\"status\":\"" jstr($2) "\", \"count\":"$1"}"}' | paste -sd, -)
GLOBAL_METHOD=$(awk -F'\t' '{print $5}' "$ALL_FILTERED" | sort | uniq -c | sort -nr | awk "$JSON_ESCAPE_AWK"'{print "{\"method\":\"" jstr($2) "\", \"count\":"$1"}"}' | paste -sd, -)

GLOBAL_JSON="\"global_stats\": {
    \"hourly_trend\": [$GLOBAL_HOURLY],
    \"top_urls_hits\": [$GLOBAL_URL_HITS],
    \"top_urls_traffic\": [$GLOBAL_URL_TRAFFIC],
    \"top_ips\": [$GLOBAL_IPS],
    \"status_distribution\": [$GLOBAL_STATUS],
    \"method_distribution\": [$GLOBAL_METHOD]
}"

# ================= 5. 分域名独立统计 =================
DOMAIN_STATS_ARRAY=()
UNIQUE_DOMAINS=$(awk -F'\t' '{print $7}' "$ALL_FILTERED" | sort -u)

for d in $UNIQUE_DOMAINS; do
    D_FILE="${TEMP_DIR}/${d}.log"
    awk -F'\t' -v d="$d" '$7 == d' "$ALL_FILTERED" > "$D_FILE"

    D_REQ_COUNT=$(wc -l < "$D_FILE")
    D_URL_HITS=$(awk -F'\t' '{print $2}' "$D_FILE" | sort | uniq -c | sort -nr | head -n $TOP_N | awk "$JSON_ESCAPE_AWK"'{print "{\"url\":\"" jstr(substr($0, index($0,$2))) "\", \"count\":"$1"}"}' | paste -sd, -)
    D_URL_TRAFFIC=$(awk -F'\t' '{traffic[$2]+=$3} END {for(u in traffic) print traffic[u], u}' "$D_FILE" | sort -nr | head -n $TOP_N | awk "$JSON_ESCAPE_AWK"'{print "{\"url\":\"" jstr(substr($0, index($0,$2))) "\", \"traffic\":"$1"}"}' | paste -sd, -)
    D_IPS=$(awk -F'\t' '{print $1}' "$D_FILE" | sort | uniq -c | sort -nr | head -n $TOP_N | awk "$JSON_ESCAPE_AWK"'{print "{\"ip\":\"" jstr($2) "\", \"count\":"$1"}"}' | paste -sd, -)
    D_STATUS=$(awk -F'\t' '{print $4}' "$D_FILE" | sort | uniq -c | sort -nr | awk "$JSON_ESCAPE_AWK"'{print "{\"status\":\"" jstr($2) "\", \"count\":"$1"}"}' | paste -sd, -)
    D_METHOD=$(awk -F'\t' '{print $5}' "$D_FILE" | sort | uniq -c | sort -nr | awk "$JSON_ESCAPE_AWK"'{print "{\"method\":\"" jstr($2) "\", \"count\":"$1"}"}' | paste -sd, -)
    D_DOMAIN_JSON=$(printf '%s' "$d" | awk "$JSON_ESCAPE_AWK"'{print jstr($0)}')

    D_JSON="{
        \"domain\": \"$D_DOMAIN_JSON\",
        \"total_requests\": $D_REQ_COUNT,
        \"top_urls_hits\": [$D_URL_HITS],
        \"top_urls_traffic\": [$D_URL_TRAFFIC],
        \"top_ips\": [$D_IPS],
        \"status_distribution\": [$D_STATUS],
        \"method_distribution\": [$D_METHOD]
    }"
    DOMAIN_STATS_ARRAY+=("$D_JSON")
done

DOMAIN_STATS_JSON=$(IFS=,; echo "${DOMAIN_STATS_ARRAY[*]}")

# ================= 6. 输出最终 JSON =================
cat <<EOF
{
    "code": 0,
    "msg": "success",
    "data": {
        $GLOBAL_JSON,
        "domain_stats": [$DOMAIN_STATS_JSON]
    }
}
EOF
