# 公共能力需求：SMTP、验证码、通知与媒体库

> **状态：** `in-progress`（需求已确认，首个实现切片已落地）
> **批次：** `B1.7d-common-capabilities`
> **确认日期：** 2026-09-02
> **说明：** 本文是已确认需求的公开摘要；当前实现、契约和验收记录持续同步，未收口项以开发文档的差异表为准。

## 1. 目标

把邮件、验证码、通知和媒体资源整理为可复用的进程内公共能力。业务模块只依赖稳定的 Go
接口和 DTO，管理端负责配置、模板、测试和审计；三套管理端 UI（`web-antd`、`web-ele`、
`web-naive`）保持相同契约和交互语义。

| 编号 | 约束摘要 |
| --- | --- |
| `COMMON-001` | 以具名端口提供邮件、通知、验证码、媒体目录和媒体引用能力。 |
| `COMMON-002` | 统一 scope/principal/caller/locale/request context，并在服务层执行权限、审计与错误规范。 |
| `COMMON-003` | 设置快照、缓存失效、outbox/jobs、幂等、字典/i18n、分页、日志和指标采用可复用窄入口。 |
| `COMMON-004` | 三套 UI 共享 OpenAPI 字段、权限码、错误 key 和引导步骤 schema。 |

## 2. 已确认的功能需求

### 2.1 公共入口

提供以下具名端口，不建立无边界的万能 `internal/common` 服务：

- `MailSender`：按模板、语言和调用者策略发送通用邮件。
- `NotificationService`：发送密码变更成功、异地登录等通知类型邮件。
- `VerificationCodeService`：签发和校验验证码，业务方只接入发送与校验。
- `MediaCatalog`：上传、查询、读取、签名 URL、分类和软删除媒体资源。
- `MediaUsageService`：维护 Logo 等业务资源引用。

调用上下文包含 `tenant`、`org`、主体、稳定 `caller_key`、`locale` 和 request/trace ID。端口
内部统一执行权限、数据范围、审计、幂等和错误码；SMTP 凭据、对象存储 key、模板表字段和验证码
存储字段由公共服务管理。

### 2.2 SMTP、模板与调用者

1. 管理端可新建、编辑、启停调用者；预置调用者和管理端自身使用稳定不可变的 `caller_key`。
2. 每个调用者可配置允许使用的 SMTP 账号、默认账号、权重和策略：
   `weighted_random`、`round_robin`。禁用、不健康或冷却中的账号自动排除，并按配置回退。
3. 模板增删改只在管理端完成。模板支持系统默认、租户覆盖、变量 schema、zh-CN/en-US 多语言
   变体及语言回退（用户语言 → 租户默认 → 系统默认）。
4. 生产邮件走数据库事务 outbox 与 jobs relay；管理端测试发送使用独立同步路径，不创建验证码
   challenge。投递记录保存脱敏结果、provider message ID、重试次数和审计关联。
   模板测试请求包含受控收件人、locale 和变量，返回 `message_id/status/is_test`，并执行 allowlist、
   固定测试变量与审计校验。
   管理端入口为 `POST /api/admin/v1/notification/templates/{id}/test`，请求体为
   `{recipient, locale, variables}`，服务端固定 `is_test=true` 且不创建验证码 challenge。
5. 账号、调用者、路由、模板和通知策略保存最终态后即时生效；界面仅呈现最终态与审计，
   不设置业务版本号流程。运行时缓存失效在集群内目标为 5 秒以内；加密根密钥和 provider
   根拓扑仍按维护窗口处理。
6. 邮件服务管理页按“SMTP 账户、调用者、通知模板、验证码策略、投递记录”五个 Tag 组织操作；
   每个 Tag 保留独立列表、编辑/筛选和状态反馈，三套前端的路由、字段和权限语义一致。
7. 模板测试收件人为空或格式不正确时，提交动作必须在输入框附近给出可读错误并保持焦点；不得出现
   点击后无反馈的空状态。连接测试、模板测试和投递详情均展示成功/失败/处理中状态及可定位信息。

### 2.3 验证码与通知

- 默认策略：6 位纯数字、10 分钟有效期、最多 5 次失败、重发间隔 60 秒、每小时 5 次；后台可
  在约束范围内调整长度、字符集、TTL、失败次数、频率和模板。
- challenge 状态：`pending_send → active → consumed|expired|send_failed`；重发将旧记录标记为
  `superseded`，同一 caller/purpose/recipient 仅最新记录有效。
- 数据库 challenge 是权威；Redis 用于限流和热点摘要。验证码只保存 HMAC/加盐摘要，校验使用
  常时比较，成功原子消费。
- 幂等键范围：`(tenant, caller, purpose, recipient, client_key)`；同 payload 重试返回同一结果，
  不同 payload 返回冲突。

### 2.4 媒体库与 Logo

1. 资源作用域显式为 `system|tenant|org`，可见性按 `org → tenant → system` 合并，系统资源只读。
2. 列表支持 MIME 精确值、文件类型族、分类/子分类、游标分页和作用域过滤；响应只返回资源 ID、
   元数据、状态和受控 URL；provider key/path 留在服务内部。
3. `media_usages` 记录 Logo 等引用。软删除先检查引用，异步清理对象；业务配置只保存资源 ID。
4. 预置图片 1 在管理端可用前由 seed/reconcile 提前写入媒体库，使用稳定资源键 `system.logo.default`、manifest 和 SHA-256，可重复 reconcile；
   系统预置资源不计入租户配额，复制后的资源计入配额。
5. Logo 区域支持“从媒体库选择”和“直接上传”。选择器展示所有可见分类及子分类，图片可选，
   非图片以置灰禁用态展示；上传成功后自动选中并写入资源引用。

### 2.5 管理端引导 UI

SMTP 页面和媒体库页面各有“使用说明”入口，点击打开侧边抽屉。抽屉提供三个流程：

- **普通使用流程：** 进入页面 → 配置/上传 → 测试 → 保存 → 即时生效 → 查看结果/故障提示。
- **开发者对接流程：** 权限 → caller_key → Go 端口 → 参数与 locale → 幂等 → 错误处理 → 验收/回滚。
- **调用示例：** 展示可复制的模板测试、验证码签发/校验和投递记录查询请求，并标明占位符替换规则。

步骤 ID、代码块、权限、错误 key 和中英文文案使用共享 schema；三套 UI 的顺序、焦点、键盘关闭、
响应式和已读状态（用户级保存、租户可重置）保持一致。

## 3. 其他优先提取的公共代码

按现有实现建立窄入口并配套契约测试：租户/认证上下文、RBAC/数据范围、设置快照与缓存失效、
审计事件、任务队列/outbox、幂等键、字典/i18n、统一错误与分页、request/trace ID、结构化日志、
指标和健康检查。短信、Webhook、推送、Feature Flag、全文媒体搜索和标签体系列入后续路线。

## 4. 实施计划与当前进度

| 阶段 | 交付 |
|---|---|
| P0 | **已完成首轮**：Go/OpenAPI 端口、DTO、错误码映射、权限边界、迁移草案、状态机和共享引导 schema 已冻结。 |
| P1 | **已落地切片**：caller/account/template/策略运行时、最终态更新和热更新入口已提供；持久化审计接线继续收口。 |
| P2 | **部分完成**：测试发送、验证码状态/限流/幂等已提供；持久化 outbox/jobs relay 与失败补偿待收口。 |
| P3 | **已落地切片**：`MediaCatalog`、分类/作用域/引用、provider seam、预置资源 reconcile 和迁移模型已提供。 |
| P4 | **已落地切片**：管理 API、OpenAPI 路径和三套 UI 的 SMTP/媒体库引导已接入；邮件服务五个 Tag、模板测试收件人校验和投递记录筛选已实现，Logo 业务接线继续完善。 |
| P5 | **已落地切片**：共享双语引导 schema、普通/开发者/示例三段侧边抽屉、焦点/Escape/响应式行为已接入并进行一致性验收。 |
| P6 | **待执行**：将真实密码找回或通知调用方切换到公共端口，完成端到端回归。 |
| P7 | **待执行**：双库迁移、Go/前端测试、E2E、axe、证据、回滚和 UAT 全部收口；完成后清理临时任务正文。 |

## 5. 验收门槛

- 多租户/组织边界和 system/tenant/org 继承结果一致。
- SMTP 账号路由、禁用/故障回退、多语言模板和即时生效在单实例与集群验证通过。
- outbox 重试、重复投递窗口、验证码过期/失败/限流/并发校验均有测试记录。
- 媒体 MIME/分类查询、非图片禁用、Logo 引用保护、预置资源 reconcile 和配额规则通过。
- 三套 UI 的路由、权限、字段、错误文案、抽屉流程和键盘/响应式行为等价。
- 敏感字段脱敏；迁移、patch、manifest、verification record 和可执行 rollback 可复核。

## 6. 文档入口

- [精简公开 API 对接参考](../integration/common-capabilities-api.md)
- [完整公共能力开发使用文档](../development/common-capabilities.md)
- [公开文档总览](../README.md)

实现以本文和精简 API 参考作为对接契约；当前代码路径、migration 草案、OpenAPI 操作 ID、错误码清单
和验证记录已在开发/对接文档中维护，后续收口后再补齐生产部署证据。
