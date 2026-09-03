# admin 版本迁移

放置后台 IAM、设置、字典、文件、邮件、任务和导入导出能力的真实升级版本。
版本文件只执行显式 GORM Migrator 操作，并在文档中注明租户/组织作用域。

`v002_common_capabilities.go` 是公共邮件、验证码和媒体能力的显式升级入口：
它创建新增表，为已有 `file_objects`、`smtp_accounts`、`email_messages` 表补充
字段和索引，并保持重复执行幂等。`metadata_json` 会先以可空列加入、回填 `{}`
（包含软删除行）后再收紧为 `NOT NULL`，避免旧数据在严格 PostgreSQL/MySQL
配置下升级失败。该版本不在服务启动时隐式执行，需由经过审核的迁移命令调用
`Up`/`Down`；`Down` 只应在确认数据库由该版本从 v001 形态升级后执行，
fresh schema 请保留最终列形态并通过备份/恢复回滚。

`Up`/`Down` 开始时会读取数据库表目录并校验 v001 的三个基线表均已存在；缺少任一表会
立即返回带表名的前置条件错误，不会只创建新增表后误报升级成功，也不会在未知基线
上执行回滚。MySQL 的 DDL
由引擎自动提交，跨多个 `ALTER TABLE` 不具备 PostgreSQL 那样的原子回滚；若中途
失败，修复缺失前置条件或冲突后可安全重试，所有已执行步骤均由存在性检查保护；索引目录
读取错误会显式中止，不会把未知状态当作“索引不存在”。

本版本保留 `smtp_accounts` 原有 `uq_smtp_accounts_tenant_name` 与
`uq_smtp_accounts_tenant_endpoint` 的 tenant-wide 唯一范围；新增 `scope_type` 只用于
路由和查询索引，不在升级窗口内重写唯一键，避免旧数据发生隐式冲突。若后续需要把
账号唯一性收紧到组织作用域，应另行提供冲突报告、数据清理和独立版本迁移。

`media_categories.path` 保留 1024 字符容量但不建立 MySQL 全值索引；按 utf8mb4
计算会超过 InnoDB 3072 字节键上限。树查询使用 scope/parent 索引，路径检索如需
索引应按数据库方言另行采用前缀或摘要列。
