# 数据库建表迁移

服务端的全新安装使用一份 GORM schema 文件：
`server/migrations/schema.go`。迁移入口按配置选择 MySQL 或 PostgreSQL，使用
`global.DB` 对应的主库连接执行 `Migrator().CreateTable`，然后写入必要的系统
初始数据。

表结构的唯一来源是
`server/internal/platform/persistence/model`。Model 按能力前缀分组在同一个
Go package 中，迁移入口只负责按顺序注册它们；这样同一份字段定义既能用于
建表，也能用于后续 Repository 的 GORM CRUD。

## 配置与执行

`database.driver` 支持 `mysql`、`postgres`（以及兼容别名 `pgsql`），
`read_write` 模式只使用 `primary_dsn` 建表。运行时由
`server/internal/platform/persistence/gormdb` 初始化 GORM，并发布全局句柄：

```go
global.DB.Migrator().CreateTable(&migrations.User{})
```

命令行仍提供统一的状态、建表和本地回滚入口：

```text
go -C server run ./cmd/migrate status --config configs/server.yaml
go -C server run ./cmd/migrate up --config configs/server.yaml
go -C server run ./cmd/migrate down --steps 1 --config configs/server.yaml
```

## 全新安装约定

- schema 文件中的字段直接是最终形态；`up` 只检查表是否存在并调用
  `CreateTable`，不执行 `AutoMigrate`、`ALTER TABLE`、`ADD COLUMN` 或补丁式
  `ADD INDEX`。
- `CreateTable` 的重复执行是幂等的：已存在的表保持原样，缺少的表继续创建。
- 表和字段带有简短中文 `comment`，MySQL 在建表语句中写入列/表备注，
  PostgreSQL 使用 GORM 的 `COMMENT ON` 语句写入备注。
- 回滚仅用于本地全新安装演练，会按逆序删除本文件创建的表；生产环境应先
  完成备份与恢复演练。

## Model 模块边界

当前模型按使用端和能力归类，避免为后台和用户端复制同一张身份表：

| 模块 | 当前模型 | 说明 |
| --- | --- | --- |
| `shared` | `AppMetadata`、`Tenant`、`Organization`、`User` | 安装元数据、租户、组织和共享身份 |
| `auth` | `AuthSession` | 后台与未来用户端共用的会话 |
| `audit` | `AuthAuditEvent` | 认证审计记录 |
| `admin` | `Role`、`UserRole`、`Menu`、`Permission`、`IAMPolicy`、`IAMDataScope` | 后台 IAM 能力 |
| `admin` | 设置、字典、文件、邮件、任务、导入导出模型 | 当前均由后台 API 管理 |
| `client` | 暂无持久化模型 | 用户端目前只有基础探活接口，后续新增模型放入 client 版本 |

代码文件使用 `shared_*`、`identity_*`、`admin_*`、`audit_*` 等前缀归档，仍
保持单一 `model` package，避免跨模块 Model 产生导入环。

## CRUD 与关系查询

迁移 Model 只包含列、索引、检查约束和 COMMENT。关系清单由
`model.Relations()` 维护，供查询层设计 `has one`、`has many`、`belongs to`
时参考；关系查询使用专用 read/aggregate Model 或显式 JOIN，查询类型不加入
`migrations.Models()`。

新 Repository 可直接使用 GORM 泛型 API：

```go
user := model.User{Username: "demo", TenantID: tenantID}
if err := gorm.G[model.User](db).Create(ctx, &user); err != nil {
    return err
}

user, err := gorm.G[model.User](db).
    Where("tenant_id = ? AND id = ?", tenantID, userID).
    First(ctx)
```

Repository 必须自行注入租户、组织、active 和权限条件；关联写入采用显式事务
控制，避免把一次单表 `Create` 扩展为隐式关联写入。

## 后续版本升级

当前基线版本为 `v001`，仍由 `schema.go` 一次性创建全部初始表。后续升级使用
显式 GORM `Migrator` 操作，按模块和版本放在：

```text
server/migrations/versions/shared/v002_*.go
server/migrations/versions/admin/v002_*.go
server/migrations/versions/client/v002_*.go
```

升级由 `migrate up/status/down` 或安装器显式触发，应用启动只读取状态。版本记录
预留使用 `gvba_sys_app_metadata` 的 `schema:*` 元数据键，并在升级事务中锁定对应记录。
基线文件保持不变，升级文件提供明确的 `Up`，可逆版本按需提供 `Down`。

### TODO（vNext）

- 为后续版本迁移补充按模块的 `Up`/`Down` 与升级演练，基线 `schema.go` 保持不变。
- 为用户端（`client`）首次持久化模型补充独立版本和端到端租户隔离测试；当前后台
  与共享模型边界见上表。
- 记录泛型迁移后仍需收敛的 projection 性能基线（p95 和数据库往返次数），目标是
  相对基线回归不超过 10%。

所有现有 Repository 的 CRUD 已切换到 `gorm.G[T]`；固定 projection 或迁移备注
之外的手写 SQL 由 `scripts/check-sql-allowlist.py` 持续拦截。例外 ID、原因及测试
命令见 [`docs/sql-allowlist.md`](./sql-allowlist.md)，新增例外必须先更新该清单。

## 关系与索引约定（必须遵守）

业务关系只保留 `*_id` 字段，不创建数据库外键约束，也不创建外键索引。
一致性、级联删除和跨租户校验由应用事务负责。GORM 初始化统一设置
`DisableForeignKeyConstraintWhenMigrating: true`，schema 模型不声明关联字段或
`constraint` 标签；查询所需的普通组合索引仍可按业务需要声明，它们不是外键
索引。后续新增表或字段必须继续遵守这一规则。
