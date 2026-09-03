export interface CapabilityGuide {
  title: string;
  audience: string;
  steps: string[];
  developer: string[];
  locales?: Partial<Record<'en-US' | 'zh-CN', CapabilityGuideLocale>>;
}

export interface CapabilityGuideLocale {
  title: string;
  audience: string;
  steps: string[];
  developer: string[];
}

export const commonCapabilitiesGuide = {
  mail: {
    title: 'SMTP integration guide',
    audience: 'For operators configuring reliable outbound email.',
    steps: [
      'Create an SMTP account with host, port, sender identity, and TLS mode.',
      'Save the account, enable it, then run Test connection.',
      'Use weighted accounts for gradual rollout and review delivery records.',
    ],
    developer: [
      'Call the SMTP account API with tenant-scoped credentials.',
      'Store passwords server-side; send only redacted status to clients.',
      'Retry transient provider errors and correlate requests with delivery IDs.',
    ],
    locales: {
      'zh-CN': {
        title: 'SMTP 对接使用说明',
        audience: '面向配置可靠外发邮件的运维人员。',
        steps: [
          '新建 SMTP 账号，填写主机、端口、发件人和 TLS 模式。',
          '保存并启用账号，然后执行连接测试。',
          '使用加权账号池逐步发布，并查看投递记录。',
        ],
        developer: [
          '使用租户范围的凭据调用 SMTP 账号和通知公共 API。',
          '密码只在服务端保存，客户端只展示脱敏状态。',
          '对临时 provider 错误重试，并用投递 ID 关联请求。',
        ],
      },
      'en-US': {
        title: 'SMTP integration guide',
        audience: 'For operators configuring reliable outbound email.',
        steps: [
          'Create an SMTP account with host, port, sender identity, and TLS mode.',
          'Save the account, enable it, then run Test connection.',
          'Use weighted accounts for gradual rollout and review delivery records.',
        ],
        developer: [
          'Call the SMTP account API with tenant-scoped credentials.',
          'Store passwords server-side; send only redacted status to clients.',
          'Retry transient provider errors and correlate requests with delivery IDs.',
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
