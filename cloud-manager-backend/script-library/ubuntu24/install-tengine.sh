#!/bin/bash
# Tengine 安装（云管平台脚本库下发，经 stdin 交给 bash -s，脚本不落盘）
# 占位符由控制端渲染成字面值；本脚本可重复执行（幂等），重跑不会覆盖已有证书与站点配置。
# 每个安装步骤都封装成函数，按顺序在文件末尾统一调用。
# 注意：脚本经 stdin 执行，不能用 BASH_SOURCE[0] == $0 守卫，否则函数都不会被调用。
set -o pipefail

# ===== 参数（由脚本库渲染注入）=====
TENGINE_VERSION="{{TENGINE_VERSION}}"
NGINX_DIR="{{NGINX_DIR}}"
WWW_USER="{{WWW_USER}}"
INSTALL_CERTBOT="{{INSTALL_CERTBOT}}"
# ==================================

NGINX_INSTALL_DIR="/usr/local/tengine-${TENGINE_VERSION}"
SRC_DIR="/usr/local/src"
SSL_DIR="${NGINX_DIR}/conf/ssl"
ROTATE_SCRIPT="/usr/local/sbin/nginx_log_rotate.sh"

check_ok() {
    if [ $? -ne 0 ]; then
        echo "Error: $1"
        exit 1
    fi
}

# ---------- 步骤 1：环境检查 ----------
step_check_env() {
    echo "==> 安装 Tengine ${TENGINE_VERSION} -> ${NGINX_INSTALL_DIR}（软链 ${NGINX_DIR}）"
    if [ "$(id -u)" -ne 0 ]; then
        echo "请用 root 凭证执行本脚本"
        exit 1
    fi
    # NGINX_DIR 必须是软链；若已是真实目录，ln -sfn 会把链接建到目录内部形成嵌套
    if [ -e "${NGINX_DIR}" ] && [ ! -L "${NGINX_DIR}" ]; then
        echo "Error: ${NGINX_DIR} 已存在且不是软链，请先备份/移除该目录，或更换 NGINX_DIR 参数"
        exit 1
    fi
}

# ---------- 步骤 2：安装编译依赖 ----------
step_install_deps() {
    export DEBIAN_FRONTEND=noninteractive
    apt update
    check_ok 'apt update'
    apt install -y gcc make libpcre3-dev zlib1g-dev libssl-dev libxml2-dev libxslt1-dev \
        libgd-dev libgeoip-dev build-essential libpcre2-dev pkgconf wget openssl cron
    check_ok 'apt install 编译依赖'
}

# ---------- 步骤 3：下载、编译、安装（同版本已装则跳过，便于重跑）----------
step_compile_install() {
    if [ -x "${NGINX_INSTALL_DIR}/sbin/nginx" ]; then
        echo "==> ${NGINX_INSTALL_DIR} 已存在，跳过编译"
        return 0
    fi
    # 用子 shell 隔离 cd，避免把工作目录带到后续步骤
    (
        mkdir -p "${SRC_DIR}"
        cd "${SRC_DIR}" || exit 1
        [ -f "tengine-${TENGINE_VERSION}.tar.gz" ] || \
            wget "https://tengine.taobao.org/download/tengine-${TENGINE_VERSION}.tar.gz"
        check_ok "wget tengine-${TENGINE_VERSION}"
        [ -d "tengine-${TENGINE_VERSION}" ] || tar -zxf "tengine-${TENGINE_VERSION}.tar.gz"
        check_ok 'tar 解压'
        cd "tengine-${TENGINE_VERSION}" || exit 1

        id -u "${WWW_USER}" >/dev/null 2>&1 || useradd "${WWW_USER}"
        check_ok "useradd ${WWW_USER}"

        ./configure --prefix="${NGINX_INSTALL_DIR}" \
            --with-pcre \
            --with-http_ssl_module \
            --with-http_v2_module \
            --with-http_realip_module \
            --with-http_addition_module \
            --with-http_gunzip_module \
            --with-http_gzip_static_module \
            --with-http_auth_request_module \
            --with-http_random_index_module \
            --with-http_stub_status_module \
            --with-stream \
            --with-stream_ssl_module \
            --with-file-aio \
            --with-http_secure_link_module \
            --with-threads \
            --with-stream_realip_module
        check_ok './configure'
        make
        check_ok 'make'
        make install
        check_ok 'make install'
    )
    check_ok '编译安装 Tengine'
}

# ---------- 步骤 4：软链、目录、别名 ----------
step_init_dirs() {
    ln -sfn "${NGINX_INSTALL_DIR}" "${NGINX_DIR}"
    check_ok "ln -s -> ${NGINX_DIR}"
    mkdir -p "${NGINX_DIR}/conf/vhosts" "${NGINX_DIR}/logs" "${SSL_DIR}"
    check_ok '创建 nginx 目录'
    grep -q 'cdnginx' /root/.bashrc 2>/dev/null || \
        echo "alias cdnginx='cd ${NGINX_DIR}/conf/vhosts'" >> /root/.bashrc
}

# ---------- 步骤 5：SSL 公共参数（已存在就不覆盖）----------
step_ssl_params() {
    if [ ! -s "${SSL_DIR}/options-ssl-nginx.conf" ]; then
    cat > "${SSL_DIR}/options-ssl-nginx.conf" <<'SSL_CONF_EOF'
# Certbot 风格的 SSL 公共参数（由安装脚本生成）
ssl_session_cache shared:le_nginx_SSL:10m;
ssl_session_timeout 1440m;
ssl_protocols TLSv1.2 TLSv1.3;
ssl_prefer_server_ciphers off;
ssl_ciphers "ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384";
SSL_CONF_EOF
    fi
    if [ ! -s "${SSL_DIR}/ssl-dhparams.pem" ]; then
        echo "首次生成 DH 参数（2048 bit），请稍候..."
        openssl dhparam -out "${SSL_DIR}/ssl-dhparams.pem" 2048 2>/dev/null
        check_ok 'openssl dhparam'
    fi
}

# ---------- 步骤 6：首次安装写配置；sentinel 存在则保留线上已改过的配置 ----------
step_write_configs() {
    if [ -f "${NGINX_DIR}/conf/.cm-tengine-configured" ]; then
        echo "==> 已存在 .cm-tengine-configured，保留现有 nginx 配置"
        return 0
    fi
    cat > "${NGINX_DIR}/conf/nginx.conf" <<EOF
user ${WWW_USER} ${WWW_USER};
worker_processes auto;
worker_cpu_affinity auto;
error_log ${NGINX_DIR}/logs/nginx_error.log crit;
pid ${NGINX_DIR}/logs/nginx.pid;
worker_rlimit_nofile 51200;

events
{
    use epoll;
    worker_connections 60000;
}

http
{
    include mime.types;
    default_type application/octet-stream;
    server_names_hash_bucket_size 3526;
    server_names_hash_max_size 4096;
    log_format main '\$remote_addr - \$remote_user [\$time_local] "\$request" '
                    '\$status \$body_bytes_sent "\$http_referer" '
                    '"\$http_user_agent" "\$http_x_forwarded_for" '
                    'Host="\$http_host" '
                    'Content-Type="\$http_content_type" '
                    'Request-URI="\$request_uri" '
                    'Query-String="\$query_string"';
    sendfile on;
    tcp_nopush on;
    keepalive_timeout 30;
    client_header_timeout 3m;
    client_body_timeout 3m;
    send_timeout 3m;
    connection_pool_size 256;
    client_header_buffer_size 1k;
    large_client_header_buffers 8 4k;
    request_pool_size 4k;
    output_buffers 4 32k;
    postpone_output 1460;
    client_max_body_size 50m;
    client_body_buffer_size 256k;
    client_body_temp_path ${NGINX_DIR}/client_body_temp;
    proxy_temp_path ${NGINX_DIR}/proxy_temp;
    fastcgi_temp_path ${NGINX_DIR}/fastcgi_temp;
    fastcgi_intercept_errors on;
    tcp_nodelay on;
    gzip on;
    gzip_min_length 1k;
    gzip_buffers 4 8k;
    gzip_comp_level 5;
    gzip_http_version 1.1;
    gzip_types text/plain application/x-javascript text/css text/htm application/xml;
    server_tokens off;
    server_tag off;
    port_in_redirect off;
    include vhosts/*.conf;
    map \$http_user_agent \$is_mobile {
        default 0;
        "~*(iPhone|iPod|Android|Windows\sPhone|BlackBerry|Mobile|Opera\sMini)" 1;
    }
}
EOF

    cat > "${NGINX_DIR}/conf/vhosts/default.conf" <<EOF
server {
    listen 80 default_server;
    listen 443 ssl default_server;
    server_name _;

    ssl_certificate ${SSL_DIR}/default.crt;
    ssl_certificate_key ${SSL_DIR}/default.key;

    return 444;
}
EOF

    cat > /etc/systemd/system/nginx.service <<EOF
[Unit]
Description=The NGINX HTTP and reverse proxy server
After=network.target remote-fs.target nss-lookup.target
Wants=network.target

[Service]
Type=forking
PIDFile=${NGINX_DIR}/logs/nginx.pid
ExecStartPre=${NGINX_DIR}/sbin/nginx -t
ExecStart=${NGINX_DIR}/sbin/nginx
ExecReload=${NGINX_DIR}/sbin/nginx -s reload
ExecStop=/bin/kill -s QUIT \$MAINPID
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
    touch "${NGINX_DIR}/conf/.cm-tengine-configured"
    check_ok '写入 nginx 配置'
}

# ---------- 步骤 7：兼容软链 ----------
step_links() {
    # 已存在真实目录时不覆盖，只维护软链，避免把链接建到目录里面去
    if [ -L /etc/nginx ]; then
        ln -sfn "${NGINX_DIR}/conf" /etc/nginx
    elif [ ! -e /etc/nginx ]; then
        ln -s "${NGINX_DIR}/conf" /etc/nginx
    fi
    ln -sfn "${NGINX_DIR}/sbin/nginx" /usr/bin/nginx
    check_ok '维护 /etc/nginx 与 /usr/bin/nginx 软链'
}

# ---------- 步骤 8：默认站点自签证书（已存在则不覆盖）----------
step_default_cert() {
    if [ -s "${SSL_DIR}/default.crt" ] && [ -s "${SSL_DIR}/default.key" ]; then
        return 0
    fi
    openssl req -x509 -nodes -sha256 -days 36500 -newkey rsa:2048 \
        -keyout "${SSL_DIR}/default.key" \
        -out "${SSL_DIR}/default.crt" \
        -subj "/CN=default"
    check_ok 'openssl req 自签名证书'
}

# ---------- 步骤 9：校验配置并启动服务 ----------
step_start_service() {
    # 启动前先校验配置，避免配错还把服务 enable 起来
    "${NGINX_DIR}/sbin/nginx" -t
    check_ok 'nginx -t'
    systemctl daemon-reload
    systemctl enable --now nginx.service
    check_ok 'systemctl enable nginx'
}

# ---------- 步骤 10：certbot 与证书到期监控（可选）----------
step_certbot() {
    if [ "${INSTALL_CERTBOT}" != "yes" ]; then
        return 0
    fi
    apt install -y snapd
    check_ok 'apt install snapd'
    snap install core || echo "警告：snap core 安装失败，certbot 可能不可用"
    snap refresh core || echo "警告：snap core 刷新失败"
    snap install --classic certbot || echo "警告：certbot 安装失败，SSL 定时任务仍会写入但不生效"
    [ -e /snap/bin/certbot ] && ln -sfn /snap/bin/certbot /usr/bin/certbot

    # 用带引号的 heredoc，让这些变量在生成的脚本运行时才展开
    cat > /usr/local/sbin/check_ssl_expiry.sh <<'CHECK_SSL_EOF'
#!/bin/bash
# 导出 certbot 证书剩余天数给 node_exporter（每天 0 点由 cron 调用）
set -u
OUTPUT_DIR="/var/lib/node_exporter/textfile_collector"
METRIC_FILE="$OUTPUT_DIR/ssl_expiry.prom"
mkdir -p "$OUTPUT_DIR"
: > "$METRIC_FILE"
current_epoch=$(date +%s)

/usr/bin/certbot certificates 2>/dev/null | awk -v current="$current_epoch" -v metric_file="$METRIC_FILE" '
/Certificate Name:/ {
  split($0, domains, ": ")
  gsub(/ /, "", domains[2])
}
/Expiry Date:/ {
  split($3, date, "T")
  cmd = "date -d \"" date[1] "\" +%s 2>/dev/null"
  cmd | getline expiry_epoch
  close(cmd)
  if (expiry_epoch == "") {
    print "Error parsing date for: " domains[2] > "/dev/stderr"
    next
  }
  days_left = int((expiry_epoch - current) / 86400)
  print "ssl_cert_expiry_days{domain=\"" domains[2] "\"} " days_left >> metric_file
}'
CHECK_SSL_EOF
    chmod +x /usr/local/sbin/check_ssl_expiry.sh

    systemctl enable --now cron 2>/dev/null || service cron start 2>/dev/null || true
    # 这里的 ${NGINX_DIR} 由安装脚本展开，写入 cron 的是最终绝对路径
    cat > /etc/cron.d/tengine_ssl <<EOF
# Tengine / Certbot 维护任务（由 install-tengine.sh 自动注入，勿手动修改）
00 00 * * * root /bin/bash /usr/local/sbin/check_ssl_expiry.sh 2>/dev/null
0 3 * * * root /usr/bin/certbot renew --quiet --renew-hook '${NGINX_DIR}/sbin/nginx -s reload'
EOF
    chmod 644 /etc/cron.d/tengine_ssl
}

# ---------- 步骤 11：日志切割（复用平台唯一真源 scripts/nginx_log_rotate.sh）----------
step_logrotate() {
    systemctl enable --now cron 2>/dev/null || service cron start 2>/dev/null || true

    # 脚本库经 stdin 执行、不落盘，无法引用外部文件，因此把平台脚本原样内嵌。
    # 内容必须与 cloud-manager-backend/scripts/nginx_log_rotate.sh 逐字节一致：
    # 后端 go:embed 会按 md5 同步到同一路径，一致就不算旧版本、不会覆盖并留 .bak。
    # 用带引号的 heredoc 保证脚本里的变量在 cron 运行时才展开。
    cat > "${ROTATE_SCRIPT}" <<'__NGINX_LOG_ROTATE_SH__'
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
__NGINX_LOG_ROTATE_SH__
    check_ok "写 ${ROTATE_SCRIPT}"
    chmod +x "${ROTATE_SCRIPT}"

    # 与后端 logs_handler.go 口径一致：已有任意调度时不再补写，避免一天切割两次。
    # NGINX_DIR 通过 cron 环境变量传给切割脚本（平台脚本默认 /usr/local/nginx）。
    cron_text="$( { crontab -l 2>/dev/null; cat /etc/crontab /etc/cron.d/* /var/spool/cron/crontabs/* 2>/dev/null; } || true )"
    if ! printf '%s\n' "${cron_text}" | grep -q 'nginx_log_rotate.sh'; then
        cat > /etc/cron.d/nginx_log_rotate <<EOF
# nginx 日志切割（由 install-tengine.sh 注入；脚本本体由平台按 md5 同步）
NGINX_DIR=${NGINX_DIR}
0 0 * * * root ${ROTATE_SCRIPT} >/dev/null 2>&1
EOF
        chmod 0644 /etc/cron.d/nginx_log_rotate
    fi
}

# ---------- 步骤 12：收尾验证 ----------
step_verify() {
    echo "==> Tengine 安装完成"
    "${NGINX_DIR}/sbin/nginx" -v 2>&1 || true
}

# ============================================================
# 统一调用：顺序即安装流程（stdin 下发，必须直接调用，不做 source 守卫）
# ============================================================
step_check_env
step_install_deps
step_compile_install
step_init_dirs
step_ssl_params
step_write_configs
step_links
step_default_cert
step_start_service
step_certbot
step_logrotate
step_verify
