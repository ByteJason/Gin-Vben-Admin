# 管理端前端（admin）

当前接口版本：`1.0.0-dev`

本目录是独立的 pnpm workspace。全新 clone 提供三套 UI 模板和一套安装页面：

- `apps/web-antd`：Ant Design Vue
- `apps/web-ele`：Element Plus
- `apps/web-naive`：Naive UI
- `apps/install`：首次安装页面（不单独维护 lockfile）

前端通过 `/api` 访问根目录的 Gin 服务 `server/`。

初始化脚本要求 Node.js `^22.18.0` 或 `^24.12.0`，以及 pnpm `>=11.0.0`（仓库锁定 `pnpm@11.16.0`）。如果能力卡片把 Node/pnpm 标为不兼容，脚本会在任何事务、锁或模板移动前停止；请先用本机已有的 Corepack 或 pnpm 升级，再重新打开安装页。

生产菜单、默认首页、兼容路由、访问码和页面布局契约统一维护在 [`../docs/admin-information-architecture.md`](../docs/admin-information-architecture.md)，三套 UI 必须保持业务语义等价。

## 初始化与验证

本机首次安装有两条互斥路径；选择其中一条，不要先后重复执行。两条路径都保留三套源码，
管理端开发服务器都只在网页后端安装完成后启动一次。

### A. 浏览器优先（普通用户推荐）

从仓库根目录只启动普通 Go API：

```text
cd server
go run ./cmd/api/main.go
```

打开 [http://127.0.0.1:8080/install](http://127.0.0.1:8080/install)，在网页中选择
Ant Design Vue、Element Plus 或 Naive UI，再完成数据库、Redis、管理员和默认项配置。网页准备任务
会按选择执行冻结 lockfile 的过滤安装，三套 `apps/web-*` 源码始终保留。安装路由只接受真实
loopback 来源和 loopback Host，不信任代理头。

### B. CLI 优先（开发者）

若选择本路径，请跳过 A。先在 `admin/` 预览选择、写入本机 profile，并统一安装当前 UI 依赖：

```text
cd admin
pnpm run ui:select -- ele --check
pnpm run ui:select -- ele
pnpm run ui:install

cd ../server
go run ./cmd/api/main.go
```

再打开 [http://127.0.0.1:8080/install](http://127.0.0.1:8080/install)。网页将 CLI 已选 UI
显示为只读项，只继续后端安装；此时不要另开一个 `pnpm run dev`。`pnpm run ui:install` 会从
`.ui-profile.local.json` 解析选中包，底层执行
`pnpm install --filter <selected-package>... --frozen-lockfile`，并刷新依赖收据。

选择、依赖安装或网页安装中断后，重新打开页面即可继续固定选择；合法的零移动 journal 在 Go
重启后仍显示为 `ui_prepare`。网页安装事务不会把密码或 DSN 写入事务文件。

网页安装失败时，页面会保留当前输入并显示失败步骤、稳定原因标识、数据库代码（若有）、任务 ID；菜单或权限种子与旧数据冲突时还会显示安全的资源类型和资源 ID。服务端终端可按任务 ID 查找 `installation.job.failed`，结构化日志不会输出 SQL、DSN 或凭据。

旧版 Windows 若在 `INSTALLER_BUILD_FAILED` 后留下严格一致的 profile、旧 receipt 和 `.runtime/init-backup/<transaction>/`，网页准备任务会复用现有迁移器接管备份、隔离旧 receipt，并只重试所选 UI 的依赖。任何冲突、符号链接、额外条目或 receipt/backup 不匹配都会保持现场并显示稳定错误标识。

状态和恢复命令：

```text
pnpm run init -- --check                 # 严格只读，显示 INIT_REASON/INIT_ACTION
pnpm run init -- --reset --confirm-reset  # 仅网页安装完成前
```

无参数执行 `pnpm run init` 不再询问 UI，只输出安装页地址；`--check`、`--reset` 保留为维护入口。
完成 UI 选择并安装对应依赖后，公开的 `dev`、`build`、`preview` 会从本机
`.ui-profile.local.json`（旧版则回退到 `.ui-profile.json`）自动分发到当前包。workspace 的
`ui_prepared` 与 `installed` 都允许前端分发；`.runtime/install/.installed` 只控制后端安装状态和
业务 API 门禁，不作为前端依赖/build 门禁：

```text
pnpm run dev
pnpm run build
pnpm run preview
```

安装成功后停止旧服务端，并在两个终端分别运行。管理端只启动一次，本地依赖统一由
`ui:install` 解析当前 profile：

```text
# 仓库根目录，终端 1：服务端
cd server
go run ./cmd/api/main.go

# 仓库根目录，终端 2：管理端
cd admin
# ui:install 内部执行 pnpm install --filter 当前 profile... --frozen-lockfile
pnpm run ui:install
pnpm run dev
```

后续拉取先检查分支状态，再刷新当前 UI 依赖：

```text
git status
git pull --ff-only
cd admin
pnpm run ui:install
```

选择器保留三个 tracked UI 路径，不会因为本机只激活一套而制造 `modify/delete` 冲突；用户自己的
同文件修改或分叉提交仍按普通 Git rebase/merge 流程处理，`--ff-only` 只适用于能够快进的分支。

旧版已经删除/暂存两套 UI 的工作区需一次性收敛：先用 `pnpm run init -- --check` 完成或恢复任何
活动事务，然后从当前 `HEAD` 只恢复两套**未选择**的 tracked 目录，再对原选择执行
`pnpm run ui:select -- <antd|ele|naive>` 和 `pnpm run ui:install`。例如原选择为 `ele`：

```text
git restore --source=HEAD -- admin/apps/web-antd admin/apps/web-naive
cd admin
pnpm run ui:select -- ele
pnpm run ui:install
```

这样保留已选择 UI 的本地适配，同时消除另外两棵树的本机删除；随后回到仓库根目录执行
`git pull --ff-only`。未选目录若也有需要保留的修改，恢复前先备份或提交。

已安装后切换 UI：

```text
cd admin
pnpm run ui:select -- naive --check
pnpm run ui:select -- naive
pnpm run ui:install
pnpm run dev
```

切换只改变活动 profile、派生收据和切换报告；根 `.env` 已存在时只精确更新
`APP_UI_ACTIVE`，其余键保持不变。三套源码与公共业务层不变，报告中的
`sourceAdapter`、`targetAdapter`、`adapterChecks=[route,theme,form,component]` 与
`uiSpecific=revalidate-adapter` 提醒人工复核 adapter。后端 `.installed` 原文件保持，历史目录保存
字节一致副本，因此服务端仍为 `installed`，现有数据库、管理员和业务 API 继续可用，无需重跑网页
后端安装。不要提交 `.ui-profile.local.json`，也不要手动编辑 `.runtime/install/` 中的 journal 和报告。

以下状态由程序维护，**不要手动删除、改名或编辑**：

| 路径 | 作用 |
| --- | --- |
| `admin/.ui-profile.local.json` | 本机 UI 选择，已忽略且优先于旧版 profile |
| `admin/.ui-profile.json` | 旧版兼容 profile，新流程不改写 |
| `.runtime/install/` | 事务根目录；内部短期租约、清理墓碑、`environment-backup/` 等均由程序维护，请勿删除或编辑 |
| `.runtime/install/.installed` | 后端安装完成标记和业务 API 门禁；不作为前端依赖/build 门禁，切换 UI 时保持原文件 |
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
| `.runtime/install/workspace-transaction.json` | 零移动 journal：`switching_ui` 用于原子切换恢复，`dependencies_pending` 用于依赖准备；中断后以同一 UI 重跑，未完成时 dev/build 会被门禁阻止 |
| `.runtime/install/workspace-dependencies.json` | 当前 UI 与 lockfile 摘要；`pnpm run ui:install` 成功后刷新 |
| `.runtime/install/ui-switch-report.json` | 切换前后选择及公共层/adapter 人工复核提示 |
| `.runtime/install/ui-switch-history/` | 已安装切换时归档的后端 marker 字节副本；原 marker 保持不变 |
| `.runtime/install/transaction.json` | 旧版 UI 移动事务兼容记录 |
| `.runtime/install/ui-backup/` | 仅旧版迁移/恢复可能出现 |
| `.runtime/install/legacy-prepared-migration.json` | 旧版构建失败状态的可中断迁移 journal；`--check` 只读识别，重跑 init 自动续接 |
| `.runtime/install/legacy-recovery/<transaction>/.ui-init-receipt.json` | 可逆隔离的旧 receipt；只用于迁移来源证据 |
| `.runtime/init-backup/` | 旧版 UI 暂存目录；合法目标由迁移器接管，冲突时保持不变 |
| `.runtime/init-recovery/` | 旧版历史恢复记录；迁移过程中原样保留 |

遇到中断时重新运行 init 或重新打开安装页，让程序恢复这些状态。CI/Docker 显式传入 `ADMIN_UI=antd|ele|naive`；本机 `.ui-profile.local.json` 被 `.dockerignore` 排除，不会污染镜像选择。旧版 tracked profile 与显式部署值不一致时输出 `UI_PROFILE_MISMATCH`。若网页安装需要替换已有 `.env`，`environment-backup/` 仅在事务期间以 `0600` 权限保存原文件用于补偿，并在完成标记提交后精确清理；不要复制或提交它。

init 会使用 Node.js 内置文件 API，将所选模板的 `.env.development.example` 和 `.env.production.example` 原子复制为对应本地环境文件；已有本地文件保持原字节。新流程只验证本机选择、状态目录和三套模板完整性，零移动 journal 的 `moves` 永远为空。仅在接管旧版 source-moving 现场时，兼容恢复器才会额外验证硬链接、目录同步、重命名、删除和跨目录移动能力，并用可恢复事务续接。安装页会显示稳定的失败阶段、原因、逻辑目录范围、所需操作和任务 ID；只有已经进入依赖安装阶段且日志已成功创建时才显示 `.runtime/install/dependency-install.log`。旧版本完成安装但缺少整个文件时，通用 `dev/build/preview` 分发器会在启动前自动补齐；已有旧文件缺少标题时由共享 Vite 配置提供默认值，不改写其中的自定义地址或敏感字段。开发环境默认 API 地址为 `/api`，即使本地环境文件意外缺失也会回退到同源 `/api`，再由 Vite 代理转发到 Gin 的 `/api` 根路径（`http://localhost:8080/api`）。普通用户与源码工作区都只使用通用的 profile 驱动命令。
