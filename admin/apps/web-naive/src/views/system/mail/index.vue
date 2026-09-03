<script setup lang="ts">
import type {
  EmailMessage,
  NotificationCaller,
  NotificationTemplate,
  SMTPAccount,
  SMTPAccountInput,
  VerificationPolicy,
} from '#/api/core/mail';

import { computed, nextTick, onMounted, reactive, ref } from 'vue';

import { useAccess } from '@vben/access';
import { ManagementPage, notify } from '@vben/common-ui';
import { preferences } from '@vben/preferences';
import { commonCapabilitiesGuide } from '@vben/types';

import {
  deleteNotificationCallerApi,
  deleteNotificationTemplateApi,
  deleteSMTPAccountApi,
  getEmailMessageApi,
  listEmailMessagesApi,
  listNotificationCallersApi,
  listNotificationTemplatesApi,
  listSMTPAccountsApi,
  listVerificationPoliciesApi,
  publishNotificationTemplateApi,
  saveNotificationCallerApi,
  saveNotificationTemplateApi,
  saveSMTPAccountApi,
  testNotificationTemplateApi,
  testSMTPAccountApi,
  updateVerificationPolicyApi,
} from '#/api/core/mail';
import { $t } from '#/locales';

const { hasAccessByCodes } = useAccess();
const canManage = computed(() => hasAccessByCodes(['system:mail:manage']));

const emptyForm = (): SMTPAccountInput => ({
  name: '',
  enabled: true,
  host: '',
  port: 587,
  username: '',
  password: '',
  weight: 1,
  fromEmail: '',
  fromName: '',
  implicitTls: false,
});

const accounts = ref<SMTPAccount[]>([]);
const messages = ref<EmailMessage[]>([]);
const form = reactive<SMTPAccountInput>(emptyForm());
const editingId = ref<string>();
const loading = ref(false);
const saving = ref(false);
const testingId = ref('');
const guideOpen = ref(false);
const guideSection = ref<'operator' | 'developer' | 'examples'>('operator');
const guideDrawer = ref<HTMLElement | null>(null);
let guideReturnFocus: HTMLElement | null = null;
const guide = computed(
  () =>
    commonCapabilitiesGuide.mail.locales?.[
      preferences.app.locale === 'zh-CN' ? 'zh-CN' : 'en-US'
    ] ?? commonCapabilitiesGuide.mail,
);
async function openGuide() {
  guideReturnFocus =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
  guideOpen.value = true;
  guideSection.value = 'operator';
  await nextTick();
  guideDrawer.value?.focus();
}
function closeGuide() {
  guideOpen.value = false;
  const target = guideReturnFocus;
  guideReturnFocus = null;
  void nextTick(() => target?.focus());
}
const testResult = ref('');
type TestFeedbackTone = 'error' | 'info' | 'success';
type TestFeedback = { message: string; tone: TestFeedbackTone };
const testResultTone = ref<TestFeedbackTone>('info');
const accountTestFeedback = reactive<Record<string, TestFeedback>>({});
const templateTestFeedback = reactive<Record<string, TestFeedback>>({});
const accountTestAt = reactive<Record<string, string>>({});

type MailTab = 'accounts' | 'callers' | 'templates' | 'policies' | 'records';
const activeTab = ref<MailTab>('accounts');

const templateRecipientInput = ref<HTMLInputElement | null>(null);
const templateRecipientError = ref('');
const templateLocaleEditor = ref<'en-US' | 'zh-CN'>('zh-CN');

type RecordRange = '7d' | '30d' | 'all';
type RecordSource = 'all' | 'business' | 'system' | 'template_test';
type RecordFilters = {
  accountId: string;
  keyword: string;
  range: RecordRange;
  source: RecordSource;
  status: string;
};
const recordFilters = reactive<RecordFilters>({
  accountId: '',
  keyword: '',
  range: '7d',
  source: 'all',
  status: '',
});
const recordPage = ref(1);
const recordPageSize = ref(10);
const recordsLoading = ref(false);
const recordDetail = ref<EmailMessage | null>(null);
const recordDetailLoading = ref(false);
const lastSyncedAt = ref('');
const recordRows = computed(() => {
  const keyword = recordFilters.keyword.trim().toLowerCase();
  const since =
    recordFilters.range === '7d'
      ? Date.now() - 7 * 24 * 60 * 60 * 1000
      : recordFilters.range === '30d'
        ? Date.now() - 30 * 24 * 60 * 60 * 1000
        : 0;
  return messages.value.filter((message) => {
    const createdAt = Date.parse(message.createdAt);
    if (since > 0 && (!Number.isFinite(createdAt) || createdAt < since))
      return false;
    if (recordFilters.status && message.status !== recordFilters.status)
      return false;
    if (
      recordFilters.accountId &&
      message.smtpAccountId !== recordFilters.accountId
    )
      return false;
    if (
      recordFilters.source !== 'all' &&
      messageSource(message) !== recordFilters.source
    )
      return false;
    if (keyword) {
      const haystack = [
        message.subject,
        message.recipients.map((recipient) => recipient.address).join(' '),
        message.callerKey,
        message.templateKey,
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
      if (!haystack.includes(keyword)) return false;
    }
    return true;
  });
});
const recordTotalPages = computed(() =>
  Math.max(1, Math.ceil(recordRows.value.length / recordPageSize.value)),
);
const visibleMessages = computed(() => {
  const start = (recordPage.value - 1) * recordPageSize.value;
  return recordRows.value.slice(start, start + recordPageSize.value);
});

function selectTab(tab: MailTab) {
  activeTab.value = tab;
  if (tab === 'records' && messages.value.length === 0) void loadRecords();
}

function onTabKeydown(event: KeyboardEvent, index: number) {
  const keys = [
    'ArrowRight',
    'ArrowDown',
    'ArrowLeft',
    'ArrowUp',
    'Home',
    'End',
  ];
  if (!keys.includes(event.key)) return;
  event.preventDefault();
  const next =
    event.key === 'Home'
      ? 0
      : event.key === 'End'
        ? mailTabs.value.length - 1
        : (index +
            (event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : -1) +
            mailTabs.value.length) %
          mailTabs.value.length;
  const tab = mailTabs.value[next];
  if (!tab) return;
  selectTab(tab.key);
  void nextTick(() => document.getElementById(`mail-tab-${tab.key}`)?.focus());
}

type RoutingPolicy = 'round_robin' | 'weighted_random';
type CallerDraft = {
  defaultAccountId: string;
  enabled: boolean;
  key: string;
  module: string;
  name: string;
  routingPolicy: RoutingPolicy;
  smtpAccountIds: string;
  weights: string;
};
type TemplateDraft = {
  defaultLocale: 'en-US' | 'zh-CN';
  enabled: boolean;
  enBody: string;
  enSubject: string;
  key: string;
  published: boolean;
  purpose: string;
  testLocale: 'en-US' | 'zh-CN';
  testRecipient: string;
  testVariables: string;
  variables: string;
  zhBody: string;
  zhSubject: string;
};
type PolicyDraft = {
  callerKey: string;
  charset: string;
  codeLength: number;
  hourlyLimit: number;
  maxFailures: number;
  purpose: string;
  resendIntervalSeconds: number;
  ttlSeconds: number;
};
type PolicyRow = {
  draft: PolicyDraft;
  key: string;
  policy: VerificationPolicy;
};

const callers = ref<NotificationCaller[]>([]);
const templates = ref<NotificationTemplate[]>([]);
const policies = ref<VerificationPolicy[]>([]);
const callerEditingId = ref<string>();
const templateEditingId = ref<string>();
const policyDrafts = reactive<Record<string, PolicyDraft>>({});
const callerSaving = ref(false);
const templateSaving = ref(false);
const policySaving = ref('');
const templateTesting = ref('');
const callerForm = reactive<CallerDraft>({
  key: '',
  name: '',
  module: '',
  enabled: true,
  smtpAccountIds: '',
  defaultAccountId: '',
  routingPolicy: 'weighted_random',
  weights: '',
});
const templateForm = reactive<TemplateDraft>({
  key: '',
  purpose: '',
  defaultLocale: 'zh-CN',
  variables: '',
  zhSubject: '',
  zhBody: '',
  enSubject: '',
  enBody: '',
  enabled: true,
  published: false,
  testRecipient: '',
  testLocale: 'zh-CN',
  testVariables: '',
});
const callerPage = ref(1);
const callerPageSize = ref(10);
const visibleCallers = computed(() => {
  const start = (callerPage.value - 1) * callerPageSize.value;
  return callers.value.slice(start, start + callerPageSize.value);
});
const callerTotalPages = computed(() =>
  Math.max(1, Math.ceil(callers.value.length / callerPageSize.value)),
);
const mailTabs = computed<
  Array<{ count?: number; key: MailTab; label: string }>
>(() => [
  {
    key: 'accounts',
    label: String($t('page.mail.tabAccounts')),
    count: accounts.value.length,
  },
  {
    key: 'callers',
    label: String($t('page.mail.tabCallers')),
    count: callers.value.length,
  },
  {
    key: 'templates',
    label: String($t('page.mail.tabTemplates')),
    count: templates.value.length,
  },
  {
    key: 'policies',
    label: String($t('page.mail.tabPolicies')),
    count: policies.value.length,
  },
  {
    key: 'records',
    label: String($t('page.mail.tabRecords')),
    count: messages.value.length,
  },
]);
const policyRows = computed<PolicyRow[]>(() => {
  const rows: PolicyRow[] = [];
  for (const policy of policies.value) {
    const key = policyKey(policy);
    const draft = policyDrafts[key];
    if (key && draft) rows.push({ policy, key, draft });
  }
  return rows;
});

const hasAccounts = computed(() => accounts.value.length > 0);

const selectedTemplate = computed(() => {
  if (templateEditingId.value) {
    return (
      templates.value.find(
        (template) => template.id === templateEditingId.value,
      ) ?? templates.value[0]
    );
  }
  return templates.value[0];
});

const templatePreview = computed(() => {
  const source = selectedTemplate.value;
  const locale = templateForm.testLocale;
  const localeContent = source?.locales?.[locale];
  const subject =
    locale === 'en-US' ? templateForm.enSubject : templateForm.zhSubject;
  const body = locale === 'en-US' ? templateForm.enBody : templateForm.zhBody;
  const variables = source?.variables?.length
    ? completeTemplateVariables(
        source,
        parseVariables(templateForm.testVariables),
        locale,
      )
    : parseVariables(templateForm.testVariables);
  return {
    body: renderTemplate(
      body || localeContent?.body || source?.body || '',
      variables,
    ),
    subject: renderTemplate(
      subject || localeContent?.subject || source?.subject || '',
      variables,
    ),
  };
});

function renderTemplate(value: string, variables: Record<string, string>) {
  return value.replace(
    /\{\{\s*\.?([\w-]+)\s*\}\}/gu,
    (_match, key: string) => variables[key] ?? `{{ ${key} }}`,
  );
}

function accountName(accountId?: string) {
  if (!accountId) return String($t('page.mail.notSet'));
  return (
    accounts.value.find((account) => account.id === accountId)?.name ??
    accountId
  );
}

function messageSource(message: EmailMessage): RecordSource {
  if (message.isTest) return 'template_test';
  if (message.callerKey || message.templateKey) return 'business';
  return 'system';
}

function sourceLabel(source: RecordSource) {
  if (source === 'template_test')
    return String($t('page.mail.sourceTemplateTest'));
  if (source === 'business') return String($t('page.mail.sourceBusiness'));
  if (source === 'system') return String($t('page.mail.sourceSystem'));
  return String($t('page.mail.allSources'));
}

function messageStatusTone(status: string) {
  if (status === 'sent' || status === 'succeeded' || status === 'delivered')
    return 'ok';
  if (status === 'failed' || status === 'error') return 'bad';
  return 'pending';
}

function formatDate(value?: string) {
  if (!value) return String($t('page.mail.notSet'));
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatDuration(message: EmailMessage) {
  const sent = message.sentAt ? Date.parse(message.sentAt) : NaN;
  const created = Date.parse(message.createdAt);
  if (Number.isFinite(sent) && Number.isFinite(created) && sent >= created)
    return `${sent - created} ms`;
  return String($t('page.mail.notSet'));
}

function statusLabel(status: string) {
  if (status === 'sent' || status === 'succeeded' || status === 'delivered')
    return String($t('page.mail.statusSent'));
  if (status === 'failed' || status === 'error')
    return String($t('page.mail.statusFailed'));
  if (status === 'sending') return String($t('page.mail.statusSending'));
  if (status === 'pending' || status === 'queued')
    return String($t('page.mail.statusPending'));
  if (status === 'retrying') return String($t('page.mail.statusRetrying'));
  return status || String($t('page.mail.statusUnknown'));
}

function recordRangeBounds() {
  const to = new Date();
  if (recordFilters.range === 'all') return { from: undefined, to: undefined };
  const days = recordFilters.range === '30d' ? 30 : 7;
  return {
    from: new Date(to.getTime() - days * 24 * 60 * 60 * 1000).toISOString(),
    to: to.toISOString(),
  };
}

function resetForm() {
  Object.assign(form, emptyForm());
  editingId.value = undefined;
}

function resetCallerForm() {
  Object.assign(callerForm, {
    key: '',
    name: '',
    module: '',
    enabled: true,
    smtpAccountIds: '',
    defaultAccountId: '',
    routingPolicy: 'weighted_random',
    weights: '',
  });
  callerEditingId.value = undefined;
}

function resetTemplateForm() {
  Object.assign(templateForm, {
    key: '',
    purpose: '',
    defaultLocale: 'zh-CN',
    variables: '',
    zhSubject: '',
    zhBody: '',
    enSubject: '',
    enBody: '',
    enabled: true,
    published: false,
    testRecipient: '',
    testLocale: 'zh-CN',
    testVariables: '',
  });
  templateEditingId.value = undefined;
  templateRecipientError.value = '';
}

function splitCSV(value: string) {
  return value
    .split(/[\n,]/u)
    .map((item) => item.trim())
    .filter(Boolean);
}

function parseWeights(value: string) {
  const weights: Record<string, number> = {};
  for (const item of splitCSV(value)) {
    const [id, rawWeight] = item.split('=', 2);
    const weight = Number(rawWeight);
    if (id?.trim() && Number.isInteger(weight) && weight > 0) {
      weights[id.trim()] = weight;
    }
  }
  return weights;
}

function callerKey(value: NotificationCaller) {
  return value.key || value.callerKey || value.id;
}

function templateKey(value: NotificationTemplate) {
  return value.key || value.templateKey || value.id;
}

function editCaller(value: NotificationCaller) {
  callerEditingId.value = value.id;
  Object.assign(callerForm, {
    key: callerKey(value),
    name: value.name,
    module: value.module ?? '',
    enabled: value.enabled,
    smtpAccountIds: (value.smtpAccountIds ?? value.accountIds ?? []).join(', '),
    defaultAccountId: value.defaultAccountId ?? '',
    routingPolicy: (value.routingPolicy ??
      value.strategy ??
      'weighted_random') as RoutingPolicy,
    weights: Object.entries(value.weights ?? {})
      .map(([id, weight]) => `${id}=${weight}`)
      .join(', '),
  });
}

async function saveCaller() {
  if (!canManage.value || !callerForm.key.trim() || !callerForm.name.trim())
    return;
  callerSaving.value = true;
  try {
    await saveNotificationCallerApi(
      {
        key: callerForm.key.trim(),
        callerKey: callerForm.key.trim(),
        name: callerForm.name.trim(),
        module: callerForm.module.trim() || undefined,
        enabled: callerForm.enabled,
        smtpAccountIds: splitCSV(callerForm.smtpAccountIds),
        defaultAccountId: callerForm.defaultAccountId.trim() || undefined,
        routingPolicy: callerForm.routingPolicy,
        weights: parseWeights(callerForm.weights),
      },
      callerEditingId.value,
    );
    notify('success', String($t('page.mail.callerSaved')));
    resetCallerForm();
    await load();
  } catch {
    notify('error', String($t('page.mail.callerSaveError')));
  } finally {
    callerSaving.value = false;
  }
}

async function removeCaller(value: NotificationCaller) {
  if (
    !canManage.value ||
    !window.confirm(
      String($t('page.mail.callerDeleteConfirm', { name: value.name })),
    )
  )
    return;
  try {
    await deleteNotificationCallerApi(value.id);
    notify('success', String($t('page.mail.callerDeleted')));
    if (callerEditingId.value === value.id) resetCallerForm();
    await load();
  } catch {
    notify('error', String($t('page.mail.callerDeleteError')));
  }
}

function editTemplate(value: NotificationTemplate) {
  templateRecipientError.value = '';
  const locales = value.locales ?? {};
  const zh = locales['zh-CN'];
  const en = locales['en-US'];
  templateEditingId.value = value.id;
  const testVariables = completeTemplateVariables(
    value,
    parseVariables(templateForm.testVariables),
    templateForm.testLocale,
  );
  Object.assign(templateForm, {
    key: templateKey(value),
    purpose: value.purpose ?? templateKey(value),
    defaultLocale: (value.defaultLocale === 'en-US' ? 'en-US' : 'zh-CN') as
      | 'en-US'
      | 'zh-CN',
    variables: (value.variables ?? []).join(', '),
    zhSubject:
      zh?.subject ??
      (value.defaultLocale === 'zh-CN' ? (value.subject ?? '') : ''),
    zhBody:
      zh?.body ?? (value.defaultLocale === 'zh-CN' ? (value.body ?? '') : ''),
    enSubject:
      en?.subject ??
      (value.defaultLocale === 'en-US' ? (value.subject ?? '') : ''),
    enBody:
      en?.body ?? (value.defaultLocale === 'en-US' ? (value.body ?? '') : ''),
    enabled: value.enabled !== false,
    published: value.published === true,
    testVariables: serializeVariables(testVariables),
  });
}

function templatePayload() {
  const locales: Record<
    string,
    { body: string; locale: string; subject: string }
  > = {};
  if (templateForm.zhSubject.trim() || templateForm.zhBody.trim()) {
    locales['zh-CN'] = {
      locale: 'zh-CN',
      subject: templateForm.zhSubject.trim(),
      body: templateForm.zhBody,
    };
  }
  if (templateForm.enSubject.trim() || templateForm.enBody.trim()) {
    locales['en-US'] = {
      locale: 'en-US',
      subject: templateForm.enSubject.trim(),
      body: templateForm.enBody,
    };
  }
  return {
    key: templateForm.key.trim(),
    templateKey: templateForm.key.trim(),
    purpose: templateForm.purpose.trim() || templateForm.key.trim(),
    defaultLocale: templateForm.defaultLocale,
    variables: splitCSV(templateForm.variables),
    locales,
    enabled: templateForm.enabled,
    published: templateForm.published,
  };
}

async function saveTemplate() {
  if (!canManage.value || !templateForm.key.trim()) return;
  templateSaving.value = true;
  try {
    await saveNotificationTemplateApi(
      templatePayload(),
      templateEditingId.value,
    );
    notify('success', String($t('page.mail.templateSaved')));
    resetTemplateForm();
    await load();
  } catch {
    notify('error', String($t('page.mail.templateSaveError')));
  } finally {
    templateSaving.value = false;
  }
}

async function publishTemplate(value: NotificationTemplate) {
  if (!canManage.value) return;
  try {
    await publishNotificationTemplateApi(templateKey(value));
    notify('success', String($t('page.mail.templatePublished')));
    await load();
  } catch {
    notify('error', String($t('page.mail.templatePublishError')));
  }
}

async function removeTemplate(value: NotificationTemplate) {
  if (
    !canManage.value ||
    !window.confirm(
      String(
        $t('page.mail.templateDeleteConfirm', { name: templateKey(value) }),
      ),
    )
  )
    return;
  try {
    await deleteNotificationTemplateApi(templateKey(value));
    notify('success', String($t('page.mail.templateDeleted')));
    if (templateEditingId.value === value.id) resetTemplateForm();
    await load();
  } catch {
    notify('error', String($t('page.mail.templateDeleteError')));
  }
}

function parseVariables(value: string): Record<string, string> {
  const result: Record<string, string> = {};
  for (const item of splitCSV(value)) {
    const [key, ...rest] = item.split('=');
    if (key?.trim()) result[key.trim()] = rest.join('=').trim();
  }
  return result;
}

function sampleVariableValue(name: string, locale: string) {
  const key = name.trim().toLowerCase();
  const english = locale.trim().toLowerCase().startsWith('en');
  if (key.includes('code') || key.includes('otp') || key.includes('token'))
    return '123456';
  if (key.includes('expire') || key.includes('ttl'))
    return english ? '10 minutes' : '10 分钟';
  if (key.includes('email')) return 'user@example.test';
  if (key.includes('location') || key.includes('ip'))
    return english ? 'Sample location' : '示例地点';
  if (key.includes('name')) return english ? 'Sample User' : '示例用户';
  return english ? 'Sample value' : '示例值';
}

function completeTemplateVariables(
  value: NotificationTemplate,
  provided: Record<string, string>,
  locale: string,
) {
  const result: Record<string, string> = {};
  for (const name of value.variables ?? []) {
    if (Object.prototype.hasOwnProperty.call(provided, name)) {
      result[name] = provided[name] ?? '';
    } else {
      result[name] = sampleVariableValue(name, locale);
    }
  }
  return result;
}

function serializeVariables(values: Record<string, string>) {
  return Object.entries(values)
    .map(([key, value]) => `${key}=${value}`)
    .join(', ');
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object'
    ? (value as Record<string, unknown>)
    : undefined;
}

function responseData(value: unknown) {
  const record = asRecord(value);
  const nested = asRecord(record?.data);
  return nested && ('status' in nested || 'messageId' in nested)
    ? nested
    : (record ?? {});
}

function errorData(value: unknown) {
  const record = asRecord(value);
  const response = asRecord(record?.response);
  const responseBody = asRecord(response?.data);
  if (responseBody) return responseBody;
  const nested = asRecord(record?.data);
  return nested &&
    ('message' in nested || 'errors' in nested || 'code' in nested)
    ? nested
    : (record ?? {});
}

function formatTestError(value: unknown, fallbackKey: string, codeKey: string) {
  const payload = errorData(value);
  const first = Array.isArray(payload.errors)
    ? asRecord(payload.errors[0])
    : undefined;
  const messageKey =
    typeof first?.messageKey === 'string' ? first.messageKey : '';
  const params = asRecord(first?.params);
  if (messageKey === 'notification.template.variableMissing') {
    return String(
      $t('page.mail.templateMissingVariable', {
        variable: String(params?.variable ?? ''),
      }),
    );
  }
  if (messageKey === 'notification.template.variableInvalid') {
    return String(
      $t('page.mail.templateInvalidVariable', {
        variable: String(params?.variable ?? ''),
      }),
    );
  }
  if (messageKey === 'notification.recipient.invalid') {
    return String($t('page.mail.recipientInvalid'));
  }
  const code = payload.code;
  if (code !== undefined && code !== null && String(code) !== '10000') {
    return String($t(codeKey, { code: String(code) }));
  }
  return String($t(fallbackKey));
}

function setTestFeedback(
  target: Record<string, TestFeedback>,
  id: string,
  message: string,
  tone: TestFeedbackTone,
) {
  const feedback = { message, tone };
  target[id] = feedback;
  testResult.value = message;
  testResultTone.value = tone;
  notify(tone, message);
}

function feedbackMessage(target: Record<string, TestFeedback>, id: string) {
  return target[id]?.message ?? '';
}

function feedbackTone(target: Record<string, TestFeedback>, id: string) {
  return target[id]?.tone ?? 'info';
}

function validateTemplateRecipient(templateId?: string, focusOnError = false) {
  const recipient = templateForm.testRecipient.trim();
  let message = '';
  if (!recipient) {
    message = String($t('page.mail.testRecipientRequired'));
  } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/u.test(recipient)) {
    message = String($t('page.mail.testRecipientInvalid'));
  }
  templateRecipientError.value = message;
  if (message) {
    testResult.value = message;
    testResultTone.value = 'error';
    if (templateId)
      templateTestFeedback[templateId] = { message, tone: 'error' };
    notify('error', message);
    if (focusOnError)
      void nextTick(() => templateRecipientInput.value?.focus());
    return false;
  }
  return true;
}

function clearTemplateRecipientError() {
  if (templateForm.testRecipient.trim()) templateRecipientError.value = '';
}

async function testTemplate(value: NotificationTemplate) {
  if (!canManage.value || !validateTemplateRecipient(value.id, true)) return;
  templateTesting.value = value.id;
  testResult.value = '';
  try {
    const variables = completeTemplateVariables(
      value,
      parseVariables(templateForm.testVariables),
      templateForm.testLocale,
    );
    templateForm.testVariables = serializeVariables(variables);
    const result = await testNotificationTemplateApi(templateKey(value), {
      recipient: templateForm.testRecipient.trim(),
      locale: templateForm.testLocale,
      variables,
    });
    const data = responseData(result);
    const status = String(data.status ?? 'queued');
    const tone: TestFeedbackTone =
      status === 'failed' ? 'error' : status === 'queued' ? 'info' : 'success';
    setTestFeedback(
      templateTestFeedback,
      value.id,
      String($t('page.mail.templateTestResult', { status })),
      tone,
    );
  } catch (caught) {
    setTestFeedback(
      templateTestFeedback,
      value.id,
      formatTestError(
        caught,
        'page.mail.templateTestError',
        'page.mail.templateTestErrorWithCode',
      ),
      'error',
    );
  } finally {
    templateTesting.value = '';
  }
}

function syncPolicyDrafts(values: VerificationPolicy[]) {
  const next: Record<string, PolicyDraft> = {};
  for (const value of values) {
    const key = value.key || value.policyKey || value.purpose || '';
    if (!key) continue;
    next[key] = {
      callerKey: value.callerKey ?? '',
      purpose: value.purpose ?? key,
      codeLength: value.codeLength ?? value.length ?? 6,
      charset: value.charset ?? 'numeric',
      ttlSeconds: value.ttlSeconds ?? 600,
      maxFailures: value.maxFailures ?? 5,
      resendIntervalSeconds:
        value.resendIntervalSeconds ?? value.resendAfterSeconds ?? 60,
      hourlyLimit: value.hourlyLimit ?? value.maxSendsPerHour ?? 5,
    };
  }
  for (const key of Object.keys(policyDrafts)) delete policyDrafts[key];
  Object.assign(policyDrafts, next);
}

function policyKey(value: VerificationPolicy) {
  return value.key || value.policyKey || value.purpose || '';
}

async function savePolicy(value: VerificationPolicy) {
  const key = policyKey(value);
  const draft = policyDrafts[key];
  if (!canManage.value || !draft || !key) return;
  policySaving.value = key;
  try {
    await updateVerificationPolicyApi(key, {
      callerKey: draft.callerKey.trim() || undefined,
      purpose: draft.purpose.trim() || key,
      codeLength: draft.codeLength,
      charset: draft.charset,
      ttlSeconds: draft.ttlSeconds,
      maxFailures: draft.maxFailures,
      resendIntervalSeconds: draft.resendIntervalSeconds,
      hourlyLimit: draft.hourlyLimit,
    });
    notify('success', String($t('page.mail.policySaved')));
    await load();
  } catch {
    notify('error', String($t('page.mail.policySaveError')));
  } finally {
    policySaving.value = '';
  }
}

function edit(account: SMTPAccount) {
  if (!canManage.value) return;
  editingId.value = account.id;
  Object.assign(form, {
    name: account.name,
    enabled: account.enabled,
    host: account.host,
    port: account.port,
    username: account.username,
    password: '',
    weight: account.weight,
    fromEmail: account.fromEmail,
    fromName: account.fromName ?? '',
    implicitTls: account.implicitTls,
  });
  testResult.value = '';
}

async function load() {
  loading.value = true;
  try {
    const [accountResult, messageResult] = await Promise.all([
      listSMTPAccountsApi(),
      listEmailMessagesApi({ limit: 100, offset: 0 }),
    ]);
    accounts.value = Array.isArray(accountResult) ? accountResult : [];
    messages.value = messageResult?.items ?? [];
    lastSyncedAt.value = new Date().toISOString();
    // Common capability registries are optional during a rolling upgrade; the
    // legacy account/record view remains usable while each registry catches up.
    const [callerResult, templateResult, policyResult] =
      await Promise.allSettled([
        listNotificationCallersApi(),
        listNotificationTemplatesApi(),
        listVerificationPoliciesApi(),
      ]);
    callers.value =
      callerResult.status === 'fulfilled' ? callerResult.value : [];
    templates.value =
      templateResult.status === 'fulfilled' ? templateResult.value : [];
    policies.value =
      policyResult.status === 'fulfilled' ? policyResult.value : [];
    syncPolicyDrafts(policies.value);
  } catch {
    notify('error', String($t('page.mail.loadError')));
  } finally {
    loading.value = false;
  }
}

async function loadRecords() {
  recordsLoading.value = true;
  try {
    const range = recordRangeBounds();
    const result = await listEmailMessagesApi({
      accountId: recordFilters.accountId || undefined,
      from: range.from,
      keyword: recordFilters.keyword.trim() || undefined,
      limit: 100,
      offset: 0,
      source: recordFilters.source === 'all' ? undefined : recordFilters.source,
      status: recordFilters.status || undefined,
      to: range.to,
    });
    messages.value = result?.items ?? [];
    lastSyncedAt.value = new Date().toISOString();
    recordPage.value = 1;
  } catch {
    notify('error', String($t('page.mail.recordsLoadError')));
  } finally {
    recordsLoading.value = false;
  }
}

function queryRecords() {
  recordPage.value = 1;
  void loadRecords();
}

function resetRecordFilters() {
  Object.assign(recordFilters, {
    accountId: '',
    keyword: '',
    range: '7d',
    source: 'all',
    status: '',
  });
  queryRecords();
}

function changeRecordPage(page: number) {
  recordPage.value = Math.min(Math.max(page, 1), recordTotalPages.value);
}

async function openMessageDetail(message: EmailMessage) {
  recordDetail.value = message;
  recordDetailLoading.value = true;
  try {
    const result = await getEmailMessageApi(message.id, false);
    recordDetail.value = result ?? message;
  } catch {
    // The list response is already enough for the non-sensitive detail panel.
  } finally {
    recordDetailLoading.value = false;
  }
}

function closeMessageDetail() {
  recordDetail.value = null;
}

async function save() {
  if (!canManage.value) return;
  if (!form.name.trim() || !form.host.trim() || !form.fromEmail.trim()) {
    notify('error', String($t('page.mail.saveErrorRequired')));
    return;
  }
  saving.value = true;
  try {
    await saveSMTPAccountApi({ ...form }, editingId.value);
    notify(
      'success',
      String($t(editingId.value ? 'page.mail.updated' : 'page.mail.created')),
    );
    resetForm();
    await load();
  } catch {
    notify('error', String($t('page.mail.saveError')));
  } finally {
    saving.value = false;
  }
}

async function test(account: SMTPAccount) {
  if (!canManage.value) return;
  testingId.value = account.id;
  testResult.value = '';
  try {
    const result = await testSMTPAccountApi(account.id);
    const data = responseData(result);
    const status = String(data.status ?? '').toLowerCase();
    const success = status === 'ok';
    setTestFeedback(
      accountTestFeedback,
      account.id,
      success
        ? String($t('page.mail.testSuccess', { name: account.name }))
        : String(
            $t('page.mail.testFailure', {
              code: String(data.code ?? 'provider_unavailable'),
              name: account.name,
              stage: String(data.stage ?? 'unknown'),
            }),
          ),
      success ? 'success' : 'error',
    );
    accountTestAt[account.id] = formatDate(
      String(data.checkedAt ?? new Date().toISOString()),
    );
  } catch (caught) {
    setTestFeedback(
      accountTestFeedback,
      account.id,
      formatTestError(
        caught,
        'page.mail.testError',
        'page.mail.testErrorWithCode',
      ),
      'error',
    );
    accountTestAt[account.id] = formatDate(new Date().toISOString());
  } finally {
    testingId.value = '';
  }
}

async function remove(account: SMTPAccount) {
  if (!canManage.value) return;
  if (
    !window.confirm(
      String($t('page.mail.deleteConfirm', { name: account.name })),
    )
  )
    return;
  testingId.value = account.id;
  try {
    await deleteSMTPAccountApi(account.id);
    notify('success', String($t('page.mail.deleted')));
    if (editingId.value === account.id) resetForm();
    await load();
  } catch {
    notify('error', String($t('page.mail.deleteError')));
  } finally {
    testingId.value = '';
  }
}

onMounted(load);
</script>

<template>
  <ManagementPage
    class="mail-page"
    :aria-busy="loading || saving || recordsLoading"
    aria-labelledby="mail-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.mail.eyebrow') }}</p>
        <h1 id="mail-title">{{ $t('page.mail.title') }}</h1>
        <p class="description">{{ $t('page.mail.description') }}</p>
      </div>
      <div class="heading-actions">
        <button class="secondary" type="button" @click="openGuide">
          {{ $t('page.mail.guideButton') }}
        </button>
        <button
          class="secondary"
          type="button"
          :disabled="loading"
          @click="load"
        >
          {{ $t('page.mail.refresh') }}
        </button>
      </div>
    </header>

    <aside
      v-if="guideOpen"
      ref="guideDrawer"
      class="guide-drawer"
      role="dialog"
      aria-modal="true"
      aria-labelledby="mail-guide-title"
      tabindex="-1"
      @click.self="closeGuide"
      @keydown.esc="closeGuide"
    >
      <div class="guide-panel">
        <div class="section-heading">
          <div>
            <p class="eyebrow">{{ $t('page.mail.guideButton') }}</p>
            <h2 id="mail-guide-title">{{ guide.title }}</h2>
          </div>
          <button class="secondary" type="button" @click="closeGuide">
            {{ $t('page.mail.guideClose') }}
          </button>
        </div>
        <p class="description">{{ guide.audience }}</p>
        <nav
          class="guide-tabs"
          role="tablist"
          :aria-label="$t('page.mail.guideButton')"
        >
          <button
            v-for="item in [
              { key: 'operator', label: $t('page.mail.guideNormal') },
              { key: 'developer', label: $t('page.mail.guideDeveloper') },
              { key: 'examples', label: $t('page.mail.guideExamples') },
            ]"
            :id="'guide-tab-' + item.key"
            :key="item.key"
            class="guide-tab"
            :class="{ active: guideSection === item.key }"
            type="button"
            role="tab"
            :aria-selected="guideSection === item.key"
            :tabindex="guideSection === item.key ? 0 : -1"
            @click="
              guideSection = item.key as 'operator' | 'developer' | 'examples'
            "
          >
            {{ item.label }}
          </button>
        </nav>
        <section v-if="guideSection === 'operator'" class="guide-section">
          <h3>{{ $t('page.mail.guideNormal') }}</h3>
          <ol class="guide-list">
            <li v-for="step in guide.steps" :key="step">{{ step }}</li>
          </ol>
          <h3>{{ $t('page.mail.guideTroubleshooting') }}</h3>
          <p class="guide-note">
            {{ $t('page.mail.guideTroubleshootingText') }}
          </p>
        </section>
        <section v-else-if="guideSection === 'developer'" class="guide-section">
          <h3>{{ $t('page.mail.guideDeveloper') }}</h3>
          <ul class="guide-list">
            <li v-for="step in guide.developer" :key="step">{{ step }}</li>
          </ul>
          <div class="guide-callout">
            <strong>{{ $t('page.mail.guideImmediateEffect') }}</strong>
            <p>{{ $t('page.mail.guideImmediateEffectText') }}</p>
          </div>
        </section>
        <section v-else class="guide-section">
          <h3>{{ $t('page.mail.guideExamples') }}</h3>
          <p class="guide-note">{{ $t('page.mail.guideExamplesHint') }}</p>
          <pre
            v-for="example in guide.examples || []"
            :key="example"
            class="guide-code"
          ><code>{{ example }}</code></pre>
        </section>
      </div>
    </aside>

    <nav
      class="mail-tabs"
      role="tablist"
      :aria-label="$t('page.mail.tabsLabel')"
    >
      <button
        v-for="(tab, index) in mailTabs"
        :id="'mail-tab-' + tab.key"
        :key="tab.key"
        class="mail-tab"
        :class="{ active: activeTab === tab.key }"
        type="button"
        role="tab"
        :aria-selected="activeTab === tab.key"
        :aria-controls="'mail-panel-' + tab.key"
        :tabindex="activeTab === tab.key ? 0 : -1"
        @click="selectTab(tab.key)"
        @keydown="onTabKeydown($event, index)"
      >
        <span>{{ tab.label }}</span>
        <small v-if="tab.count !== undefined">{{ tab.count }}</small>
      </button>
    </nav>

    <main class="tab-panel">
      <section
        v-if="activeTab === 'accounts'"
        id="mail-panel-accounts"
        class="tab-content"
        role="tabpanel"
        aria-labelledby="mail-tab-accounts"
      >
        <article class="table-card">
          <div class="section-heading">
            <div>
              <p class="eyebrow">{{ $t('page.mail.poolStatus') }}</p>
              <h2>{{ $t('page.mail.accountPool') }}</h2>
              <p class="helper">{{ $t('page.mail.accountsSubtitle') }}</p>
            </div>
            <div class="toolbar-actions">
              <span class="count">{{
                $t('page.mail.accountCount', { count: accounts.length })
              }}</span>
              <button
                class="secondary"
                type="button"
                :disabled="loading"
                @click="load"
              >
                {{ $t('page.mail.refresh') }}
              </button>
              <button
                v-if="canManage"
                class="primary"
                type="button"
                @click="resetForm"
              >
                {{ $t('page.mail.addSmtpAccount') }}
              </button>
            </div>
          </div>
          <p v-if="!loading && !hasAccounts" class="empty-state">
            {{ $t('page.mail.emptyAccounts') }}
          </p>
          <div v-else class="table-scroll">
            <table>
              <caption class="sr-only">
                {{
                  $t('page.mail.accountList')
                }}
              </caption>
              <thead>
                <tr>
                  <th>{{ $t('page.mail.name') }}</th>
                  <th>{{ $t('page.mail.host') }}</th>
                  <th>{{ $t('page.mail.port') }}</th>
                  <th>{{ $t('page.mail.security') }}</th>
                  <th>{{ $t('page.mail.sender') }}</th>
                  <th>{{ $t('page.mail.weight') }}</th>
                  <th>{{ $t('page.mail.status') }}</th>
                  <th>{{ $t('page.mail.latestTest') }}</th>
                  <th>{{ $t('page.mail.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="account in accounts" :key="account.id">
                  <td>
                    <strong>{{ account.name }}</strong>
                    <small>{{
                      account.username || $t('page.mail.noAuthUsername')
                    }}</small>
                  </td>
                  <td>{{ account.host }}</td>
                  <td>{{ account.port }}</td>
                  <td>
                    {{
                      account.implicitTls
                        ? $t('page.mail.implicitTlsShort')
                        : $t('page.mail.startTlsPlain')
                    }}
                  </td>
                  <td>
                    <strong>{{ account.fromName || account.fromEmail }}</strong>
                    <small>{{ account.fromEmail }}</small>
                  </td>
                  <td>{{ account.weight }}</td>
                  <td>
                    <span
                      class="status-pill"
                      :class="account.enabled ? 'ok' : 'off'"
                    >
                      {{
                        account.enabled
                          ? $t('page.mail.enabled')
                          : $t('page.mail.disabled')
                      }}
                    </span>
                  </td>
                  <td>
                    <span
                      v-if="accountTestFeedback[account.id]"
                      class="status-pill"
                      :class="
                        feedbackTone(accountTestFeedback, account.id) ===
                        'success'
                          ? 'ok'
                          : 'bad'
                      "
                    >
                      {{
                        feedbackTone(accountTestFeedback, account.id) ===
                        'success'
                          ? $t('page.mail.testPassed')
                          : $t('page.mail.testFailed')
                      }}
                    </span>
                    <small v-if="accountTestAt[account.id]">{{
                      accountTestAt[account.id]
                    }}</small>
                    <small v-else>{{ $t('page.mail.notTested') }}</small>
                  </td>
                  <td class="actions">
                    <button
                      v-if="canManage"
                      type="button"
                      :disabled="testingId === account.id"
                      @click="test(account)"
                    >
                      {{
                        testingId === account.id
                          ? $t('page.mail.testing')
                          : $t('page.mail.testConnection')
                      }}
                    </button>
                    <button
                      v-if="canManage"
                      type="button"
                      @click="edit(account)"
                    >
                      {{ $t('page.mail.edit') }}
                    </button>
                    <button
                      v-if="canManage"
                      class="danger"
                      type="button"
                      :disabled="testingId === account.id"
                      @click="remove(account)"
                    >
                      {{ $t('page.mail.delete') }}
                    </button>
                    <span
                      v-if="accountTestFeedback[account.id]"
                      class="test-feedback"
                      :class="feedbackTone(accountTestFeedback, account.id)"
                      role="status"
                    >
                      {{ feedbackMessage(accountTestFeedback, account.id) }}
                    </span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </article>

        <article
          v-if="canManage"
          class="editor-card"
          aria-labelledby="mail-editor-title"
        >
          <div class="section-heading">
            <div>
              <p class="eyebrow">{{ $t('page.mail.accountPool') }}</p>
              <h2 id="mail-editor-title">
                {{
                  editingId
                    ? $t('page.mail.editAccount')
                    : $t('page.mail.newAccount')
                }}
              </h2>
            </div>
            <button
              v-if="editingId"
              class="secondary"
              type="button"
              @click="resetForm"
            >
              {{ $t('page.mail.cancelEdit') }}
            </button>
          </div>
          <form class="account-form" @submit.prevent="save">
            <label
              ><span>{{ $t('page.mail.accountName') }}</span
              ><input v-model="form.name" autocomplete="off" required
            /></label>
            <label
              ><span>{{ $t('page.mail.smtpHost') }}</span
              ><input v-model="form.host" autocomplete="off" required
            /></label>
            <label
              ><span>{{ $t('page.mail.smtpPort') }}</span
              ><input
                v-model.number="form.port"
                min="1"
                max="65535"
                type="number"
                required
            /></label>
            <label
              ><span>{{ $t('page.mail.smtpUsername') }}</span
              ><input v-model="form.username" autocomplete="username"
            /></label>
            <label
              ><span>{{ $t('page.mail.smtpPassword') }}</span
              ><input
                v-model="form.password"
                autocomplete="new-password"
                type="password"
                :placeholder="
                  editingId ? $t('page.mail.passwordPlaceholder') : ''
                "
            /></label>
            <label
              ><span>{{ $t('page.mail.weight') }}</span
              ><input
                v-model.number="form.weight"
                min="1"
                type="number"
                required
            /></label>
            <label
              ><span>{{ $t('page.mail.fromEmail') }}</span
              ><input
                v-model="form.fromEmail"
                autocomplete="email"
                type="email"
                required
            /></label>
            <label
              ><span>{{ $t('page.mail.fromName') }}</span
              ><input v-model="form.fromName" autocomplete="off"
            /></label>
            <label class="toggle"
              ><input v-model="form.enabled" type="checkbox" /><span>{{
                $t('page.mail.enableSmtp')
              }}</span></label
            >
            <label class="toggle"
              ><input v-model="form.implicitTls" type="checkbox" /><span>{{
                $t('page.mail.implicitTls')
              }}</span></label
            >
            <div class="form-actions">
              <button class="primary" type="submit" :disabled="saving">
                {{ saving ? $t('page.mail.saving') : $t('page.mail.save') }}
              </button>
            </div>
          </form>
        </article>
      </section>

      <section
        v-else-if="activeTab === 'callers'"
        id="mail-panel-callers"
        class="tab-content"
        role="tabpanel"
        aria-labelledby="mail-tab-callers"
      >
        <div class="two-column">
          <article
            v-if="canManage"
            class="editor-card"
            aria-labelledby="caller-editor-title"
          >
            <div class="section-heading">
              <div>
                <p class="eyebrow">{{ $t('page.mail.callerEyebrow') }}</p>
                <h2 id="caller-editor-title">
                  {{
                    callerEditingId
                      ? $t('page.mail.callerEdit')
                      : $t('page.mail.callerNew')
                  }}
                </h2>
              </div>
              <button
                v-if="callerEditingId"
                class="secondary"
                type="button"
                @click="resetCallerForm"
              >
                {{ $t('page.mail.cancelEdit') }}
              </button>
            </div>
            <p class="helper">{{ $t('page.mail.callerHelp') }}</p>
            <form class="capability-form" @submit.prevent="saveCaller">
              <label
                ><span>{{ $t('page.mail.callerKey') }}</span
                ><input
                  v-model="callerForm.key"
                  :disabled="Boolean(callerEditingId)"
                  autocomplete="off"
                  required
              /></label>
              <label
                ><span>{{ $t('page.mail.callerName') }}</span
                ><input v-model="callerForm.name" autocomplete="off" required
              /></label>
              <label
                ><span>{{ $t('page.mail.callerModule') }}</span
                ><input v-model="callerForm.module" autocomplete="off"
              /></label>
              <label
                ><span>{{ $t('page.mail.callerAccounts') }}</span
                ><input
                  v-model="callerForm.smtpAccountIds"
                  :placeholder="$t('page.mail.callerAccountsPlaceholder')"
                  autocomplete="off"
              /></label>
              <label
                ><span>{{ $t('page.mail.callerDefaultAccount') }}</span
                ><input
                  v-model="callerForm.defaultAccountId"
                  autocomplete="off"
              /></label>
              <label
                ><span>{{ $t('page.mail.callerRouting') }}</span
                ><select v-model="callerForm.routingPolicy">
                  <option value="weighted_random">
                    {{ $t('page.mail.weightedRandom') }}
                  </option>
                  <option value="round_robin">
                    {{ $t('page.mail.roundRobin') }}
                  </option>
                </select></label
              >
              <label class="wide"
                ><span>{{ $t('page.mail.callerWeights') }}</span
                ><input
                  v-model="callerForm.weights"
                  :placeholder="$t('page.mail.callerWeightsPlaceholder')"
                  autocomplete="off"
              /></label>
              <label class="toggle"
                ><input v-model="callerForm.enabled" type="checkbox" /><span>{{
                  $t('page.mail.enabled')
                }}</span></label
              >
              <div class="form-actions">
                <button class="primary" type="submit" :disabled="callerSaving">
                  {{
                    callerSaving
                      ? $t('page.mail.saving')
                      : $t('page.mail.saveCaller')
                  }}
                </button>
              </div>
            </form>
          </article>
          <article class="table-card" aria-labelledby="caller-list-title">
            <div class="section-heading">
              <div>
                <p class="eyebrow">{{ $t('page.mail.callerEyebrow') }}</p>
                <h2 id="caller-list-title">{{ $t('page.mail.callerList') }}</h2>
                <p class="helper">
                  {{ $t('page.mail.callerCount', { count: callers.length }) }}
                </p>
              </div>
              <button
                class="secondary"
                type="button"
                :disabled="loading"
                @click="load"
              >
                {{ $t('page.mail.refresh') }}
              </button>
            </div>
            <p v-if="!callers.length" class="empty-state">
              {{ $t('page.mail.emptyCallers') }}
            </p>
            <div v-else class="table-scroll compact-scroll">
              <table class="compact-table">
                <caption class="sr-only">
                  {{
                    $t('page.mail.callerList')
                  }}
                </caption>
                <thead>
                  <tr>
                    <th>{{ $t('page.mail.callerName') }}</th>
                    <th>{{ $t('page.mail.callerKey') }}</th>
                    <th>{{ $t('page.mail.callerModule') }}</th>
                    <th>{{ $t('page.mail.callerRouting') }}</th>
                    <th>{{ $t('page.mail.status') }}</th>
                    <th>{{ $t('page.mail.actions') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="caller in visibleCallers" :key="caller.id">
                    <td>
                      <strong>{{ caller.name }}</strong
                      ><small
                        >{{
                          caller.smtpAccountIds?.length ??
                          caller.accountIds?.length ??
                          0
                        }}
                        {{ $t('page.mail.accountUnit') }}</small
                      >
                    </td>
                    <td>
                      <code>{{ callerKey(caller) }}</code>
                    </td>
                    <td>{{ caller.module || $t('page.mail.notSet') }}</td>
                    <td>
                      {{
                        caller.routingPolicy ||
                        caller.strategy ||
                        $t('page.mail.weightedRandom')
                      }}
                    </td>
                    <td>
                      <span
                        class="status-pill"
                        :class="caller.enabled ? 'ok' : 'off'"
                        >{{
                          caller.enabled
                            ? $t('page.mail.enabled')
                            : $t('page.mail.disabled')
                        }}</span
                      >
                    </td>
                    <td class="actions">
                      <button
                        v-if="canManage"
                        type="button"
                        @click="editCaller(caller)"
                      >
                        {{ $t('page.mail.edit') }}</button
                      ><button
                        v-if="canManage && !caller.systemOwned"
                        class="danger"
                        type="button"
                        @click="removeCaller(caller)"
                      >
                        {{ $t('page.mail.delete') }}
                      </button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-if="callers.length > callerPageSize" class="pagination">
              <span>{{
                $t('page.mail.pageSummary', {
                  current: callerPage,
                  total: callerTotalPages,
                })
              }}</span>
              <div class="pagination-actions">
                <button
                  class="secondary"
                  type="button"
                  :disabled="callerPage <= 1"
                  @click="callerPage -= 1"
                >
                  {{ $t('page.mail.previous') }}
                </button>
                <button
                  class="secondary"
                  type="button"
                  :disabled="callerPage >= callerTotalPages"
                  @click="callerPage += 1"
                >
                  {{ $t('page.mail.next') }}
                </button>
              </div>
            </div>
          </article>
        </div>
      </section>

      <section
        v-else-if="activeTab === 'templates'"
        id="mail-panel-templates"
        class="tab-content"
        role="tabpanel"
        aria-labelledby="mail-tab-templates"
      >
        <div class="two-column template-layout">
          <article
            v-if="canManage"
            class="editor-card"
            aria-labelledby="template-editor-title"
          >
            <div class="section-heading">
              <div>
                <p class="eyebrow">{{ $t('page.mail.templateEyebrow') }}</p>
                <h2 id="template-editor-title">
                  {{
                    templateEditingId
                      ? $t('page.mail.templateEdit')
                      : $t('page.mail.templateNew')
                  }}
                </h2>
              </div>
              <button
                v-if="templateEditingId"
                class="secondary"
                type="button"
                @click="resetTemplateForm"
              >
                {{ $t('page.mail.cancelEdit') }}
              </button>
            </div>
            <p class="helper">{{ $t('page.mail.templateHelp') }}</p>
            <form class="capability-form" @submit.prevent="saveTemplate">
              <label
                ><span>{{ $t('page.mail.templateKey') }}</span
                ><input
                  v-model="templateForm.key"
                  :disabled="Boolean(templateEditingId)"
                  autocomplete="off"
                  required
              /></label>
              <label
                ><span>{{ $t('page.mail.templatePurpose') }}</span
                ><input v-model="templateForm.purpose" autocomplete="off"
              /></label>
              <label
                ><span>{{ $t('page.mail.templateDefaultLocale') }}</span
                ><select v-model="templateForm.defaultLocale">
                  <option value="zh-CN">zh-CN</option>
                  <option value="en-US">en-US</option>
                </select></label
              >
              <label class="wide"
                ><span>{{ $t('page.mail.templateVariables') }}</span
                ><input
                  v-model="templateForm.variables"
                  :placeholder="$t('page.mail.templateVariablesPlaceholder')"
                  autocomplete="off"
                />
                <div
                  v-if="splitCSV(templateForm.variables).length"
                  class="chip-list"
                >
                  <span
                    v-for="variable in splitCSV(templateForm.variables)"
                    :key="variable"
                    class="chip"
                    >{{ variable }}</span
                  >
                </div></label
              >
              <div
                class="locale-tabs wide"
                role="tablist"
                :aria-label="$t('page.mail.templateLocale')"
              >
                <button
                  class="locale-tab"
                  :class="{ active: templateLocaleEditor === 'zh-CN' }"
                  type="button"
                  role="tab"
                  :aria-selected="templateLocaleEditor === 'zh-CN'"
                  @click="templateLocaleEditor = 'zh-CN'"
                >
                  简体中文 zh-CN
                </button>
                <button
                  class="locale-tab"
                  :class="{ active: templateLocaleEditor === 'en-US' }"
                  type="button"
                  role="tab"
                  :aria-selected="templateLocaleEditor === 'en-US'"
                  @click="templateLocaleEditor = 'en-US'"
                >
                  English en-US
                </button>
              </div>
              <label v-if="templateLocaleEditor === 'zh-CN'" class="wide"
                ><span>{{ $t('page.mail.templateSubject') }}</span
                ><input v-model="templateForm.zhSubject" autocomplete="off"
              /></label>
              <label v-else class="wide"
                ><span>{{ $t('page.mail.templateSubject') }}</span
                ><input v-model="templateForm.enSubject" autocomplete="off"
              /></label>
              <label v-if="templateLocaleEditor === 'zh-CN'" class="wide"
                ><span>{{ $t('page.mail.templateBody') }}</span
                ><textarea v-model="templateForm.zhBody" rows="8"></textarea>
              </label>
              <label v-else class="wide"
                ><span>{{ $t('page.mail.templateBody') }}</span
                ><textarea v-model="templateForm.enBody" rows="8"></textarea>
              </label>
              <label class="toggle"
                ><input
                  v-model="templateForm.enabled"
                  type="checkbox"
                /><span>{{ $t('page.mail.enabled') }}</span></label
              >
              <label class="toggle"
                ><input
                  v-model="templateForm.published"
                  type="checkbox"
                /><span>{{ $t('page.mail.templatePublishNow') }}</span></label
              >
              <div class="form-actions">
                <button
                  class="secondary"
                  type="button"
                  @click="resetTemplateForm"
                >
                  {{ $t('page.mail.reset') }}</button
                ><button
                  class="primary"
                  type="submit"
                  :disabled="templateSaving"
                >
                  {{
                    templateSaving
                      ? $t('page.mail.saving')
                      : $t('page.mail.saveTemplate')
                  }}
                </button>
              </div>
            </form>
          </article>

          <div class="template-side">
            <article
              class="preview-card"
              aria-labelledby="template-preview-title"
            >
              <div class="section-heading">
                <div>
                  <p class="eyebrow">
                    {{ $t('page.mail.templatePreviewEyebrow') }}
                  </p>
                  <h2 id="template-preview-title">
                    {{ $t('page.mail.templatePreview') }}
                  </h2>
                </div>
                <span class="locale-badge">{{ templateForm.testLocale }}</span>
              </div>
              <div class="preview-controls">
                <label
                  ><span>{{ $t('page.mail.testLocale') }}</span
                  ><select v-model="templateForm.testLocale">
                    <option value="zh-CN">zh-CN</option>
                    <option value="en-US">en-US</option>
                  </select></label
                >
                <label
                  ><span>{{ $t('page.mail.testVariables') }}</span
                  ><input
                    v-model="templateForm.testVariables"
                    :placeholder="$t('page.mail.testVariablesPlaceholder')"
                    autocomplete="off"
                /></label>
              </div>
              <div class="preview-email">
                <p class="preview-label">{{ $t('page.mail.subject') }}</p>
                <strong>{{
                  templatePreview.subject || $t('page.mail.previewEmpty')
                }}</strong>
                <p class="preview-label">{{ $t('page.mail.body') }}</p>
                <pre>{{
                  templatePreview.body || $t('page.mail.previewEmpty')
                }}</pre>
              </div>
              <form
                class="test-form"
                novalidate
                @submit.prevent="
                  selectedTemplate && testTemplate(selectedTemplate)
                "
              >
                <label :class="{ invalid: templateRecipientError }">
                  <span>{{ $t('page.mail.testRecipient') }}</span>
                  <input
                    ref="templateRecipientInput"
                    v-model="templateForm.testRecipient"
                    type="email"
                    autocomplete="email"
                    :aria-invalid="Boolean(templateRecipientError)"
                    aria-describedby="template-recipient-error"
                    @input="clearTemplateRecipientError"
                    @blur="validateTemplateRecipient()"
                  />
                  <small
                    v-if="templateRecipientError"
                    id="template-recipient-error"
                    class="field-error"
                    role="alert"
                    >{{ templateRecipientError }}</small
                  >
                </label>
                <button
                  class="primary"
                  type="submit"
                  :disabled="!selectedTemplate || Boolean(templateTesting)"
                >
                  {{
                    templateTesting
                      ? $t('page.mail.testing')
                      : $t('page.mail.testTemplate')
                  }}
                </button>
              </form>
            </article>

            <article
              class="table-card template-list-card"
              aria-labelledby="template-list-title"
            >
              <div class="section-heading">
                <div>
                  <p class="eyebrow">{{ $t('page.mail.templateEyebrow') }}</p>
                  <h2 id="template-list-title">
                    {{ $t('page.mail.templateList') }}
                  </h2>
                  <p class="helper">
                    {{
                      $t('page.mail.templateCount', { count: templates.length })
                    }}
                  </p>
                </div>
                <button
                  class="secondary"
                  type="button"
                  :disabled="loading"
                  @click="load"
                >
                  {{ $t('page.mail.refresh') }}
                </button>
              </div>
              <p v-if="!templates.length" class="empty-state">
                {{ $t('page.mail.emptyTemplates') }}
              </p>
              <div v-else class="template-list">
                <div
                  v-for="template in templates"
                  :key="template.id"
                  class="template-row"
                >
                  <div class="template-row-copy">
                    <strong>{{
                      template.name || template.purpose || templateKey(template)
                    }}</strong
                    ><code>{{ templateKey(template) }}</code
                    ><small
                      >{{
                        template.published
                          ? $t('page.mail.templatePublishedState')
                          : $t('page.mail.templateDraftState')
                      }}
                      ·
                      {{
                        (template.variables || []).join(', ') ||
                        $t('page.mail.noVariables')
                      }}</small
                    >
                  </div>
                  <div class="actions">
                    <span
                      class="status-pill"
                      :class="template.enabled === false ? 'off' : 'ok'"
                      >{{
                        template.enabled === false
                          ? $t('page.mail.disabled')
                          : $t('page.mail.enabled')
                      }}</span
                    ><button
                      v-if="canManage"
                      type="button"
                      @click="editTemplate(template)"
                    >
                      {{ $t('page.mail.edit') }}</button
                    ><button
                      v-if="canManage"
                      type="button"
                      :disabled="templateTesting === template.id"
                      @click="testTemplate(template)"
                    >
                      {{
                        templateTesting === template.id
                          ? $t('page.mail.testing')
                          : $t('page.mail.testTemplate')
                      }}</button
                    ><button
                      v-if="canManage && !template.published"
                      type="button"
                      @click="publishTemplate(template)"
                    >
                      {{ $t('page.mail.publishTemplate') }}</button
                    ><button
                      v-if="canManage"
                      class="danger"
                      type="button"
                      @click="removeTemplate(template)"
                    >
                      {{ $t('page.mail.delete') }}</button
                    ><span
                      v-if="templateTestFeedback[template.id]"
                      class="test-feedback"
                      :class="feedbackTone(templateTestFeedback, template.id)"
                      role="status"
                      >{{
                        feedbackMessage(templateTestFeedback, template.id)
                      }}</span
                    >
                  </div>
                </div>
              </div>
            </article>
          </div>
        </div>
      </section>

      <section
        v-else-if="activeTab === 'policies'"
        id="mail-panel-policies"
        class="tab-content"
        role="tabpanel"
        aria-labelledby="mail-tab-policies"
      >
        <article class="table-card">
          <div class="section-heading">
            <div>
              <p class="eyebrow">{{ $t('page.mail.policyEyebrow') }}</p>
              <h2>{{ $t('page.mail.policyTitle') }}</h2>
              <p class="helper">{{ $t('page.mail.policyHelp') }}</p>
            </div>
            <button
              class="secondary"
              type="button"
              :disabled="loading"
              @click="load"
            >
              {{ $t('page.mail.refresh') }}
            </button>
          </div>
          <p v-if="!policyRows.length" class="empty-state">
            {{ $t('page.mail.emptyPolicies') }}
          </p>
          <div v-else class="policy-grid">
            <div v-for="row in policyRows" :key="row.key" class="policy-row">
              <div class="policy-heading">
                <strong
                  ><code>{{ row.key }}</code></strong
                ><small>{{ row.policy.purpose || row.key }}</small>
              </div>
              <div class="policy-fields">
                <label
                  ><span>{{ $t('page.mail.callerKey') }}</span
                  ><input v-model="row.draft.callerKey"
                /></label>
                <label
                  ><span>{{ $t('page.mail.codeLength') }}</span
                  ><input
                    v-model.number="row.draft.codeLength"
                    min="4"
                    max="10"
                    type="number"
                /></label>
                <label
                  ><span>{{ $t('page.mail.codeCharset') }}</span
                  ><select v-model="row.draft.charset">
                    <option value="numeric">
                      {{ $t('page.mail.numeric') }}
                    </option>
                    <option value="alphanumeric">
                      {{ $t('page.mail.alphanumeric') }}
                    </option>
                  </select></label
                >
                <label
                  ><span>{{ $t('page.mail.codeTTL') }}</span
                  ><input
                    v-model.number="row.draft.ttlSeconds"
                    min="60"
                    max="1800"
                    type="number"
                /></label>
                <label
                  ><span>{{ $t('page.mail.maxFailures') }}</span
                  ><input
                    v-model.number="row.draft.maxFailures"
                    min="1"
                    type="number"
                /></label>
                <label
                  ><span>{{ $t('page.mail.resendInterval') }}</span
                  ><input
                    v-model.number="row.draft.resendIntervalSeconds"
                    min="1"
                    type="number"
                /></label>
                <label
                  ><span>{{ $t('page.mail.hourlyLimit') }}</span
                  ><input
                    v-model.number="row.draft.hourlyLimit"
                    min="1"
                    type="number"
                /></label>
                <button
                  class="primary"
                  type="button"
                  :disabled="policySaving === row.key"
                  @click="savePolicy(row.policy)"
                >
                  {{
                    policySaving === row.key
                      ? $t('page.mail.saving')
                      : $t('page.mail.savePolicy')
                  }}
                </button>
              </div>
            </div>
          </div>
        </article>
      </section>

      <section
        v-else
        id="mail-panel-records"
        class="tab-content"
        role="tabpanel"
        aria-labelledby="mail-tab-records"
      >
        <article class="table-card">
          <div class="section-heading">
            <div>
              <p class="eyebrow">{{ $t('page.mail.auditTrail') }}</p>
              <h2>{{ $t('page.mail.recordsTitle') }}</h2>
              <p class="description">
                {{ $t('page.mail.recordsDescription') }}
              </p>
            </div>
            <div class="sync-meta">
              <span v-if="lastSyncedAt">{{
                $t('page.mail.lastSynced', { time: formatDate(lastSyncedAt) })
              }}</span
              ><button
                class="primary"
                type="button"
                :disabled="recordsLoading"
                @click="loadRecords"
              >
                {{
                  recordsLoading
                    ? $t('page.mail.syncing')
                    : $t('page.mail.syncLatest')
                }}
              </button>
            </div>
          </div>
          <form class="record-filters" @submit.prevent="queryRecords">
            <label
              ><span>{{ $t('page.mail.recordRange') }}</span
              ><select v-model="recordFilters.range">
                <option value="7d">{{ $t('page.mail.range7d') }}</option>
                <option value="30d">{{ $t('page.mail.range30d') }}</option>
                <option value="all">{{ $t('page.mail.rangeAll') }}</option>
              </select></label
            >
            <label
              ><span>{{ $t('page.mail.recordStatus') }}</span
              ><select v-model="recordFilters.status">
                <option value="">{{ $t('page.mail.allStatuses') }}</option>
                <option value="sent">{{ $t('page.mail.statusSent') }}</option>
                <option value="sending">
                  {{ $t('page.mail.statusSending') }}
                </option>
                <option value="retrying">
                  {{ $t('page.mail.statusRetrying') }}
                </option>
                <option value="failed">
                  {{ $t('page.mail.statusFailed') }}
                </option>
              </select></label
            >
            <label
              ><span>{{ $t('page.mail.recordAccount') }}</span
              ><select v-model="recordFilters.accountId">
                <option value="">{{ $t('page.mail.allAccounts') }}</option>
                <option
                  v-for="account in accounts"
                  :key="account.id"
                  :value="account.id"
                >
                  {{ account.name }}
                </option>
              </select></label
            >
            <label
              ><span>{{ $t('page.mail.recordSource') }}</span
              ><select v-model="recordFilters.source">
                <option value="all">{{ $t('page.mail.allSources') }}</option>
                <option value="business">
                  {{ $t('page.mail.sourceBusiness') }}
                </option>
                <option value="template_test">
                  {{ $t('page.mail.sourceTemplateTest') }}
                </option>
                <option value="system">
                  {{ $t('page.mail.sourceSystem') }}
                </option>
              </select></label
            >
            <label class="filter-keyword"
              ><span>{{ $t('page.mail.recordKeyword') }}</span
              ><input
                v-model="recordFilters.keyword"
                :placeholder="$t('page.mail.recordKeywordPlaceholder')"
                autocomplete="off"
            /></label>
            <div class="filter-actions">
              <button class="primary" type="submit">
                {{ $t('page.mail.query') }}</button
              ><button
                class="secondary"
                type="button"
                @click="resetRecordFilters"
              >
                {{ $t('page.mail.reset') }}
              </button>
            </div>
          </form>
          <div class="record-summary">
            <span>{{
              $t('page.mail.recordsCount', { count: recordRows.length })
            }}</span
            ><span v-if="recordFilters.source !== 'all'">{{
              sourceLabel(recordFilters.source)
            }}</span>
          </div>
          <p v-if="!recordRows.length" class="empty-state">
            {{ $t('page.mail.recordNoData') }}
          </p>
          <div v-else class="table-scroll">
            <table>
              <caption class="sr-only">
                {{
                  $t('page.mail.messageList')
                }}
              </caption>
              <thead>
                <tr>
                  <th>{{ $t('page.mail.subject') }}</th>
                  <th>{{ $t('page.mail.recipients') }}</th>
                  <th>{{ $t('page.mail.sender') }}</th>
                  <th>{{ $t('page.mail.deliverySource') }}</th>
                  <th>{{ $t('page.mail.status') }}</th>
                  <th>{{ $t('page.mail.retryCount') }}</th>
                  <th>{{ $t('page.mail.deliveryTime') }}</th>
                  <th>{{ $t('page.mail.createdAt') }}</th>
                  <th>{{ $t('page.mail.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="message in visibleMessages" :key="message.id">
                  <td>
                    <strong>{{
                      message.subject || $t('page.mail.noSubject')
                    }}</strong
                    ><small
                      ><code>{{
                        message.templateKey || message.callerKey || message.id
                      }}</code></small
                    >
                  </td>
                  <td>
                    {{
                      message.recipients
                        .map((recipient) => recipient.address)
                        .join(', ')
                    }}
                  </td>
                  <td>
                    <strong>{{ accountName(message.smtpAccountId) }}</strong>
                  </td>
                  <td>
                    <span class="source-pill" :class="messageSource(message)">{{
                      sourceLabel(messageSource(message))
                    }}</span>
                  </td>
                  <td>
                    <span
                      class="status-pill"
                      :class="messageStatusTone(message.status)"
                      >{{ statusLabel(message.status) }}</span
                    ><small v-if="message.lastErrorCode" class="error-code">{{
                      message.lastErrorCode
                    }}</small>
                  </td>
                  <td>{{ message.attemptCount }}</td>
                  <td>{{ formatDuration(message) }}</td>
                  <td>
                    <strong>{{ formatDate(message.createdAt) }}</strong
                    ><small>{{
                      message.locale || $t('page.mail.notSet')
                    }}</small>
                  </td>
                  <td>
                    <button
                      class="link-button"
                      type="button"
                      @click="openMessageDetail(message)"
                    >
                      {{ $t('page.mail.detail') }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="recordRows.length" class="pagination">
            <span>{{
              $t('page.mail.pageSummary', {
                current: recordPage,
                total: recordTotalPages,
              })
            }}</span>
            <div class="pagination-actions">
              <label class="page-size"
                ><span>{{ $t('page.mail.pageSize') }}</span
                ><select
                  v-model.number="recordPageSize"
                  @change="recordPage = 1"
                >
                  <option :value="10">10</option>
                  <option :value="20">20</option>
                  <option :value="50">50</option>
                </select></label
              >
              <button
                class="secondary"
                type="button"
                :disabled="recordPage <= 1"
                @click="changeRecordPage(recordPage - 1)"
              >
                {{ $t('page.mail.previous') }}
              </button>
              <button class="page-current" type="button" disabled>
                {{ recordPage }}
              </button>
              <button
                class="secondary"
                type="button"
                :disabled="recordPage >= recordTotalPages"
                @click="changeRecordPage(recordPage + 1)"
              >
                {{ $t('page.mail.next') }}
              </button>
            </div>
          </div>
        </article>
      </section>
    </main>

    <aside
      v-if="recordDetail"
      class="detail-drawer"
      role="dialog"
      aria-modal="true"
      aria-labelledby="record-detail-title"
      @click.self="closeMessageDetail"
    >
      <div class="detail-panel">
        <div class="section-heading">
          <div>
            <p class="eyebrow">{{ $t('page.mail.recordsTitle') }}</p>
            <h2 id="record-detail-title">{{ $t('page.mail.recordDetail') }}</h2>
          </div>
          <button class="secondary" type="button" @click="closeMessageDetail">
            {{ $t('page.mail.close') }}
          </button>
        </div>
        <p v-if="recordDetailLoading" class="helper">
          {{ $t('page.mail.loadingDetail') }}
        </p>
        <dl class="detail-grid">
          <div>
            <dt>{{ $t('page.mail.subject') }}</dt>
            <dd>{{ recordDetail.subject || $t('page.mail.noSubject') }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.mail.recipients') }}</dt>
            <dd>
              {{
                recordDetail.recipients
                  .map((recipient) => recipient.address)
                  .join(', ')
              }}
            </dd>
          </div>
          <div>
            <dt>{{ $t('page.mail.smtpAccount') }}</dt>
            <dd>{{ accountName(recordDetail.smtpAccountId) }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.mail.deliverySource') }}</dt>
            <dd>{{ sourceLabel(messageSource(recordDetail)) }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.mail.status') }}</dt>
            <dd>
              <span
                class="status-pill"
                :class="messageStatusTone(recordDetail.status)"
                >{{ statusLabel(recordDetail.status) }}</span
              >
            </dd>
          </div>
          <div>
            <dt>{{ $t('page.mail.retryCount') }}</dt>
            <dd>{{ recordDetail.attemptCount }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.mail.createdAt') }}</dt>
            <dd>{{ formatDate(recordDetail.createdAt) }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.mail.updatedAt') }}</dt>
            <dd>{{ formatDate(recordDetail.updatedAt) }}</dd>
          </div>
          <div v-if="recordDetail.lastErrorCode">
            <dt>{{ $t('page.mail.errorCode') }}</dt>
            <dd>{{ recordDetail.lastErrorCode }}</dd>
          </div>
          <div v-if="recordDetail.providerMessageId">
            <dt>{{ $t('page.mail.providerMessageId') }}</dt>
            <dd>{{ recordDetail.providerMessageId }}</dd>
          </div>
        </dl>
        <p class="privacy-note">{{ $t('page.mail.bodyStoredEncrypted') }}</p>
      </div>
    </aside>
  </ManagementPage>
</template>

<style scoped>
.mail-page {
  --ink: #172033;
  --muted: #64748b;
  --line: #dbe3ef;
  --surface: #ffffff;
  --surface-soft: #f8fafc;
  --accent: #2563eb;
  --accent-soft: #eff6ff;
  --ok: #15803d;
  --danger: #b42318;
  --warning: #a16207;
  color: var(--ink);
  padding-bottom: 32px;
}

.page-heading,
.section-heading {
  display: flex;
  gap: 20px;
  align-items: flex-start;
  justify-content: space-between;
}

.page-heading {
  padding: 8px 0 18px;
}

.eyebrow {
  margin: 0 0 6px;
  color: #5267d9;
  font-size: 0.72rem;
  font-weight: 800;
  letter-spacing: 0.12em;
}

h1 {
  margin: 0 0 8px;
  font-size: clamp(1.7rem, 4vw, 2.5rem);
  letter-spacing: -0.02em;
}

h2 {
  margin: 0;
  font-size: 1.15rem;
  letter-spacing: -0.01em;
}

h3 {
  margin: 0 0 10px;
  font-size: 0.96rem;
}

.description,
.muted,
small,
.helper {
  color: var(--muted);
}

.description {
  margin: 0;
  line-height: 1.55;
}

.helper {
  margin: 8px 0 0;
  font-size: 0.84rem;
  line-height: 1.5;
}

.heading-actions,
.toolbar-actions,
.sync-meta,
.filter-actions,
.actions,
.pagination,
.pagination-actions,
.form-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.actions {
  min-width: 0;
}

.heading-actions,
.toolbar-actions {
  justify-content: flex-end;
}

.toolbar-actions .count {
  margin-right: 4px;
}

.mail-tabs {
  display: flex;
  gap: 0;
  overflow-x: auto;
  padding: 0 16px;
  margin-top: 6px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 12px 12px 0 0;
}

.mail-tab,
.locale-tab,
.guide-tab {
  min-height: 48px;
  padding: 0 18px;
  color: #52637e;
  background: transparent;
  border: 0;
  border-bottom: 2px solid transparent;
  cursor: pointer;
  font: inherit;
  white-space: nowrap;
}

.mail-tab {
  display: inline-flex;
  gap: 7px;
  align-items: center;
  font-size: 0.88rem;
}

.mail-tab small {
  min-width: 20px;
  padding: 2px 6px;
  color: #52637e;
  background: var(--surface-soft);
  border-radius: 999px;
  text-align: center;
}

.mail-tab:hover,
.mail-tab:focus-visible,
.locale-tab:hover,
.locale-tab:focus-visible,
.guide-tab:hover,
.guide-tab:focus-visible {
  color: var(--accent);
  outline: none;
}

.mail-tab.active,
.locale-tab.active,
.guide-tab.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
  font-weight: 700;
}

.tab-panel {
  min-width: 0;
  padding: 0;
  border: 1px solid var(--line);
  border-top: 0;
  border-radius: 0 0 12px 12px;
  background: #f3f6fb;
}

.tab-content {
  display: grid;
  gap: 16px;
  padding: 16px;
}

.editor-card,
.table-card,
.preview-card {
  min-width: 0;
  padding: 20px;
  background: color-mix(in srgb, var(--surface) 96%, #dbeafe);
  border: 1px solid var(--line);
  border-radius: 12px;
  box-shadow: 0 8px 24px rgb(30 41 59 / 6%);
}

.two-column {
  display: grid;
  grid-template-columns: minmax(300px, 0.8fr) minmax(0, 1.2fr);
  gap: 16px;
  align-items: start;
}

.template-layout {
  grid-template-columns: minmax(360px, 1.2fr) minmax(320px, 0.8fr);
}

.template-side {
  display: grid;
  gap: 16px;
  min-width: 0;
}

.table-scroll {
  margin-top: 16px;
  overflow-x: auto;
}

.compact-scroll {
  margin-top: 10px;
}

table {
  width: 100%;
  min-width: 980px;
  border-collapse: collapse;
}

.compact-table {
  min-width: 760px;
}

th,
td {
  padding: 12px 10px;
  vertical-align: middle;
  text-align: left;
  border-bottom: 1px solid var(--line);
}

th {
  color: #64748b;
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  white-space: nowrap;
}

td {
  font-size: 0.85rem;
}

td small,
.template-row-copy small,
.detail-grid dd {
  display: block;
  margin-top: 4px;
  font-weight: 400;
  line-height: 1.35;
}

.account-form,
.capability-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  margin-top: 18px;
}

.account-form {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.account-form label,
.capability-form label,
.preview-controls label,
.test-form label,
.record-filters label,
.page-size {
  display: grid;
  gap: 6px;
  min-width: 0;
  color: #40516d;
  font-size: 0.78rem;
  font-weight: 700;
}

.account-form input,
.capability-form input,
.capability-form select,
.capability-form textarea,
.preview-controls input,
.preview-controls select,
.test-form input,
.record-filters input,
.record-filters select,
.policy-fields input,
.policy-fields select,
.page-size select {
  box-sizing: border-box;
  width: 100%;
  min-height: 40px;
  padding: 8px 10px;
  color: var(--ink);
  background: var(--surface);
  border: 1px solid #cbd5e1;
  border-radius: 8px;
  font: inherit;
}

.capability-form textarea {
  min-height: 150px;
  resize: vertical;
}

.account-form input {
  min-height: 42px;
}

.account-form label.wide,
.capability-form label.wide,
.capability-form .wide,
.capability-form .form-actions {
  grid-column: 1 / -1;
}

.account-form .form-actions {
  grid-column: 1 / -1;
  justify-content: flex-end;
}

.account-form input:focus,
.capability-form input:focus,
.capability-form select:focus,
.capability-form textarea:focus,
.preview-controls input:focus,
.preview-controls select:focus,
.test-form input:focus,
.record-filters input:focus,
.record-filters select:focus,
.policy-fields input:focus,
.policy-fields select:focus,
button:focus-visible {
  outline: 3px solid rgb(37 99 235 / 24%);
  outline-offset: 2px;
}

.toggle {
  display: flex !important;
  grid-template-columns: auto 1fr;
  gap: 8px !important;
  align-items: center;
  padding-top: 24px;
}

.toggle input {
  width: 18px;
  height: 18px;
  accent-color: var(--accent);
}

.primary,
.secondary,
.actions button,
.link-button,
.page-current {
  min-height: 40px;
  padding: 0 13px;
  cursor: pointer;
  background: var(--surface);
  border: 1px solid #cbd5e1;
  border-radius: 8px;
  font: inherit;
  transition:
    box-shadow 0.15s ease,
    background-color 0.15s ease,
    color 0.15s ease;
}

.primary {
  color: #fff;
  background: var(--accent);
  border-color: var(--accent);
  font-weight: 700;
}

.primary:hover,
.secondary:hover,
.actions button:hover,
.link-button:hover {
  box-shadow: 0 4px 12px rgb(15 23 42 / 12%);
}

.primary:disabled,
.secondary:disabled,
.actions button:disabled,
.page-current:disabled {
  cursor: not-allowed;
  opacity: 0.55;
  box-shadow: none;
}

.link-button {
  color: var(--accent);
  border-color: transparent;
  background: transparent;
}

.danger {
  color: var(--danger) !important;
}

.feedback {
  padding: 12px 14px;
  margin: 12px 0 0;
  border-radius: 9px;
  line-height: 1.4;
}

.feedback.error {
  color: #8b1e1e;
  background: #fef2f2;
  border: 1px solid #fecaca;
}

.feedback.success {
  color: #166534;
  background: #f0fdf4;
  border: 1px solid #bbf7d0;
}

.feedback.info {
  color: #1e40af;
  background: var(--accent-soft);
  border: 1px solid #bfdbfe;
}

.test-feedback {
  display: block;
  flex: 1 0 100%;
  width: 100%;
  flex-basis: 100%;
  min-width: 0;
  max-width: 100%;
  font-size: 0.78rem;
  line-height: 1.35;
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: break-word;
}

.test-feedback.success {
  color: var(--ok);
}

.test-feedback.error {
  color: var(--danger);
}

.test-feedback.info {
  color: #1e40af;
}

.status-pill,
.source-pill,
.locale-badge,
.chip {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  padding: 3px 9px;
  border-radius: 999px;
  font-size: 0.73rem;
  font-weight: 800;
  white-space: nowrap;
}

.status-pill.ok {
  color: var(--ok);
  background: #dcfce7;
}

.status-pill.off,
.status-pill.bad {
  color: #b42318;
  background: #fee2e2;
}

.status-pill.pending {
  color: var(--warning);
  background: #fef3c7;
}

.source-pill.business {
  color: #1d4ed8;
  background: #dbeafe;
}

.source-pill.template_test {
  color: #7c3aed;
  background: #ede9fe;
}

.source-pill.system {
  color: #475569;
  background: #e2e8f0;
}

.locale-badge {
  color: #1d4ed8;
  background: #dbeafe;
}

.chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 3px;
}

.chip {
  min-height: 22px;
  padding: 2px 8px;
  color: #1d4ed8;
  background: #eff6ff;
  border: 1px solid #bfdbfe;
  font-weight: 600;
}

.locale-tabs {
  display: flex;
  gap: 2px;
  border-bottom: 1px solid var(--line);
}

.locale-tab {
  min-height: 38px;
  padding: 0 12px;
  font-size: 0.82rem;
}

.preview-card {
  background: #fbfdff;
}

.preview-controls {
  display: grid;
  gap: 10px;
  margin-top: 14px;
}

.preview-email {
  padding: 16px;
  margin-top: 14px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 10px;
}

.preview-label {
  margin: 0 0 4px;
  color: var(--muted);
  font-size: 0.72rem;
  font-weight: 700;
  text-transform: uppercase;
}

.preview-email strong {
  display: block;
  margin-bottom: 16px;
  font-size: 1rem;
}

.preview-email pre {
  min-height: 90px;
  margin: 0;
  color: #334155;
  white-space: pre-wrap;
  font: inherit;
  line-height: 1.6;
}

.test-form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  min-width: 0;
  gap: 10px;
  align-items: end;
  margin-top: 14px;
}

.test-form label.invalid input {
  border-color: #dc2626;
}

.field-error {
  display: block;
  max-width: 100%;
  color: #b42318;
  font-size: 0.74rem;
  font-weight: 600;
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: break-word;
}

.template-list {
  display: grid;
  gap: 8px;
  margin-top: 14px;
}

.template-row {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  min-width: 0;
  padding: 12px;
  background: var(--surface-soft);
  border: 1px solid var(--line);
  border-radius: 9px;
}

.template-row > .actions {
  flex: 0 1 22rem;
  justify-content: flex-end;
}

.template-row-copy {
  display: grid;
  gap: 3px;
  min-width: 0;
}

.template-row-copy strong,
.template-row-copy code {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.policy-grid {
  display: grid;
  gap: 12px;
  margin-top: 16px;
}

.policy-row {
  padding: 14px;
  background: var(--surface-soft);
  border: 1px solid var(--line);
  border-radius: 10px;
}

.policy-heading small {
  display: block;
  margin-top: 4px;
}

.policy-fields {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  margin-top: 14px;
}

.policy-fields label {
  display: grid;
  gap: 5px;
  color: #40516d;
  font-size: 0.76rem;
  font-weight: 700;
}

.policy-fields > button {
  align-self: end;
}

.record-filters {
  display: grid;
  grid-template-columns: 1fr 1fr 1.2fr 1.2fr 2fr auto;
  gap: 10px;
  align-items: end;
  padding: 14px;
  margin-top: 18px;
  background: var(--surface-soft);
  border: 1px solid var(--line);
  border-radius: 10px;
}

.filter-keyword {
  min-width: 180px;
}

.record-summary {
  display: flex;
  gap: 12px;
  justify-content: space-between;
  padding: 14px 2px 0;
  color: var(--muted);
  font-size: 0.8rem;
}

.error-code {
  color: var(--danger);
}

.pagination {
  justify-content: space-between;
  padding-top: 14px;
  color: var(--muted);
  font-size: 0.8rem;
}

.pagination-actions {
  justify-content: flex-end;
}

.page-size {
  display: inline-flex;
  grid-template-columns: auto auto;
  gap: 6px;
  align-items: center;
  font-size: 0.76rem;
  font-weight: 600;
}

.page-size select {
  min-height: 34px;
  padding: 5px 7px;
}

.page-current {
  min-width: 40px;
  color: var(--accent);
  border-color: var(--accent);
  background: var(--accent-soft);
}

.empty-state {
  padding: 30px 0 10px;
  margin: 0;
  color: var(--muted);
  text-align: center;
}

.guide-drawer,
.detail-drawer {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  justify-content: flex-end;
  background: rgb(15 23 42 / 38%);
}

.guide-panel,
.detail-panel {
  width: min(38rem, 100%);
  height: 100%;
  overflow: auto;
  padding: 24px;
  background: var(--surface);
  box-shadow: -12px 0 32px rgb(15 23 42 / 18%);
}

.guide-tabs {
  display: flex;
  gap: 2px;
  margin-top: 20px;
  border-bottom: 1px solid var(--line);
}

.guide-tab {
  min-height: 40px;
  padding: 0 10px;
  font-size: 0.82rem;
}

.guide-section {
  padding-top: 18px;
}

.guide-list {
  padding-left: 22px;
  margin: 0;
}

.guide-list li {
  margin: 10px 0;
  line-height: 1.55;
}

.guide-note {
  padding: 12px;
  margin: 0;
  color: #475569;
  background: var(--surface-soft);
  border: 1px solid var(--line);
  border-radius: 8px;
  line-height: 1.55;
}

.guide-callout {
  padding: 12px;
  margin-top: 16px;
  color: #1e40af;
  background: var(--accent-soft);
  border: 1px solid #bfdbfe;
  border-radius: 8px;
}

.guide-callout p {
  margin: 6px 0 0;
  line-height: 1.5;
}

.guide-code {
  padding: 12px;
  margin: 10px 0 0;
  overflow-x: auto;
  color: #dbeafe;
  background: #0f172a;
  border-radius: 8px;
  font-size: 0.78rem;
  line-height: 1.55;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.detail-grid {
  display: grid;
  gap: 0;
  margin: 20px 0 0;
  border-top: 1px solid var(--line);
}

.detail-grid > div {
  display: grid;
  grid-template-columns: 9rem minmax(0, 1fr);
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--line);
}

.detail-grid dt {
  color: var(--muted);
  font-size: 0.78rem;
  font-weight: 700;
}

.detail-grid dd {
  margin: 0;
  overflow-wrap: anywhere;
}

.privacy-note {
  padding: 12px;
  margin-top: 18px;
  color: var(--muted);
  background: var(--surface-soft);
  border-radius: 8px;
  font-size: 0.8rem;
  line-height: 1.5;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

@media (max-width: 1180px) {
  .account-form {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .record-filters {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .filter-keyword,
  .filter-actions {
    grid-column: span 2;
  }
}

@media (max-width: 900px) {
  .two-column,
  .template-layout {
    grid-template-columns: 1fr;
  }

  .policy-fields {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .record-filters {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .filter-keyword,
  .filter-actions {
    grid-column: auto;
  }
}

@media (max-width: 620px) {
  .page-heading,
  .section-heading {
    flex-direction: column;
  }

  .heading-actions,
  .toolbar-actions,
  .sync-meta {
    justify-content: flex-start;
  }

  .account-form,
  .capability-form,
  .policy-fields,
  .record-filters {
    grid-template-columns: 1fr;
  }

  .account-form .form-actions,
  .capability-form label.wide,
  .capability-form .wide,
  .capability-form .form-actions,
  .filter-keyword,
  .filter-actions {
    grid-column: auto;
  }

  .toggle {
    padding-top: 0;
  }

  .test-form {
    grid-template-columns: 1fr;
  }

  .template-row {
    align-items: flex-start;
    flex-direction: column;
  }

  .record-summary,
  .pagination {
    align-items: flex-start;
    flex-direction: column;
  }

  .pagination-actions {
    justify-content: flex-start;
  }

  .detail-grid > div {
    grid-template-columns: 1fr;
    gap: 4px;
  }

  .guide-panel,
  .detail-panel {
    padding: 18px;
  }
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before {
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
    animation-duration: 0.01ms !important;
  }
}
</style>
