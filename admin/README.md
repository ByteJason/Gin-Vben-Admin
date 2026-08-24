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
pnpm run init
```

先在另一个终端进入 `server/` 并运行 `go run ./cmd/api/main.go`。`pnpm run init` 必须在本目录执行，但不需要预先运行 `pnpm install`：init 只使用 Node.js 内置模块。它先通过 `127.0.0.1` 检查普通 API 的 health、安装状态和 `/install` 页面，再交互选择 `antd`、`ele` 或 `naive`；确认后原子保留所选应用，将另外两套移动到仓库根目录 `.runtime/install/ui-backup/<transaction>/`，写入 `.ui-profile.json`，并对缩减后的 workspace 自动执行 `pnpm install --frozen-lockfile`。安装路由只接受真实 loopback 来源和 loopback Host，不信任代理头；请始终使用 init 输出的本机 URL。

UI 移动、依赖安装或 UI 重置中断后，再次运行对应的 init 命令会从最小未完成步骤继续，不会重新选择 UI。网页安装事务也会在服务重启后恢复到可重试状态，且不会把密码或 DSN 写入事务文件。

旧版 Windows 若在 `INSTALLER_BUILD_FAILED` 后留下严格一致的 profile、旧 receipt 和 `.runtime/init-backup/<transaction>/`，新版 init 会把备份迁入当前布局、隔离旧 receipt，并只重试所选 UI 的依赖。迁移中断可直接重跑，输出 `INIT_LEGACY_MIGRATION=resumed|completed`；也可直接运行 `pnpm run init -- --reset --confirm-reset` 恢复三套 UI，无需先安装依赖。任何冲突、符号链接、额外条目或 receipt/backup 不匹配都会以 `LEGACY_PREPARED_STATE_INVALID` 保持现场，不会运行 pnpm。

状态和恢复命令：

```text
pnpm run init -- --check                 # 严格只读，显示 INIT_REASON/INIT_ACTION
pnpm run init -- --reset --confirm-reset  # 仅网页安装完成前
```

安装完成前，公开的 `dev`、`build`、`preview` 会要求先运行 init；完成后它们从 `.ui-profile.json` 自动分发到唯一保留的应用：

```text
pnpm run dev
pnpm run build
pnpm run preview
```

安装成功后先重启普通 Go API，再依次运行 `pnpm run build` 和 `pnpm run dev`。

以下状态由程序维护，**不要手动删除、改名或编辑**：

| 路径 | 作用 |
|---|---|
| `admin/.ui-profile.json` | 记录唯一选择的 UI，并驱动通用命令分发 |
| `.runtime/install/` | 事务根目录；内部短期租约、清理墓碑、`environment-backup/` 等均由程序维护，请勿删除或编辑 |
| `.runtime/install/.installed` | 最后原子写入的安装完成标记和 build 门禁 |
| `.runtime/install/admin-init.lock` | schema 2 init 进程租约；绑定 PID 与启动身份，真实 owner 即使心跳暂停也不会按 TTL 误回收，崩溃或 PID 复用后安全恢复 |
| `.runtime/install/admin-init.lock.reclaim` | init 回收失效租约时的原子墓碑；回收中断后由下一次 init 自动处理 |
| `.runtime/install/admin-init-heartbeat/` | 绑定进程启动身份的一主一 owner 双 UUID 心跳；长时间安装时保持活跃，单通道异常或系统暂停不会误解锁 |
| `.runtime/install/apply.lock` | 服务端网页安装/回滚全程持有的进程租约；与 `admin-init.lock` 双向互斥，服务端重启时在同一 guard 下安全回收崩溃残留，请勿手动删除 |
| `.runtime/install/dependency-install.lock` | 后台依赖安装监督进程的持久租约；绑定监督进程与 pnpm Worker，防止前台 init 中断后重复安装 |
| `.runtime/install/dependency-install.lock.reclaim` | 依赖安装租约的原子回收墓碑；确认监督进程及其后代结束后才会清理 |
| `.runtime/install/dependency-install-heartbeat/` | 后台依赖安装监督进程按 UUID 隔离的心跳 |
| `.runtime/install/dependency-install.log` | `pnpm install` 与 lifecycle 的本地诊断日志；init 会打印路径，失败时可查看但不要删除或编辑 |
| `.runtime/install/dependency-job-gate-<UUID>.json` | Windows kill-on-close Job Object 完成绑定后发布的短期门闩；正常结束时自动清理，强制终止或断电残片按 UUID 隔离且不阻塞重跑，请勿手动删除 |
| `.runtime/install/process.guard` | 服务端安装锁共用的持久跨进程保护文件；空闲时也不要删除 |
| `.runtime/install/.installed.lock` | 并发安装互斥锁 |
| `.runtime/install/transaction.json` | 不含敏感字段的 UI 选择、重置与网页安装恢复记录 |
| `.runtime/install/ui-backup/` | 初始化期间暂存的未选 UI |
| `.runtime/install/legacy-prepared-migration.json` | 旧版构建失败状态的可中断迁移 journal；`--check` 只读识别，重跑 init 自动续接 |
| `.runtime/install/legacy-recovery/<transaction>/.ui-init-receipt.json` | 可逆隔离的旧 receipt；只用于迁移来源证据 |
| `.runtime/init-backup/` | 旧版 UI 暂存目录；合法目标由迁移器接管，冲突时保持不变 |
| `.runtime/init-recovery/` | 旧版历史恢复记录；迁移过程中原样保留 |

遇到中断时重新运行 init 或重新打开安装页，让程序恢复这些状态。Docker 在已有 profile 时自动使用它；全新 CI 可显式传入 `ADMIN_UI=antd|ele|naive`，显式值与 profile 不一致时输出 `UI_PROFILE_MISMATCH`。
若网页安装需要替换已有 `.env`，`environment-backup/` 仅在事务期间以 `0600` 权限保存原文件用于补偿，并在完成标记提交后精确清理；不要复制或提交它。

首次运行时，将所选模板的 `.env.development.example` 复制为 `.env.development`，并按需调整端口。开发环境默认 API 地址为 `/api`，由 Vite 代理转发到 Gin 的 `/api` 根路径（`http://localhost:8080/api`）。普通用户与源码工作区都只使用通用的 profile 驱动命令。
