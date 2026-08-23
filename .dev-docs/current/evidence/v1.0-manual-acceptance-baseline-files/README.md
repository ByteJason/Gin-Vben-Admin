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
│   ├── packages/
│   ├── internal/
│   ├── scripts/
│   ├── tests/contract/
│   ├── package.json
│   ├── pnpm-workspace.yaml
│   ├── pnpm-lock.yaml
│   └── Dockerfile
├── server/                             # Gin HTTP 服务
│   ├── cmd/api/
│   ├── cmd/migrate/
│   ├── configs/server.example.yaml
│   ├── internal/bootstrap/
│   ├── internal/config/
│   ├── internal/transport/http/
│   ├── go.mod
│   ├── go.sum
│   └── Dockerfile
├── contracts/                          # OpenAPI、错误码、响应 schema
├── deploy/                             # Docker Compose 配置
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
node ./scripts/bootstrap.mjs --ui antd --database mysql --skip-install
pnpm --dir admin install --frozen-lockfile
go -C server mod download
```

启动管理端：

```text
pnpm --dir admin run dev:antd
```

启动服务端：

```text
go -C server run ./cmd/api
```

编辑本地 `server/configs/server.yaml` 或设置环境变量后，使用显式命令管理数据库迁移：

```text
go -C server run ./cmd/migrate status
go -C server run ./cmd/migrate up
go -C server run ./cmd/migrate down --steps 1
```

配置支持 MySQL/PostgreSQL 的 `single`、`read_write`、`cluster_endpoint` 模式，以及 Redis 的 `single`、`sentinel`、`cluster` 模式。默认示例关闭外部依赖；启用时请通过本地配置或环境变量填写连接信息。

启用管理端认证时，在本地配置中填写至少 32 字节的 `auth.jwt_secret`（或设置 `AUTH_JWT_SECRET`），并同时启用数据库与 Redis。登录、刷新和登出接口见 `contracts/openapi/admin-v1.yaml`；refresh token 只保存在 HttpOnly Cookie。

选择其他 UI 模板时，将 `antd` 替换为 `ele` 或 `naive`。

### 源码部署

管理端构建：

```text
pnpm --dir admin install --frozen-lockfile
pnpm --dir admin run build:antd
```

构建结果位于 `admin/apps/web-antd/dist/`，可交给 Nginx 或其他静态文件服务。服务端构建：

```text
go -C server build -o ./bin/server-api ./cmd/api
./server/bin/server-api
```

运行时配置使用 `server/configs/server.example.yaml` 复制出的本地配置或环境变量；实际密钥和连接串不提交到 Git。

### Docker 部署

单机部署（服务端、管理端、单机 MySQL、单机 Redis）：

```text
docker compose -f deploy/docker-compose.yml up -d --build
docker compose -f deploy/docker-compose.yml ps
docker compose -f deploy/docker-compose.yml run --rm migrate status
```

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
