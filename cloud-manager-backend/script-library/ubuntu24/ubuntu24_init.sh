#!/bin/bash
# 针对 ubuntu-24.04 初始化（云管平台脚本库下发，经 stdin 交给 bash -s，默认 root 执行）
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

# ===== 参数（由脚本库渲染注入）=====
# heredoc 里的参数占位符会渲染成字面值；这里额外定义 SSH_PORT 供 ufw/echo 引用，
# 否则 set -u 会因为未定义变量直接退出。
SSH_PORT="{{SSH_PORT}}"
# ==================================

if [ "$(id -u)" -ne 0 ]; then
    echo "请用 root 凭证执行本脚本"
    exit 1
fi

# 备份并更换为阿里云镜像源（24.04 为 deb822 格式，源文件在 /etc/apt/sources.list.d/ubuntu.sources）
# 注意：云镜像自带 cn.archive.ubuntu.com 前缀，只替换子串会拼出 cn.mirrors.aliyun.com（不存在，DNS失败），必须整域名替换
UBUNTU_SOURCES=/etc/apt/sources.list.d/ubuntu.sources
if [ -f "${UBUNTU_SOURCES}" ]; then
  [ -f "${UBUNTU_SOURCES}.bak" ] || cp "${UBUNTU_SOURCES}" "${UBUNTU_SOURCES}.bak"
  sed -i -E 's#https?://(cn\.)?(archive|security)\.ubuntu\.com#http://mirrors.aliyun.com#g; s#https?://cn\.mirrors\.aliyun\.com#http://mirrors.aliyun.com#g' "${UBUNTU_SOURCES}"
fi

# 解决vim不能黏贴问题，Ubuntu 24.04 及更新版本默认不能粘贴，需要设置一下
for f in /usr/share/vim/vim*/defaults.vim; do
  if [ -f "$f" ]; then
    sed -i 's/mouse=a/mouse-=a/g' "$f"
  fi
done

tee /etc/profile.d/custom-ps1.sh > /dev/null << 'EOF'
PROMPT_COMMAND='PS1="[\[\033[01;32m\]\u@\h\[\033[00m\] \[\033[01;34m\]\W\[\033[00m\]]\\$ "'
EOF

# 加载快捷键
grep '/usr/share/bash-completion/bash_completion' /root/.bashrc || echo 'if [ -f /usr/share/bash-completion/bash_completion ]; then
    . /usr/share/bash-completion/bash_completion
fi' >> /root/.bashrc

# 磁盘挂载，如果有的话，$1指定盘(默认nvme1n1盘)，$2指定挂载目录（默认data）
# bash disk.sh
# bash disk.sh nvme0n1

# 管理员用户
# user02 ALL=(ALL:ALL) NOPASSWD: ALL

# 修改时区并开启网络时间同步（容器 / 无 systemd 环境失败不阻断后续初始化）
timedatectl set-timezone Asia/Shanghai || echo "警告：时区设置失败，请手动检查"
timedatectl set-ntp true || echo "警告：NTP 开启失败，请手动检查"

# 创建开发用户和只读用户，并设置权限
# user01为开发用户，负责运行程序
# usero2为只读用户，负责临时上去查看信息

# useradd -m -s /bin/bash user01
# echo "I8E99N9Dk9d0a8aNWsSf" |passwd --stdin user01


# 先禁止某些命令，再允许其他免密
#echo 'user01 ALL=(ALL) !/bin/su, !/usr/bin/sudo -i
#user01 ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/user01

# 更安全的方式（推荐生产环境）
# 只对常用命令免密，其他需要密码
#echo 'user01 ALL=(ALL) ALL, !/bin/su, !/usr/bin/sudo -i
#user01 ALL=(ALL) NOPASSWD: /usr/bin/apt, /usr/bin/apt-get, /usr/bin/docker, /bin/systemctl' > /etc/sudoers.d/user01


apt update
apt install curl vim git net-tools htop lsof tree jq unzip zip bash-completion wget lrzsz subversion -y

# SVN 配置函数，确保在无桌面环境下也能存储密码
# 原函数名带 !，虽然 bash 能解析，但和 history expansion / POSIX 习惯冲突，改成普通名字
svn_config() {
    # 确保 ~/.subversion 目录存在（首次运行 svn 时会生成，这里主动创建更稳妥）
    mkdir -p ~/.subversion/auth

    # 首次安装后 config 还不存在，直接生成一份，否则下面的 sed 全部跳过、配置等于没做
    if [ ! -f ~/.subversion/config ]; then
        printf '[auth]\nstore-passwords = yes\nstore-auth-creds = yes\npassword-stores =\n' > ~/.subversion/config
        return 0
    fi

    # 修改 config：开启存储开关，并清空 password-stores 以适配无桌面环境
    # 删掉可能存在的旧配置行，避免重复
    sed -i '/^\[auth\]/,/^\[/ { /^store-passwords/d; /^store-auth-creds/d; /^password-stores/d }' ~/.subversion/config

    # 在 [auth] 段后插入正确配置（如果 [auth] 段不存在，就追加到文件末尾）
    if grep -q "^\[auth\]" ~/.subversion/config; then
        sed -i '/^\[auth\]/a store-passwords = yes\nstore-auth-creds = yes\npassword-stores =' ~/.subversion/config
    else
        printf '\n[auth]\nstore-passwords = yes\nstore-auth-creds = yes\npassword-stores =\n' >> ~/.subversion/config
    fi
}
svn_config

# 避免 apt 安装/升级时 needrestart 弹交互重启提示（24.04 新增组件）
mkdir -p /etc/needrestart/conf.d
echo '$nrconf{restart} = "a";' > /etc/needrestart/conf.d/99-auto-restart.conf

# 修改 ssh 端口（drop-in 方式，幂等；占位符由控制端渲染成字面值，这里不需要 shell 变量）
# 注意：sshd_config 只支持「整行注释」，行尾 # 注释会被当成参数值的一部分，导致 sshd -t 失败
mkdir -p /etc/ssh/sshd_config.d
cat > /etc/ssh/sshd_config.d/01-init.conf <<EOF
Port {{SSH_PORT}}
UseDNS no
GSSAPIAuthentication no
PermitEmptyPasswords no
ClientAliveInterval 60
MaxAuthTries 3
# 允许 root 用密码或密钥直接登录
PermitRootLogin {{PermitRootLogin}}
# 允许所有用户通过密码登录 SSH
PasswordAuthentication {{PasswordAuthentication}}
EOF

# 如启用了 ufw，先放行新端口，避免重启 ssh 后连不上
if command -v ufw >/dev/null 2>&1; then ufw allow "${SSH_PORT}/tcp" || true; fi

# 24.04 默认由 ssh.socket 监听 22 端口，不禁用会让 Port 配置不生效
systemctl disable --now ssh.socket 2>/dev/null || echo "警告：未禁用 ssh.socket，请手动确认新端口是否生效"
# 校验通过才重启；注意 AWS 需同时在安全组放行 ${SSH_PORT}，新连接请使用新端口
sshd -t && systemctl enable --now ssh || { echo "sshd 配置有误，已终止"; exit 1; }
systemctl restart ssh
systemctl status ssh || true
echo "SSH 端口已修改为 ${SSH_PORT}，请确认防火墙/安全组已放行，新连接使用该端口"

# 2G 交换分区（小内存云盘防止 OOM，可按实际内存调整大小）系统内存不够才设置
# if [ ! -f /swapfile ]; then
#   fallocate -l 2G /swapfile
#   chmod 600 /swapfile
#   mkswap /swapfile
#   swapon /swapfile
# fi
# grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab

# 内核/网络参数优化（BBR，24.04 内核默认支持）
cat > /etc/sysctl.d/99-tuning.conf <<'EOF'
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.ipv4.tcp_tw_reuse=1
vm.swappiness=10
fs.file-max=1048576
EOF
sysctl --system >/dev/null || echo "警告：部分 sysctl 参数应用失败，请手动检查"

# 限制 journal 日志大小，防止写满系统盘
mkdir -p /etc/systemd/journald.conf.d
cat > /etc/systemd/journald.conf.d/99-size.conf <<'EOF'
[Journal]
SystemMaxUse=200M
EOF
systemctl restart systemd-journald || echo "警告：systemd-journald 重启失败，请手动检查"

# 设置vim缩进
grep -q "set tabstop=4" /etc/vim/vimrc || cat >> /etc/vim/vimrc << 'EOF'
set tabstop=4
set shiftwidth=4
EOF

# 常用命令做别名
grep -q 'cdsbin' /root/.bashrc || echo "alias cdsbin='cd /usr/local/sbin'" >> /root/.bashrc

echo 'export EDITOR=/usr/bin/vim.basic' | tee /etc/profile.d/editor.sh > /dev/null
echo 'export VISUAL=/usr/bin/vim.basic' | tee -a /etc/profile.d/editor.sh > /dev/null

echo "==> ubuntu 24.04 初始化完成"
