export interface CapabilityGuide {
  title: string;
  audience: string;
  steps: string[];
  developer: string[];
  examples?: string[];
  locales?: Partial<Record<'en-US' | 'zh-CN', CapabilityGuideLocale>>;
}

export interface CapabilityGuideLocale {
  title: string;
  audience: string;
  steps: string[];
  developer: string[];
  examples?: string[];
}

export const commonCapabilitiesGuide = {
  mail: {
    title: 'SMTP integration guide',
    audience: 'For operators configuring reliable outbound email.',
    steps: [
      'On SMTP accounts, enter the host, port, sender identity, credentials, and TLS mode.',
      'Save and enable the account, then run Test connection to verify DNS, handshake, and authentication.',
      'On Callers, register the module key and choose the account pool, default account, and weighted or round-robin routing.',
      'On Templates, maintain subject, body, variables, and zh-CN/en-US variants; preview before publishing.',
      'On Verification policies, set length, charset, TTL, failure and rate limits; review results on Delivery records.',
    ],
    developer: [
      'Create a stable caller key and route it to one or more enabled SMTP account IDs; routing changes take effect without a service restart.',
      'Publish a locale-aware notification template, pass only declared variables, and use the template test action before rollout.',
      'Issue and verify challenges through the verification API; keep recipient validation, idempotency keys, and delivery IDs in the calling service.',
    ],
    examples: [
      'Template test: POST /api/admin/v1/notification/templates/{templateKey}/test with {"recipient":"user@example.test","locale":"zh-CN","variables":{"code":"123456"}}.',
      'Challenge flow: POST /api/admin/v1/notification/verification/challenges, then POST /api/admin/v1/notification/verification/challenges/{id}/verify with {"code":"123456"}.',
      'Delivery lookup: GET /api/admin/v1/mail/messages?status=failed&limit=20; correlate the returned message id with your request log.',
    ],
    locales: {
      'zh-CN': {
        title: 'SMTP 对接使用说明',
        audience: '面向配置可靠外发邮件的运维人员。',
        steps: [
          '在“SMTP 账户”页新建账号，填写主机、端口、发件人、认证信息和 TLS 模式。',
          '保存并启用账号，点击“测试连接”确认 DNS、握手和认证均通过。',
          '在“调用者”页登记模块调用键，选择账号池、默认账号及加权随机或轮询策略。',
          '在“通知模板”页维护主题、正文、变量和 zh-CN/en-US 多语言版本，先预览再发布。',
          '在“验证码策略”页设置位数、字符集、有效期、失败次数和频率；最后在“投递记录”页查询结果。',
        ],
        developer: [
          '先创建稳定的调用者键，并将其路由到一个或多个已启用 SMTP 账号；路由保存后无需重启服务即可生效。',
          '发布支持多语言的通知模板，只传入模板声明的变量，并在发布前使用模板测试。',
          '通过验证码挑战接口完成发送与校验；调用方负责收件人校验、幂等键和投递 ID 关联。',
        ],
        examples: [
          '模板测试：POST /api/admin/v1/notification/templates/{templateKey}/test，请求体 {"recipient":"user@example.test","locale":"zh-CN","variables":{"code":"123456"}}。',
          '验证码流程：先 POST /api/admin/v1/notification/verification/challenges，再用 {"code":"123456"} POST /api/admin/v1/notification/verification/challenges/{id}/verify。',
          '投递查询：GET /api/admin/v1/mail/messages?status=failed&limit=20，用返回的 message id 关联业务日志。',
        ],
      },
      'en-US': {
        title: 'SMTP integration guide',
        audience: 'For operators configuring reliable outbound email.',
        steps: [
          'On SMTP accounts, enter the host, port, sender identity, credentials, and TLS mode.',
          'Save and enable the account, then run Test connection to verify DNS, handshake, and authentication.',
          'On Callers, register the module key and choose the account pool, default account, and weighted or round-robin routing.',
          'On Templates, maintain subject, body, variables, and zh-CN/en-US variants; preview before publishing.',
          'On Verification policies, set length, charset, TTL, failure and rate limits; review results on Delivery records.',
        ],
        developer: [
          'Create a stable caller key and route it to one or more enabled SMTP account IDs; routing changes take effect without a service restart.',
          'Publish a locale-aware notification template, pass only declared variables, and use the template test action before rollout.',
          'Issue and verify challenges through the verification API; keep recipient validation, idempotency keys, and delivery IDs in the calling service.',
        ],
        examples: [
          'Template test: POST /api/admin/v1/notification/templates/{templateKey}/test with {"recipient":"user@example.test","locale":"en-US","variables":{"code":"123456"}}.',
          'Challenge flow: POST /api/admin/v1/notification/verification/challenges, then POST /api/admin/v1/notification/verification/challenges/{id}/verify with {"code":"123456"}.',
          'Delivery lookup: GET /api/admin/v1/mail/messages?status=failed&limit=20; correlate the returned message id with your request log.',
        ],
      },
    },
  },
  files: {
    title: 'Media library integration guide',
    audience: 'For teams selecting and uploading reusable media assets.',
    steps: [
      'Filter by category, choose an asset, and preview or download it.',
      'Upload an image or media file, choose its ACL, then select it from the list.',
      'Use signed URLs for temporary access and clean up stale objects with preview first.',
    ],
    developer: [
      'Use list, upload, preview, and signed URL APIs with tenant context.',
      'Treat non-image files as selectable only for their supported purpose; keep image pickers image-only.',
      'Validate MIME and size client-side for feedback; enforce limits on the server.',
    ],
    locales: {
      'zh-CN': {
        title: '媒体库对接使用说明',
        audience: '面向选择和上传可复用媒体资源的团队。',
        steps: [
          '按分类筛选资源，选择后预览或下载。',
          '上传图片或媒体文件，设置 ACL 后从资源库选择。',
          '使用短期签名 URL 临时访问，清理前先执行预览。',
        ],
        developer: [
          '在可信租户上下文中调用列表、上传、预览和签名 URL API。',
          '非图片资源在 Logo 选择器中保持置灰，业务按用途决定是否可选。',
          '客户端可先校验 MIME 和大小以改善反馈，服务端仍是最终限制。',
        ],
      },
      'en-US': {
        title: 'Media library integration guide',
        audience: 'For teams selecting and uploading reusable media assets.',
        steps: [
          'Filter by category, choose an asset, and preview or download it.',
          'Upload an image or media file, choose its ACL, then select it from the list.',
          'Use signed URLs for temporary access and clean up stale objects with preview first.',
        ],
        developer: [
          'Use list, upload, preview, and signed URL APIs with tenant context.',
          'Treat non-image files as selectable only for their supported purpose; keep image pickers image-only.',
          'Validate MIME and size client-side for feedback; enforce limits on the server.',
        ],
      },
    },
  },
} as const satisfies Record<'files' | 'mail', CapabilityGuide>;
