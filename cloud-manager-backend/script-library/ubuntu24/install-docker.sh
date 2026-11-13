#!/bin/bash

check_ok() {
    if [ $? != 0 ]
    then
        echo "Error, Check the error log."
        exit 1
    fi
}

function install_docker(){
    # 更新软件源并安装依赖
    sudo apt-get update
    sudo apt-get install ca-certificates -y

    # 国内用阿里镜像
    sudo install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://mirrors.aliyun.com/docker-ce/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    sudo chmod a+r /etc/apt/keyrings/docker.gpg

    echo \
      "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://mirrors.aliyun.com/docker-ce/linux/ubuntu \
      $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
      sudo tee /etc/apt/sources.list.d/docker.list > /dev/null


    # # 添加 Docker 官方 GPG 密钥
    # sudo install -m 0755 -d /etc/apt/keyrings
    # sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
    # sudo chmod a+r /etc/apt/keyrings/docker.asc

    # # 添加 Docker APT 源
    # sudo tee /etc/apt/sources.list.d/docker.list << EOF
    # deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable
    # EOF

    # 再次更新软件源
    sudo apt-get update

    # 安装 Docker 引擎
    sudo apt-get install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin -y


    # 后续优化配置
    # 免 sudo 运行 docker 命令
    # 默认情况下，每次运行 docker 命令都需要加 sudo。如果你的服务器是个人使用或内部开发环境，可以将自己的用户加入 docker 用户组来免去这个麻烦。
    # sudo usermod -aG docker $USER
    # 注意：执行完这条命令后，你需要退出当前终端会话并重新登录，权限才会生效 。

    cat > /etc/docker/daemon.json <<EOF
{
  "data-root": "{{docker_data}}",
  "log-driver": "local",
  "log-opts": {
    "max-size": "20m",
    "max-file": "5"
  }
}
EOF

    sudo systemctl daemon-reload
    sudo systemctl restart docker
}