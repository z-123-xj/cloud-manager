# 云管平台 - 前端（Cloud Manager Front）

云管平台 Web 前端。**Vue 3（CDN 引入）+ 原生 HTML/CSS/JS，无构建工具、无 npm 依赖**，页面由后端服务直接托管，无需单独部署。

- 后端仓库：[cloud-manager-backend](https://github.com/z-123-xj/cloud-manager-backend)
- 功能详细说明 / API 列表：见后端仓库 README.md 与 PROJECT.md

## 本地预览

前端通过相对路径 `/api/...` 与后端**同源**通信，必须配合后端一起运行：

两个仓库统一放在同一个 `cloud-manager/` 项目目录下，本地结构对应生产的 `/opt/cloud-manager/`：

```bash
# 1. 克隆到同一个项目目录
mkdir cloud-manager && cd cloud-manager
git clone https://github.com/z-123-xj/cloud-manager-backend.git
git clone https://github.com/z-123-xj/cloud-manager-front.git

# 2. 按后端 README 完成配置、初始化后启动后端
cd cloud-manager-backend && cp config.example.yaml config.yaml && go build -o devops . && ./devops init && ./devops

# 3. 访问
#    http://localhost:8080
```

后端默认从 `../cloud-manager-front` 读前端（即同在 `cloud-manager/` 下的同级仓库）；若放在别处，改后端 `config.yaml` 的 `frontend.dir` 或设环境变量 `FRONTEND_DIR`。

> 注意：不要用 `http-server` / `live-server` 等单独起前端，会产生跨域问题，也无法验证真实联调效果。

## 生产部署（Nginx 反向代理）

生产环境建议用 Nginx 做反向代理 + HTTPS，只开放 443。部署配置统一收拢在仓库的 [`deploy/`](deploy/) 目录：

| 文件 | 用途 |
|------|------|
| `deploy/nginx.conf` | 生产 vhost：定义 `upstream cloud_backend`（127.0.0.1:8080）、`server_name`、`root`，再 include 公共规则 |
| `deploy/nginx.common.conf` | 公共规则（缓存、gzip、WebSocket、SSE、SFTP、隐藏文件），生产与本地 Docker **共用一份** |
| `deploy/nginx.local.conf` | 本地 Docker vhost：上游改为 `host.docker.internal:8080`，root 指向 bind mount |
| `deploy/start.ps1` | 本地 Docker 一键启动，路径基于脚本所在目录自动推导 |
| `deploy/cloud-manager.service` | 后端 systemd 单元 |

生产与本地只有上游 / root / server_name 三处差异，其余规则改 `nginx.common.conf` 一处即可，不会再出现两边规则漂移。

- **静态文件**：`root` 指向 `cloud-manager-front` 目录，Nginx 直接托管 `index.html` / `terminal.html` / `assets/`；生产 `/opt/cloud-manager/` 对应仓库的 `cloud-manager/` 项目目录，两个仓库保持同级
- **API**：`/api/` 全部反代到 Go 后端（默认 `127.0.0.1:8080`，同源无跨域）
- **隐藏文件**：公共规则已拒绝访问 `.git` 等目录（放行 `.well-known` 供证书签发），避免仓库元数据被 Web 读到

| 场景 | 关键配置（均在 `nginx.common.conf`） |
|------|----------|
| 页面托管 | vhost 的 `root` + 公共规则 `try_files $uri $uri/ /index.html` |
| 静态资源缓存 | `location /assets/` + `expires 7d`（页面引用带 `?v=` 版本参数，改后自动刷新） |
| 独立页 HTML 不缓存 | `location ~ ^/assets/pages/.*\.html$` |
| WebSocket（堡垒机终端 `/api/bastion/ws/`） | 独立 `location` + `Upgrade` / `Connection` 头透传 |
| SSE（同步进度 `/api/sync/progress`） | `proxy_buffering off` + `proxy_cache off` |
| SFTP 分片上传（单片 2MB） | `client_max_body_size 100m` + `proxy_request_buffering off` |
| 长连接保活 | `proxy_read_timeout / proxy_send_timeout 3600s` |

```bash
# 1. 在前端仓库根目录执行：把 deploy/ 内容放到 /etc/nginx/cloud-manager/
#    （公共规则从这里 include；用 cp deploy/* 保证重复执行不会嵌套出 deploy/deploy）
install -d /etc/nginx/cloud-manager
cp deploy/* /etc/nginx/cloud-manager/

# 2. 放置 vhost（Debian/Ubuntu 放 sites-available 并软链到 sites-enabled；RHEL/CentOS 放 conf.d/）
cp /etc/nginx/cloud-manager/nginx.conf /etc/nginx/conf.d/cloud-manager.conf

# 3. 修改 server_name、root，以及 upstream 里的后端端口（与 config.yaml 的 server.port 对应）
vi /etc/nginx/conf.d/cloud-manager.conf

# 4. 校验并重载
nginx -t && systemctl reload nginx
```

本地开发可用 `deploy/start.ps1` 起 Docker 版 Nginx（默认 http://localhost:8081），它挂载前端目录和 `deploy/`，改前端文件刷新即生效。

> 注意：静态文件交由 Nginx 托管后，后端的前端托管能力仍保留（未使用，目录由后端 `config.yaml` 的 `frontend.dir` 配置），回滚时只需改回全量反代即可。Nginx 工作用户（通常 `www-data` / `nginx`）需要对 root 目录有读权限。

更多部署细节（systemd、安全建议）见后端仓库 README「部署」章节。

## 文件结构

项目目录 `cloud-manager/` 下含本仓库与 `cloud-manager-backend/`（生产对应 `/opt/cloud-manager/`）；本仓库内部结构：

```
cloud-manager-front/
├── index.html          # 主页面：登录 + 侧边栏导航 + 多 Tab 内容区
├── terminal.html       # 堡垒机终端独立页面（xterm.js，WebSocket SSH）
├── deploy/             # 部署配置（生产 + 本地 Docker + systemd）
│   ├── nginx.conf           # 生产 vhost（upstream / server_name / root）
│   ├── nginx.common.conf    # 公共规则，两个环境共用
│   ├── nginx.local.conf     # 本地 Docker vhost
│   ├── start.ps1            # 本地 Docker 一键启动
│   └── cloud-manager.service # 后端 systemd 单元
└── assets/
    ├── css/
    │   └── style.css   # 全局样式
    └── js/
        ├── app.js      # Vue 应用主体（所有页面模块逻辑）
        └── vue.global.prod.js  # Vue 3 生产环境库
```

## 页面模块

| 模块 | 说明 |
|------|------|
| 登录 | 用户名 / 密码，失败 5 次锁定提示 |
| 首页 | 云账号 / 域名 / ECS 统计卡片 |
| 域名管理 | 列表、漏斗筛选、到期监控、详情、同步 |
| DNS 解析 | 解析记录增删改查、启停、抽屉表单 |
| ECS 实例 | 列表、筛选、手动维护、同步 |
| 地域管理 | 三平台地域启用 / 禁用 |
| 云账号管理 | AccessKey 托管（Secret 脱敏展示）、账号地域绑定（管理员） |
| 用户管理 | 用户 CRUD、账号解锁（管理员） |
| 堡垒机 | SSH 凭证、用户组授权、Web 终端、SFTP（后端开关可控） |
| 批量执行 | 勾选多台已授权主机，粘贴命令或脚本并发执行，逐台查看输出并保留执行历史 |
| 全量同步 | 域名 + DNS + ECS 一键同步，SSE 实时进度 |

## 认证方式

- 登录成功后 Token 存 `localStorage.cloud_token`，用户信息存 `localStorage.cloud_user` / `cloud_role`
- 全局 `authFetch()` 自动附加 `Authorization` 头
- 收到 401 自动登出跳转登录页

## 修改前端后的生效方式

后端以**无缓存**方式读取前端文件：改完刷新浏览器即可看到变化（HTML 直接生效；静态资源通过 URL 上的 `?v=` 版本参数强制刷新）。
