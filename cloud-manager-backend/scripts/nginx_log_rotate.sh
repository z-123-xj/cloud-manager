#!/bin/bash
# nginx 日志切割（每天 0 点由 cron 调用）+ 分析数据预生成
#
# 由云管平台作为唯一真源下发（md5 不一致时覆盖同步到本路径），与 nginx_analyzer.sh
# 组成「备份 + 分析」闭环：
#   1) 原始日志照常 mv 到备份目录 -> USR1 -> 单文件 gzip（保持原有行为不变）；
#   2) gzip 之前调用 nginx_analyzer.sh --prepare，把这份日志一次性解析成分析页所需的
#      7 列 TSV，压缩后写入 PREPARED_DIR（与原始备份同名，只是多了 .tsv）；
#   3) 分析脚本优先读 TSV.gz；缺失、坏文件、log_format 变更时自动回退原始 .gz，
#      因此平台上线之前的历史备份仍可直接分析，不需要补跑任何迁移；
#   4) 分析脚本不存在（平台从未运行过）时自动跳过第 2 步，只做原始备份。
#
# 备份按「日志所属自然日」命名：0 点切割的是刚刚结束的前一天的日志，
# 所以后缀取昨天，与分析页按 START_DATE/END_DATE 展开备份文件的口径一致。
# 历史备份沿用旧命名不做迁移；首次切换若撞名，旧文件移入 legacy/ 留档，避免被覆盖。
#
# 目录可用环境变量覆盖，便于在测试机上用合成日志验证。
set -u
export LC_ALL=C

NGINX_DIR="${NGINX_DIR:-/usr/local/nginx}"
LOG_DIR="${LOG_DIR:-${NGINX_DIR}/logs}"
BACKUP_DIR="${BACKUP_DIR:-/data/backup/nginx_log}"
PREPARED_DIR="${NGINX_PREPARED_LOG_DIR:-${BACKUP_DIR}/analysis}"
LEGACY_DIR="${BACKUP_DIR}/legacy"
PID_FILE="${PID_FILE:-${LOG_DIR}/nginx.pid}"
KEEP_DAYS="${KEEP_DAYS:-30}"
ANALYZER_SCRIPT="${NGINX_ANALYZER_SCRIPT:-/usr/local/sbin/nginx_analyzer.sh}"
FORMAT_TPL_FILE="${NGINX_LOG_FORMAT_FILE:-/usr/local/sbin/nginx_log_format.tpl}"

TS=$(date -d "yesterday" +%Y%m%d 2>/dev/null) || TS=""
if [ -z "$TS" ]; then
    echo "[nginx_log_rotate] 无法计算前一天日期，请确认 date 支持 -d 参数" >&2
    exit 1
fi

mkdir -p "${BACKUP_DIR}" || exit 1
mkdir -p "${PREPARED_DIR}" || exit 1

# 已有 crontab 与平台补装的 cron.d 若同时触发，串行化避免重复切割
if command -v flock >/dev/null 2>&1; then
    exec 9>"${BACKUP_DIR}/.rotate.lock"
    flock -n 9 || { echo "[nginx_log_rotate] 已有切割任务在运行，本次跳过" >&2; exit 0; }
fi

# 平台探测到的 log_format 会落盘到 FORMAT_TPL_FILE；切割时复用，保证「预生成」
# 与「在线分析」用同一份模板。文件不存在（平台从未运行 / 探测失败）时传 auto。
PREP_FORMAT="auto"
if [ -s "${FORMAT_TPL_FILE}" ]; then
    PREP_FORMAT=$(cat "${FORMAT_TPL_FILE}")
fi

# 预生成失败一律不影响原始备份：不发布 TSV，分析脚本自然回退原始 .gz
prepare_log() {
    local src="$1" base tsv gz rows
    base=$(basename "$src")
    tsv="${PREPARED_DIR}/.${base}.tsv.$$"
    gz="${PREPARED_DIR}/${base}.tsv.gz"

    if ! bash "${ANALYZER_SCRIPT}" --prepare "$src" "$PREP_FORMAT" > "$tsv" 2>/dev/null; then
        rm -f "$tsv"
        echo "[nginx_log_rotate] 预生成失败，保留原始备份兜底: ${base}" >&2
        return 1
    fi
    # 首行必须是版本 + 指纹头；否则视为预生成异常，不做发布
    if ! head -1 "$tsv" | grep -q '^#nginx-prepared-v1 '; then
        rm -f "$tsv"
        echo "[nginx_log_rotate] 预生成数据头异常，保留原始备份兜底: ${base}" >&2
        return 1
    fi
    rows=$(( $(wc -l < "$tsv") - 1 ))
    if [ "$rows" -le 0 ]; then
        # error.log 等非 access 格式日志：没有可解析行，不生成空 TSV，
        # 让分析脚本继续按原逻辑回退原始备份（结果本来也是空）
        rm -f "$tsv"
        echo "[nginx_log_rotate] 无可解析日志行，跳过预生成: ${base}" >&2
        return 0
    fi
    if ! gzip -c "$tsv" > "${gz}.tmp.$$"; then
        rm -f "$tsv" "${gz}.tmp.$$"
        echo "[nginx_log_rotate] 压缩预生成数据失败，保留原始备份兜底: ${base}" >&2
        return 1
    fi
    rm -f "$tsv"
    # 原子替换：分析脚本永远不会读到写了一半的 TSV
    mv -f "${gz}.tmp.$$" "$gz"
    echo "[nginx_log_rotate] 预生成分析数据 ${rows} 行: ${gz}"
}

# 1) 移动所有当前日志，并加上「日志所属自然日」后缀
moved_files=()
for f in "${LOG_DIR}"/*.log; do
    [ -e "$f" ] || continue
    base=$(basename "$f")
    dst="${BACKUP_DIR}/${base}.${TS}"
    # 旧脚本按「当天」命名，切换规则后新旧会撞同一个日期后缀；
    # 先把同名旧备份挪进 legacy/，否则 gzip -f 会覆盖掉一天数据。
    if [ -f "${dst}.gz" ]; then
        mkdir -p "${LEGACY_DIR}" 2>/dev/null
        mv "${dst}.gz" "${LEGACY_DIR}/${base}.${TS}.$$.gz" 2>/dev/null
        echo "[nginx_log_rotate] 同名旧备份已留档至 ${LEGACY_DIR}/: ${base}.${TS}.gz" >&2
    fi
    mv "$f" "$dst" || continue
    moved_files+=("$dst")
done

# 2) 通知 nginx 重新打开日志文件（USR1），否则 nginx 继续写已移走的文件
if [ -f "${PID_FILE}" ]; then
    kill -USR1 "$(cat "${PID_FILE}")" 2>/dev/null
fi

# 3) 压缩原始备份，同时产出分析用的预生成数据
if [ ${#moved_files[@]} -gt 0 ]; then
    for f in "${moved_files[@]}"; do
        if [ -f "${ANALYZER_SCRIPT}" ]; then
            prepare_log "$f" || true
        else
            echo "[nginx_log_rotate] 未找到分析脚本 ${ANALYZER_SCRIPT}，本次仅备份原始数据" >&2
        fi
        gzip -f "$f"
    done
fi

# 4) 清理超过保留天数的压缩包（原始备份、预生成数据、撞名留档口径一致）
find "${BACKUP_DIR}" -maxdepth 1 -name '*.log.*.gz' -mtime +"${KEEP_DAYS}" -delete
find "${PREPARED_DIR}" -maxdepth 1 -name '*.tsv.gz' -mtime +"${KEEP_DAYS}" -delete
find "${PREPARED_DIR}" -maxdepth 1 -name '.*.tsv.*' -mtime +1 -delete 2>/dev/null
find "${LEGACY_DIR}" -maxdepth 1 -type f -mtime +"${KEEP_DAYS}" -delete 2>/dev/null
