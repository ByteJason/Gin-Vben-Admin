# GORM 版本升级目录

`../schema.go` 是当前 `v001` 全新安装基线，负责一次性注册并创建全部初始
Model。基线发布后保持不变；后续数据库升级按模块放入以下目录：

```text
shared/v002_*.go
admin/v002_*.go
client/v002_*.go
```

每个版本文件只执行明确的 GORM `Migrator` 操作或数据回填，例如
`AddColumn`、`AlterColumn`、`CreateIndex` 和事务内的 `Update`。版本入口由迁移
Runner 和对应的显式升级命令调用；应用启动阶段只读取状态，不会隐式执行升级。当前
后台数据清理版本 `admin/v003_settings_mail_cleanup.go` 使用
`go run ./cmd/migrate settings-mail-cleanup --config <写库配置>` 显式触发（`v003`
等别名等价），不会被普通 `migrate up` 误执行。

当前目录暂不放空的伪版本实现。出现第一个真实升级需求时，再新增对应模块的
`v002_*.go`，并同时补充 `Up`、验证和可逆 `Down`（若该版本具备安全回滚条件）。

迁移版本记录约定使用 `app_metadata` 中的 `schema:*` 键，记录模块版本、摘要和
dirty 状态；升级事务需锁定对应元数据行。
