# 管理端前端（admin）

本目录是独立的 pnpm workspace，保留三套 UI 模板：

- `apps/web-antd`：Ant Design Vue
- `apps/web-ele`：Element Plus
- `apps/web-naive`：Naive UI

`web-antdv-next`、`web-tdesign` 和上游 mock 服务已从工作区裁剪。前端通过 `/api` 访问根目录的 Gin 服务 `server/`，不在此目录启动模拟服务。

## 初始化与验证

```bash
pnpm install --frozen-lockfile
pnpm run test:b1
pnpm run dev:antd   # 或 dev:ele / dev:naive
```

首次运行时，将对应模板的 `.env.development.example` 复制为 `.env.development`，并按需调整端口。开发环境默认 API 地址为 `/api`，由 Vite 代理转发到 Gin 的 `/api` 根路径（`http://localhost:8080/api`）。

构建单个模板：

```bash
pnpm run build:antd
pnpm run build:ele
pnpm run build:naive
```
