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

> 本节采用单仓库非破坏选择：三套模板同时保留，选择只影响本机运行分发。

仓库始终保留 `admin/apps/web-antd`、`admin/apps/web-ele`、`admin/apps/web-naive`
三套 UI 源码。选择器只维护被 Git 忽略的本机 profile、依赖收据和运行配置，不会删除、移动或
重命名任何模板。网页提供 Ant Design Vue、Element Plus、Naive UI 三个选项。

#### 1. 拉取、检查环境

```text
corepack enable
corepack prepare pnpm@11.16.0 --activate
```

先按上面的“获取源码”进入仓库。不要先执行无过滤条件的 `pnpm install`；下面的浏览器优先和
CLI 优先流程 **二选一**，不要混用。两条流程都会保留三套源码，并只安装当前 UI 的依赖闭包。

#### 2A. 浏览器优先（普通用户推荐）

首次安装只启动 Go 服务端，暂不启动管理端开发服务器：

```text
cd server
go run ./cmd/api/main.go
```

打开 [http://127.0.0.1:8080/install](http://127.0.0.1:8080/install)，选择 Ant Design Vue、
Element Plus 或 Naive UI。网页准备任务会写入本机选择，并在后台对选中包执行冻结 lockfile 的
过滤安装；另外两套源码保持原样。

在网页中完成数据库、Redis、管理员和默认项配置并看到安装成功后，进入下面“两条路径共同的完成步骤”。

#### 2B. CLI 优先（开发者）

若选择本路径，请跳过 2A。先预览并写入本机选择，再用统一命令安装当前 UI 的依赖：

```text
cd admin
pnpm run ui:select -- --check
pnpm run ui:select -- ele --dry-run
pnpm run ui:select -- ele
pnpm run ui:install
```

可选值为 `antd`、`ele`、`naive`。选择记录在
`admin/.ui-profile.local.json`（已加入 `.gitignore`）。随后启动 Go 服务端并打开安装页：

```text
cd ../server
go run ./cmd/api/main.go
# 浏览器打开 http://127.0.0.1:8080/install
```

此时网页把 CLI 已选 UI 显示为只读项，只继续数据库、Redis、管理员和默认项安装。看到安装成功后，
进入下面的共同完成步骤；不要在网页安装完成前另开一个 `pnpm run dev`。

#### 两条路径共同的完成步骤

停止安装时启动的服务端，再打开两个终端。管理端只在此处启动一次：

```text
# 终端 1：服务端
cd server
go run ./cmd/api/main.go

# 终端 2：管理端
cd admin
# ui:install 内部执行 pnpm install --filter 当前 profile... --frozen-lockfile
pnpm run ui:install
pnpm run dev
```

`ui:install` 会解析当前 profile、安装选中包的依赖闭包并刷新本机依赖收据；重复执行是幂等的。
浏览器准备阶段已完成同一项过滤安装，安装成功后再次执行用于统一所有本地启动、拉取和切换流程。

若首次登录持续提示凭据无效，先停止正在运行的 Go 服务端，再在仓库的 `server` 目录重设
**安装收据中记录的初始管理员**密码；成功后重新启动服务端。
命令不会通过参数或输出暴露密码，也不会接收任意用户名；两次输入必须一致，并遵循安装页相同的
6–72 位 ASCII 字母加数字规则。输入时终端不回显：

```text
read -rs NEW_ADMIN_PASSWORD
printf '\n'
read -rs NEW_ADMIN_PASSWORD_CONFIRM
printf '\n'
printf '%s\n%s\n' "$NEW_ADMIN_PASSWORD" "$NEW_ADMIN_PASSWORD_CONFIRM" | go run ./cmd/admin-password reset
unset NEW_ADMIN_PASSWORD NEW_ADMIN_PASSWORD_CONFIRM
```

成功输出 `ADMIN_PASSWORD_RESET=OK` 和 `LOGIN_FAILURE_STATE_RESET=OK`；该操作同时清除数据库和
Redis 中该账号的登录失败/锁定状态。使用非默认 YAML 时追加 `--config PATH`。

团队成员可以各自选择。Node/CLI 还接受 `ADMIN_UI`，并兼容 `APP_UI` 别名；CI/Docker 仅使用
显式 `ADMIN_UI=antd|ele|naive`，不把个人偏好提交到分支。

#### 3. 运行与安装状态

`dev`、`build`、`preview` 只分发当前选择的包；公共业务包和三套 UI adapter 仍由同一
workspace 管理。有效 profile 处于 `ui_prepared` 时，前端命令已经可以分发到所选包；
`.runtime/install/.installed` 只证明后端数据库/管理员安装完成，并控制业务 API 门禁，不是
前端依赖或构建门禁。教程仍把 `pnpm run dev` 放在网页安装之后，以免开发页面请求尚未开放的
业务 API。

维护检查可在任意终端执行：

```text
cd admin
pnpm run init -- --check
```

#### 4. 后续拉取上游更新

```text
git status
git pull --ff-only
cd admin
pnpm run ui:install
pnpm run dev
```

选择器不会删除 tracked UI 路径，因此不会因为“本机选了其中一套”制造 `modify/delete` 冲突。
用户自己修改了上游同一文件、提交历史已经分叉或存在未提交改动时，仍按普通 Git 工作流先提交、
暂存、rebase 或 merge；`git pull --ff-only` 只适用于当前分支能够快进的情况。lockfile 或公共包
变化后重新执行 `pnpm run ui:install`，它会刷新当前 UI 的依赖收据。

##### 从旧版“删除两套 UI”工作区一次性收敛

旧版本已经执行过 source-moving 初始化的用户，先停止 `init/dev`。若 `pnpm run init -- --check`
显示仍有未完成事务，先按输出续跑或在网页后端安装完成前执行
`pnpm run init -- --reset --confirm-reset`；不要绕过活动 journal。事务已完成后，把**未选择的两套**
tracked 目录恢复到当前提交，再写入同一个本机选择。以下示例假设旧选择为 `ele`，因此只恢复另外两套：

```text
# 仓库根目录；不会覆盖已选择的 web-ele 本地适配
git restore --source=HEAD -- admin/apps/web-antd admin/apps/web-naive
cd admin
pnpm run ui:select -- ele
pnpm run ui:install
cd ..
git status --short
git pull --ff-only
```

若旧选择是 `antd` 或 `naive`，对应替换命令中的两条未选目录。未选目录里若也有需要保留的本地
改动，先单独备份或提交；恢复后 Git 不再看到这两棵树的本机删除，后续更新就进入上面的普通
fast-forward 流程。旧 `.ui-profile.json`/receipt 只保留作兼容证据，本机
`.ui-profile.local.json` 从此优先，日常不再运行 source-moving 初始化。

#### 5. 切换 UI

切换不会恢复、删除或重新安装后端，也不会改写数据库：

```text
cd admin
pnpm run ui:select -- naive --check   # 查看计划
pnpm run ui:select -- naive           # 写入新选择
pnpm run ui:install                    # 只安装新 UI 的依赖闭包
pnpm run dev
```

切换报告写入 `.runtime/install/ui-switch-report.json`，包含
`previousUi`、`selectedUi`、`changedBranch=selectedUi`、`commonLayer=preserved` 和
`sourceAdapter`、`targetAdapter`、`uiSpecific=revalidate-adapter`，以及
`adapterChecks=[route,theme,form,component]`。这些字段表示需要人工复核新 UI adapter；报告不会
伪造自动迁移结果。选择器原子更新活动 profile；若根 `.env` 已存在，则只精确
更新 `APP_UI_ACTIVE`，其余配置保持不变。

已完成网页安装的工作区会保留原 `.runtime/install/.installed`，并在
`.runtime/install/ui-switch-history/` 保存字节一致的历史副本作为审计证据。服务端继续报告
`installed`，现有数据库、管理员和业务 API 保持可用；切换后只需运行 `pnpm run ui:install`，
无需再次执行网页后端安装。

#### 6. CI / Docker

```text
ADMIN_UI=antd pnpm --dir admin install --filter @vben/web-antd... --frozen-lockfile
ADMIN_UI=antd pnpm --dir admin run build
ADMIN_UI=naive docker compose -f deploy/docker-compose.yml up -d --build
```

CI 建议用矩阵分别验证三套 UI；每次使用同一份 `admin/pnpm-lock.yaml`，不生成分叉 lockfile。
没有 `ADMIN_UI` 且没有本机选择时，构建门禁会提示先选择目标。

登录后默认进入 `/dashboard/analytics`“运行概览”。生产菜单、兼容路由、访问码与各页面职责见 [`docs/admin-information-architecture.md`](docs/admin-information-architecture.md)。

编辑本地 `server/configs/server.yaml` 或设置环境变量后，使用显式命令管理数据库建表迁移。全新安装由
`server/migrations/schema.go` 注册 `server/internal/platform/persistence/model` 中的 Model，
通过 GORM `Migrator().CreateTable` 一次完成；后续版本按模块使用显式 GORM Migrator
升级。详细约定见
[`docs/database-migration.md`](docs/database-migration.md)。

```text
go -C server run ./cmd/migrate status
go -C server run ./cmd/migrate up
go -C server run ./cmd/migrate down --steps 1
```

建表只使用配置指定的 MySQL/PostgreSQL 主库，不执行增量 `ALTER`/`ADD` 操作；业务关系不创建数据库外键约束或外键索引。
后台、共享身份和预留用户端的 Model 边界，以及后续升级目录见迁移文档。

配置支持 MySQL/PostgreSQL 的 `single`、`read_write`、`cluster_endpoint` 模式，以及 Redis 的 `single`、`sentinel`、`cluster` 模式。默认示例关闭外部依赖；启用时请通过本地配置或环境变量填写连接信息。

启用管理端认证时，在本地配置中填写至少 32 字节的 `auth.jwt_secret`（或设置 `AUTH_JWT_SECRET`），并同时启用数据库与 Redis。登录、刷新和登出接口见 `contracts/openapi/admin-v1.yaml`；refresh token 只保存在 HttpOnly Cookie。

#### 初始化状态文件

以下文件或目录由网页准备任务、Node 初始化器和服务端共同维护，**不要手动删除、改名或编辑**。个人选择文件不进入 Git；中断后重新运行选择命令或重新打开 `/install`：

维护检查仍可使用 `pnpm run init -- --check`；网页安装前的旧版恢复入口为
`pnpm run init -- --reset --confirm-reset`。新项目的日常选择使用上面的 `ui:select`，不执行
源码移动清理。

| 路径 | 作用 |
|---|---|
| `admin/.ui-profile.local.json` | 本机 UI 选择（已忽略）；优先级高于旧版 tracked profile |
| `admin/.ui-profile.json` | 旧版兼容读取入口；新选择流程不会改写它 |
| `.runtime/install/` | 初始化事务根目录；租约、依赖收据、切换报告等由程序维护，请勿删除或编辑 |
| `.runtime/install/workspace-transaction.json` | 非破坏 journal；`switching_ui` 保护 selector/`APP_UI_ACTIVE` 的原子切换，`dependencies_pending` 保护依赖准备，`moves` 始终为空。中断后用同一目标重跑；journal 存在时 dev/build 门禁保持关闭 |
| `.runtime/install/workspace-dependencies.json` | 当前 UI 与 lockfile 摘要；`pnpm run ui:install` 成功后刷新 |
| `.runtime/install/ui-switch-report.json` | 切换前后 UI、公共层保留与 adapter 复核提示 |
| `.runtime/install/ui-switch-history/` | 已安装工作区切换时保存的后端安装标记只读历史副本；原标记保持不变 |
| `.runtime/install/.installed` | 网页后端安装完成标记和业务 API 门禁；不作为前端依赖/build 门禁，切换 UI 时保留 |
| `.runtime/install/admin-init.lock` | 网页 UI 准备子进程的 schema 2 租约；绑定 PID 与进程启动身份，真实 owner 即使心跳长时间暂停也不会被 TTL 误回收，崩溃或 PID 复用后由程序安全回收 |
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
| `.runtime/install/transaction.json` | 旧版 UI 移动事务和网页安装阶段的兼容记录 |
| `.runtime/install/ui-backup/` | 仅旧版迁移/恢复可能出现；新选择流程不会创建 |
| `.runtime/install/legacy-prepared-migration.json` | 接管旧版 Windows 构建失败状态的持久迁移 journal；`--check` 只读识别，重跑 init 从已完成的 rename 继续 |
| `.runtime/install/legacy-recovery/<transaction>/.ui-init-receipt.json` | 迁移时可逆隔离的旧 receipt；用于证明来源，不参与当前事务，请勿移动或改写 |
| `.runtime/init-backup/` | 旧版 init 的 UI 暂存目录；严格合法的目标事务由新版原子迁走，冲突现场保持原样 |
| `.runtime/init-recovery/` | 旧版 init 的历史恢复记录；新版迁移会保留其内容，不要手动清理 |

删除这些状态会使 profile、源码布局和安装结果失去一致性；程序检测到不一致时会保护现场并给出稳定状态码。
若安装需要替换已有 `.env`，`environment-backup/` 会短暂保存权限为 `0600` 的原文件用于补偿；完成标记提交后程序会按事务精确删除，勿复制或提交该目录。

### 源码部署

管理端构建（先完成上面的 UI 选择，并安装当前目标依赖）：

```text
cd admin
pnpm run ui:install
pnpm run build
```

构建结果位于当前选择的 `admin/apps/web-<ui>/dist/`，可交给 Nginx 或其他静态文件服务。服务端构建：

```text
go -C server build -o ./bin/server-api ./cmd/api
./server/bin/server-api
```

运行时配置使用 `server/configs/server.example.yaml` 复制出的本地配置或环境变量；实际密钥和连接串不提交到 Git。

### Docker 部署

#### 源码首次安装

全新 clone 请先使用“2A. 浏览器优先”通过 Go `/install` 完成 UI、数据库、Redis 和管理员安装。
当前 Compose 样例不代理 `/install`、服务端镜像不携带 embedded 安装页，也没有为
`.runtime/install/.installed` 声明持久化挂载，因此不能把下面的 Compose 命令当作首次安装或
首次登录入口。

#### 已安装环境与镜像/基础设施冒烟

部署平台已经安全提供持久化后端安装标记、运行配置和数据库后，可显式选择一个 UI 构建镜像。
仓库自带 Compose 默认只验证服务端、管理端、单机 MySQL 和单机 Redis 的构建与基础设施拓扑：

```text
ADMIN_UI=antd docker compose -f deploy/docker-compose.yml up -d --build
docker compose -f deploy/docker-compose.yml ps
docker compose -f deploy/docker-compose.yml run --rm migrate status
```

CI/Docker 通过 `ADMIN_UI=antd|ele|naive` 显式选择构建模板；
`admin/.ui-profile.local.json` 已同时加入 `.gitignore` 与 `.dockerignore`，个人选择不会进入镜像上下文。
旧版 tracked `.ui-profile.json` 仅作兼容回退；若它与显式部署选择冲突，构建以
`UI_PROFILE_MISMATCH` 停止，不会静默构建另一套 UI。

`deploy/` 只保留这一份 Compose 入口和两份 Alpine Dockerfile；迁移服务会在 API 前启动，
服务端使用本地配置或环境变量中的 `database.driver` 与 DSN。若没有部署平台提供的持久安装状态，
业务 API 会按设计返回 423；完整限制和验收步骤见手工验收文档的 Docker 阶段。

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
