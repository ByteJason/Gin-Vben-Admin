# Gin-Vben-Admin

基于 Go、Gin、Vue 3 与 Vue Vben Admin 的企业级后台管理基础平台。

当前版本：`1.0.0-dev`

## 项目链接

- 项目文档：
- 在线演示：
- 贡献指南：
- 交流群：

## 基本介绍

项目提供管理端 UI 模板、Gin HTTP 服务、OpenAPI 契约、开发脚本和 Docker Compose 配置，适合企业后台、SaaS 管理台、ERP、CRM、CMS、OA 等项目的基础建设。

## 主要功能

- 管理端布局、路由、主题和国际化基础能力
- 管理端接口契约与统一响应模型
- 管理端登录、refresh 轮换、登出、账号/IP 限流和失败锁定（启用认证配置后）
- `tenant_id` 必填的租户/组织隔离与默认拒绝策略
- Gin 健康检查、统一响应和 request ID
- OpenAPI、错误码和响应 schema
- MySQL/PostgreSQL 的单机、读写分离与集群端点配置；Redis single/Sentinel/Cluster 配置
- 显式数据库迁移、本地加密备份与恢复校验
- Prometheus 指标和 OTLP tracing 导出（默认关闭，可在管理端配置）
- Windows、macOS、Linux 通用的 Node.js 开发命令
- 源码运行与 Docker Compose 部署
- 四组生产菜单、真实运行概览与数据库/Redis 资源监控

## 技术选型

| 层级 | 技术 |
|---|---|
| 管理端 | Vue 3、TypeScript、Vue Vben Admin |
| UI 模板 | Ant Design Vue、Element Plus、Naive UI |
| 服务端 | Go 1.24、Gin |
| API 契约 | OpenAPI |
| 数据服务 | MySQL 9.7、PostgreSQL 18.4、Redis 8.10 |
| 工程化 | pnpm、Turborepo、Node.js、GitHub Actions |
| 部署 | Docker、Docker Compose |

## 目录树

```text
.
├── admin/                              # 管理端 pnpm workspace
│   ├── apps/web-antd/
│   ├── apps/web-ele/
│   ├── apps/web-naive/
│   ├── apps/install/                  # 首次安装静态页面
│   ├── packages/
│   ├── internal/
│   ├── scripts/
│   ├── tests/
│   ├── package.json
│   ├── pnpm-workspace.yaml
│   └── pnpm-lock.yaml
├── server/                             # Gin HTTP 服务
│   ├── cmd/api/
│   ├── cmd/migrate/
│   ├── configs/server.example.yaml
│   ├── internal/bootstrap/
│   ├── internal/config/
│   ├── internal/transport/http/
│   ├── go.mod
│   └── go.sum
├── contracts/                          # OpenAPI、错误码、响应 schema
├── deploy/                             # 单机 Compose 与两份 Alpine Dockerfile
│   ├── docker-compose.yml
│   ├── admin.Dockerfile
│   └── server.Dockerfile
├── scripts/                            # 跨平台 Node.js 脚本
├── tests/contract/                     # 根级契约测试
├── docs/                               # 公开文档
├── LICENSES/                           # 第三方许可证
├── .github/
├── LICENSE
├── NOTICE
├── README.md
└── .gitignore
```

## 快速开始

### 环境要求

- Git
- Go `>= 1.24`
- Node.js `^22.18.0 || ^24.12.0`
- pnpm `11.16.0`
- Docker Engine + Compose v2（使用 Docker 部署时）

Windows 推荐 PowerShell 7，macOS 推荐 zsh，Linux 使用 bash。启用 pnpm：

```text
corepack enable
corepack prepare pnpm@11.16.0 --activate
```

没有 Corepack 时：

```text
npm install --global pnpm@11.16.0
```

### 获取源码

```text
git clone REPOSITORY_URL Gin-Vben-Admin
cd Gin-Vben-Admin
```

### 初始化与源码运行

全新 clone 不要先在 `admin/` 执行 `pnpm install`，否则 pnpm 会安装三套 UI 的依赖。先启动普通 API；它在未初始化状态下也会提供安装页面和安装 API：

```text
cd server
go run ./cmd/api/main.go
```

安装页面和安装 API 只接受真实 loopback 来源与 `localhost`/`127.0.0.1`/`::1` Host；不要通过局域网地址打开。浏览器写操作还必须是同源 `application/json` 请求，代理头不会扩大该边界。

另开一个终端，在 `admin/` 内选择并初始化一套 UI：

```text
cd admin
pnpm run init
```

`init` 只依赖 Node.js 内置模块，因此此时不需要 `node_modules`。它先检查 `http://127.0.0.1:8080` 的 health、安装状态和安装页面，并在移动 UI 前检查所选目录与环境模板；检查通过后让用户选择 Ant Design Vue、Element Plus 或 Naive UI，原子保留所选应用，将另外两套暂存到 `.runtime/install/ui-backup/<transaction>/`，写入 `admin/.ui-profile.json`，并从所选模板的 tracked example 原子生成 development/production 本地环境文件。已有本地文件保持原字节，旧文件缺少的新公开默认项由运行时回退提供。随后它对只剩一套 UI 的 workspace 自动执行 `pnpm install --frozen-lockfile` 并打开 `/install`。这样不会下载另外两套 UI 的专属依赖。

如果 UI 移动、依赖安装或 UI 重置中断，再次执行对应的 init 命令会读取事务状态并从最小未完成步骤继续，不会要求重新选择 UI。网页安装的数据库、Redis、管理员和环境配置也使用无凭据的持久化事务记录；服务重启后恢复到可重试状态，`.installed` 始终在所有安装步骤成功后最后原子写入。

旧版 Windows 初始化若在构建阶段以 `INSTALLER_BUILD_FAILED` 结束，并留下严格匹配的 `.ui-profile.json`、`.ui-init-receipt.json` 与 `.runtime/init-backup/<transaction>/`，新版 `pnpm run init` 会自动接管：把旧备份迁入当前事务目录、可逆隔离旧 receipt，只为已选 UI 继续安装依赖。迁移或依赖安装再次中断时直接重跑；`INIT_LEGACY_MIGRATION=resumed|completed` 会说明本次是续跑还是完成。如需放弃该选择，可直接执行 `pnpm run init -- --reset --confirm-reset`，无需先完成依赖安装。旧状态存在冲突、符号链接、额外条目或 receipt 不匹配时，程序保持原字节并给出 `LEGACY_PREPARED_STATE_INVALID`，不会启动 pnpm。

可先用 `pnpm run init -- --check` 只读检查状态。网页安装完成前需要重新选择时，使用 `pnpm run init -- --reset --confirm-reset` 恢复三套模板。初始化完成前直接运行 `pnpm run dev`、`pnpm run build` 或 `pnpm run preview` 会提示先执行 init。

在网页中完成数据库、Redis、管理员和默认项配置并看到安装成功后，停止旧服务端，并打开两个终端分别运行。`pnpm install` 会幂等校验所选 UI 的依赖并补齐本地链接，不会恢复或安装另外两套 UI：

```text
# 仓库根目录，终端 1：服务端
cd server
go run ./cmd/api/main.go

# 仓库根目录，终端 2：管理端
cd admin
pnpm install
pnpm run dev
```

登录后默认进入 `/dashboard/analytics`“运行概览”。生产菜单、兼容路由、访问码与各页面职责见 [`docs/admin-information-architecture.md`](docs/admin-information-architecture.md)。

编辑本地 `server/configs/server.yaml` 或设置环境变量后，使用显式命令管理数据库迁移：

```text
go -C server run ./cmd/migrate status
go -C server run ./cmd/migrate up
go -C server run ./cmd/migrate down --steps 1
```

配置支持 MySQL/PostgreSQL 的 `single`、`read_write`、`cluster_endpoint` 模式，以及 Redis 的 `single`、`sentinel`、`cluster` 模式。默认示例关闭外部依赖；启用时请通过本地配置或环境变量填写连接信息。

启用管理端认证时，在本地配置中填写至少 32 字节的 `auth.jwt_secret`（或设置 `AUTH_JWT_SECRET`），并同时启用数据库与 Redis。登录、刷新和登出接口见 `contracts/openapi/admin-v1.yaml`；refresh token 只保存在 HttpOnly Cookie。

#### 初始化状态文件

以下文件或目录由初始化器和服务端共同维护，**不要手动删除、改名或编辑**。中断后直接重新运行 `pnpm run init` 或重新打开 `/install`：

| 路径 | 作用 |
|---|---|
| `admin/.ui-profile.json` | 唯一 UI 的稳定声明，也是 `build/dev/preview` 的分发依据 |
| `.runtime/install/` | 初始化事务根目录；其内部短期租约、清理墓碑、`environment-backup/` 等也全部由程序维护，请勿删除或编辑 |
| `.runtime/install/.installed` | 所有安装步骤成功后最后写入的完成标记，也是公开构建命令的门禁 |
| `.runtime/install/admin-init.lock` | `pnpm run init` 的 schema 2 进程租约；绑定 PID 与进程启动身份，真实 owner 即使心跳长时间暂停也不会被 TTL 误回收，崩溃或 PID 复用后由程序安全回收 |
| `.runtime/install/admin-init.lock.reclaim` | 回收崩溃进程租约时使用的原子墓碑；可能短暂存在或在回收进程中断后保留，重跑 init 会自动恢复 |
| `.runtime/install/admin-init-heartbeat/` | 按租约 UUID 隔离并绑定进程启动身份的一主一 owner 双 init 心跳；同步安装依赖期间持续更新，单通道异常或系统暂停不会误解锁 |
| `.runtime/install/apply.lock` | 服务端网页安装/回滚全程持有的进程租约；与 `admin-init.lock` 双向互斥，服务端重启时会在同一 guard 下安全回收崩溃残留，请勿手动删除 |
| `.runtime/install/dependency-install.lock` | 后台依赖安装监督进程的持久租约；绑定监督进程与实际 pnpm Worker 生命周期，前台 init 中断后也用于阻止第二次安装重叠 |
| `.runtime/install/dependency-install.lock.reclaim` | 依赖安装租约的原子回收墓碑；仅由 init 在监督进程及其后代均结束后恢复或清理 |
| `.runtime/install/dependency-install-heartbeat/` | 后台依赖安装监督进程按 UUID 写入的心跳；长时间安装时保持租约活跃 |
| `.runtime/install/dependency-install.log` | `pnpm install` 及 lifecycle 命令的本地诊断日志；init 会输出该路径，安装失败时可查看但不要删除或编辑 |
| `.runtime/install/dependency-job-gate-<UUID>.json` | Windows 下确认依赖监督进程已进入 kill-on-close Job Object 的短期门闩；正常结束时自动清理，强制终止或断电留下的 UUID 隔离残片不阻塞后续 init，请勿手动删除 |
| `.runtime/install/process.guard` | 服务端安装锁共用的持久跨进程保护文件；即使当前没有安装任务也不要删除 |
| `.runtime/install/.installed.lock` | 防止并发安装或并发改写完成标记的短期互斥锁 |
| `.runtime/install/transaction.json` | 不含密码和 DSN 的 UI 选择/重置、网页安装阶段、恢复与补偿记录 |
| `.runtime/install/ui-backup/` | UI 选择事务中暂存的两套未选模板，安装完成后由程序清理 |
| `.runtime/install/legacy-prepared-migration.json` | 接管旧版 Windows 构建失败状态的持久迁移 journal；`--check` 只读识别，重跑 init 从已完成的 rename 继续 |
| `.runtime/install/legacy-recovery/<transaction>/.ui-init-receipt.json` | 迁移时可逆隔离的旧 receipt；用于证明来源，不参与当前事务，请勿移动或改写 |
| `.runtime/init-backup/` | 旧版 init 的 UI 暂存目录；严格合法的目标事务由新版原子迁走，冲突现场保持原样 |
| `.runtime/init-recovery/` | 旧版 init 的历史恢复记录；新版迁移会保留其内容，不要手动清理 |

删除这些状态会使 profile、源码布局和安装结果失去一致性；程序检测到不一致时会保护现场并给出稳定状态码。
若安装需要替换已有 `.env`，`environment-backup/` 会短暂保存权限为 `0600` 的原文件用于补偿；完成标记提交后程序会按事务精确删除，勿复制或提交该目录。

### 源码部署

管理端构建（先完成上面的 UI 选择与网页安装；依赖已经由 `pnpm run init` 安装）：

```text
cd admin
pnpm run build
```

构建结果位于 `.ui-profile.json` 中 `appDirectory` 对应应用的 `dist/`，可交给 Nginx 或其他静态文件服务。服务端构建：

```text
go -C server build -o ./bin/server-api ./cmd/api
./server/bin/server-api
```

运行时配置使用 `server/configs/server.example.yaml` 复制出的本地配置或环境变量；实际密钥和连接串不提交到 Git。

### Docker 部署

单机部署（服务端、管理端、单机 MySQL、单机 Redis）：

```text
ADMIN_UI=antd docker compose -f deploy/docker-compose.yml up -d --build
docker compose -f deploy/docker-compose.yml ps
docker compose -f deploy/docker-compose.yml run --rm migrate status
```

全新、尚未提交 `.ui-profile.json` 的 CI/clone 通过 `ADMIN_UI=antd|ele|naive` 显式选择构建模板；已提交 profile 后可以省略该变量，也可以传入相同值。显式值与 profile 不一致时构建以 `UI_PROFILE_MISMATCH` 停止，不会静默构建另一套 UI。

生产 `deploy/` 只保留这一份 Compose 入口和两份 Alpine Dockerfile；服务端迁移服务会在 API 前启动，服务端使用本地配置或环境变量中的 `database.driver` 与 DSN。

开发/验收拓扑（双库、读写分离、Redis Sentinel/Cluster、Prometheus/Grafana 和 Mailpit）按需生成到 `.runtime/compose/`，不进入正式部署目录：

```text
node scripts/prepare-runtime-compose.mjs
docker compose -f .runtime/compose/dev.yaml config --quiet
docker compose -f .runtime/compose/postgres.yaml config --quiet
docker compose -f .runtime/compose/read-write.yaml config --quiet
docker compose -f .runtime/compose/ha.yaml config --quiet
docker compose -f .runtime/compose/observability.yaml config --quiet
```

停止服务：

```text
docker compose -f deploy/docker-compose.yml down
```

默认管理端端口为 `5173`，服务端端口为 `8080`；可通过 Compose 环境变量调整。

## 使用说明

- 管理端 API 地址默认使用 `/api`，开发代理指向本地 Gin 服务。
- 管理端连通性检查：`GET /api/admin/v1/ping`。
- 健康检查：`GET /health/live`、`GET /health/ready`。
- 管理端接口契约位于 `contracts/openapi/`。
- 本地配置、日志、数据库卷和构建产物按 `.gitignore` 规则处理。

候选版本运行手册见 [`docs/release/0.9.0-rc-runbook.md`](docs/release/0.9.0-rc-runbook.md)。贡献、安全、变更和第三方归属文件分别见 [`CONTRIBUTING.md`](CONTRIBUTING.md)、[`SECURITY.md`](SECURITY.md)、[`CHANGELOG.md`](CHANGELOG.md) 和 [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md)。

从全新 clone、首次安装到每个产品功能与边界数据的逐项人工检查，见 [`docs/manual-acceptance/1.0.0-dev-end-to-end.md`](docs/manual-acceptance/1.0.0-dev-end-to-end.md)。

## 验证

```text
node --test tests/contract/contract.test.mjs
pnpm --dir admin run test:smoke
pnpm --dir admin run check:type
go -C server test ./...
node ./scripts/verify.mjs --scope basic
```

可选本地观测组件：

```text
node scripts/prepare-runtime-compose.mjs
docker compose -f .runtime/compose/observability.yaml --profile observability up -d
```

发布包校验：

```text
node scripts/release/package.mjs --check
```

## 贡献指南

贡献流程文档：

- CONTRIBUTING：
- 安全报告：
- 变更记录：

提交代码前请运行相关测试，并保持提交内容聚焦单一改动。

## 许可证

本项目采用 [MIT License](LICENSE)。前端基于 [Vue Vben Admin](https://github.com/vbenjs/vue-vben-admin)；上游许可证文本见 [`LICENSES/Vue-Vben-Admin-MIT.txt`](LICENSES/Vue-Vben-Admin-MIT.txt)，归属信息见 [`NOTICE`](NOTICE)。
