# 云管平台 - 后端项目概览

## 技术栈
- **Go 1.26**（`go.mod` 实际为 `go 1.26.4`）, **Gin** Web框架, **GORM** (MySQL)
- **Viper** 配置管理
- 阿里云 / 腾讯云 / AWS OpenAPI SDK
- 自定义 AES 加密 Token（**非标准 JWT**，见"认证"章节）

## 项目结构

本仓库位于 `cloud-manager/` 项目目录下，与 `cloud-manager-front/` 同级（生产对应 `/opt/cloud-manager/`）。

```
cloud-manager-backend/
├── main.go                          # 入口：--config 配置加载/数据库/路由，定时同步+优雅关闭
│   # 说明：除 main.go 外，pkg/cloud 下含 aliyun/ tencent/ aws/ cloudflare/ bastion 五个平台子包
├── config.yaml                      # 数据库/Redis/堡垒机/服务器资源限制配置
├── go.mod / go.sum                  # Go 模块依赖
├── internal/
│   ├── config/config.go             # 配置结构体（MySQL/Bastion/ServerConfig）
│   ├── models/
│   │   ├── core.go                  # DB 初始化 (InitDB)，不使用 AutoMigrate
│   │   ├── user.go                  # User 模型 + 角色常量 + 最大登录失败次数
│   │   ├── account.go               # CloudAccount 模型 (cloud_accounts 表)
│   │   ├── domain.go                # Domain + DnsDomain 模型
│   │   ├── domain_sync.go           # 域名同步数据库操作
│   │   ├── ecs.go                   # EcsInstance 模型（含 source 字段区分 sync/manual）
│   │   ├── ecs_sync.go              # ECS同步数据库操作
│   │   ├── cloud_region.go          # 云平台地域模型
│   │   ├── account_region.go        # 账号地域绑定模型
│   │   ├── bastion_credential.go    # SSH凭证模型
│   │   ├── bastion_binding.go       # 堡垒机资产授权绑定模型
│   │   └── user_group.go            # 堡垒机用户组模型
│   ├── router/router.go             # 总路由 SetupRouter()，注册中间件 + 前端静态文件
│   ├── login/
│   │   ├── login.go                 # 登录 Handler + Token 解析 + AuthMiddleware + AdminMiddleware + ReadonlyMiddleware
│   │   └── router.go                # 登录路由
│   ├── user/
│   │   ├── user.go                  # 用户 CRUD + UnlockUser（解锁账号）
│   │   └── routes.go                # 用户路由
│   ├── cloud_account/
│   │   ├── cloud_account.go         # 云账号 CRUD + GetAccountCount
│   │   └── router.go                # 云账号路由
│   ├── repository/                  # 仓库层（预留，暂未使用）
│   └── pkg/
│       ├── cloud/
│       │   ├── router.go            # cloud 模块总路由注册（域名/DNS/ECS/同步/堡垒机）
│       │   ├── interface.go         # DNSProvider / DomainProvider 接口定义
│       │   ├── sync.go              # 统一同步引擎（SyncAllResources）+ SSE 进度推送
│       │   ├── sync_db.go           # 同步相关数据库操作
│       │   ├── domain_handler.go    # 域名注册列表查询 Handler
│       │   ├── dns_domain_handler.go # DNS可管理域名列表查询 Handler
│       │   ├── dns_handler.go       # DNS解析记录 CRUD Handler（跨平台路由）
│       │   ├── ecs_handler.go       # ECS实例 CRUD Handler
│       │   ├── region_handler.go    # 地域管理 Handler
│       │   ├── region_seed.go       # 三平台地域种子数据
│       │   ├── account_region_handler.go # 账号地域绑定 Handler
│       │   ├── aliyun/
│       │   │   ├── syncer.go        # 阿里云同步器（实现 CloudSyncer 接口）
│       │   │   ├── reques_api.go    # 通用 API 请求封装
│       │   │   ├── get_domain_list.go # 域名注册列表同步
│       │   │   ├── get_dns_domain_list.go # DNS可管理域名列表同步
│       │   │   ├── get_ecs.go       # ECS同步
│       │   │   ├── dns.go           # DNS 端点常量
│       │   │   └── dns/             # DNS 记录操作
│       │   │       ├── common.go        # 公共请求/响应结构体 + 凭证提取
│       │   │       ├── describe_records.go # 查询DNS解析记录列表
│       │   │       ├── add_record.go     # 添加DNS解析记录
│       │   │       ├── update_record.go  # 智能修改DNS记录（先查再比，分类调用API）
│       │   │       ├── delete_record.go  # 删除DNS解析记录
│       │   │       ├── set_status.go     # 启用/禁用DNS记录
│       │   │       └── update_remark.go  # 修改DNS记录备注
│       │   ├── tencent/
│       │   │   ├── syncer.go        # 腾讯云同步器
│       │   │   ├── domain.go        # 域名注册列表同步
│       │   │   ├── dns.go           # DNS 端点常量
│       │   │   ├── get_dns_domain_list.go # DNS可管理域名列表同步
│       │   │   └── dns/             # DNS 记录操作（与阿里云结构一致）
│       │   │       ├── common.go / add_record.go / delete_record.go
│       │   │       ├── describe_records.go / set_status.go / update_record.go
│       │   ├── aws/
│       │   │   ├── syncer.go        # AWS 同步器
│       │   │   └── ec2.go           # EC2实例同步
│       │   ├── cloudflare/          # Cloudflare 域名同步（API v4，Bearer Token）
│       │   │   ├── client.go        # API 客户端（信封解析/分页/超时）
│       │   │   ├── zones.go         # Zones（域名）拉取 → domains 表
│       │   │   └── syncer.go        # 同步器（ECS/DNS域名自动跳过）
│       │   └── bastion/             # 堡垒机（SSH资产管理）+ 批量执行
│       │       ├── router.go        # 堡垒机 / 批量执行路由
│       │       ├── credential_handler.go # SSH凭证管理
│       │       ├── binding_handler.go # 资产授权绑定管理
│       │       ├── group_handler.go # 用户组管理
│       │       ├── ssh_handler.go   # WebSocket SSH 终端
│       │       ├── logs_handler.go  # Nginx 日志排查
│       │       ├── sshspec.go       # 公共SSH原语（授权校验/凭证解密/远程执行）
│       │       ├── batch_handler.go # 批量命令/脚本执行（并发/取消/结果落库）
│       │       └── ssh/
│       │           ├── session.go   # SSH 会话管理
│       │           └── sftp.go      # SFTP 文件传输
├── pkg/utils/
│   ├── crypt.go                     # AES-256-CFB 加解密（密钥当前硬编码，可切换环境变量）
│   └── utils.go                     # 工具函数
└── scripts/init_db.sql              # 数据库建表 DDL（手动执行）
```

## 路由架构
```
/api
├── /healthz         GET     (无需认证) 健康检查
├── /login/          POST    (无需认证) 登录
├── [AuthMiddleware + ReadonlyMiddleware] ────────
│   ├── /domains                     GET     域名注册列表（跨平台查库）
│   ├── /domains/filter-options      GET     域名漏斗筛选选项
│   ├── /domains/:domain             GET     域名详情
│   ├── /domains/:domain/dns         GET     查询DNS解析记录（实时调云API）
│   ├── /domains/:domain/dns         POST    添加DNS解析记录
│   ├── /domains/:domain/dns         PUT     修改DNS解析记录（智能比对）
│   ├── /domains/:domain/dns         DELETE  删除DNS解析记录
│   ├── /domains/:domain/dns/status  PUT     启用/禁用DNS记录
│   ├── /dns-domains                 GET     DNS可管理域名列表
│   ├── /dns-domains/filter-options  GET     DNS域名漏斗筛选选项
│   ├── /cdn-domains                 GET     CDN加速域名列表（阿里云CDN/AWS CloudFront）
│   ├── /cdn-domains/filter-options  GET     CDN域名筛选选项
│   ├── /ecs                         GET     ECS实例列表
│   ├── /ecs                         POST    手动添加ECS（管理员）
│   ├── /ecs/:id                     PUT     手动修改ECS（管理员，仅手动实例）
│   ├── /ecs/:id                     DELETE  手动删除ECS（管理员，仅手动实例）
│   ├── /ecs/:id/ssh-port            PUT     维护SSH端口（管理员，同步/手动实例均可，列表双击修改）
│   ├── /ecs/:id/name                PUT     维护实例名称（管理员，同步/手动实例均可，改名并锁定，同步不再覆盖）
│   ├── /ecs/filter-options          GET     ECS漏斗筛选选项
│   ├── /regions                     GET     地域列表
│   ├── /regions/:id                 PUT     更新地域启用状态
│   ├── /accounts/:account_id/regions GET   账号地域绑定
│   ├── /accounts/:account_id/regions PUT   更新账号地域绑定
│   ├── /accounts/count              GET     云账号总数（首页展示）
│   ├── /sync/domains                POST    触发域名注册列表同步
│   ├── /sync/dns                    POST    触发DNS可管理域名同步
│   ├── /sync/ecs                    POST    触发ECS实例同步
│   ├── /sync/cdn                    POST    触发CDN加速域名同步（阿里云CDN/AWS CloudFront）
│   ├── /sync/all                    POST    触发全量同步（域名+DNS+ECS+CDN）
│   ├── /sync/progress               GET     SSE 同步进度推送
│   ├── /account/:id/sync            POST    按需同步指定账号的资源（管理员，body: {resources:[domains/dns/ecs/cdn/all]}）
│   ├── /config                      GET     前端配置（堡垒机开关等）
│   └── /bastion/*                   *       堡垒机模块（config.yaml 可控开关）
├── [AuthMiddleware + AdminMiddleware] ─────────
│   ├── /user/                       GET     用户列表
│   ├── /user/                       POST    创建用户
│   ├── /user/:id                    GET     查询单个用户
│   ├── /user/:id                    PUT     更新用户
│   ├── /user/:id                    DELETE  删除用户
│   ├── /user/:id/unlock             PUT     解锁被锁定的账号
│   ├── /accounts/                   GET     云账号列表
│   ├── /accounts/                   POST    添加云账号
│   ├── /accounts/:id                GET     查询单个云账号
│   ├── /accounts/:id                PUT     更新云账号
│   └── /accounts/:id                DELETE  删除云账号
```

## 认证
- 基于 AES 加密的自定义 Token（非标准 JWT）
- Token 格式：`userID:role:expireAt` → AES 加密 → Base64
- 登录成功后返回 token，前端存 `localStorage`
- 请求头 `Authorization: Bearer <token>` 或直接传 token
- WebSocket 兼容从 URL Query 传 token
- 中间件验证后注入 `user_id`（字符串）和 `role` 到 Gin Context
- 兼容旧格式 token（`userID:expireAt`）：回退查数据库获取 role
- 登录失败 5 次自动锁定账号，管理员可通过 `/user/:id/unlock` 解锁

## 角色体系
| 角色 | 常量 | 说明 |
|------|------|------|
| admin | `RoleAdmin` | 管理员：完整读写权限 |
| user | `RoleUser` | 普通用户：可读可写（非管理接口） |
| readonly | `RoleReadonly` | 只读用户：仅允许 GET 请求 |

## 核心数据流

### 同步流程（统一入口）
1. 前端触发 `POST /api/sync/{domains|dns|ecs|all}` → 返回 `task_id`
2. 后端启动 `SyncAllResources(resourceType, taskID)` goroutine
3. 查询 `cloud_accounts` 表中所有 `enabled=true` 的账号
4. 按 `platform` 字段分发到对应同步器（`aliyun.Syncer` / `tencent.Syncer` / `aws.Syncer` / `cloudflare.Syncer`）
5. 遍历账号解密 AccessSecret → 调用云平台 API 分页拉取数据 → 写入本地数据库
6. 同步完成后，标记未在本次同步中更新的旧数据为 `is_offline=1`
7. 前端通过 SSE 端点 `/api/sync/progress?task_id=xxx` 实时获取进度
8. 定时任务：每天凌晨 0 点自动执行 `all` 类型全量同步

### DNS 解析记录修改流程
1. 前端调用 `PUT /api/domains/:domain/dns`
2. 后端先调用云 API 查询当前记录的实际值
3. 与前端提交的新值逐字段比对：
   - **标识字段变更**（RR/Type/Value）→ 直接调用 `UpdateDomainRecord`
   - **仅属性字段变更**（TTL/Line/Weight/Priority）→ 先 `DeleteDomainRecord` 再 `AddDomainRecord`（绕过阿里云 DomainRecordDuplicate 限制）
   - **备注变更** → 调用 `UpdateDomainRecordRemark`
   - **状态变更** → 调用 `SetDomainRecordStatus`

### 前端查询流程
- 域名/ECS/DNS域名查询不走云 API，直接查本地数据库
- 支持 keyword、platform、account_id、status、is_offline 等筛选
- 支持分页
- ECS 支持手动 CRUD（source 字段标记为 `manual`）

## 初始化与数据库升级（init 子命令）
`init_db.sql` 是**唯一表结构标准源**，只维护标准 `CREATE TABLE` + 种子 `INSERT`（用 `IF NOT EXISTS` / `INSERT IGNORE` 保证幂等），**不手写 ALTER 自愈块**——历史缺口由 `init` 子命令通用比对补齐。

运行方式：
```
./devops init [--init-sql scripts/init_db.sql] [--init-admin admin] [--init-password Admin@123456]
```
`InitSystem`（`internal/models/seed.go`）分四步（幂等，可重复跑）：
1. **建表**：执行 `init_db.sql` 的 `CREATE TABLE IF NOT EXISTS`，确保标准表存在。
2. **结构对齐** `syncSchemaColumns`：解析标准结构，比对 `information_schema`，按表分类：
   - **受保护表**（`protectedTables` 变量，可编辑）：`users`、`cloud_accounts`、`account_regions`、`bastion_credentials`、`bastion_group_assets`、`bastion_group_credentials`、`user_groups`、`user_group_members`、`batch_tasks`、`batch_task_results` → 只补缺失列/索引；多余列、类型不一致、多余/不一致索引**仅告警，绝不 DROP/MODIFY**（这些配置不可由同步恢复）。
   - **可重建表**（其余每日同步/种子表，如 `ecs_instances`、`domains`、`dns_domains`、`cloud_regions`）→ 补缺失列/索引、删多余索引（不丢数据）；遇多余列或列类型不一致 → **整表 DROP+CREATE 重建**（数据下次同步/种子恢复）。
3. **灌种子** `importSeeds`：执行 `INSERT`（放在结构对齐之后，确保被重建表重新灌入种子）。
4. **确保管理员** `ensureAdmin`：`role=admin` 计数 >0 跳过，否则创建管理员。

> 设计要点：新增/调整列只需改 `init_db.sql`，下次 `init` 自动补齐；受保护范围是可编辑的单一变量，若某张"可重建表"含需持久配置，移入 `protectedTables` 即可。

## 数据模型

### User (users 表)
字段：id, username, password(AES加密), email, role(admin/user/readonly), real_name, login_attempts(失败计数), locked(锁定标志), add_time

### CloudAccount (cloud_accounts 表)
字段：id, account_id(业务主键), platform(aliyun/aws/tencent/cloudflare), site(cn/intl), name, access_key(明文), access_secret(AES加密), enabled, region_mode(global/custom/off，ECS同步地域模式), created_at, updated_at

> `region_mode` 控制 ECS 同步地域范围：`global`=使用全局地域（默认）、`custom`=按 `account_regions` 自定义、`off`=不同步 ECS（域名/DNS 仍同步，适用于仅有域名资源的账号）。

> **Cloudflare 特例**（platform=cloudflare）：无国内/国际站概念，`site` 无意义（前端选择 Cloudflare 时隐藏站点下拉，值固定为 `intl` 静默提交）；凭证体系也与 AK/SK 不同——`access_key` 存 Cloudflare Account ID，`access_secret` 存 API Token（Bearer 认证，需 Zone:Read 权限）。**同步流向**：Cloudflare 是 DNS 服务商而非注册商，zone 里的域名均从其他注册商注册、NS 转入，无到期/续费概念，因此 zones 只同步至 `dns_domains`（DNS 可管理域名列表），**不进入** `domains`（注册域名列表）；无 ECS/地域概念。**记录数/NS 两列**：zones 列表接口不返回，同步时按 zone 补充查询（`dns_records?per_page=1` 的 `total_count` + zone 详情的 `name_servers`）。**解析记录 CRUD**：`internal/pkg/cloud/cloudflare/dns/` 子包实现查/增/改/删（`dns_handler.go` 按平台分发；记录操作先按域名解析 zone_id；PUT 为全量替换，需先读当前记录合并未提交字段）；无记录级启停概念，SetStatus 返回明确提示。**CF 专属表单字段**（前端按查询响应返回的 `platform` 切换）：代理状态（proxied 橙云，仅 A/AAAA/CNAME）、TTL 自动（ttl=1）、完整记录类型列表（结构化类型 SRV/LOC/HTTPS/SVCB/NAPTR 需在 CF 面板配置，后端明确拦截）、注释 100 字符上限且不影响解析；隐藏阿里云特有的解析线路/权重字段。

### Domain (domains 表)
字段：id, account_id, platform, account_name, domain_name, owner, auto_renew_enabled, registration_date, expiration_date, expiration_days_left, expiration_status_code, remark, is_offline(0/1), created_at, updated_at

### DnsDomain (dns_domains 表)
字段：id, account_id, platform, account_name, domain_name(唯一), ali_domain, starmark, record_count, dns_servers, create_time, created_at, updated_at

### CdnDomain (cdn_domains 表)
字段：id, account_id, platform, account_name, domain_name, cname, product, status, biz_type, origin_domain, https_on, aliases, created_at, updated_at

> CDN 加速域名快照表，数据来源：阿里云 CDN（`DescribeUserDomains`）/ 阿里云 DCDN 全站加速（`DescribeDcdnDomains`）/ AWS CloudFront（`ListDistributions`）。
> `product` 字段区分产品类型：`cdn`=CDN，`dcdn`=全站加速（仅阿里云有 DCDN，独立产品、独立 API，同步时分别拉取）。
> 同步策略为**整账号先删后插**（云控制台实时快照，无本地维护字段，无需 is_offline 标记）。
> 平台概念差异：AWS 的用户域名在 `Aliases` 里，取第一个作为 `domain_name`、平台分配的 `xxx.cloudfront.net` 作为 `cname`、全部别名存入 `aliases`；
> HTTPS 状态阿里云列表接口不返回（默认 false），AWS 看 `ViewerProtocolPolicy`。腾讯云/Cloudflare 无此资源（`SyncCdn` 打印跳过）。
> 同步入口由 `runSync(taskID, accountID, resources)` 统一驱动：`accountID` 为空=全账号（各资源页全局同步按钮），
> 非空=单账号按需同步（账号管理页「同步」按钮，`resources` 为勾选的资源类型列表）。

### EcsInstance (ecs_instances 表)
字段：id, account_id, platform, site, account_name, instance_id, instance_name, description, status, cpu, memory, instance_type, os_name, public_ip_address, eip_address, private_ip_address, region_name, zone_name, creation_time, is_offline, source(sync/manual), created_at, updated_at

> 本地维护字段（云 API 不提供、同步不覆盖）：`ssh_port`（SSH 端口，同步默认 22，平台可双击修改）。实例名称（`instance_name`）已支持双击直接调用云端改名接口（阿里云 `ModifyInstanceAttribute` / AWS 更新 `Name` 标签 / 腾讯云 `ModifyInstancesAttribute`），改名后云同步自动保持一致，不再需要锁定。

### CloudRegion (cloud_regions 表)
字段：id, platform, region_id, region_name, region_endpoint, enabled, created_at, updated_at

### AccountRegion (account_regions 表)
字段：id, account_id, region_id, enabled, created_at, updated_at

### BastionCredential (bastion_credentials 表)
字段：id, name, auth_type(password/private_key), username, password(AES加密), private_key(AES加密), passphrase(AES加密), created_by, created_at, updated_at

### BastionAssetBinding (bastion_asset_bindings 表) —— 已废弃
原资产授权绑定表已拆分为 `bastion_group_assets` + `bastion_group_credentials` 两张独立表（见下）。

### BastionGroupAsset (bastion_group_assets 表)
字段：id, group_id, ecs_id, created_at（用户组-资产授权）

### BastionGroupCredential (bastion_group_credentials 表)
字段：id, group_id, credential_id, created_at（用户组-凭证授权）

### UserGroup (user_groups 表)
字段：id, name, description, created_at, updated_at

### UserGroupMember (user_group_members 表)
字段：id, group_id, user_id

### BatchTask (batch_tasks 表)
字段：id, title, script(明文审计), shell, network, credential_id, credential_name, concurrency, timeout_sec, status(running/succeeded/partial/failed/canceled), total, succeeded, failed, created_by, created_name, client_ip, err_msg, started_at, finished_at

### BatchTaskResult (batch_task_results 表)
字段：id, task_id, ecs_id, asset_name, host, port, status(pending/running/success/failed/canceled), exit_code, stdout, stderr, err_msg, duration_ms, started_at, finished_at

## 开发规范（AI / 开发者必读）

以下为长期有效的项目约定，改动代码前必须先读：

1. **详细注释**：所有代码必须添加详细中文注释，重点说明"为什么这么做"，而非只描述"做了什么"。
2. **代码是唯一真相源（Single Source of Truth）**：文档是代码的投影。发现代码与本文件 / README.md 不一致时，以实际代码为准，并同步修正文档。
3. **文档同步**：每次代码修改后，必须同步更新相关文档（本文件 / README.md），不允许代码与文档长期分叉。
4. **数据库变更流程**：
   - 涉及表结构变更时，向使用者提供 SQL 语句由其手动执行；
   - 同时维护 `scripts/init_db.sql`（唯一表结构标准源），只写标准 `CREATE TABLE` + 种子 `INSERT`；
   - **不手写 ALTER 自愈块**，历史缺口统一由 `init` 子命令通用比对补齐（见上文"初始化与数据库升级"）。
5. **配置管理**：真实配置（含数据库连接信息）禁止提交到仓库。仓库只保留 `config.example.yaml` 模板；`config.yaml` 在部署时生成，已被 `.gitignore` 忽略。配置加载由 `main.go` 的 `loadConfig()` 完成：`--config <路径>` 优先，未指定时读取当前工作目录下的 `config.yaml`。
6. **AES 密钥**：`AES_SECRET_KEY` 环境变量优先（必须 32 字节）；未设置时回退内置占位密钥（仅兼容历史加密数据）。涉及加密数据的密钥变更，必须先评估存量数据解密影响。
7. **日志级别**：生产保持 `info`；`debug` 会记录请求体与 SSH 认证细节，仅限本地排查时临时使用。
