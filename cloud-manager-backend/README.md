# 云管平台（Cloud Manager）

多云资源统一管理平台：集中管理 **阿里云 / 腾讯云 / AWS / Cloudflare** 的域名、DNS 解析记录、ECS 实例，并提供 SSH 堡垒机（Web 终端 + SFTP）能力。

- 前端仓库：[cloud-manager-front](https://github.com/z-123-xj/cloud-manager-front)（Vue 3，由本服务直接托管，无需单独部署）
- 开发文档（架构 / 数据流 / 代码结构）：[PROJECT.md](PROJECT.md)

## 功能特性

### 多云资源管理
| 模块 | 说明 |
|------|------|
| 云账号 | 多平台（aliyun / aws / tencent / cloudflare）AccessKey / API Token 托管，Secret 加密存储、脱敏展示 |
| 域名管理 | 注册域名自动同步、到期天数监控（30 天内 / 已过期漏斗）、关键字 / 平台 / 账号 / 状态下钻筛选 |
| DNS 解析 | 解析记录增删改查、启用 / 禁用、智能修改；**Cloudflare zones 自动同步至 DNS 域名列表**（记录 CRUD 暂仅支持阿里云 / 腾讯云） |
| CDN 域名 | 阿里云 CDN + DCDN 全站加速 / AWS CloudFront 加速域名自动同步（产品类型 / 域名 / CNAME / 状态 / 业务类型 / 源站 / HTTPS），腾讯云与 Cloudflare 无此资源自动跳过 |
| ECS 实例 | 云上实例自动同步 + 手动维护（`source` 字段区分 sync / manual）、多维度筛选 |

> **Cloudflare 账号**：仅同步 DNS 域名（Zones → DNS 域名列表）——Cloudflare 是 DNS 服务商，没有 ECS / 地域 / 注册域名概念，这些同步自动跳过（CDN 即代理，无独立 CDN 产品）。凭证填写方式：AccessKey = Cloudflare **Account ID**（32 位十六进制，用于按账户过滤），AccessSecret = **API Token**（面板 My Profile → API Tokens 创建，需 Zone → Zone → Read 权限）。
| 地域管理 | 三平台地域种子数据；按账号配置同步范围：全局 / 自定义 / 不同步（仅域名 DNS） |

### 平台能力
- **统一同步引擎**：域名 / DNS / ECS / CDN 一键全量同步；云平台账号管理页支持**按账号按需同步**（勾选资源类型组合）
- **每日定时同步**：凌晨 0 点自动执行全量同步
- **同步日志**：页面不再展示同步日志面板，同步过程输出到服务端日志文件（`logs/` 按天轮转）与标准输出（`journalctl -u cloud-manager` 可看），页面通过 toast 反馈触发/完成状态
- **堡垒机**（可配置开关）：SSH 凭证托管（密码 / 私钥）、用户组授权、Web 终端、SFTP 文件传输、空闲会话自动回收
- **批量执行**：在已授权的 ECS 资产上批量下发命令或脚本（sh / bash / python3），支持并发控制、单台超时、中途取消；逐台记录 stdout / stderr / 退出码与执行人，任务与结果全部落库可审计
- **用户与权限**：admin / user / readonly 三角色；登录失败 5 次自动锁定，管理员可解锁

## 技术栈

- **后端**：Go 1.26+ · Gin · GORM(MySQL) · Viper
- **前端**：Vue 3（CDN 引入，无构建工具），默认位于 `../cloud-manager-front`（后端 `frontend.dir` 可配置）
- **认证**：AES 加密自定义 Token（非 JWT），前端存 localStorage

## 环境要求

| 组件 | 要求 | 说明 |
|------|------|------|
| Go | 1.26+ | 仅构建阶段需要 |
| MySQL | 5.7+ / 8.0 | **必需**，数据存储 |
| Redis | — | `config.yaml` 中为预留配置项，当前版本代码未使用 |

## 快速开始

### 1. 准备数据库

```sql
CREATE DATABASE cloud_manager DEFAULT CHARACTER SET utf8mb4;
```

### 2. 生成配置

```bash
cp config.example.yaml config.yaml
```

编辑 `config.yaml`，至少修改 `database.mysql` 的 `host` / `user` / `password`。完整配置项见下文[配置说明](#配置说明)。

### 3. 构建

```bash
go build -o devops .
```

### 4. 初始化数据库（幂等，可重复执行）

```bash
./devops init
```

- 自动创建全部表结构（`scripts/init_db.sql` 为唯一表结构标准源）
- **升级到含「批量执行」的版本后需重新执行一次 `./devops init`**，用于创建 `batch_tasks` / `batch_task_results` 两张表（均列为受保护表，已有执行记录不会被重建）
- 创建默认管理员 `admin`，默认密码 `Admin@123456`
- 自定义管理员：`./devops init --init-admin admin --init-password 'YourStrongPassword'`
- **首次登录后请立即修改密码**

### 5. 启动

```bash
./devops
# 或指定其他位置的配置文件：
# ./devops --config /etc/cloud-manager/config.yaml
```

默认监听 `:8080`，访问 `http://<服务器IP>:8080` 进入登录页。

### 6. 登录使用

1. 使用第 4 步创建的管理员账号登录
2. 进入「云账号」添加云平台 AccessKey（阿里云 / 腾讯云 / AWS）
3. 点击各页面「同步」按钮拉取数据（域名 / DNS / ECS / CDN），或在「云平台账号」页对单个账号按需勾选同步（或等待每日凌晨自动同步）
4. 按需在「用户」创建普通 / 只读账号，在「堡垒机」配置 SSH 凭证与授权

## 配置说明

配置文件默认读取**当前工作目录**下的 `config.yaml`（注意是启动进程时的工作目录，不是二进制所在目录）。也可以用 `--config` 参数指定任意路径的配置文件：

```bash
./devops --config /etc/cloud-manager/config.yaml
```

- `--config` 支持绝对 / 相对路径，格式按扩展名识别（yaml / yml / json / toml 等 viper 支持的格式）
- 未指定 `--config` 时保持默认行为：查找当前目录的 `config.yaml`，找不到则启动失败
- 配置文件修改后需重启服务生效

### database.mysql — 数据库连接

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `host` | — | MySQL 地址 |
| `port` | `3306` | MySQL 端口 |
| `user` | — | 数据库用户 |
| `password` | — | 数据库密码 |
| `dbname` | `cloud_manager` | 数据库名 |
| `charset` | `utf8mb4` | 字符集 |

### redis — 预留

当前版本未使用 Redis，可忽略。

### frontend — 前端静态文件目录

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `dir` | `../cloud-manager-front` | 后端自带前端托管时读取的目录，相对路径以进程工作目录为基准；可用环境变量 `FRONTEND_DIR` 覆盖。目录不再要求与后端同级，生产由 Nginx 托管静态时此项不参与 |

### bastion — 堡垒机

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `enabled` | `false`（模板） | 是否启用堡垒机功能；关闭后前端不显示堡垒机入口 |
| `max_sessions` | `200` | 最大并发 SSH 会话数 |
| `session_timeout` | `30` | SSH 会话无操作自动断开时长（分钟），仅统计真实终端 I/O |
| `default_ssh_port` | `22` | 全局默认 SSH 端口：资产 `ecs_instances.ssh_port` 为空或非法时回退到此端口（老数据 / 漏设资产兜底） |
| `batch_max_concurrency` | `20` | 单个批量执行任务的最大并发台数 |
| `batch_max_timeout_sec` | `3600` | 单台主机执行超时上限（秒） |
| `batch_max_script_bytes` | `32768` | 脚本内容最大字节数 |
| `batch_max_output_bytes` | `16384` | 单台 stdout / stderr 各自留存的最大字节数 |
| `batch_max_assets` | `200` | 单个任务最多可选主机数 |

### log — 日志

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `level` | `info` | 日志级别：debug / info / warn / error。**生产建议 info**，debug 会记录请求体与 SSH 认证细节 |
| `path` | `./logs` | 日志目录，按天轮转（`cloud-manager-YYYY-MM-DD.log`）；为空则仅输出到控制台 |
| `max_age_days` | `7` | 日志保留天数，`0` 为永久保留 |

### server — 服务与资源限制

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `port` | `8080` | HTTP 监听端口 |
| `memory_limit_mb` | `512` | Go 运行时内存软限制（MB），超限后 GC 更激进 |
| `gc_percent` | `50` | GC 触发阈值百分比，默认 100，越小 GC 越频繁 |
| `read_timeout` | `30` | HTTP 读取超时（秒） |
| `write_timeout` | `60` | HTTP 写入超时（秒） |
| `idle_timeout` | `120` | HTTP 空闲连接超时（秒） |

> K8s 部署注意：Pod memory limit 应大于 `memory_limit_mb`（建议留 20% 余量），否则 OOMKill 会直接杀 Pod，来不及 GC 回收。

## 子命令

| 命令 | 说明 |
|------|------|
| `./devops init` | 初始化数据库：建表 + 结构对齐 + 灌种子 + 确保管理员。**幂等，升级后可重复执行** |
| `./devops resetpw --username <用户> --new-password <新密码>` | 应急重置指定用户密码并解锁 |

## 部署

### 目录结构

前后端两个仓库统一放在同一个 `cloud-manager/` 项目目录下，本地该目录对应生产的 `/opt/cloud-manager/`。后端自带的前端托管能力（直连 `:8080` 或从 Nginx 回滚时用）通过 `config.yaml` 的 `frontend.dir` 定位前端目录，默认 `../cloud-manager-front`（即同在项目目录下的同级仓库），也可用环境变量 `FRONTEND_DIR` 覆盖；若把前端挪到别处，改这一项即可。生产上静态文件由 Nginx 直接托管，后端此项不参与。

沿用默认值时的部署布局：

```
/opt/cloud-manager/            # 本地同样叫 cloud-manager/，两个目录结构一一对应
├── cloud-manager-backend/     # 本仓库：config.yaml、devops、logs/
└── cloud-manager-front/       # 前端仓库：index.html、assets/、deploy/
                               # deploy/ 内含 Nginx vhost、公共规则、systemd 单元
```

启动时的工作目录必须是 `cloud-manager-backend/`（`viper` 读取当前目录的 `config.yaml`，`frontend.dir` 的相对路径也以此为基准）。

Nginx 与 systemd 的部署配置统一从前端仓库的 `deploy/` 取，见前端仓库 README「生产部署」章节。

### systemd 服务单元

单元文件已收拢到前端仓库 `deploy/cloud-manager.service`，README 不再维护副本：

```bash
cp /opt/cloud-manager/cloud-manager-front/deploy/cloud-manager.service /etc/systemd/system/
# 按需修改 WorkingDirectory / ExecStart / AES_SECRET_KEY
vi /etc/systemd/system/cloud-manager.service
systemctl daemon-reload
systemctl enable --now cloud-manager
```

`WorkingDirectory` 必须是后端目录：viper 从这里读 `config.yaml`，`frontend.dir` 的相对路径也以此为基准。生产务必把 `AES_SECRET_KEY` 换成 32 字节随机值（生成：`openssl rand -base64 24`）。

### 安全建议

1. **首登改密**：默认管理员密码 `Admin@123456` 仅用于初始化，登录后立即修改
2. **设置 `AES_SECRET_KEY`**：环境变量优先（必须 32 字节）；未设置时回退到源码内置的公开占位密钥，仅用于兼容历史加密数据，生产必须覆盖
3. **日志级别用 info**：debug 会记录请求体与 SSH 认证细节
4. **保护 config.yaml**：含数据库凭证，已被 `.gitignore` 忽略，切勿提交到公开仓库
5. **网络暴露**：建议 Nginx 反向代理 + HTTPS，防火墙只开放必要端口
6. **云账号权限最小化**：建议为同步创建只读权限的子账号（域名 / DNS 只读 + ECS 只读）

## 常见问题

**登录提示账号已锁定？**
登录连续失败 5 次会自动锁定。管理员可在「用户管理」点击解锁，或在服务器执行：
```bash
./devops resetpw --username admin --new-password 'NewPassword'
```

**改了配置不生效？**
配置仅在启动时读取，修改 `config.yaml` 后需重启服务：`systemctl restart cloud-manager`

**升级后表结构对不上？**
重新执行 `./devops init`，会自动补齐缺失的列 / 索引（受保护表只增不删，可安全重复执行）。

**前端页面 404？**
确认 `cloud-manager-front` 与 `cloud-manager-backend` 同在 `cloud-manager/`（生产 `/opt/cloud-manager/`）下且同级，服务的工作目录在 `cloud-manager-backend/` 下。

**服务器 SSH 端口不是 22，堡垒机连不上？**
ECS 实例表 `ecs_instances` 新增了 `ssh_port` 列（`INT NOT NULL DEFAULT 22`）：
- 云同步下来的资产自动带默认值 22
- 服务器改过端口：管理员在「ECS 实例」列表**双击「SSH 端口」列**直接修改（同步资产也可以改），回车/失焦保存，即时生效
- 端口为空或非法时回退到全局配置 `bastion.default_ssh_port`（默认 22）
- 升级后老库会自动补列：重新执行 `./devops init` 即可（存量行填充 22，不丢数据）

## 目录结构

```
cloud-manager-backend/
├── main.go                  # 入口：配置/数据库/路由初始化、定时同步、优雅关闭
├── config.example.yaml      # 配置模板（复制为 config.yaml 使用）
├── scripts/init_db.sql      # 数据库建表 DDL（唯一表结构标准源）
├── internal/                # 业务代码（模型 / 路由 / 云平台 SDK 封装 / 堡垒机）
└── pkg/utils/               # AES 加解密、工具函数
```

更详细的架构说明见 [PROJECT.md](PROJECT.md)。
