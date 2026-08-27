# 生产 SQL allowlist

业务持久化统一使用 `gorm.G[T]`。`Raw`、`Exec`、`Table`、`Joins` 和
`Model` 是受控逃生口，仅可用于固定 projection、迁移备注或驱动探测；禁止把
运行时标识符拼接进 SQL，值必须作为绑定参数传入。每个例外都在
[`sql-allowlist.json`](./sql-allowlist.json) 中登记稳定 ID、精确路径/行模式、原因
和验证命令。规则应尽量只覆盖一行和一个用途，业务查询迁移完成后立即删除对应规则。

## 检查命令

在仓库根目录运行：

```bash
python3 scripts/check-sql-allowlist.py
python3 scripts/sql-allowlist.test.py
```

扫描器遍历 `server/internal/platform` 和 `server/migrations` 的非测试 Go 文件，
发现未登记调用即返回状态码 `1`。配置错误返回 `2`；通过时输出规则数量和
`production source clean`。CI 应在 Go 测试前执行该检查。

当前例外包括：

| ID | 用途 |
| --- | --- |
| `MIGRATION_POSTGRES_TABLE_COMMENT` | GORM 建表后写入 PostgreSQL 表备注 |
| `TASKS_RUN_LOG_JOIN_PROJECTION` | 任务日志按父 run 做租户/组织过滤的固定列投影 |
| `IMPORT_ERRORS_JOB_JOIN_PROJECTION` | 导入错误按父 job 做租户/组织过滤的固定列投影 |
| `IAM_*_COMPAT` | 保留现有 sqlmock 契约的查询构造器适配器，正式路径仍返回泛型 projection |

所有 projection 都使用固定列清单和绑定参数；新增例外前应先补充泛型 API 或
专用 projection row，并在对应模块测试中覆盖租户、组织和空结果边界。

## 关系、外键与模块边界

Model 只保存关系所需的 `*_id` 标量字段。迁移必须保持
`DisableForeignKeyConstraintWhenMigrating: true`，不生成外键约束或外键索引；即使
存在 `has one`、`has many` 或 `belongs to` 语义，也通过应用事务、租户校验和显式
projection JOIN 实现。该规则适用于基线建表和后续版本迁移，违反时 scanner 之外
还需在 schema review 中拦截。

后台模块（`admin`）包含 IAM、设置、字典、文件、邮件、任务和导入导出；共享模块
（`shared`）维护租户、组织、用户及安装元数据；认证/审计由 `auth`/`audit` 维护。
用户端（`client`）暂时没有持久化表，新增用户端模型应放入独立版本目录并复用共享
身份表，禁止复制后台用户表或跨模块建立数据库外键。
