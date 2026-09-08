export type Locale = 'zh-CN';

export const messages: Record<Locale, Record<string, string>> = {
  'zh-CN': {
    cancel: '取消',
    collapse: '收起',
    confirm: '确认',
    expand: '展开',
    prompt: '提示',
    reset: '重置',
    submit: '提交',
    toggleSidebar: '切换侧边栏',
    confirmTitle: '请确认',
    'formArray.action': '操作',
    'formArray.add': '添加一行',
    'formArray.empty': '暂无数据',
  },
};

export const getMessages = (locale: Locale) => messages[locale];
