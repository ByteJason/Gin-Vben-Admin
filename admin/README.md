# 管理端前端（admin）

当前接口版本：`0.2.0-dev`

本目录是独立的 pnpm workspace。全新 clone 提供三套 UI 模板和一套安装页面：

- `apps/web-antd`：Ant Design Vue
- `apps/web-ele`：Element Plus
- `apps/web-naive`：Naive UI
- `apps/install`：首次安装页面（不单独维护 lockfile）

前端通过 `/api` 访问根目录的 Gin 服务 `server/`。

## 初始化与验证

```text
pnpm install --frozen-lockfile
pnpm run test:smoke
pnpm run init
```

`pnpm run init` 必须在本目录执行。它会交互选择 `antd`、`ele` 或 `naive`，先以 `INIT_PREFLIGHT=ok` 展示只读权限预检和保留/暂存清单；确认后保留所选应用，将另外两套移动到仓库根目录 `.runtime/init-backup/<transaction>/`，并写入可提交的 `.ui-profile.json`。安装页面只读显示该选择；网页安装完成后，按提示结束 init 进程并重启 Go 服务。

状态和恢复命令：

```text
pnpm run init -- --check
pnpm run init -- --reset --confirm-reset  # 仅网页安装完成前
```

安装完成前，公开的 `dev`、`build`、`preview` 会要求先运行 init；完成后它们从 `.ui-profile.json` 自动分发到唯一保留的应用：

```text
pnpm run dev
pnpm run build
pnpm run preview
```

`.ui-profile.json` 随 Git 提交，使团队和 CI 使用同一套 UI。`.ui-init-receipt.json`、`.ui-init-runtime.json`、`apps/install/.installed` 和 `.runtime/init-backup/` 都是本机状态，不提交。Docker 在已有 profile 时自动使用它；全新 CI 可显式传入 `ADMIN_UI=antd|ele|naive`，显式值与 profile 不一致时输出 `UI_PROFILE_MISMATCH`。

首次运行时，将所选模板的 `.env.development.example` 复制为 `.env.development`，并按需调整端口。开发环境默认 API 地址为 `/api`，由 Vite 代理转发到 Gin 的 `/api` 根路径（`http://localhost:8080/api`）。`build:antd`、`build:ele`、`build:naive` 仅保留为 CI/内部显式构建入口，普通用户使用上面的 profile 驱动命令。
