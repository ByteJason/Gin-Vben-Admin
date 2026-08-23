# Gin-Vben-Admin

基于 Go、Gin、Vue 3 与 Vue Vben Admin 的企业级后台管理基础平台。

当前版本：`0.9.0-rc`

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
│   ├── cmd/init/                      # 仅 pnpm run init 使用的临时服务
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

```text
pnpm --dir admin install --frozen-lockfile
go -C server mod download
```

首次启动必须在 `admin/` 内选择并初始化一套 UI：

```text
cd admin
pnpm run init
```

命令会让你选择 Ant Design Vue、Element Plus 或 Naive UI，先只读预检目录权限并展示保留/暂存清单；确认后保留所选应用，将另外两套暂存到根目录 `.runtime/init-backup/`，写入可提交的 `.ui-profile.json`，构建并启动仅监听 `127.0.0.1` 的临时安装服务。安装页只读显示命令行选择，不再重复选择 UI。完成网页安装后，按页面提示在终端按 `Ctrl+C` 结束 init。

如果上次运行在选择 UI 前意外中断，仅留下本机 receipt 或 runtime 记录，而三套模板、profile 和安装 marker 仍是首次启动布局，再次执行 `pnpm run init` 会自动恢复：旧记录先可逆备份到 `.runtime/init-recovery/<id>/`，随后在同一次命令中继续选择 UI，不要求用户检查隐藏文件。输出中的 `INIT_REASON` 说明状态原因，`INIT_ACTION` 给出下一动作；证据不足时保持现场并只显示可提交给维护者的状态代码。

可先用 `pnpm run init -- --check` 只读检查状态；该命令不会触发自动恢复。网页安装完成前需要重新选择时，使用 `pnpm run init -- --reset --confirm-reset` 恢复三套模板。初始化前直接运行 `pnpm run dev`、`pnpm run build` 或 `pnpm run preview` 会提示先执行 init。

安装完成后，分别在两个终端启动服务端和已选管理端：

```text
# 仓库根目录，终端 1
go -C server run ./cmd/api

# admin/，终端 2
pnpm run dev
```

编辑本地 `server/configs/server.yaml` 或设置环境变量后，使用显式命令管理数据库迁移：

```text
go -C server run ./cmd/migrate status
go -C server run ./cmd/migrate up
go -C server run ./cmd/migrate down --steps 1
```

配置支持 MySQL/PostgreSQL 的 `single`、`read_write`、`cluster_endpoint` 模式，以及 Redis 的 `single`、`sentinel`、`cluster` 模式。默认示例关闭外部依赖；启用时请通过本地配置或环境变量填写连接信息。

启用管理端认证时，在本地配置中填写至少 32 字节的 `auth.jwt_secret`（或设置 `AUTH_JWT_SECRET`），并同时启用数据库与 Redis。登录、刷新和登出接口见 `contracts/openapi/admin-v1.yaml`；refresh token 只保存在 HttpOnly Cookie。

所选 UI 的稳定信息位于 `admin/.ui-profile.json`；该文件随 Git 协作，安装 receipt、运行 receipt、marker 和备份目录只保留在本机。

### 源码部署

管理端构建：

```text
pnpm --dir admin install --frozen-lockfile
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
