# cloud-manager 前端 NGINX 一键启动（本地 Docker）
# 用法:
#   .\start.ps1                  启动（容器已存在则重建，应用最新配置/挂载）
#   .\start.ps1 -Port 8082       指定宿主机端口
#   docker rm -f cloud-nginx     停止并删除容器
# 前置条件: Docker Desktop 已安装且在运行
#
# 路径全部基于本脚本所在目录推导，不再硬编码工作区绝对路径；
# 把 cloud-manager-front 仓库放到任何位置都能直接运行。
param(
    [int]$Port = 8081,
    [string]$Name = "cloud-nginx"
)
$ErrorActionPreference = "Stop"

$deployDir = $PSScriptRoot                       # .../cloud-manager-front/deploy
$frontDir  = Split-Path -Parent $deployDir      # .../cloud-manager-front
$localConf = Join-Path $deployDir "nginx.local.conf"
$commonConf = Join-Path $deployDir "nginx.common.conf"

foreach ($f in @($localConf, $commonConf, (Join-Path $frontDir "index.html"))) {
    if (-not (Test-Path $f)) {
        throw "缺少必要文件: $f（请确认 deploy/ 与前端文件完整）"
    }
}

# 同名容器先删掉，保证每次启动都应用最新的挂载路径和配置
docker rm -f $Name 2>$null | Out-Null

# deploy/ 整个目录挂到 /etc/nginx/cloud-manager，供 vhost 与公共规则 include；
# nginx.local.conf 单独挂成默认 vhost。两者路径与生产部署保持一致。
docker run -d --name $Name `
  -p "${Port}:80" `
  -v "${frontDir}:/usr/share/nginx/html:ro" `
  -v "${deployDir}:/etc/nginx/cloud-manager:ro" `
  -v "${localConf}:/etc/nginx/conf.d/default.conf:ro" `
  --add-host host.docker.internal:host-gateway `
  --restart unless-stopped `
  nginx:alpine

if ($LASTEXITCODE -ne 0) {
    throw "容器启动失败：请确认 Docker Desktop 正在运行，且前端目录已在 文件共享 中放行"
}

Write-Host "NGINX started: http://localhost:$Port"
Write-Host "  front dir : $frontDir"
Write-Host "  deploy dir: $deployDir"
Write-Host "  stop      : docker rm -f $Name"
