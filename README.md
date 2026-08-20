# Gin-Vben-Admin

基于 Go、Gin、Vue 3 与 Vue Vben Admin 的企业级后台管理基础平台。

当前版本：`0.2.0-dev`

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
- Gin 健康检查、统一响应和 request ID
- OpenAPI、错误码和响应 schema
- MySQL、PostgreSQL、Redis 开发环境编排与显式数据库迁移
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

选择其他 UI 模板时，将 `antd` 替换为 `ele` 或 `naive`。

### 源码部署

管理端构建：

```text
pnpm --dir admin install --frozen-lockfile
pnpm --dir admin run build:antd
```

构建结果位于 `admin/apps/web-antd/dist/`，可交给 Nginx 或其他静态文件服务。服务端构建：

```text
go -C server build ./cmd/api
go -C server run ./cmd/api
```

运行时配置使用 `server/configs/server.example.yaml` 复制出的本地配置或环境变量；实际密钥和连接串不提交到 Git。

### Docker 部署

开发环境（包含服务端、管理端、MySQL、Redis）：

```text
docker compose -f deploy/compose.dev.yaml up -d --build
docker compose -f deploy/compose.dev.yaml run --rm --entrypoint /migrate server up
docker compose -f deploy/compose.dev.yaml ps
```

需要同时启动 PostgreSQL 时增加 `--profile postgres`。

只启动数据库依赖：

```text
docker compose -f deploy/compose.dependencies.yaml up -d
```

停止服务：

```text
docker compose -f deploy/compose.dev.yaml down
```

默认管理端端口为 `5173`，服务端端口为 `8080`；可通过 Compose 环境变量调整。

## 使用说明

- 管理端 API 地址默认使用 `/api`，开发代理指向本地 Gin 服务。
- 管理端连通性检查：`GET /api/admin/v1/ping`。
- 健康检查：`GET /health/live`、`GET /health/ready`。
- 管理端接口契约位于 `contracts/openapi/`。
- 本地配置、日志、数据库卷和构建产物按 `.gitignore` 规则处理。

## 验证

```text
node --test tests/contract/contract.test.mjs
pnpm --dir admin run test:smoke
pnpm --dir admin run check:type
go -C server test ./...
node ./scripts/verify.mjs --scope basic
```

## 贡献指南

贡献流程文档：

- CONTRIBUTING：
- 安全报告：
- 变更记录：

提交代码前请运行相关测试，并保持提交内容聚焦单一改动。

## 许可证

本项目采用 [MIT License](LICENSE)。前端基于 [Vue Vben Admin](https://github.com/vbenjs/vue-vben-admin)；上游许可证文本见 [`LICENSES/Vue-Vben-Admin-MIT.txt`](LICENSES/Vue-Vben-Admin-MIT.txt)，归属信息见 [`NOTICE`](NOTICE)。
