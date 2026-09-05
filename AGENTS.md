# Gin-Vben-Admin 项目协作规范

本文件是仓库级的 AI/vibecoding 协作规则。它与 `CONTRIBUTING.md`、`contracts/` 和
`.dev-docs/architecture/repository-structure-and-git-policy-v0.4.0.md` 一起构成开发约定；
若局部目录存在更具体的 `AGENTS.md`，以更具体文件为准。

## 1. 总原则：业务代码与 Gin-Vben-Admin 基座低耦合

Gin-Vben-Admin 基座提供启动装配、HTTP 传输、配置、数据库/缓存/日志、鉴权和前端工作区能力。
业务模块只依赖稳定的接口和契约，不依赖基座的具体实现。新增业务应优先落在独立模块中，
通过显式端口（interface）接入基座；替换数据库、Web 框架适配器、消息提供商或 UI 模板时，
业务用例和领域模型不应被迫重写。

判断标准：删除或替换一个基座适配器后，业务模块仍能通过内存实现、测试替身或另一适配器运行。

## 2. 仓库边界与依赖方向

| 边界 | 职责 | 可以依赖 | 不应依赖 |
| --- | --- | --- | --- |
| `contracts/` | OpenAPI、错误码、跨端 schema 的唯一契约源 | 无业务实现 | handler、数据库查询、UI 组件 |
| `server/internal/domain/<domain>/` | 领域模型、值对象和不依赖基础设施的业务规则 | 标准库、领域端口 | Gin、GORM、Redis 客户端、HTTP request/response、`admin/` |
| `server/internal/application/<domain>/` | 用例编排、事务边界和端口调用 | `domain/`、稳定端口、契约映射 | Gin handler、ORM model、第三方 SDK |
| `server/internal/transport/http/` | 路由、参数校验、认证上下文、DTO 映射、错误响应 | 模块用例、生成契约、Gin 适配器 | 数据库和缓存实现、业务规则 |
| `server/internal/platform/` | DB/cache/log/observability/provider 等基座适配器 | 第三方 SDK、标准库、模块端口 | 具体业务用例和页面逻辑 |
| `server/internal/bootstrap/` | 配置读取、依赖装配、生命周期管理 | transport、platform、modules | 业务规则（只能组装，不能承载业务） |
| `admin/` | 管理端 UI、状态和 API client | 生成的 API client、公开 HTTP 契约、前端 workspace 包 | `server/internal`、Go 包、数据库实现 |
| `scripts/` | 跨边界编排、生成、验证 | 各边界公开命令 | 深层导入业务包、业务规则 |

新增业务的依赖方向固定为：`transport → application → domain`；`application` 定义的端口由
`platform` 实现；`transport`/`application` 通过 `contracts` 的公开结构完成映射；`bootstrap` 只负责把
全部实现组装起来。用图表示：

```text
transport/http  ──────→  application  ──────→  domain
       │                       ↑                │
       └──── contracts ────────┘                │
                               platform ────────┘
bootstrap ───────────────→  transport / application / platform
admin ───────────────────→  public HTTP API / generated client
```

业务域之间通过公开的用例/端口协作，不通过彼此的 repository、handler、ORM model 或全局变量
协作。跨模块共享规则必须放在明确的领域包或契约中，并只有一个权威实现。

现有代码中若已经存在历史层间例外，不要为了本规则顺手大规模重构；新增代码遵循上述方向，修改旧代码时
在直接相关且风险可控的范围内逐步提取端口和映射层。

## 3. 新增业务的落点规则

1. 先在 `contracts/` 定义或更新请求、响应、错误码和兼容性规则；生成文件视为只读产物。
2. 在 `server/internal/domain/<domain>/` 创建领域类型和值对象；在 `server/internal/application/<domain>/`
   创建用例服务及其端口。端口使用业务语言命名，不暴露 `*gorm.DB`、Gin `*Context` 或第三方 SDK 类型。
3. 在 `server/internal/platform/` 实现 repository、provider、cache 等适配器，并在 bootstrap 中显式注入。
4. 在 `server/internal/transport/http/<scope>/` 添加薄 handler：只做鉴权上下文提取、输入校验、DTO 映射、
   调用用例和统一错误转换；不得把查询、事务编排或业务分支塞进 handler。
5. 在 `admin/` 只消费公开 API 或生成 client；服务端目录、数据库字段和内部错误细节不得进入前端。
6. 为正常流程、校验失败、资源不存在、权限不足、重复提交和外部依赖失败补测试；需要持久化时同时提供
   migration、回滚策略和幂等性说明。

## 4. Gin-Vben-Admin 现有规范必须保持

- 服务端唯一代码边界是 `server/`，前端唯一工作区边界是 `admin/`；不要新增根级 `backend/`、`apps/`、
  `packages/`、`internal/` 或万能 `global/`、`utils/`。
- 管理 API 使用既有 scope（例如 `/api/admin/v1`）；错误响应、分页、鉴权和审计行为遵循现有契约。
- 使用模块化单体和显式依赖注入，不为尚未存在的分布式部署增加服务、事件总线或抽象层。
- 配置使用 typed config 和 example 文件；凭据、运行时数据、构建产物、本地缓存不入 Git。
- 保持 Go/TypeScript 的现有格式化、lint、命名和测试工具；新增依赖前先复用已有能力并说明必要性。
- 不直接修改生成代码、上游快照或第三方版权文件；确需修改时先更新源契约/同步脚本并记录原因。

## 5. Vibecoding 执行流程

每次实现前后按以下顺序执行，不以静态 mock 或“计划完成”代替实现：

1. **读**：检查相关目录、入口、契约、依赖、测试和现有约定，确认改动边界。
2. **定**：写出简短方案，列出模块、接口/数据变化、风险、兼容性和验证命令。
3. **契约先行**：先锁定 API/schema/错误结构，再分别实现 server 与 admin；共享结构只保留一个来源。
4. **纵向切片**：一次完成一条真实流程（契约 → 用例 → 适配器 → handler → 前端 → 测试），避免只交付页面或 mock。
5. **验证**：至少运行受影响的单测/契约测试、`go test` 或前端 typecheck/build，并执行 `git diff --check`。
6. **收口**：检查 loading/empty/error/permission/重复提交等状态，确认没有调试代码、假数据、密钥或无用依赖。

## 6. 提交前检查清单

- [ ] 新业务位于明确的 `domain`/`application` 边界，未反向依赖 `transport`、`platform` 或 `admin` 实现。
- [ ] 外部依赖通过端口和显式注入接入，测试可替换为 fake/in-memory 实现。
- [ ] 契约、迁移、错误结构、权限和兼容性已同步。
- [ ] 正常、异常、空数据和重复调用路径均有验证记录。
- [ ] 已运行相关检查、`node ./scripts/verify.mjs --scope basic`（适用时）和 `git diff --check`。
- [ ] 提交信息使用 Conventional Commits；scope/主题优先使用中文，例如 `feat(用户模块): 增加用户查询用例`。

完整的边界说明见 [`docs/development/gin-vben-business-boundaries.md`](docs/development/gin-vben-business-boundaries.md)。
