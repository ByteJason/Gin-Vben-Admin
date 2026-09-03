# 公共能力开发使用文档（开发实施与接线）

> **状态：** `in-progress`（首个实现切片已落地，生产化收口持续进行）
> **批次：** `B1.7d-common-capabilities`
> **更新时间：** 2026-09-03
> **适用范围：** Go 后端内部调用、管理端 SMTP/媒体库、三套管理端 UI 的统一接线。
> **基线：** 当前工作区 `main`；实现切片与文档同步更新，验证记录位于 `.runtime/evidence/`。

本文是开发者使用和实现公共能力的长期入口。本文件保留已确认契约、当前实现路径、实现约束、示例和验收方式；
代码接入时请先阅读“当前实现与目标差异”，生产部署前按差异表完成收口项。

## 1. 目标与边界

### 1.1 本批次目标

1. 把 SMTP 邮件、邮件模板、验证码和通知邮件抽成后端 Go 内部公共入口。
2. 把媒体库抽成可供其他模块复用的 `MediaCatalog`；业务只保存资源标识，不拼对象键或 URL。
3. 管理后台可创建调用者、账号绑定、模板和验证码策略；管理端自身也是一个受控调用者。
4. 管理界面保存可编辑配置后立即生效；不要求调用方理解业务配置版本号。
5. SMTP、媒体库页面在 `web-antd`、`web-ele`、`web-naive` 保持路由、字段、权限、文案和交互语义等价，并提供“普通使用流程”和“开发者对接流程”引导抽屉。
6. Logo 支持从媒体库选择或直接上传；配置保存资源 ID，并记录资源引用。
7. 以 TDD/checkpoint 分段实现，最后一次切换到新公共入口并删除重复旧链路。

### 1.2 本批次边界与后续路线

- 业务模块的接入方式固定为进程内 Go 端口；管理 REST 端点负责资源维护与受控测试。
- SMS、Webhook、推送、Feature Flag、全文媒体搜索和标签体系列入后续路线。
- 基础能力保持具名端口和清晰边界，不合并为万能 `CommonService`。
- 数据库、Redis、对象存储根端点和主加密根密钥保留启动级维护边界。

对象存储扩展、导入导出列处理、外部 provider 的真实资源验收沿既有路线执行；本文件只规定可替换的 port 和验收钩子。

## 2. 当前仓库基线：已实现与待改造

| 能力 | 当前代码事实 | 目标改造 |
| --- | --- | --- |
| SMTP | `server/internal/application/mail.Service` 保留租户账号、投递记录、重试和密文正文；`server/internal/application/mail/ports.go`、`server/internal/application/notification/runtime.go` 已提供 caller/模板/策略解析、同步测试发送和验证码入口 | 持久化 outbox/relay、真实 provider 健康回退和审计落库继续收口 |
| 密码找回 | `server/internal/bootstrap/http.go` 构造并注入公共 notification runtime；预置 caller/template/policy 可通过管理端热更新 | 迁移一个真实密码找回或通知调用方，完成端到端回归 |
| 媒体 | `server/internal/application/file/catalog.go` 提供 `CatalogAdapter`/`MediaCatalog`，支持流式上传、作用域、分类、签名 URL、引用保护和预置 reconcile | 持久化 catalog repository、对象存储清理 worker 和配额对账继续收口 |
| 文件模型 | `file_objects` 已扩展 category/provider/status/metadata/reconcile 字段；新增 `media_categories`、`media_usages` 模型 | 双库兼容迁移、旧数据回填和生产回滚演练 |
| 设置 | `server/internal/application/settings/runtime_snapshot.go` 提供 immutable snapshot、generation 和订阅失效；应用服务可即时读取最终态 | 集群广播与持久化审计接线继续收口 |
| UI/API | 三套 `system/mail`、`system/files` 页面已接入共享引导 schema、侧边抽屉、媒体图片筛选基础能力；`admin/packages/api-client/src/generated/admin-v1.ts` 已由 OpenAPI 生成并包含 challenge/media endpoint 常量 | 模板管理交互、Logo 业务引用和 E2E/axe 一致性验收 |
| 任务 | 已有 `jobs.Queue`/worker 和持久化任务相关能力 | outbox relay、provider 清理、补偿和 reconcile 复用具名 jobs port |

当前实现的内存 map 在服务重启后缺少完整的文件元数据恢复链。若现场存在旧数据，正式切换前建立 manifest/双写回填并完成 owner、分类和 ACL 对账；对象目录扫描仅作为补充线索。

## 3. 架构原则与依赖顺序

采用 ports-and-adapters 和具名窄接口，每个能力维持自己的 `domain`、`application`、`platform` 和 `transport` 边界：

```text
scope / authorization
        ↓
audit.Publisher + jobs.Dispatcher + dictionary.Reader/LocaleResolver
        ├─ MailSender
        ├─ NotificationService + VerificationCodeService
        └─ media.Catalog
                ↓
        认证、Logo、导入导出和管理端调用方迁移
```

约束：

- `context.Context` 必须携带已认证、已绑定的 `tenant.Context`/principal；service 不从普通请求参数推断租户、组织或 caller。
- 管理端 HTTP API 仅管理资源和测试发送；业务模块在同一进程中注入 Go port。
- provider 只处理协议/对象字节；授权、租户过滤、模板选择、审计、幂等和生命周期由 application 层负责。
- 所有 mutation 产生统一 audit envelope（request ID、actor、scope、resource、result、脱敏字段）。
- List 默认返回 metadata，不返回正文/验证码/对象字节；二进制和受控正文必须显式调用读取接口。

本地无认证单节点 profile（`auth.enabled=false` 且有效 tenant mode 为 `single`）可由服务端注入
`system.admin` actor 以便立即保存 branding；auth 关闭但 tenant 为 `multi` 时不会注入该 actor，
设置写入仍要求已验证的管理身份。

## 4. 已冻结 Go 公共契约（实现 API）

下列代码是当前实现形状，名称和包路径已冻结；业务模块直接依赖这些窄接口，避免出现第二份业务实现。
本节未限定的 `SendResult`、`Recipient`、`SendMode`、`DeliveryStatus` 由
`notification/ports.go` 作为 provider-neutral DTO 统一定义，`mail/ports.go` 以类型别名暴露；
`NotificationRequest`、`IssueRequest`、`VerifyRequest` 属于 `notification` 包，媒体 DTO 与
`URLPurpose` 属于 `file` 包。这样保持现有 `mail → notification` 依赖方向，避免 Go import cycle；
直接接线时以精简 API 参考中的包限定名为准。

### 4.1 邮件与通知发送

邮件与通知的字段级入参/返回值以 [精简 API 对接参考](../integration/common-capabilities-api.md) 为单一参考；
本节补充账号路由、模板快照和 outbox 约束。

```go
// server/internal/application/notification/ports.go
type SendMode string
type DeliveryStatus string
type Recipient struct { Address, Kind string }
type SendResult struct {
    MessageID          string
    Status             DeliveryStatus
    PolicyGeneration   string
    TemplateGeneration string
}

type NotificationService interface {
    Send(context.Context, NotificationRequest) (SendResult, error)
}

type NotificationRequest struct {
    CallerKey      string            // 可信代码上下文中的稳定键
    Purpose        string            // 受控通知用途
    Recipients     []Recipient       // 地址与 kind
    Variables      map[string]string // 严格变量 schema
    Locale         string            // 可省略，由 LocaleResolver 回退
    IdempotencyKey string
    Mode           SendMode          // Production 或 AdminTest
}

// server/internal/application/mail/ports.go
type SendMode = notification.SendMode
type DeliveryStatus = notification.DeliveryStatus
type Recipient = notification.Recipient
type SendResult = notification.SendResult

type MailSender interface {
    Send(context.Context, SendRequest) (SendResult, error)
}

type SendRequest struct {
    CallerKey      string
    TemplateKey    string
    Recipients     []Recipient
    Variables      map[string]string
    Locale         string
    IdempotencyKey string
    Mode           SendMode
}
```

`MailSender` 处理通用模板邮件；`NotificationService` 接收受控的通知用途（例如
`security.password-changed`、`security.unusual-login`），内部复用模板、账号路由和 outbox。
两者共享 `SendResult`、幂等和审计语义。模板发布快照可带内部 `template_generation`，它只用于
重试和审计关联；管理界面保存的是单一最终态。

业务模块不传 SMTP 账号 ID、密码、权重或 provider endpoint。`CallerKey` 必须来自可信的进程调用上下文；普通 HTTP body/header 中的 caller 字段不参与身份判定。未知或停用 caller、模板未发布、变量缺失和无可用账号均 fail-closed。

调用者记录由后台创建，系统内置 caller 由代码/迁移 reconcile；建议字段：`caller_key`（稳定键）、显示名、模块、capabilities、scope、enabled、system_owned、created_by。系统管理端 caller 使用固定键 `system.admin`（实际键可由实现常量替换）。

### 4.2 SMTP 账号路由

每个 caller 维护账号允许列表、默认账号、权重和策略；管理界面提供：

```text
weighted_random | round_robin
```

`single`/有序故障回退仅作为既有兼容适配器的内部策略，首期管理界面不新增选项。

worker 每次 dispatch 再过滤 disabled、软删除、冷却或健康检查失败的账号。调用者策略可覆盖账号默认权重；已建立连接/发送中的请求固定本次快照，尚未 dispatch 的 outbox 可按最新启用和路由策略执行，实际 account/policy generation 写入投递记录。

SMTP 账号 scope 必须与媒体一致地显式建模（system/tenant/org）；现有 `smtp_accounts.tenant_id NOT NULL` 和租户级唯一索引需在 migration 设计中明确兼容方案。没有明确系统账号授权时，默认只允许 tenant/org 内使用。

### 4.3 验证码

验证码方法的字段级入参/返回值以 [精简 API 对接参考](../integration/common-capabilities-api.md) 为单一参考；
本节保留策略、状态和安全实现约束。

```go
type VerificationCodeService interface {
    Issue(context.Context, IssueRequest) (ChallengeRef, error)
    Verify(context.Context, VerifyRequest) error
}

type IssueRequest struct {
    CallerKey string
    Purpose   string // 例如 email_change、password_reset
    Recipient string
    Locale    string
    Variables map[string]string
    IdempotencyKey string
}

type VerifyRequest struct {
    ChallengeID string
    Code        string
    IdempotencyKey string
}
```

`purpose`、caller、tenant/org 和规范化 recipient 共同隔离 challenge。默认策略：6 位纯数字、有效期 10 分钟、最多 5 次失败、重发间隔 60 秒、每小时 5 次；后台可在规定范围内调整（长度 4–10、有效期 1–30 分钟等）。验证码摘要使用 HMAC/加盐摘要和常时比较，Redis、日志和审计保存摘要与计数。

同一 caller+purpose+recipient 只保留最新 challenge；重发在事务中使旧 challenge `superseded`，旧 outbox dispatch 前再次检查当前性。校验成功原子消费，失败达到上限立即失效。

### 4.4 媒体目录

字段级的公开对接形状（含 `ResourceRef.Selectable`、`DisabledReason`、`ScopeType`、幂等字段和
逐方法返回表）以 [精简 API 对接参考](../integration/common-capabilities-api.md) 为单一参考；本节补充
状态机、provider 和迁移约束。

```go
type MediaCatalog interface {
    Upload(context.Context, UploadInput) (ResourceRef, error)
    Get(context.Context, ResourceID) (ResourceRef, error)
    List(context.Context, MediaFilter) (MediaPage, error)
    Open(context.Context, ResourceID, OpenOptions) (io.ReadCloser, error)
    SignedURL(context.Context, ResourceID, URLRequest) (URLRef, error)
    Delete(context.Context, ResourceID, DeleteOptions) error
    ListCategories(context.Context, CategoryFilter) ([]CategoryRef, error)
    CreateCategory(context.Context, CategoryInput) (CategoryRef, error)
    UpdateCategory(context.Context, CategoryID, CategoryPatch) (CategoryRef, error)
    DeleteCategory(context.Context, CategoryDeleteRequest) error
}

type ResourceID string
type CategoryID string
type ScopeType string  // system、tenant、org
type ACL string        // private、public-read
type MediaStatus string // pending、ready、failed、deleting、deleted
type URLPurpose string  // preview、download

type MediaFilter struct {
    MIMEExact, MIMEFamily string
    CategoryID            CategoryID
    ScopeType             ScopeType
    IncludeDescendants    bool
    OwnerID, Cursor        string
    Offset                int // 旧客户端兼容；同时提供 cursor 时 cursor 优先
    Limit                 int
    Status                MediaStatus
}

type ResourceRef struct {
    ID           ResourceID
    Name, MIME   string
    Size         int64
    SHA256       string
    CategoryID   CategoryID
    ScopeType    ScopeType
    ACL          ACL
    Status       MediaStatus
    CreatedAt    time.Time
    UpdatedAt    time.Time
    URLHints     map[string]bool // preview/download availability
    Selectable   bool
    DisabledReason string
}

type MediaUsageService interface {
    Attach(context.Context, UsageInput) (UsageRef, error)
    Detach(context.Context, DetachRequest) error
    ListByResource(context.Context, ResourceID) ([]UsageRef, error)
}

type CategoryDeleteRequest struct {
    ID             CategoryID
    IdempotencyKey string
}

type DetachRequest struct {
    UsageID        string
    IdempotencyKey string
}
```

当前 `UploadInput` 使用 `io.Reader`、声明大小、文件名、ACL、category、metadata 和幂等键；兼容适配器可包装 `[]byte`，但 HTTP 层不得无限制 `ReadAll`。当前 `file.Store` 契约包含 `Put`、`Delete`、`SignURL`，支持读取的 provider 可额外实现 `Get` seam 供 `Open` 使用；持久化远端 provider 在后续切片扩展 `Stat`/流式 `Open` 时仍不得把对象内容塞回 metadata。

`ResourceRef` 只包含 opaque ID、名称、canonical MIME、大小、SHA-256、category、scope、ACL、status、created/updated 时间、provider-neutral URL hints，以及供 Logo 选择器使用的 `Selectable`/`DisabledReason`。`List` 使用 `created_at DESC, id DESC` 稳定 cursor；`offset` 仅为旧路由兼容。过滤器首期支持：

| 过滤器 | 语义 |
| --- | --- |
| `MIMEExact` / `MIMEFamily` | canonical MIME 或 `image/*` 等族；服务端解析，不拼任意 SQL |
| `ScopeType` | `system`、`tenant` 或 `org`；缺省按当前 context 合并可见层级 |
| `CategoryID` + `IncludeDescendants` | 明确是否包含整棵子树 |
| `OwnerID`、名称前缀/包含 | 受授权范围约束 |
| 大小/创建时间 | 有上下界，避免无界扫描 |
| ACL、status、cursor、offset、limit、sort | 默认仅 `ready` 且 limit 有上限；同时提供 cursor/offset 时 cursor 优先 |

“获取整个资源库”应理解为受限分页迭代；大批量导出另走 jobs/artifact，不返回无限内存切片。

### 4.5 作用域与引用

当前实现使用显式 `scope_type=system|tenant|org`。可见性查找顺序为 **org → tenant → system**（组织覆盖租户，租户覆盖系统；当前层未命中再回退）。非公开资源还必须匹配当前 principal；platform-admin 仅在内部 catalog 授权 seam 显式放宽跨作用域检查，系统资源始终只读。租户/组织可复制后覆盖。生产化仍需补齐双库迁移、回填和授权回归。

`media_usages` 记录 `resource_id、scope、caller/module、entity_type、entity_id、field`，并以唯一键防重复；`Attach`/`Detach` 的 `UsageInput`/`DetachRequest` 带业务幂等键。当前 Logo adapter 读取旧值后先更新 branding 设置，再绑定新 usage、解绑旧 usage；任一步失败时补偿设置和新旧 usage，页面保留旧 Logo。后续持久化实现将设置与 usage 纳入服务端事务。被引用资源只能软删除/停用，物理清理由后台任务执行。

## 5. 数据模型与迁移（实现切片与收口项）

迁移必须遵守仓库约定：显式 `server/migrations/versions/admin/v002_*.go`，Up/Down、锁和校验齐全；启动流程不使用 AutoMigrate；模型只保留标量 `*_id`，不声明 GORM 关系或数据库外键。`model/registry.go`、`comments.go`、`relations.go` 及硬编码契约测试要同步更新。

### 5.1 媒体表

**`file_objects` 扩展（已落地切片）**：

```text
scope_type, category_id, provider_id, lifecycle_status,
metadata_json, original_extension, detected_mime, reconcile_key,
pending_at, ready_at, deleted_at
```

保留现有 object key 唯一性保护，但 object key 只由 provider adapter 解释。建议索引：`(scope, category_id, created_at)`、`(scope, mime, created_at)`、`(scope, owner_id, created_at)`、`(status, created_at)`。

**`media_categories`（新增）**：

```text
id, scope_type, tenant_id, org_id, parent_id, path, depth,
name, sort_order, enabled, system_owned, created_at, updated_at, deleted_at
```

同一父节点名称唯一；移动操作使用事务锁/乐观版本，更新整棵 path/depth 并拒绝环路。

**`media_usages`（新增）**：

```text
id, scope_type, tenant_id, org_id, resource_id, caller_key,
module, entity_type, entity_id, field, created_at, updated_at, deleted_at
```

### 5.2 通知与验证码表

- `notification_callers`：稳定 caller key、能力、scope、启停、system_owned。
- `notification_caller_accounts`：caller 与 SMTP account 的 allowlist、weight、priority、strategy、默认标记。
- `notification_templates` + `notification_template_versions`：模板键、locale、变量 schema、draft/published/disabled/soft-deleted、供投递重试使用的不可变发布快照、审计字段；后台配置界面只保存和呈现当前最终态，快照标识仅用于投递关联。
- `verification_policies`：purpose/caller/scope、长度、字符集、TTL、失败/重发/频率限制。
- `verification_challenges`：challenge ID、purpose/caller、recipient digest（原文按最小化/加密策略保存）、code digest、状态、失败次数、过期、重发时间、message ID、template key/generation、locale。
- 现有 `email_messages` 优先扩展为邮件 outbox：增加 caller/template/generation、`is_test`、challenge ID 和 relay 状态；后续渠道出现统一需求时再抽象通用表。

所有新增字段必须有中文 field comment、created/updated/deleted 时间和明确 scope 索引。SMTP 唯一索引是否从“tenant-wide”扩展为“scope-aware”需在 migration SQL 和数据冲突报告中明确。

### 5.3 预置图片 manifest

预置图使用稳定 `asset_key=system.logo.default` 与 SHA-256 manifest；当前内置文件为
`server/internal/application/file/assets/system-logo-default.svg`，MIME 为 `image/svg+xml`，
SHA-256 为 `641cdc62fb9093ed5715f0792b611a70c0dc7a8f65246b409cdbcb30822b36e1`。
`CatalogAdapter.ReconcilePreset` 可重复执行并返回同一 opaque resource ID；系统资源只读，租户通过复制后覆盖。
持久化 manifest、配额对账和双库回填仍属于生产化收口项。

## 6. 运行时流程与状态机

### 6.1 配置热更新

用户可编辑的 SMTP 账号、caller、账号路由、模板、验证码策略、通知策略和媒体元数据保存后即时生效；加密根密钥、对象存储根端点、数据库/Redis 拓扑等启动级维护项走受控重载或维护窗口。

推荐实现：

1. 管理 mutation 在主库事务中校验并提交最终状态，同时写审计。
2. 提交后本实例同步替换 immutable snapshot；内部生成号/时间戳只用于缓存一致性、诊断和审计，调用方和 UI 继续使用稳定业务键。
3. Redis Pub/Sub 发送失效通知；其他实例按 generation 拉取并原子替换，传播目标不超过 5 秒。
4. 拉取/解析失败保留上一份可用快照并告警；发送请求记录实际 generation。
5. UI 并发保存使用 `updated_at`/ETag 乐观检测；冲突返回可本地化错误，不静默覆盖。

“无版本号”表示业务不需要管理模板/配置版本，不表示运行时可以没有内部一致性标记。

### 6.2 生产邮件、测试邮件与验证码

当前实现切片使用进程内 runtime map 和已有 mail service 适配器完成状态、模板、限流、幂等与同步发送验证；
下列流程图是生产持久化 outbox/relay 的冻结接线目标，迁移与 relay 收口前不要把内存状态当作跨重启权威数据。

```text
Issue
  └─ DB transaction: verification_challenge(pending_send)
                  + email_messages(outbox,pending)
                  + recipients/usage/audit
       └─ relay/jobs（幂等发布与重试）
            ├─ provider accepted → challenge active, message sent
            └─ retries exhausted → challenge send_failed, message failed
```

- DB challenge + email outbox 在同一数据库事务；Redis 限流/热点摘要和队列发布采用 relay 的最终一致语义。
- worker 发送前检查 challenge 是否仍是最新、未取消、未过期；重发会使旧记录 `superseded`。
- SMTP 已接受但 worker 在更新 DB 前崩溃的窗口，使用 provider message ID、attempt 记录和 reconcile 任务去重/补偿；网络发送按幂等键与补偿任务管理。
- 管理端连接测试只做 DNS/TCP/TLS/EHLO/AUTH，不产生生产消息；模板测试可注入固定测试变量，`is_test=true`，独立于可校验 challenge，并写入审计。收件人 allowlist 的配置/策略存储在 P2/P7 收口接入前，不将当前切片当作生产 allowlist 约束。
- Redis 清空或不可用时，验证码按 fail-closed 处理；DB challenge 是权威状态，Redis 仅限流/热点摘要。

状态枚举：

```text
pending_send → active → consumed
            ↘ send_failed
active       ↘ expired / superseded / locked
```

### 6.3 媒体上传与删除

```text
pending (metadata + provider intent)
   ├─ provider Put/verify/hash → ready
   └─ failure → failed + compensating cleanup
ready → deleting (soft delete) → deleted (physical cleanup job)
```

每个对象固定所属 provider；切换默认 provider 只影响新上传，历史对象迁移走独立任务。hash 冲突按既有 DEC-091 进入提示分支，上传同时校验扩展名、canonical MIME、内容探测、大小、配额和预留的病毒扫描 hook。`Open` 支持取消/限流，当前 HTTP adapter 支持单一 `Range` 并返回 206/Content-Range；多段 Range 与真实远端流式 provider 仍在生产化收口。

### 6.4 语言回退

模板 locale 解析顺序：显式调用 locale → `users.locale` → 租户默认 → 系统默认（当前核心为 `zh-CN`、`en-US`）。邮件记录保存实际 locale、template key/generation；缺译时回退并写审计提示。正式开发需给 `users` 增加 locale 字段和 migration。

## 7. 管理 API 与 UI（实现与收口）

### 7.1 管理 API（HTTP）

HTTP 仅供管理端和受控测试，不是业务模块公共调用入口。已实现端点、当前 `/mail/*` 与 `/files/*`
兼容端点及方法/operationId 见 [精简 API 对接参考](../integration/common-capabilities-api.md)。资源清单：

```text
/api/admin/v1/notification/callers
/api/admin/v1/notification/templates
/api/admin/v1/notification/templates/{id}/test
/api/admin/v1/notification/verification-policies[/{policy_key}]
/api/admin/v1/mail/accounts
/api/admin/v1/mail/messages
/api/admin/v1/media/library
/api/admin/v1/media/library/{id}   # PATCH/PUT/DELETE
/api/admin/v1/media/categories
/api/admin/v1/media/resources/{id}
/api/admin/v1/media/resources/{id}/open|signed-url
/api/admin/v1/media/usages[/{id}]
/api/admin/v1/media/library/{id}/usage
/api/admin/v1/settings/branding   # 复用设置中心保存 logoResourceId
```

现有后端 `/api/admin/v1/files` 与前端 UI 路由 `/system/files` 保留兼容窗口，由 adapter 转发到 `MediaCatalog`；新媒体端点统一使用 cursor、scope、MIME/category filters 和 `selectable`/`disabledReason` 元数据。所有 ID 路径参数 URL 编码，TTL 有上限（推荐默认 15 分钟、最大 24 小时）。

管理 DTO 要区分 create/update patch：布尔值、数字和“保留原密码”使用指针/field mask/显式 clear 标记，避免零值语义歧义。密钥只返回 `passwordConfigured` 等摘要，secret 字段保持写入专用。

### 7.2 权限与审计

建议权限码：

```text
notification:callers:read/manage
notification:templates:read/manage/publish/test
notification:verification:read/manage
system:mail:read/manage/test
media:library:read/manage
system:settings:read/manage     # Logo 设置沿用设置中心权限
```

每项 mutation 和测试动作写审计：`caller.created/updated/disabled`、`template.published/tested/rolled_back`、`verification.policy.updated`、`mail.account.tested`、`mail.message.queued/sent/failed`、`media.uploaded/moved/soft_deleted/restored`、`settings.branding.updated`。审计只保留脱敏 recipient、摘要、ID、scope、policy/template generation；不记录密码、验证码、正文或对象密钥。

### 7.3 使用说明引导

SMTP 和媒体库页面都提供“使用说明”入口，打开侧边抽屉并分为：

1. **普通使用流程：** 页面用途、权限、常见操作、失败提示和恢复动作。
2. **开发者对接流程：** Go 接口、请求字段、错误处理、幂等、scope 要求和示例。

当前三套 UI 共用轻量 guide schema（标题、受众、普通步骤、开发者步骤和 locale 文案），前端本地化静态 JSON 优先；抽屉支持 Esc/遮罩关闭、再次打开及焦点返回。步骤 ID、代码块、权限、错误 key、外链、已读状态和租户重置属于后续 P5 扩展。后台编辑需求进入独立 guide 资源与权限设计，静态引导继续与邮件模板分离。

**SMTP 普通使用步骤：**

1. 查看账号池健康状态与当前作用域。
2. 新建或选择 SMTP 账号，执行连接测试。
3. 创建 caller，绑定账号、策略和权重。
4. 创建模板的 zh-CN/en-US 变体，执行变量校验与预览。
5. 设置验证码和通知策略，执行管理员收件人测试发送。
6. 保存最终态，查看即时生效提示、消息状态和审计记录。

**SMTP 开发者对接步骤：**

1. 申请对应 caller 的读取/发送权限并取得稳定 `caller_key`。
2. 通过依赖注入获得 `MailSender`、`NotificationService` 或 `VerificationCodeService`。
3. 传入可信 scope、模板 key、locale、变量和幂等键。
4. 根据稳定错误 key 处理限流、模板变量、账号池和 outbox 状态。
5. 在 fixture 中验证发送、重试、验证码消费和回滚路径。

**媒体库普通使用步骤：**

1. 选择当前租户/组织可见的目录并上传资源。
2. 完成 MIME、大小、hash 和分类校验，按目录移动或重命名。
3. 使用类型族、分类/子分类和分页筛选资源，预览或下载。
4. 在 Logo 区域打开媒体选择器；图片条目直接选择，其他文件查看置灰原因。
5. 也可直接上传图片，上传成功后自动选中并保存引用。

**媒体库开发者对接步骤：**

1. 注入 `MediaCatalog` 和 `MediaUsageService`，从 context 取得 scope。
2. 使用 `List` 的 MIME family、category subtree、cursor 和 limit 获取资源页。
3. 用 `SignedURL` 获取短期预览地址，需要字节流时调用 `Open`。
4. 在业务设置中保存 `resource_id`，通过 usage 接口绑定实体字段。
5. 处理 `ready/failed/deleted/media_in_use` 状态，并在异步清理完成后刷新页面。

### 7.4 Logo 选择器

- 查询所有当前主体可见的 system/tenant/org 分类和子分类，合并顺序 org→tenant→system、去重并稳定排序。
- 所有资源仍显示；`image/*` 可选，非图片保留条目并显示置灰、`aria-disabled` 和 `disabledReason`，键盘操作跳过选择。
- 直接上传必须先通过 image MIME/内容检测；上传成功后自动选中，失败不修改旧 Logo。
- 保存的是 `resource_id`（而非最终 URL），同时写 `media_usages(module=branding, field=logo)`；切换时按“设置 → 新 usage → 旧 usage”执行，失败则反向补偿并保留旧 Logo；被引用资源软删除前提示/保护。
- 预置图使用 manifest asset key；更换 provider 或签名 URL 不需要改前端配置。
- 管理端 Logo 选择器验收前先执行预置图片 1 的 seed/reconcile，确保 `system.logo.default` 已出现在可见资源页。

## 8. 开发者接入示例

> 示例展示当前契约；`catalog`、`notify`、`verify` 由依赖注入提供，`ctx` 必须已经安装可信 tenant/principal scope。
> 示例中的 `mail`、`notification`、`file` 分别对应 `server/internal/application/mail`、
> `server/internal/application/notification`、`server/internal/application/file` 包；import 行按项目模块路径补齐。

### 8.1 发送通知

```go
result, err := notify.Send(ctx, notification.NotificationRequest{
    CallerKey:      "security.password-changed",
    Purpose:        "security.password-changed",
    Recipients:     []notification.Recipient{{Address: user.Email, Kind: "to"}},
    Variables:      map[string]string{"display_name": user.DisplayName},
    IdempotencyKey: "password-change:" + changeID,
})
if err != nil {
    // 按 errors.Is 映射 no_caller/template_unpublished/rate_limited/queued failure；
    // 不自行读取 SMTP 账号或重试 provider。
    return err
}
_ = result.MessageID
```

### 8.2 验证码发送与校验

```go
challenge, err := verify.Issue(ctx, notification.IssueRequest{
    CallerKey: "auth.email-change",
    Purpose:   "email_change",
    Recipient: email,
    Locale:    user.Locale, // 为空时走回退链
    IdempotencyKey: "email-change:" + changeID,
})
if err != nil { return err }

if err := verify.Verify(ctx, notification.VerifyRequest{
    ChallengeID: challenge.ID,
    Code:        input.Code,
    IdempotencyKey: "email-change:verify:" + challenge.ID,
}); err != nil {
    return err
}
```

业务模块只负责把收件人、用途和用户输入交给公共入口；长度、字符集、过期、失败次数、频率、模板和账号选择由公共服务负责。

### 8.3 按类型/分类获取图片

```go
page, err := catalog.List(ctx, file.MediaFilter{
    MIMEFamily:         "image/*",
    CategoryID:          categoryID,
    IncludeDescendants:  true,
    Limit:               50,
    Cursor:              cursor,
})
if err != nil { return err }
for _, resource := range page.Items {
    urlRef, err := catalog.SignedURL(ctx, resource.ID, file.URLRequest{
        Purpose: file.URLPurpose("preview"),
        TTL:     15 * time.Minute,
    })
    if err != nil { return err }
    render(resource.Name, urlRef.URL)
}
```

需要处理原始字节时才调用 `Open`，并始终 `defer Close()`；业务表保存 opaque resource ID，不保存 `ObjectKey`、provider URL 或永久 URL。大批量处理使用 cursor 循环或异步导出任务。

## 9. 错误码与排障约定

错误码是跨 UI 的稳定 key，具体 HTTP status 可由 transport 映射：

| 错误 key | 典型原因 | 调用方动作 |
| --- | --- | --- |
| `scope_required` / `scope_denied` | 缺 tenant/org 或跨 scope | 从可信 context 重试，不接受请求参数越权 |
| `caller_not_found` / `caller_disabled` | caller 未 reconcile 或已停用 | 检查后台 caller 与代码 key |
| `template_unpublished` / `template_variable_missing` | 无发布版本或变量 schema 不匹配 | 发布模板/补齐变量 |
| `test_recipient_not_allowed` / `template_test_variables_invalid` | 模板测试收件人或固定变量不符合规则 | 检查 allowlist 和测试变量 |
| `mail_no_account` / `mail_policy_invalid` | 无可用账号或策略解析失败 | 检查账号启用、绑定、权重和上一快照告警 |
| `verification_rate_limited` / `verification_expired` / `verification_locked` | 频率、TTL 或失败上限 | 按 `retry_after` 等待，并继续使用公共服务 |
| `verification_not_active` / `verification_consumed` | outbox 尚未成功或已消费 | 查询 challenge 状态，按重发策略处理 |
| `media_not_found` / `media_not_ready` | ID 错误、provider 尚未完成 | 等待状态或重新获取 metadata |
| `media_type_not_allowed` / `media_hash_conflict` | MIME/扩展/内容或 hash 冲突 | 修正上传；不自动覆盖 |
| `media_in_use` / `category_not_empty` | 引用或子节点存在 | 先解除 usage/移动资源 |
| `config_conflict` / `config_snapshot_unavailable` | ETag 冲突或快照失效 | 重新读取并保存；检查告警 |

排障顺序：记录 request ID → scope/principal → caller/template/policy key → message/challenge/resource ID → 实际 generation/provider/account → audit/attempt/job 日志。日志必须脱敏；SMTP 密码、验证码、正文、签名 URL 全串和 object key 均不进入日志。

## 10. 测试与验收矩阵

### 10.1 后端

- **契约单测：** caller reconcile、模板变量校验、locale 回退、账号权重/轮询/故障剔除、验证码摘要/常时比较/原子消费、MIME family/category subtree/cursor。
- **状态机测试：** pending→active、重发 superseded、过期/失败锁定、provider 已接受后进程崩溃、relay 重试和幂等。
- **租户与权限：** tenant/org/system 可见性、跨组织更新拒绝、ACL、Logo usage 删除保护；覆盖 PlatformAdmin 明确边界。
- **provider 契约：** local/memory 与远端 fake；远端 `Open` 不返回 Data 时仍可流式读取；超限、取消、Range、hash 冲突和补偿清理。
- **迁移：** offline SQL、MySQL/PostgreSQL schema、Up/Down、重复执行、旧数据 manifest 回填和计数/hash 对账；更新 `Definitions`/comments/relations 契约测试。
- **动态配置：** 同实例保存后立即生效、跨实例 Pub/Sub 传播≤5秒、坏快照保留上一可用值、ETag 冲突、发送记录 generation。

### 10.2 管理端

- 三 UI 使用同一 API client 类型、路由权限和 guide schema；375/768/1024/1440 断点、键盘、焦点回收、`aria-disabled`、axe、dark/reduced-motion。
- SMTP：账号 CRUD/连接测试、caller 绑定、模板草稿/发布/测试、验证码策略、消息记录和错误降级。
- 媒体：分类树/移动环路校验、上传/预览/下载/签名 URL/软删、MIME/category 过滤、预置图和 Logo 选择器。
- 引导：普通/开发者两个分段、关闭后重开、多语言缺译回退、错误 key 与示例代码可复制。

### 10.3 验证命令（当前切片已执行）

```bash
cd server
GOCACHE=/tmp/autochatgpt-go-cache GOTMPDIR=/tmp go test ./internal/bootstrap ./internal/application/settings ./internal/application/file ./internal/transport/http/commoncapabilities ./internal/application/notification ./internal/application/mail -count=1
GOCACHE=/tmp/autochatgpt-go-cache GOTMPDIR=/tmp go test -race ./internal/application/file ./internal/application/mail ./internal/application/notification ./internal/application/settings ./internal/transport/http/commoncapabilities -count=1
GOCACHE=/tmp/autochatgpt-go-cache GOTMPDIR=/tmp go vet ./...
cd ..
node scripts/generate-openapi.mjs --check
python3 scripts/check-sql-allowlist.py
python3 scripts/sql-allowlist.test.py
node --test tests/contract/v100_admin_shell_contract.test.mjs
```

真实 DB/Redis/SMTP/MinIO 仅使用运行时 fixture；凭据采用运行时注入，测试后执行精确清理。
三套前端 `vue-tsc --noEmit --skipLibCheck`、changed-file ESLint 和本地 Vite production build 均已通过；
OpenAPI 生成/检查、SQL allowlist、契约测试和 `git diff --check` 均已通过。完整 `go test ./... -count=1`
除现有 `internal/platform/observability/runtime_test.go:34` 的 sandbox IPv6 listener 权限错误外其余包通过；
该字面量失败与实现无关，需在具备回环监听权限的 CI 环境复跑。逐字输出及退出状态位于
`.runtime/evidence/2026-09-02-common-capabilities-implementation/`。

## 11. 回滚与发布门禁

每个实现切片必须生成四类可验证产物：

1. 修改后的 artifact（代码/迁移/UI）。
2. patch/diff 和文件 manifest。
3. verification record：精确 baseline/modified 命令、输入、字面量输出和 exit status。
4. 可执行 rollback：迁移 Down、旧入口恢复/adapter 切回、outbox 停止/重放、provider 补偿清理。

发布顺序建议：

```text
契约与 RED → migration/offline → repository/provider
→ notification/verification/media application
→ 一个真实调用方迁移 → 三 UI/API → 集成/E2E/UAT
→ 一次 cutover → 清理旧重复实现
```

回滚前保留原文件 hash、数据库 schema 状态、provider manifest、outbox/challenge 未完成项和审计记录。新 provider 默认切换不迁移历史对象；历史迁移是单独可暂停任务。正式开发完成且以下门禁全部通过后，才删除私有临时需求正文：接口、迁移、三 UI、调用方、测试、文档、证据、patch、manifest、rollback 和 UAT。

## 12. 生产化收口占位符

产品决策 Q24–Q39 已按推荐答案确认；下列值是生产化收口前需要写入 manifest/部署契约的占位符：

```text
PRESET_IMAGE_1_PATH / SHA256 / MIME / WIDTH / HEIGHT  # 路径/hash 已在 5.3 冻结，尺寸待验收
SYSTEM_SCOPE_REPRESENTATION       # nullable columns 或受控 sentinel，二选一并测透
DEFAULT_CALLER_KEYS               # 代码常量与 reconcile seed
SUPPORTED_LOCALES                 # 当前 zh-CN、en-US，可扩展
REDIS_INVALIDATION_CHANNEL        # 命名空间与 ACL
OUTBOX_RETRY_LIMIT / BACKOFF      # 与 jobs 默认值对齐
MEDIA_MAX_BYTES / TENANT_QUOTA    # 与配置中心最终 schema 对齐
PROVIDER_MIGRATION_OWNER          # 历史对象迁移负责人和到期日
```

这些占位符填入前，文档、测试和 API schema 使用 typed slots；示例键、路径、TTL 和 provider URL 仅作示例。

## 13. 关联文档

- [精简公开 API 对接参考](../integration/common-capabilities-api.md)
- [公开需求摘要](../requirements/common-capabilities.md)
- [公开文档索引](../README.md)
- [数据库迁移约定](../database-migration.md)

本文件同时描述当前实现与使用契约。每个后续切片完成后补充真实 API/OpenAPI 链接、migration 版本、错误码清单、
验证记录和回滚脚本路径，并更新“状态”字段；生产化门禁全部通过后再清理私有临时任务正文。
