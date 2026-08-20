# Gin-Vben-Admin

基于 Go、Gin、Vue 3 与 Vue Vben Admin 的企业级后台管理基础平台。

> **状态：B1 首个可验证工程切片已落地（文档基线 `0.3.0`）**
>
> **当前版本：`0.1.0-dev`**
>
> **产品版本：`0.1.0-dev`；API 兼容代际：`/api/{scope}/v1`**

## 当前状态

本仓库已完成 B1 的最小可运行骨架：三款管理端模板已导入，Gin health/request-id/admin/client scope、根契约、跨平台 Node 编排脚本和三平台 CI 门禁均可验证。

| 项目 | 当前结论 |
|---|---|
| 文档基线 | `0.3.0`；外部 `V1.3` 仅作来源标签 |
| 产品版本 | `0.1.0-dev`，不随文档版本联动 |
| 管理端 UI | `admin/`，保留三款初始化模板；B1 smoke 已通过 |
| Gin 服务端 | `server/`，Go 1.24 module、模块化单体；B1 `go test`/`go vet` 已通过 |
| CI 门禁 | `.github/workflows/ci.yml`，Ubuntu/macOS/Windows smoke + admin/server/Compose |
| API 契约 | `/api/admin/v1`、`/api/client/v1`，分别维护 OpenAPI |
| 公开文档 | 根 `docs/` 提供公开文档入口 |

## 当前目录

```text
.
├── admin/                                 # 管理端 Vue/Vben pnpm workspace
│   ├── apps/
│   │   ├── web-antd/
│   │   ├── web-ele/
│   │   └── web-naive/
│   ├── packages/
│   ├── internal/                          # lint/Vite/Tailwind/TS 工具链
│   ├── scripts/
│   ├── tests/contract/
│   ├── package.json
│   ├── pnpm-workspace.yaml
│   ├── pnpm-lock.yaml
│   ├── turbo.json
│   └── Dockerfile
├── server/                                # Gin 服务端唯一代码边界
│   ├── cmd/api/
│   ├── configs/server.example.yaml
│   ├── internal/
│   │   ├── bootstrap/
│   │   ├── config/
│   │   ├── transport/http/{router,middleware,admin,client,open,internal}/
│   ├── tests/evidence/
│   ├── go.mod
│   ├── go.sum
│   └── Dockerfile
├── contracts/
│   ├── openapi/admin-v1.yaml
│   ├── openapi/client-v1.yaml
│   ├── errors/
│   └── schemas/
├── deploy/{compose.dev.yaml,compose.dependencies.yaml}
├── scripts/{bootstrap.mjs,dev.mjs,verify.mjs,generate-openapi.mjs}
├── tests/contract/
├── docs/
├── .github/
├── README.md
└── .gitignore
```

## 管理端模板

管理端模板仓库保留：

- `admin/apps/web-antd`
- `admin/apps/web-ele`
- `admin/apps/web-naive`

三套模板均可直接运行；开发时选择对应的 `dev:*`、`build:*` 命令。

## 环境基线

| 组件 | 基线 |
|---|---|
| Git | 当前受支持稳定版 |
| Go | `>= 1.24` |
| Node.js | `^22.18.0 || ^24.12.0` |
| pnpm | 固定 `11.16.0` |
| MySQL | `>= 8`，Tier-1 |
| PostgreSQL | Tier-1 |
| Redis | `>= 6` |
| Docker | Docker Engine + Compose v2；统一使用 `docker compose` |

## 安装与启动（Windows / macOS / Linux）

### 1. 平台准备

| 平台 | 终端 | 容器环境 | 说明 |
|---|---|---|---|
| Windows 11 | PowerShell 7 | Docker Desktop（WSL 2） | 核心入口由 Node `.mjs` 提供，不要求 Git Bash |
| macOS | Terminal + zsh | Docker Desktop | Intel 与 Apple silicon 使用对应安装包 |
| Linux | bash | Docker Engine + Compose plugin，或 Docker Desktop | 当前用户须能执行 `docker compose` |

三平台安装 Git、Go、Node.js、npm 和 Docker 后，使用相同检查（B1 验证过 Node 脚本与 Go seam；容器完整构建需本机 Docker/网络）：

```text
git --version
go version
node --version
npm --version
docker --version
docker compose version
```

安装固定 pnpm：

```text
corepack enable
corepack prepare pnpm@11.16.0 --activate
pnpm --version
```

若 Node.js 发行版未附带 Corepack，可改用同等跨平台命令：`npm install --global pnpm@11.16.0`。

### 2. 获取项目

将 `REPOSITORY_URL` 替换为最终仓库地址；PowerShell 7、zsh、bash 使用同一组命令：

```text
git clone REPOSITORY_URL Gin-Vben-Admin
cd Gin-Vben-Admin
```

### 3. 推荐：Docker 开发环境

跨平台 Node `.mjs` 编排器使用 `fs/path`、`path.join` 和 `spawn(..., { shell: false })`，不把 `cp`、`rm`、`sed`、`awk` 或 Bash 作为核心前提。

```text
node ./scripts/bootstrap.mjs --ui antd --database mysql --skip-install
pnpm --dir admin install --frozen-lockfile
go -C server mod download
docker compose -f deploy/compose.dev.yaml up -d --build
node ./scripts/verify.mjs
docker compose -f deploy/compose.dev.yaml ps
```

查看日志和停止服务：

```text
docker compose -f deploy/compose.dev.yaml logs -f
docker compose -f deploy/compose.dev.yaml down
```

`down` 保留命名卷；确认删除本地数据库/Redis 数据后才执行 `down -v`。默认端口以 B1 实测的 Compose 配置为准。

### 4. 原生开发：管理端与服务端分别运行

先启动数据库和 Redis：

```text
docker compose -f deploy/compose.dependencies.yaml up -d
node ./scripts/bootstrap.mjs --ui antd --database mysql
```

终端 A——管理端：

```text
pnpm --dir admin install --frozen-lockfile
pnpm --dir admin run dev:antd
```

终端 B——Gin 服务端（推荐，无需切换目录）：

```text
go -C server mod download
go -C server run ./cmd/api
```

进入服务端目录后也可使用：

```text
cd server
go mod download
go run ./cmd/api
```

数据库迁移命令尚未提供；当前服务端可直接启动健康检查与 scope ping seam。

终端 C——全量验证：

```text
node ./scripts/verify.mjs
```

也可用 Node 编排器同时启动两端：

```text
node ./scripts/dev.mjs --ui antd
```

`bootstrap.mjs` 检查目标树并从 `server/configs/server.example.yaml` 生成本地 `server/configs/server.yaml`；默认不覆盖已有本地配置。安装依赖与下载 Go module 作为显式步骤执行，便于 Windows PowerShell、macOS zsh、Linux bash 看到失败位置。密钥、连接串、日志、数据库卷、会话和初始化备份均进入 `.gitignore`。

### 5. 常见问题

- PowerShell 找不到命令：重新打开终端并检查 `PATH`；流程不要求 WSL shell。
- `docker compose` 不可用：安装 Compose v2，不把旧 `docker-compose` 作为主命令。
- 端口占用：调整本地配置并重新运行 `node ./scripts/verify.mjs`。
- 行尾差异：由 `.gitattributes` 统一；Node 脚本使用跨平台路径 API。

## API 契约与健康检查

管理端首批接口：

```text
POST /api/admin/v1/auth/login
POST /api/admin/v1/auth/refresh
POST /api/admin/v1/auth/logout
GET  /api/admin/v1/auth/codes
GET  /api/admin/v1/user/info
GET  /api/admin/v1/menu/all
```

客户端接口统一从 `/api/client/v1` 起步；具体业务资源在客户端垂直切片确定后加入 `contracts/openapi/client-v1.yaml`。健康检查：

```text
GET /health/live
GET /health/ready
```

所有响应和日志贯通 request ID/trace ID；写操作按资源风险支持 `Idempotency-Key`。

## 验证

```text
node --test tests/contract/b1_contract.test.mjs
pnpm --dir admin run test:b1
go -C server test ./...
node ./scripts/verify.mjs --scope skeleton
```

持续集成入口：`.github/workflows/ci.yml`。

## 许可证

本项目采用 MIT License。前端基于 [Vue Vben Admin](https://github.com/vbenjs/vue-vben-admin)；发布包按许可证要求携带项目与上游归属文件。
