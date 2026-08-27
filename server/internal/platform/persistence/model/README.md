# 持久化 Model

这里是数据库表结构的唯一 GORM 定义来源。所有类型保持在同一个 Go package，
文件名按能力分组：

- `shared_*`：安装元数据、租户、组织
- `identity_*`：用户和认证会话
- `admin_*`：后台 IAM、设置、字典、文件、邮件、任务、导入导出
- `audit_*`：认证审计
- `types.go`：跨 MySQL/PostgreSQL 的 JSON 与二进制字段类型

`registry.go` 提供 `Definitions`、`All` 和 `ModelsFor`。初始迁移只把 `All()`
返回的标量 Model 传给 `Migrator().CreateTable`；`relations.go` 中的关系目录是
查询层元数据，不会被迁移器解析成外键或隐式 join 表。

Model 是持久化结构，不是领域对象或 HTTP DTO。Repository 负责把它们映射到领域
对象，并在每次查询中注入 tenant/org/active 等作用域。
