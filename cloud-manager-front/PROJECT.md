# 云管平台 - 前端项目概览

## 技术栈
- **Vue 3** (CDN: `vue.global.prod.js`)，使用 Composition API
- 纯 HTML/CSS/JS，无构建工具
- **xterm.js** 终端模拟器（堡垒机功能）
- SPA 单页面应用，`index.html` 为入口

## 项目结构

本仓库位于 `cloud-manager/` 项目目录下，与 `cloud-manager-backend/` 同级（生产对应 `/opt/cloud-manager/`）。

```
cloud-manager-front/
├── index.html          # 主页面：登录 + 侧边栏导航 + 多Tab内容区
├── terminal.html       # 堡垒机终端独立页面
├── assets/
│   ├── css/
│   │   └── style.css   # 全局样式（含 DNS 抽屉表单样式）
│   └── js/
│       ├── app.js      # 核心应用逻辑（Vue 实例 + 所有模块）
│       └── vue.global.prod.js  # Vue 3 生产环境库
```

## 页面模块

### 1. 登录页
- 用户名/密码表单，支持锁定提示（403 响应）
- 调用 `POST /api/login/`，成功后存 token/role 到 localStorage
- 全局函数 `authFetch()` 自动附加 `Authorization` 头
- 401 响应自动 `logout()` 跳回登录页

### 2. 首页 (Tab: home)
- 统计卡片：云账号总数、域名总数、ECS总数

### 3. 域名管理 (Tab: domains)
- 统计卡片：域名总数、在线、30天内到期、已过期、已下线
- 筛选栏：keyword、platform、account_id、is_offline、expire_start/end
- 表格列：可配置显示/隐藏，列偏好存 localStorage
- 列内筛选（漏斗）：账号、自动续费、状态
- 排序：域名列、剩余天数列
- 分页：前端分页（先拉全部数据存 `allDomains`，再切片）
- 详情弹窗
- 同步按钮：`POST /api/sync/domains` + SSE 进度

### 4. DNS解析 (Tab: dnsDomains)
- DNS可管理域名列表，筛选+分页
- 点击展开 DNS 解析记录列表（实时调云 API）
- 添加/编辑记录：抽屉面板表单（参照阿里云设计风格）
  - 记录类型带描述提示、主机记录带域名后缀
  - TTL/负载策略折叠面板（默认折叠）
  - TTL 快捷选项按钮
  - 轮询模式多记录值表格
  - 备注字数统计
- 启用/禁用、删除记录

### 5. ECS实例 (Tab: ecs)
- 统计卡片：ECS总数、在线、已下线
- 筛选栏：keyword、platform、account_id、status、is_offline
- 表格：14+ 列
- 分页：后端分页（`ecsPage/ecsSize/ecsTotal`）
- 手动添加/编辑/删除 ECS
- 同步按钮：`POST /api/sync/ecs` + SSE 进度

### 6. 地域管理 (Tab: regions)
- 云平台地域列表，按平台筛选
- 启用/禁用地域

### 7. 账号地域绑定 (编辑云账号弹窗内)
- 编辑云账号时，可设置该账号的 **ECS 同步地域** 模式（三选一）：
  - **使用全局**：同步全局所有已开通地域（默认，与「实例地域开通」一致）
  - **自定义地域**：勾选该账号需要同步的具体地域
  - **不同步（仅域名/DNS）**：该账号不拉取 ECS 实例，但域名/DNS 仍正常同步（适用于仅有域名、无 ECS 的账号）
- 后端接口：`GET/PUT /api/accounts/:account_id/regions`（`region_mode` 字段：global/custom/off）

### 8. 云账号管理（管理员）
- 云账号列表 CRUD
- AccessSecret 脱敏显示，支持 `?reveal_secret=true` 查看完整值

### 9. 用户管理（管理员）
- 用户列表 CRUD
- 解锁被锁定账号

### 10. 堡垒机 (Tab: bastion) — 可配置开关
- SSH凭证管理（密码/私钥）
- 用户组管理
- ECS资产授权绑定
- WebSocket SSH 终端（`terminal.html`）
- 支持 SFTP 文件传输

### 11. 全量同步
- 统一同步入口：域名+DNS+ECS 一键同步
- SSE 实时进度展示

## API 端点（前端调用）
| 方法 | URL | 说明 |
|------|-----|------|
| POST | `/api/login/` | 登录 |
| GET  | `/api/config` | 前端配置（堡垒机开关等） |
| GET  | `/api/domains` | 域名注册列表 |
| GET  | `/api/domains/filter-options` | 域名漏斗选项 |
| GET  | `/api/domains/{domain}` | 域名详情 |
| GET  | `/api/dns-domains` | DNS可管理域名列表 |
| GET  | `/api/dns-domains/filter-options` | DNS域名漏斗选项 |
| GET  | `/api/domains/:domain/dns` | 查询DNS解析记录 |
| POST | `/api/domains/:domain/dns` | 添加DNS解析记录 |
| PUT  | `/api/domains/:domain/dns` | 修改DNS解析记录 |
| DELETE | `/api/domains/:domain/dns` | 删除DNS解析记录 |
| PUT  | `/api/domains/:domain/dns/status` | 启用/禁用DNS记录 |
| GET  | `/api/ecs` | ECS实例列表 |
| POST | `/api/ecs` | 手动添加ECS |
| PUT  | `/api/ecs/:id` | 手动修改ECS |
| PUT  | `/api/ecs/:id/ssh-port` | 维护ECS的SSH端口（同步资产也可改，服务器改过端口时用） |
| DELETE | `/api/ecs/:id` | 手动删除ECS |
| GET  | `/api/ecs/filter-options` | ECS漏斗选项 |
| GET  | `/api/regions` | 地域列表 |
| PUT  | `/api/regions/:id` | 更新地域启用状态 |
| GET  | `/api/accounts/:account_id/regions` | 账号地域绑定 |
| PUT  | `/api/accounts/:account_id/regions` | 更新账号地域绑定 |
| POST | `/api/sync/domains` | 触发域名同步 |
| POST | `/api/sync/dns` | 触发DNS同步 |
| POST | `/api/sync/ecs` | 触发ECS同步 |
| POST | `/api/sync/all` | 触发全量同步 |
| GET  | `/api/sync/progress?task_id=` | SSE 同步进度 |
| GET  | `/api/accounts/count` | 云账号总数 |
| GET  | `/api/accounts` | 云账号列表（管理员） |
| POST | `/api/accounts` | 添加云账号（管理员） |
| PUT  | `/api/accounts/:id` | 更新云账号（管理员） |
| DELETE | `/api/accounts/:id` | 删除云账号（管理员） |
| GET  | `/api/user/` | 用户列表（管理员） |
| POST | `/api/user/` | 创建用户（管理员） |
| PUT  | `/api/user/:id` | 更新用户（管理员） |
| PUT  | `/api/user/:id/unlock` | 解锁账号（管理员） |
| DELETE | `/api/user/:id` | 删除用户（管理员） |
| *    | `/api/bastion/*` | 堡垒机相关接口 |
| GET  | `/api/bastion/batch/options` | 批量执行可选解释器与资源上限 |
| GET  | `/api/bastion/batch/tasks` | 批量执行历史（分页；非管理员仅可见自己发起的） |
| POST | `/api/bastion/batch/tasks` | 提交批量执行任务，返回 `task_id` |
| GET  | `/api/bastion/batch/tasks/:id` | 任务详情 + 每台主机结果（前端每秒轮询） |
| POST | `/api/bastion/batch/tasks/:id/cancel` | 取消运行中的任务 |

## 认证
- Token 存储在 `localStorage.cloud_token`
- 用户信息存储在 `localStorage.cloud_user` / `localStorage.cloud_role`
- 所有 API 请求通过 `authFetch()` 携带 `Authorization` 头
- 401 响应自动调用 `logout()` 跳回登录页
- 登录后同时加载 `loadColPrefs()` 恢复列偏好

## 关键状态变量
| 变量 | 用途 |
|------|------|
| `isLoggedIn` | 登录状态 |
| `currentTab` | 当前页面Tab（home/domains/dnsDomains/ecs/regions/accountRegions/accounts/users/bastion） |
| `userRole` | 用户角色（admin/user/readonly） |
| `isAdmin` | computed，是否管理员 |
| `bastionEnabled` | 堡垒机是否启用（从 /api/config 获取） |
| `sidebarDomainOpen` | 侧边栏域名分组折叠状态 |
| `sidebarServerOpen` | 侧边栏服务器分组折叠状态 |
| `sidebarBastionOpen` | 侧边栏堡垒机分组折叠状态 |
| `filters` | 域名搜索筛选条件 |
| `ecsFilters` | ECS搜索筛选条件 |
| `allDomains` | 从后端获取的全部域名数据（前端分页用） |
| `ecsList/ecsPage/ecsSize/ecsTotal` | ECS后端分页状态 |
| `dnsDomainsList` | DNS可管理域名列表 |
| `dnsRecords` | 当前选中域名的DNS解析记录 |
| `dnsDrawerOpen` | DNS编辑抽屉开关 |
| `dnsCollapseOpen` | TTL/负载策略折叠面板状态 |
| `colPrefs` | 列显示偏好（存 localStorage） |
