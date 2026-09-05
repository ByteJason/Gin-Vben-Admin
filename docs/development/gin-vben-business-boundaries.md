# 业务模块与 Gin-Vben-Admin 基座边界

这份说明给业务开发者和使用 vibecoding 的协作者提供可执行的落地规则。目标不是复制一套新架构，
而是在现有 Gin-Vben-Admin 模块化单体边界内，让业务代码可以脱离具体基座实现演进和测试。

## 快速决策

新增一个业务能力时，按下面的问题判断代码放置位置：

| 问题 | 放置位置 |
| --- | --- |
| 是否是跨端请求/响应、错误码或 schema？ | `contracts/` |
| 是否是领域规则、值对象或领域端口？ | `server/internal/domain/<domain>/` |
| 是否是用例编排、事务边界或应用端口？ | `server/internal/application/<domain>/` |
| 是否连接数据库、缓存、邮件、对象存储或第三方 SDK？ | `server/internal/platform/` 适配器 |
| 是否解析 HTTP 输入或生成 HTTP 输出？ | `server/internal/transport/http/` |
| 是否负责依赖装配和生命周期？ | `server/internal/bootstrap/` |
| 是否是管理端页面、状态或 API 调用？ | `admin/` |

## 推荐的业务模块形态

```text
server/internal/domain/orders/
├── model.go              # 领域类型和值对象
└── rules.go              # 不依赖基础设施的业务规则

server/internal/application/orders/
├── service.go            # 用例、事务边界和端口调用
├── ports.go              # repository/provider 等接口
└── service_test.go       # 不启动 Gin、不连接真实数据库的单测

server/internal/platform/orderstore/
└── repository.go         # ports.go 的具体实现

server/internal/transport/http/admin/orders/
├── handler.go            # DTO、校验、调用 service、错误映射
└── handler_test.go       # HTTP 契约/权限/状态码测试
```

`domain/` 和 `application/` 不导入 Gin、GORM、Redis 或具体 provider；`repository.go` 不把 ORM model 泄露给 handler；
`handler.go` 不实现业务规则。bootstrap 负责把这些对象组装起来并注入。

## 反耦合检查

提交前搜索新增模块是否出现以下耦合：

```text
server/internal/domain/**  -> gin / gorm / redis / transport / platform 的具体包
server/internal/application/** -> gin / gorm / redis / transport 的具体实现
server/internal/transport/** -> *gorm.DB / SQL 查询 / 业务事务分支
admin/** -> server/internal / 数据库字段 / 内部错误细节
```

出现时优先提取端口、DTO 或映射层，而不是把基座类型继续向业务层传播。

## 一个完整切片应包含

1. `contracts/` 中的契约和错误结构。
2. 模块用例、端口、领域测试。
3. 基座适配器、事务边界、migration 和 rollback（若涉及数据）。
4. 薄 HTTP handler、权限检查和契约测试。
5. 管理端真实 API 调用、loading/empty/error/permission 状态。
6. 验证命令、结果和兼容性说明。

不要把静态 mock、绕过权限的前端按钮或未接入后端的页面当作功能完成。

## 与上游/基座同步

上游模板、生成代码和基座适配器属于技术边界。同步上游时只更新对应快照或生成源，业务模块通过
稳定端口和契约吸收变化；不要在业务代码中直接 patch 上游组件，也不要让上游目录成为业务规则的唯一来源。

仓库级强制规则见根目录 [`AGENTS.md`](../../AGENTS.md)，贡献流程见 [`CONTRIBUTING.md`](../../CONTRIBUTING.md)。
