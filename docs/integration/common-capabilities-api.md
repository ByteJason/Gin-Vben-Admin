# 公共能力 API 对接参考

> **状态：** `implemented-slice`（公共端口、运行时、管理 API 与 OpenAPI 首个切片已落地）
> **批次：** `B1.7d-common-capabilities`
> **更新时间：** 2026-09-03

本文是业务开发者的直接对接入口：列出已实现文件、导出方法、参数、返回值、稳定错误和 Demo。
业务模块在同一 Go 进程内通过端口调用；管理端 HTTP 只负责配置、资源维护和受控测试发送。
持久化 outbox/relay、真实 provider 健康回退和完整业务调用方迁移仍按开发文档收口；接线时以本文和
`contracts/openapi/admin-v1.yaml` 的 operationId 为准。
状态机、迁移、热更新、测试与回滚细节见
[`docs/development/common-capabilities.md`](../development/common-capabilities.md)。

## 1. 文件与导出符号

| 能力 | 实现文件 | 主要导出符号 | 用途 |
| --- | --- | --- | --- |
| SMTP 邮件 | `server/internal/application/mail/ports.go` | `MailSender`、`SendRequest`、别名 `SendResult` | 通用模板邮件 |
| 投递 DTO | `server/internal/application/notification/ports.go` | `SendMode`、`DeliveryStatus`、`Recipient`、`SendResult` | mail/notification 共用，避免 import cycle |
| 通知 | `server/internal/application/notification/ports.go` | `NotificationService`、`NotificationRequest` | 密码变更、异地登录等通知 |
| 验证码 | `server/internal/application/notification/ports.go` | `VerificationCodeService`、`IssueRequest`、`VerifyRequest` | 签发、发送、校验、消费 |
| 媒体目录 | `server/internal/application/file/ports.go` | `MediaCatalog`、`MediaFilter`、`ResourceRef` | 上传、查询、读取、签名 URL、分类 |
| 媒体引用 | `server/internal/application/file/ports.go` | `MediaUsageService`、`UsageInput`、`DetachRequest` | Logo 等业务引用 |
| 管理 HTTP | `server/internal/transport/http/commoncapabilities/handler.go` | handlers/routes | 管理配置和受控测试 |
| OpenAPI | `contracts/openapi/admin-v1.yaml` | schemas/paths | 生成管理端 client |
| 生成 client | `admin/packages/api-client/src/generated/admin-v1.ts` | 请求/响应类型 | 三套 UI 共用 |
| 引导 schema | `admin/packages/types/src/common-capabilities-guide.ts` | 步骤、locale、权限、错误 key | 三套 UI 共用 |
| 首个调用方 | `server/internal/bootstrap/http.go` | 依赖注入、默认 caller/template/policy 与预置资源 reconcile | P6 端到端验收 |

现有 UI adapter 继续在三套同名文件扩展：

- SMTP：`admin/apps/web-antd/src/api/core/mail.ts`、`admin/apps/web-ele/src/api/core/mail.ts`、
  `admin/apps/web-naive/src/api/core/mail.ts`；页面为 `admin/apps/web-antd/src/views/system/mail/index.vue`、
  `admin/apps/web-ele/src/views/system/mail/index.vue`、`admin/apps/web-naive/src/views/system/mail/index.vue`。
- 媒体：`admin/apps/web-antd/src/api/core/files.ts`、`admin/apps/web-ele/src/api/core/files.ts`、
  `admin/apps/web-naive/src/api/core/files.ts`；页面为 `admin/apps/web-antd/src/views/system/files/index.vue`、
  `admin/apps/web-ele/src/views/system/files/index.vue`、`admin/apps/web-naive/src/views/system/files/index.vue`。

配套实现文件：`server/internal/platform/persistence/model/admin_mail_models.go`、
`server/internal/platform/persistence/model/admin_common_capabilities_models.go`、
`server/migrations/versions/admin/v002_common_capabilities.go`、
`server/internal/application/auth/metadata.go`、`server/internal/application/settings/runtime_snapshot.go`、
`server/internal/transport/http/commoncapabilities/handler.go` 和
`admin/packages/types/src/common-capabilities-guide.ts`。持久化 outbox/jobs relay 与完整业务调用方迁移
仍是后续收口项。

## 2. 调用上下文与公共约定

每个方法接收 `context.Context`。当前 `tenant.Context` 提供 `TenantID`、`Organization`、
`PlatformAdmin`；`server/internal/application/auth/metadata.go` 与 bootstrap 接线提供可信
`CallerKey`、`Locale`、request/trace metadata。HTTP body/header 中同名 caller 字段不参与身份判定。

```go
ctx = tenant.WithContext(ctx, tenant.Context{
    TenantID:     "tenant-id",
    Organization: "org-id", // 无组织范围时省略
})
// CallerKey、Locale、RequestID、TraceID 由可信应用 metadata 安装。
```

| 约定 | 规则 |
| --- | --- |
| caller | 后台登记的稳定 `caller_key`；服务端校验与可信 context 一致；它是代码注册键，不发放模块 API secret；账号凭据由服务内部保存 |
| scope | 显式 `system`、`tenant`、`org`；媒体可见性按 `org → tenant → system` 合并，system 资源只读 |
| locale | 显式 locale → 用户语言 → 租户默认 → 系统默认（`zh-CN`、`en-US`） |
| 幂等 | 产生副作用的 Go 请求带 `IdempotencyKey`；相同键/相同 payload 重试返回同一结果，payload 变化返回冲突；AdminTest 可由服务生成键 |
| 敏感数据 | 调用方不传 SMTP 账号 ID/密码、object key、验证码摘要；响应和日志只返回脱敏摘要 |
| 分页 | `List` 使用有上限的 cursor；排序固定 `created_at DESC, id DESC`；`offset` 仅为旧客户端兼容，和 cursor 同时传时 cursor 优先 |

包归属：`notification` 的 `ports.go` 负责 provider-neutral 的 `SendMode`、`DeliveryStatus`、
`SendResult`、`Recipient` 及通知/验证码请求；`mail/ports.go` 负责 `MailSender`、`SendRequest`，
并以类型别名暴露上述投递 DTO。这样保持现有 `mail → notification` provider 依赖方向，不形成 Go
import cycle。`file` 包负责媒体 ID、过滤器、资源 DTO、分类、`ScopeType`、`ACL`、`MediaStatus` 及 `URLPurpose`。

## 3. SMTP 邮件

**文件：** `server/internal/application/mail/ports.go`

```go
// mail/ports.go
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
    Mode           SendMode // Production 或 AdminTest
}
```

| 方法 | 必填/关键入参 | 返回 |
| --- | --- | --- |
| `MailSender.Send` | 已启用 caller、已发布 `TemplateKey`、至少一名合法收件人、模板变量、可选 `Locale`、生产幂等键、`Mode` | `SendResult`；当前内存/兼容 adapter 有可用 mailer 时返回 `sent`，生产 outbox 接线后可先返回 `queued`，再由 relay 变为 `sent` 或 `failed` |

管理端按 caller 维护 SMTP 账号 allowlist、默认账号、权重和 `weighted_random`/`round_robin` 策略；
禁用、不健康或冷却账号在 dispatch 时排除。模板 CRUD、发布和多语言变体只在管理端完成。
管理端自身使用预置 caller（键 `system.admin`）；其他模块由后台登记稳定 `caller_key` 后接入。

## 4. 通知与验证码

**文件：** `server/internal/application/notification/ports.go`

### 4.1 通知

```go
type SendMode string       // Production、AdminTest
type DeliveryStatus string // queued、sent、failed
type Recipient struct {
    Address string // to/cc/bcc
    Kind    string
}
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
    CallerKey      string
    Purpose        string // security.password-changed、security.unusual-login 等
    Recipients     []Recipient
    Variables      map[string]string
    Locale         string
    IdempotencyKey string
    Mode           SendMode
}
```

`Purpose` 解析已发布模板、账号路由和审计策略；邮件主题和正文来自模板。

### 4.2 验证码

```go
type VerificationCodeService interface {
    Issue(context.Context, IssueRequest) (ChallengeRef, error)
    Verify(context.Context, VerifyRequest) error
}

type IssueRequest struct {
    CallerKey      string
    Purpose        string // email_change、password_reset 等
    Recipient      string
    Locale         string
    Variables      map[string]string
    IdempotencyKey string
}

type ChallengeRef struct {
    ID        string
    ExpiresAt time.Time
    Status    string // pending_send、active、consumed、expired、superseded、locked、send_failed
}

type VerifyRequest struct {
    ChallengeID    string
    Code            string
    IdempotencyKey string
}
```

| 方法 | 入参 | 返回/语义 |
| --- | --- | --- |
| `NotificationService.Send` | caller、受控 `Purpose`、`notification.Recipient`、变量、locale、幂等键、发送模式 | `notification.SendResult`（`mail.SendResult` 是别名） |
| `VerificationCodeService.Issue` | caller、purpose、规范化收件人、locale、变量、幂等键 | `ChallengeRef`；验证码邮件写入 outbox |
| `VerificationCodeService.Verify` | challenge ID、用户输入 code、幂等键 | `nil` 表示原子消费成功；失败返回 `verification_*` 错误 key |

默认验证码策略：6 位纯数字、TTL 10 分钟、失败上限 5 次、重发间隔 60 秒、每小时 5 次。
状态为 `pending_send → active → consumed|expired|send_failed`；重发将旧 challenge 标记为
`superseded`，数据库是权威状态，Redis 只做限流和热点摘要。

## 5. 媒体目录与引用

**文件：** `server/internal/application/file/ports.go`

```go
type ResourceID string
type CategoryID string
type URLPurpose string // preview、download
type ScopeType string  // system、tenant、org
type ACL string        // private、public-read
type MediaStatus string // pending、ready、failed、deleting、deleted

type UploadInput struct {
    Reader         io.Reader
    Size           int64
    Name           string
    MIME           string
    ACL            ACL
    CategoryID     CategoryID
    Metadata       map[string]string
    IdempotencyKey string
}
type OpenOptions struct{ RangeStart, RangeEnd *int64 }
type URLRequest struct{ Purpose URLPurpose; TTL time.Duration }
type URLRef struct{ URL string; ExpiresAt time.Time }
type DeleteOptions struct{ Reason, IdempotencyKey string }
type MediaFilter struct {
    MIMEExact, MIMEFamily string // 例如 image/*
    CategoryID            CategoryID
    ScopeType             ScopeType
    IncludeDescendants    bool
    OwnerID, Cursor        string
    Offset                int  // 旧客户端兼容；同时提供 cursor 时 cursor 优先
    Limit                 int
    Status                MediaStatus // 缺省 ready
}
type ResourceRef struct {
    ID              ResourceID
    Name            string
    MIME            string
    Size            int64
    SHA256          string
    CategoryID      CategoryID
    ScopeType       ScopeType
    ACL             ACL
    Status          MediaStatus
    CreatedAt       time.Time
    UpdatedAt       time.Time
    URLHints        map[string]bool
    Selectable      bool
    DisabledReason  string
}
type MediaPage struct {
    Items      []ResourceRef
    NextCursor string
    HasMore    bool
}
type CategoryFilter struct {
    ParentID CategoryID
    ScopeType ScopeType
    IncludeDescendants bool
}
type CategoryRef struct { ID CategoryID; Name, Path string; ScopeType ScopeType }
type CategoryInput struct { ParentID CategoryID; Name, IdempotencyKey string }
type CategoryPatch struct { Name *string; Enabled *bool; IdempotencyKey string }
type CategoryDeleteRequest struct { ID CategoryID; IdempotencyKey string }
type UsageRef struct {
    ID string
    ResourceID ResourceID
    Module, EntityType, EntityID, Field string
}
type UsageInput struct {
    ResourceID ResourceID
    Module, EntityType, EntityID, Field string
    IdempotencyKey string
}
type DetachRequest struct { UsageID, IdempotencyKey string }
```

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

type MediaUsageService interface {
    Attach(context.Context, UsageInput) (UsageRef, error)
    Detach(context.Context, DetachRequest) error
    ListByResource(context.Context, ResourceID) ([]UsageRef, error)
}
```

| 方法 | 入参 | 返回/语义 |
| --- | --- | --- |
| `Upload` | reader、大小、名称、MIME、ACL、分类、metadata、幂等键 | `ResourceRef`；先 `pending`，校验后 `ready` |
| `Get` | `ResourceID` | 单个 `ResourceRef` |
| `List` | MIME exact/family、scope、分类树、owner、cursor、limit、status；旧客户端可传 offset | `MediaPage`；只返回 metadata，cursor 优先于 offset |
| `Open` | `ResourceID`、可选单一 Range | `io.ReadCloser`，调用方关闭；HTTP 适配器用 `Range: bytes=start-end` 返回 206/Content-Range，多段 Range 返回 416 |
| `SignedURL` | `ResourceID`、`URLPurpose`、TTL | `URLRef{URL, ExpiresAt}` |
| `Delete` | `ResourceID`、原因、幂等键 | `nil`；有引用返回 `media_in_use` |
| `ListCategories` | scope、父节点、是否含子树 | `[]CategoryRef` |
| `CreateCategory` / `UpdateCategory` | 分类输入或 patch（含幂等键） | `CategoryRef` |
| `DeleteCategory` | `CategoryDeleteRequest` | `nil`；非空分类返回 `category_not_empty` |
| `Attach` | 资源 ID、模块、实体类型/ID、字段、幂等键 | `UsageRef`；重复绑定返回同一引用 |
| `Detach` | `DetachRequest` | `nil` |
| `ListByResource` | `ResourceID` | `[]UsageRef` |

“整个资源库”通过 `NextCursor` 分页迭代或异步导出任务获取。业务配置只保存 opaque `ResourceID`；
不保存 object key、provider 名称或永久 URL。Logo 选择器传 `MIMEFamily: "image/*"`、分类和
`IncludeDescendants: true`；所有可见资源都显示，非图片返回 `Selectable=false`、
`DisabledReason="media_type_not_allowed"`。seed/reconcile 先写入 `system.logo.default` 预置图片；
上传成功后自动 `Attach(module="branding", field="logo")`。

## 6. Go Demo

示例所在模块可按实际包名导入（`internal` 规则要求调用方位于同一仓库模块内）：

```go
import (
    "context"
    "time"

    "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/file"
    "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/mail"
    "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
)
```

### 6.1 模板邮件

```go
result, err := mailer.Send(ctx, mail.SendRequest{
    CallerKey: "billing.invoice-ready", TemplateKey: "billing.invoice-ready",
    Recipients: []mail.Recipient{{Address: user.Email, Kind: "to"}},
    Variables: map[string]string{"invoice_no": invoice.Number}, Locale: user.Locale,
    IdempotencyKey: "invoice-ready:" + invoice.ID,
})
if err != nil { return err }
log.Printf("mail %s: %s", result.MessageID, result.Status)
```

### 6.2 通知

```go
result, err := notify.Send(ctx, notification.NotificationRequest{
    CallerKey: "security.password-changed", Purpose: "security.password-changed",
    Recipients: []notification.Recipient{{Address: user.Email, Kind: "to"}},
    Variables: map[string]string{"display_name": user.DisplayName}, Locale: user.Locale,
    IdempotencyKey: "password-change:" + changeID,
})
if err != nil { return err }
log.Printf("notification %s: %s", result.MessageID, result.Status)
```

### 6.3 验证码

```go
challenge, err := verify.Issue(ctx, notification.IssueRequest{
    CallerKey: "auth.email-change", Purpose: "email_change", Recipient: email,
    Locale: user.Locale, IdempotencyKey: "email-change:" + changeID,
})
if err != nil { return err }
if err := verify.Verify(ctx, notification.VerifyRequest{
    ChallengeID: challenge.ID, Code: input.Code,
    IdempotencyKey: "email-change:verify:" + challenge.ID,
}); err != nil { return err }
```

### 6.4 查询图片并生成预览地址

```go
page, err := media.List(ctx, file.MediaFilter{
    MIMEFamily: "image/*", CategoryID: categoryID, IncludeDescendants: true,
    Limit: 50, Cursor: cursor,
})
if err != nil { return err }
for _, resource := range page.Items {
    preview, err := media.SignedURL(ctx, resource.ID, file.URLRequest{
        Purpose: file.URLPurpose("preview"), TTL: 15 * time.Minute,
    })
    if err != nil { return err }
    render(resource.Name, preview.URL)
}
```

## 7. 管理端 HTTP（已实现路径）

业务模块优先使用 Go 端口。以下路径已由 `server/internal/transport/http/commoncapabilities/handler.go`
挂载；operationId、schema 和权限以 `contracts/openapi/admin-v1.yaml` 为准。
权限 key 使用 `notification:callers:*`、`notification:templates:*`、
`notification:verification:*`、`system:mail:*`、`media:library:*`；Logo 设置沿用
`system:settings:read/manage`，媒体引用沿用 `media:library:manage`。

| 方法 | 路径 | 作用/返回 |
| --- | --- | --- |
| `GET/POST` | `/api/admin/v1/notification/callers` | 调用者查询/创建 |
| `GET/PATCH/DELETE` | `/api/admin/v1/notification/callers/{id}` | 调用者最终态 |
| `GET/POST` | `/api/admin/v1/notification/templates` | 草稿、语言变体 |
| `PATCH/DELETE` | `/api/admin/v1/notification/templates/{id}` | 保存、停用/删除模板 |
| `POST` | `/api/admin/v1/notification/templates/{id}/publish` | 发布模板 |
| `POST` | `/api/admin/v1/notification/templates/{id}/test` | 测试邮件；返回 `message_id/status/is_test` |
| `GET` | `/api/admin/v1/notification/verification-policies` | 策略集合 |
| `PATCH` | `/api/admin/v1/notification/verification-policies/{policy_key}` | 指定策略最终态 |
| `POST` | `/api/admin/v1/notification/verification/challenges` | 签发并发送验证码，返回 `id/expiresAt/status` |
| `GET` | `/api/admin/v1/notification/verification/challenges/{id}` | 查询挑战状态 |
| `POST` | `/api/admin/v1/notification/verification/challenges/{id}/verify` | 校验并消费验证码，返回 `verified=true` |
| `GET/POST` | `/api/admin/v1/media/library` | 查询/上传资源 |
| `PATCH` | `/api/admin/v1/media/library/{id}` | 更新资源 metadata/category/status |
| `DELETE` | `/api/admin/v1/media/library/{id}` | 软删除资源（有引用返回 `media_in_use`） |
| `GET/POST` | `/api/admin/v1/media/categories` | 分类查询/创建 |
| `PATCH/DELETE` | `/api/admin/v1/media/categories/{id}` | 分类更新/删除 |
| `GET` | `/api/admin/v1/media/resources/{id}` | 资源 metadata |
| `GET` | `/api/admin/v1/media/resources/{id}/open` | 受控二进制流 |
| `GET` | `/api/admin/v1/media/resources/{id}/signed-url` | 预览/下载短期 URL |
| `GET` | `/api/admin/v1/media/library/{id}/usage` | 查询指定资源的引用 |
| `GET/PUT` | `/api/admin/v1/settings/branding` | Logo resource ID 设置（JSON value）；保存后由 UI adapter 绑定 `media_usage` |

媒体列表的 canonical 查询参数是 `mime`；当前兼容 adapter 同时接受 `mimeExact` 和 `mimeFamily`。
Branding 更新请求的 `value` 必须是结构化 JSON，例如
`{"value":{"logoResourceId":"RESOURCE_ID"},"expectedVersion":3}`，不要预先 `JSON.stringify`。

模板测试请求体为 `{recipient, locale?, variables?}`；服务端固定 `is_test=true`，不创建验证码 challenge，
未提供的已声明变量会按语言填充确定性的示例值（例如 `code=123456`、`expires_in=10 分钟`），调用方提供的值优先。
响应为 `{message_id, status, is_test}`。收件人 allowlist 的配置/策略存储仍在 P2/P7 收口，当前切片不会把任意测试地址
误报为已通过生产 allowlist。变量名或收件人不合法时，错误 `errors[0]` 会返回稳定 `messageKey` 与 `params.variable`。
HTTP mutation 使用 `Idempotency-Key`；管理端 AdminTest 未提供时可由服务端生成。

```bash
curl -X POST "$ADMIN_ORIGIN/api/admin/v1/notification/templates/TEMPLATE_ID/test" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: admin-test:TEMPLATE_ID:REQUEST_ID' \
  -d '{"recipient":"developer@example.test","locale":"zh-CN","variables":{"name":"Demo"}}'
# {"message_id":"MESSAGE_ID","status":"sent","is_test":true}
```

验证码管理端调用示例（实际业务模块优先使用 Go `VerificationCodeService`）：

```bash
curl -X POST "$ADMIN_ORIGIN/api/admin/v1/notification/verification/challenges" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: email-change:REQUEST_ID' \
  -d '{"callerKey":"auth.email-change","purpose":"email_change","recipient":"user@example.test","locale":"zh-CN"}'
# {"code":0,"data":{"id":"CHALLENGE_ID","expiresAt":"2026-09-03T02:00:00Z","status":"active"}}

curl -X POST "$ADMIN_ORIGIN/api/admin/v1/notification/verification/challenges/CHALLENGE_ID/verify" \
  -H 'Content-Type: application/json' \
  -d '{"code":"123456"}'
# {"code":0,"data":{"verified":true}}
```

迁移窗口内的当前兼容端点和前端函数：

| 方法 | 当前路径 | operationId / adapter |
| --- | --- | --- |
| `GET/POST` | `/api/admin/v1/mail/accounts` | `listSMTPAccounts` / `createSMTPAccount` |
| `PUT/DELETE` | `/api/admin/v1/mail/accounts/{id}` | `updateSMTPAccount` / `deleteSMTPAccount` |
| `POST` | `/api/admin/v1/mail/accounts/{id}/test` | `testSMTPAccount` |
| `GET/POST` | `/api/admin/v1/mail/messages` | `listEmailMessages` / `sendEmailMessage` |
| `GET` | `/api/admin/v1/mail/messages/{id}` | `getEmailMessage` |
| `GET` | `/api/admin/v1/files` | `listFiles`；旧 offset 分页 |
| `POST` | `/api/admin/v1/files/upload` | `uploadFile` |
| `GET/POST` | `/api/admin/v1/files/categories` | `listFileCategories` / `createFileCategory` |
| `PUT/PATCH/DELETE` | `/api/admin/v1/files/categories/{id}` | `updateFileCategory` / `patchFileCategory` / `deleteFileCategory` |
| `GET/DELETE` | `/api/admin/v1/files/{id}` | `getFile` / `deleteFile` |
| `GET` | `/api/admin/v1/files/{id}/download`、`/api/admin/v1/files/{id}/preview` | `downloadFile` / `previewFile` |
| `POST` | `/api/admin/v1/files/{id}/signed-url` | `signFileURL` |
| `GET` | `/api/admin/v1/files/cleanup/dry-run` | `fileCleanupDryRun` / `cleanupDryRunApi` |

三套 UI 共用生成 client、权限码、错误 key 和引导步骤 schema；SMTP/媒体库页面的“使用说明”入口
打开侧边抽屉，固定提供“普通使用流程”和“开发者对接流程”。当前 files adapter 的导出函数包括
`listFilesApi`、`listFileCategoriesApi`、`createFileCategoryApi`、`updateFileCategoryApi`、
`deleteFileCategoryApi`、`uploadFileApi`、`getFileApi`、`downloadFileApi`、`deleteFileApi`、
`signedFileUrlApi`、`cleanupDryRunApi`、`listMediaResourcesApi`、`uploadMediaResourceApi`、
`getMediaResourceApi`、`updateMediaResourceApi`、`deleteMediaResourceApi`、`openMediaResourceApi`、
`signMediaResourceUrlApi`、`listMediaUsagesApi`；mail adapter 包括 `listSMTPAccountsApi`、
`saveSMTPAccountApi`、`testSMTPAccountApi`、`deleteSMTPAccountApi`、`listEmailMessagesApi`。
OpenAPI 兼容表中的 `getEmailMessage`/`sendEmailMessage` 当前没有同名 UI adapter，业务模块应使用
Go 端口；完整 UI 导出以各套 `src/api/core/{files,mail}.ts` 为准。

## 8. 稳定错误 key

| 错误 key | 处理方向 |
| --- | --- |
| `scope_required`、`scope_denied` | 补齐可信 scope，检查数据范围 |
| `caller_not_found`、`caller_disabled` | 检查后台 caller 与代码键 |
| `template_unpublished`、`template_variable_missing` | 发布模板或补齐变量 |
| `test_recipient_not_allowed`（P2/P7 收口后启用）、`template_test_variables_invalid` | 检查测试收件人 allowlist 和固定变量；当前切片只执行固定变量校验 |
| `mail_no_account`、`mail_policy_invalid` | 检查账号启用、绑定、权重和健康状态 |
| `verification_rate_limited`、`verification_expired`、`verification_locked` | 按 `retry_after` 重新签发 |
| `verification_not_active`、`verification_consumed` | 查询 challenge 状态 |
| `media_not_found`、`media_not_ready` | 重新获取 metadata 或等待 ready |
| `media_type_not_allowed`、`media_hash_conflict` | 修正文件或选择已有资源 |
| `media_in_use`、`category_not_empty` | 解除引用或移动资源 |
| `config_conflict` | 重新读取 ETag/updated_at 后保存 |

## 9. 实施状态

- Go 端口、内存运行时、管理 handler、OpenAPI challenge/media 路径、模型/迁移草案和三套 UI 引导
  已在当前实现切片中提供；生成 client 的 endpoint 常量由 `scripts/generate-openapi.mjs` 校验。
- 生产化收口包括持久化 outbox/relay、真实 provider 健康回退、Logo 设置与 usage 的服务端事务化、真实调用方迁移、
  双库回归和 UAT。收口前保留兼容后端 API `/api/admin/v1/files` 与前端 UI 路由 `/system/files`，
  由 adapter 转发到 `MediaCatalog`。
- 每次接线应先运行 `node scripts/generate-openapi.mjs --check`、对应 Go focused/race 测试和三套
  前端 typecheck；验证字面量输出及 rollback 路径见开发文档的证据目录。
