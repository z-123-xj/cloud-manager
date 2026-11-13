#!/bin/bash
# 安装 Docker CE（阿里云镜像源），云管平台脚本库下发，经 stdin 交给 bash -s
# 脚本会直接执行安装，不再依赖 BASH_SOURCE[0] == $0 之类的守卫（stdin 方式不可靠）。
set -o pipefail

DOCKER_DATA="{{docker_data}}"

if [ "$(id -u)" -ne 0 ]; then
    echo "请用 root 凭证执行本脚本"
    exit 1
fi

check_ok() {
    if [ $? -ne 0 ]; then
        echo "Error: $1"
        exit 1
    fi
}

export DEBIAN_FRONTEND=noninteractive

# 更新软件源并安装依赖（curl / gnupg 后面导入 GPG 公钥时必须存在）
apt-get update
check_ok 'apt-get update'
apt-get install -y ca-certificates curl gnupg
check_ok 'apt-get install ca-certificates curl gnupg'

# 国内用阿里云 Docker CE 镜像源
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://mirrors.aliyun.com/docker-ce/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
check_ok '导入 Docker GPG 公钥'
chmod a+r /etc/apt/keyrings/docker.gpg

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://mirrors.aliyun.com/docker-ce/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  tee /etc/apt/sources.list.d/docker.list > /dev/null
check_ok '写入 Docker APT 源'

apt-get update
check_ok 'apt-get update（docker 源）'
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
check_ok 'apt-get install docker-ce'

# daemon.json 写之前确保目录存在；data-root 目录也一并建好
mkdir -p /etc/docker "${DOCKER_DATA}"
check_ok "mkdir -p ${DOCKER_DATA}"
cat > /etc/docker/daemon.json <<EOF
{
  "data-root": "${DOCKER_DATA}",
  "log-driver": "local",
  "log-opts": {
    "max-size": "20m",
    "max-file": "5"
  }
}
EOF
check_ok '写 /etc/docker/daemon.json'

systemctl daemon-reload
systemctl enable --now docker
check_ok 'systemctl enable docker'
systemctl is-active --quiet docker || { echo 'Error: docker 服务未正常运行'; exit 1; }

docker --version
echo "==> Docker 安装完成，data-root=${DOCKER_DATA}"
