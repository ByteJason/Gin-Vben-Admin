# Gin-Vben-Admin

基于 Go、Gin、Vue 3 与 Vue Vben Admin 的企业级后台管理基础平台。

> **状态：B1 首个可验证工程切片已落地（规划基线/文档基线 `0.3.0`）**
>
> **当前批次：B1 工程骨架与质量门禁；B2 数据基础设施待开始**
>
> **产品版本：`0.1.0-dev`；API 兼容代际：`/api/{scope}/v1`**

## 当前状态

本仓库已完成 B1 的最小可运行骨架：三款管理端模板已裁剪导入，Gin health/request-id/admin/client scope 已实现，根契约、跨平台 Node 编排脚本和三平台 CI 门禁已加入。完整业务登录、RBAC、数据库迁移和公开法律包仍按后续批次推进；下面的命令区分“当前已验证”与“后续批次接口”。

| 项目 | 当前结论 |
|---|---|
| 文档基线 | `0.3.0`；外部 `V1.3` 仅作来源标签 |
| 产品版本 | `0.1.0-dev`，不随文档版本联动 |
| 管理端 UI | `admin/`，保留三款初始化模板；B1 smoke 已通过 |
| Gin 服务端 | `server/`，Go 1.24 module、模块化单体；B1 `go test`/`go vet` 已通过 |
| CI 门禁 | `.github/workflows/ci.yml`，Ubuntu/macOS/Windows smoke + admin/server/Compose |
| API 契约 | `/api/admin/v1`、`/api/client/v1`，分别维护 OpenAPI |
| 公开文档 | 根 `docs/` 仅保留入口；专门开发资料位于不提交的 `.dev-docs/` |

## 目标目录（B1 已落地项 + 后续预留项）

```text
.
├── admin/                                 # 管理端 Vue/Vben pnpm monorepo
│   ├── apps/
│   │   ├── web-antd/
│   │   ├── web-ele/
│   │   └── web-naive/
│   ├── packages/
│   │   └── api-client/                    # B2+ 生成目标；B1 仍以根 contracts 为源
│   ├── internal/                          # lint/Vite/Tailwind/TS 工具链
│   ├── scripts/init-project/              # B5 规划；B1 不创建
│   ├── tests/contract/
│   ├── package.json
│   ├── pnpm-workspace.yaml
│   ├── pnpm-lock.yaml
│   ├── turbo.json
│   └── Dockerfile
├── server/                                # Gin 服务端唯一代码边界
│   ├── cmd/{api,migrate,setup,worker}/    # B1 仅落地 api，其余按批次加入
│   ├── configs/server.example.yaml
│   ├── internal/
│   │   ├── bootstrap/
│   │   ├── config/
│   │   ├── transport/http/{router,middleware,admin,client,open,internal}/
│   │   ├── platform/{persistence,cache,security,observability}/ # B2+
│   │   ├── generated/{adminv1,clientv1}/                        # 契约生成
│   │   └── modules/                                               # B3+
│   │       ├── identity/{domain,application/{admin,client},transport/http/{admin,client},adapter}
│   │       ├── iam/
│   │       ├── settings/
│   │       ├── audit/
│   │       ├── admin/                   # 管理端专属能力
│   │       └── client/                  # 客户端专属能力
│   ├── migrations/{mysql,postgres}/       # B2+
│   ├── tests/{contract,integration}/
│   ├── go.mod
│   ├── go.sum
│   └── Dockerfile
├── contracts/
│   ├── openapi/admin-v1.yaml
│   ├── openapi/client-v1.yaml
│   ├── errors/
│   └── schemas/
├── deploy/{compose.dev.yaml,compose.dependencies.yaml,nginx,k8s}/
├── scripts/{bootstrap.mjs,dev.mjs,verify.mjs,generate-openapi.mjs,sync-upstream.mjs}
├── tests/e2e/
├── docs/
├── .github/
├── README.md
└── .gitignore
```

### B1 当前实际已提交树

```text
admin/{apps/web-antd,apps/web-ele,apps/web-naive,packages,internal,scripts,tests,package.json,pnpm-lock.yaml}
server/{cmd/api,configs,internal/{bootstrap,config,transport/http},tests,go.mod,go.sum,Dockerfile}
contracts/{openapi,errors,schemas}   deploy/   scripts/   tests/   docs/
```

`admin/apps/web`、`admin/packages/api-client`、迁移/生成/业务模块目录属于后续批次目标，不代表 B1 已生成文件。

根目录不放 `apps/`、`packages/`、`internal/`、`go.mod` 或 pnpm workspace 文件。共享客户端的确定路径为 `admin/packages/api-client`。服务端装配入口为 `server/internal/bootstrap`，HTTP scope 入口为 `server/internal/transport/http/admin` 与 `server/internal/transport/http/client`。人工契约源为 `contracts/openapi/admin-v1.yaml` 与 `contracts/openapi/client-v1.yaml`。`backend/` 不作为管理端目录：在本项目中 `server/` 专指 Gin，`admin/` 专指管理端。

## 为什么管理端使用 `admin/` 而不是 `backend/`

`backend` 在 Go、Docker、CI 和多数团队约定中表示服务端。若把管理 UI 放在 `backend/`，会造成“后台界面”和“后端服务”同名，脚本、镜像、日志和新人排障都容易混淆。采用 `admin/` 与 `server/` 两个明确边界，目录名直接表达运行责任。

## 服务端模块规则

采用“共享领域 + 端点隔离”的模块化单体：

- `identity`、账户、审计等共享领域只实现一次；
- `/api/admin/v1` 与 `/api/client/v1` 使用独立 router、handler、application use case、权限策略和 OpenAPI；
- 仅管理端的能力放 `server/internal/modules/admin/`；仅客户端的能力放 `server/internal/modules/client/`；
- 不创建 `server/model/admin` 或 `server/model/client` 这种全局模型目录；实体放模块 `domain/`，HTTP DTO 放模块 `transport/http/`，GORM record 放模块 `adapter/persistence/gorm/`；
- 首版一个 `server/cmd/api` 进程同时挂载多个 scope，未来需要独立扩缩容时再增加 API 进程。

## UI 模板模型

管理端模板仓库保留：

- `admin/apps/web-antd`
- `admin/apps/web-ele`
- `admin/apps/web-naive`

初始化时三选一并统一生成 `admin/apps/web`。未选模板和初始化器只有在 build、typecheck、API smoke、manifest 与 rollback 均验证后才清理。

## 环境基线

| 组件 | 基线 |
|---|---|
| Git | 当前受支持稳定版 |
| Go | `>= 1.24` |
| Node.js | `^22.18.0 || ^24.12.0` |
| pnpm | 固定 `11.16.0` |
| MySQL | `>= 8`，Tier-1 |
| PostgreSQL | Tier-1；版本由 B2 兼容矩阵锁定 |
| Redis | `>= 6`；首版 single |
| Docker | Docker Engine + Compose v2；统一使用 `docker compose` |

## 安装与启动（Windows / macOS / Linux）

### 1. 平台准备

| 平台 | 终端 | 容器环境 | 说明 |
|---|---|---|---|
| Windows 11 | PowerShell 7 | Docker Desktop（WSL 2 backend） | 核心入口由 Node `.mjs` 提供，不要求 Git Bash |
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
corepack prepare pnpm@11.16.0 --activate
pnpm --version
```

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

B1 尚未加入迁移命令；数据库迁移与回滚入口随 B2 数据基础设施批次落地。当前服务端可直接启动健康检查与 scope ping seam。

终端 C——全量验证：

```text
node ./scripts/verify.mjs
```

`bootstrap.mjs` 检查目标树并从 `server/configs/server.example.yaml` 生成本地 `server/configs/server.yaml`；默认不覆盖已有本地配置。安装依赖与下载 Go module 作为显式步骤执行，便于 Windows PowerShell、macOS zsh、Linux bash 看到失败位置。密钥、连接串、日志、数据库卷、会话和初始化备份均进入 `.gitignore`。

### 5. 模板初始化（B5，当前未执行）

B1 保留 `admin/apps/web-antd`、`admin/apps/web-ele`、`admin/apps/web-naive` 三个模板，尚未提供会删除模板的初始化器。模板选择、原地生成 `admin/apps/web`、manifest 与 rollback 会在 B5 以独立小步提交实现并验证；当前使用其中任一模板进行开发，不运行不存在的 `init:project` 命令。

### 6. 常见问题

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

## TDD 与质量门禁

- 每个垂直切片遵循 Red → Green → Refactor；
- Go：单元、集成、迁移、契约和 race 测试；
- 三款管理 UI：build、typecheck、OpenAPI 类型一致性和 API smoke；
- `admin/apps/web`：Playwright E2E、axe、键盘和 375/768/1024/1440 视觉回归；
- 核心单元测试覆盖率目标 `>= 80%`；WCAG 2.2 AA；
- `.github/workflows/ci.yml` 包含 `windows-latest`、`macos-latest`、`ubuntu-latest` smoke，并在 Ubuntu 执行 admin/server/Compose 门禁。

## 开发批次

| 批次 | 周期 | 目标 |
|---|---:|---|
| B0 | W0–W1 | 需求、架构、版本、许可证与契约基线 |
| B1 | W1–W2 | admin/server 工程骨架、跨平台脚本、健康检查、CI、Compose |
| B2 | W2–W4 | 配置、MySQL/PostgreSQL、Redis |
| B3 | W4–W6 | 共享身份、管理端认证与登录安全 |
| B4 | W6–W8 | 管理端 RBAC、菜单与管理 API |
| B5 | W3–W8 | 三款管理 UI、共享 client 与初始化器 |
| B6 | W8–W10 | 设置、i18n、审计与可观测 |
| B7 | W10–W13 | 文件、消息与任务 |
| B8 | W13–W15 | 多租户、多库和灾备硬化 |
| B9 | W15–W16 | 发布、性能、安全与运营收口 |

## 文档、Git 与许可证

- `.dev-docs/` 是专门开发资料，根 `.gitignore` 排除；未来公开说明统一进入 `docs/`；
- `admin/`、`server/` 源码、测试、迁移、示例配置和锁文件入 Git；运行态、secret、报告、备份不入 Git；
- Gin-Vben-Admin 采用 MIT License，版权计划为 `Copyright (c) 2026 Gin-Vben-Admin contributors`；
- 前端基于 [Vue Vben Admin](https://github.com/vbenjs/vue-vben-admin)，发布物携带 `LICENSE`、`NOTICE`、`LICENSES/Vue-Vben-Admin-MIT.txt` 和 `THIRD_PARTY_NOTICES.md`。
