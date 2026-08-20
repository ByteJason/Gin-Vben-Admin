# Gin-Vben-Admin

基于 Go、Gin、Vue 3 与 Vue Vben Admin 的企业级后台管理基础平台。

当前版本：`0.1.0-dev`。项目提供管理端 UI 模板、Gin HTTP 服务、OpenAPI 契约和跨平台开发脚本。

## 目录

```text
.
├── admin/                              # 管理端 pnpm workspace
│   ├── apps/
│   │   ├── web-antd/
│   │   ├── web-ele/
│   │   └── web-naive/
│   ├── packages/                       # 共享前端包
│   ├── internal/                       # 前端构建与质量工具
│   ├── scripts/
│   ├── tests/contract/
│   ├── package.json
│   ├── pnpm-workspace.yaml
│   ├── pnpm-lock.yaml
│   ├── turbo.json
│   └── Dockerfile
├── server/                             # Gin HTTP 服务
│   ├── cmd/api/
│   ├── configs/server.example.yaml
│   ├── internal/
│   │   ├── bootstrap/
│   │   ├── config/
│   │   └── transport/http/
│   │       ├── admin/
│   │       ├── client/
│   │       ├── health/
│   │       ├── middleware/
│   │       ├── response/
│   │       └── router/
│   ├── tests/evidence/
│   ├── go.mod
│   ├── go.sum
│   └── Dockerfile
├── contracts/                          # OpenAPI、错误码与响应 schema
├── deploy/                              # Compose 配置
├── scripts/                             # 跨平台 Node 脚本
├── tests/contract/                      # 根级契约测试
├── docs/                                # 公开文档
├── .github/
├── README.md
└── .gitignore
```

## 管理端模板

可用模板：

- `admin/apps/web-antd`：Ant Design Vue
- `admin/apps/web-ele`：Element Plus
- `admin/apps/web-naive`：Naive UI

进入 `admin/` 后，可使用对应的 `dev:*`、`build:*` 和 `typecheck:*` 命令。

## 环境要求

| 组件 | 版本 |
|---|---|
| Git | 稳定版 |
| Go | `>= 1.24` |
| Node.js | `^22.18.0 || ^24.12.0` |
| pnpm | `11.16.0` |
| MySQL | `>= 8` |
| PostgreSQL | 受支持版本 |
| Redis | `>= 6` |
| Docker | Docker Engine + Compose v2 |

## 安装与启动（Windows / macOS / Linux）

### 平台准备

| 平台 | 推荐终端 | 容器运行时 |
|---|---|---|
| Windows 11 | PowerShell 7 | Docker Desktop（WSL 2） |
| macOS | Terminal / zsh | Docker Desktop |
| Linux | bash | Docker Engine + Compose plugin |

安装 Git、Go、Node.js、npm 和 Docker 后检查：

```text
git --version
go version
node --version
npm --version
docker --version
docker compose version
```

启用固定版本的 pnpm：

```text
corepack enable
corepack prepare pnpm@11.16.0 --activate
pnpm --version
```

没有 Corepack 时使用：

```text
npm install --global pnpm@11.16.0
```

### 获取项目

```text
git clone REPOSITORY_URL Gin-Vben-Admin
cd Gin-Vben-Admin
```

### Docker 启动

```text
node ./scripts/bootstrap.mjs --ui antd --database mysql --skip-install
pnpm --dir admin install --frozen-lockfile
go -C server mod download
docker compose -f deploy/compose.dev.yaml up -d --build
node ./scripts/verify.mjs
docker compose -f deploy/compose.dev.yaml ps
```

查看日志或停止服务：

```text
docker compose -f deploy/compose.dev.yaml logs -f
docker compose -f deploy/compose.dev.yaml down
```

### 原生启动

先启动数据库与 Redis：

```text
docker compose -f deploy/compose.dependencies.yaml up -d
node ./scripts/bootstrap.mjs --ui antd --database mysql
```

管理端：

```text
pnpm --dir admin install --frozen-lockfile
pnpm --dir admin run dev:antd
```

Gin 服务：

```text
go -C server mod download
go -C server run ./cmd/api
```

也可以进入服务目录执行：

```text
cd server
go mod download
go run ./cmd/api
```

同时启动管理端与 Gin 服务：

```text
node ./scripts/dev.mjs --ui antd
```

本地配置样例为 `server/configs/server.example.yaml`；脚本会生成 `server/configs/server.yaml`，不会覆盖已有文件。密钥、连接串、日志和运行数据使用本地配置或环境变量，不提交到 Git。

## API 与健康检查

管理端 API：

```text
POST /api/admin/v1/auth/login
POST /api/admin/v1/auth/refresh
POST /api/admin/v1/auth/logout
GET  /api/admin/v1/auth/codes
GET  /api/admin/v1/user/info
GET  /api/admin/v1/menu/all
```

健康检查：

```text
GET /health/live
GET /health/ready
```

OpenAPI 文件位于 `contracts/openapi/`；响应包含 request ID/trace ID，写操作可按接口约定使用 `Idempotency-Key`。

## 验证

```text
node --test tests/contract/b1_contract.test.mjs
pnpm --dir admin run test:b1
go -C server test ./...
node ./scripts/verify.mjs --scope skeleton
```

持续集成配置：`.github/workflows/ci.yml`。

## 许可证

本项目采用 MIT License。前端基于 [Vue Vben Admin](https://github.com/vbenjs/vue-vben-admin)；发布包按许可证要求携带项目与上游归属文件。
