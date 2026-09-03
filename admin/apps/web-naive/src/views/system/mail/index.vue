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
import { ManagementPage } from '@vben/common-ui';
import { preferences } from '@vben/preferences';
import { commonCapabilitiesGuide } from '@vben/types';

import {
  deleteNotificationCallerApi,
  deleteNotificationTemplateApi,
  deleteSMTPAccountApi,
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
const error = ref('');
const guideOpen = ref(false);
const guideDrawer = ref<HTMLElement | null>(null);
let guideReturnFocus: HTMLElement | null = null;
const guide = computed(
  () =>
    commonCapabilitiesGuide.mail.locales?.[
      preferences.app.locale === 'zh-CN' ? 'zh-CN' : 'en-US'
    ] ?? commonCapabilitiesGuide.mail,
);
async function openGuide() {
  guideReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  guideOpen.value = true;
  await nextTick();
  guideDrawer.value?.focus();
}
function closeGuide() {
  guideOpen.value = false;
  const target = guideReturnFocus;
  guideReturnFocus = null;
  void nextTick(() => target?.focus());
}
const notice = ref('');
const testResult = ref('');
type TestFeedbackTone = 'error' | 'info' | 'success';
type TestFeedback = { message: string; tone: TestFeedbackTone };
const testResultTone = ref<TestFeedbackTone>('info');
const accountTestFeedback = reactive<Record<string, TestFeedback>>({});
const templateTestFeedback = reactive<Record<string, TestFeedback>>({});

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
    routingPolicy: (value.routingPolicy ?? value.strategy ?? 'weighted_random') as RoutingPolicy,
    weights: Object.entries(value.weights ?? {})
      .map(([id, weight]) => `${id}=${weight}`)
      .join(', '),
  });
}

async function saveCaller() {
  if (!canManage.value || !callerForm.key.trim() || !callerForm.name.trim()) return;
  callerSaving.value = true;
  error.value = '';
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
    notice.value = String($t('page.mail.callerSaved'));
    resetCallerForm();
    await load();
  } catch {
    error.value = String($t('page.mail.callerSaveError'));
  } finally {
    callerSaving.value = false;
  }
}

async function removeCaller(value: NotificationCaller) {
  if (!canManage.value || !window.confirm(String($t('page.mail.callerDeleteConfirm', { name: value.name })))) return;
  try {
    await deleteNotificationCallerApi(value.id);
    notice.value = String($t('page.mail.callerDeleted'));
    if (callerEditingId.value === value.id) resetCallerForm();
    await load();
  } catch {
    error.value = String($t('page.mail.callerDeleteError'));
  }
}

function editTemplate(value: NotificationTemplate) {
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
    defaultLocale: (value.defaultLocale === 'en-US' ? 'en-US' : 'zh-CN') as 'en-US' | 'zh-CN',
    variables: (value.variables ?? []).join(', '),
    zhSubject: zh?.subject ?? (value.defaultLocale === 'zh-CN' ? value.subject ?? '' : ''),
    zhBody: zh?.body ?? (value.defaultLocale === 'zh-CN' ? value.body ?? '' : ''),
    enSubject: en?.subject ?? (value.defaultLocale === 'en-US' ? value.subject ?? '' : ''),
    enBody: en?.body ?? (value.defaultLocale === 'en-US' ? value.body ?? '' : ''),
    enabled: value.enabled !== false,
    published: value.published === true,
    testVariables: serializeVariables(testVariables),
  });
}

function templatePayload() {
  const locales: Record<string, { body: string; locale: string; subject: string; }> = {};
  if (templateForm.zhSubject.trim() || templateForm.zhBody.trim()) {
    locales['zh-CN'] = { locale: 'zh-CN', subject: templateForm.zhSubject.trim(), body: templateForm.zhBody };
  }
  if (templateForm.enSubject.trim() || templateForm.enBody.trim()) {
    locales['en-US'] = { locale: 'en-US', subject: templateForm.enSubject.trim(), body: templateForm.enBody };
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
  error.value = '';
  try {
    await saveNotificationTemplateApi(templatePayload(), templateEditingId.value);
    notice.value = String($t('page.mail.templateSaved'));
    resetTemplateForm();
    await load();
  } catch {
    error.value = String($t('page.mail.templateSaveError'));
  } finally {
    templateSaving.value = false;
  }
}

async function publishTemplate(value: NotificationTemplate) {
  if (!canManage.value) return;
  try {
    await publishNotificationTemplateApi(templateKey(value));
    notice.value = String($t('page.mail.templatePublished'));
    await load();
  } catch {
    error.value = String($t('page.mail.templatePublishError'));
  }
}

async function removeTemplate(value: NotificationTemplate) {
  if (!canManage.value || !window.confirm(String($t('page.mail.templateDeleteConfirm', { name: templateKey(value) })))) return;
  try {
    await deleteNotificationTemplateApi(templateKey(value));
    notice.value = String($t('page.mail.templateDeleted'));
    if (templateEditingId.value === value.id) resetTemplateForm();
    await load();
  } catch {
    error.value = String($t('page.mail.templateDeleteError'));
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
  if (key.includes('code') || key.includes('otp') || key.includes('token')) return '123456';
  if (key.includes('expire') || key.includes('ttl')) return english ? '10 minutes' : '10 分钟';
  if (key.includes('email')) return 'user@example.test';
  if (key.includes('location') || key.includes('ip')) return english ? 'Sample location' : '示例地点';
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
  return value && typeof value === 'object' ? (value as Record<string, unknown>) : undefined;
}

function responseData(value: unknown) {
  const record = asRecord(value);
  const nested = asRecord(record?.data);
  return nested && ('status' in nested || 'messageId' in nested) ? nested : (record ?? {});
}

function errorData(value: unknown) {
  const record = asRecord(value);
  const response = asRecord(record?.response);
  const responseBody = asRecord(response?.data);
  if (responseBody) return responseBody;
  const nested = asRecord(record?.data);
  return nested && ('message' in nested || 'errors' in nested || 'code' in nested)
    ? nested
    : (record ?? {});
}

function formatTestError(value: unknown, fallbackKey: string, codeKey: string) {
  const payload = errorData(value);
  const first = Array.isArray(payload.errors) ? asRecord(payload.errors[0]) : undefined;
  const messageKey = typeof first?.messageKey === 'string' ? first.messageKey : '';
  const params = asRecord(first?.params);
  if (messageKey === 'notification.template.variableMissing') {
    return String($t('page.mail.templateMissingVariable', { variable: String(params?.variable ?? '') }));
  }
  if (messageKey === 'notification.template.variableInvalid') {
    return String($t('page.mail.templateInvalidVariable', { variable: String(params?.variable ?? '') }));
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

function setTestFeedback(target: Record<string, TestFeedback>, id: string, message: string, tone: TestFeedbackTone) {
  const feedback = { message, tone };
  target[id] = feedback;
  testResult.value = message;
  testResultTone.value = tone;
}

function feedbackMessage(target: Record<string, TestFeedback>, id: string) {
  return target[id]?.message ?? '';
}

function feedbackTone(target: Record<string, TestFeedback>, id: string) {
  return target[id]?.tone ?? 'info';
}

async function testTemplate(value: NotificationTemplate) {
  if (!canManage.value || !templateForm.testRecipient.trim()) return;
  templateTesting.value = value.id;
  error.value = '';
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
    const tone: TestFeedbackTone = status === 'failed' ? 'error' : status === 'queued' ? 'info' : 'success';
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
      formatTestError(caught, 'page.mail.templateTestError', 'page.mail.templateTestErrorWithCode'),
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
      resendIntervalSeconds: value.resendIntervalSeconds ?? value.resendAfterSeconds ?? 60,
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
  error.value = '';
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
    notice.value = String($t('page.mail.policySaved'));
    await load();
  } catch {
    error.value = String($t('page.mail.policySaveError'));
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
  notice.value = '';
  testResult.value = '';
}

async function load() {
  loading.value = true;
  error.value = '';
  try {
    const [accountResult, messageResult] = await Promise.all([
      listSMTPAccountsApi(),
      listEmailMessagesApi({ limit: 20, offset: 0 }),
    ]);
    accounts.value = Array.isArray(accountResult) ? accountResult : [];
    messages.value = messageResult?.items ?? [];
    // Common capability registries are optional during a rolling upgrade; the
    // legacy account/record view remains usable while each registry catches up.
    const [callerResult, templateResult, policyResult] = await Promise.allSettled([
      listNotificationCallersApi(),
      listNotificationTemplatesApi(),
      listVerificationPoliciesApi(),
    ]);
    callers.value = callerResult.status === 'fulfilled' ? callerResult.value : [];
    templates.value = templateResult.status === 'fulfilled' ? templateResult.value : [];
    policies.value = policyResult.status === 'fulfilled' ? policyResult.value : [];
    syncPolicyDrafts(policies.value);
  } catch {
    error.value = String($t('page.mail.loadError'));
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (!canManage.value) return;
  if (!form.name.trim() || !form.host.trim() || !form.fromEmail.trim()) {
    error.value = String($t('page.mail.saveErrorRequired'));
    return;
  }
  saving.value = true;
  error.value = '';
  notice.value = '';
  try {
    await saveSMTPAccountApi({ ...form }, editingId.value);
    notice.value = String(
      $t(editingId.value ? 'page.mail.updated' : 'page.mail.created'),
    );
    resetForm();
    await load();
  } catch {
    error.value = String($t('page.mail.saveError'));
  } finally {
    saving.value = false;
  }
}

async function test(account: SMTPAccount) {
  if (!canManage.value) return;
  testingId.value = account.id;
  error.value = '';
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
  } catch (caught) {
    setTestFeedback(
      accountTestFeedback,
      account.id,
      formatTestError(caught, 'page.mail.testError', 'page.mail.testErrorWithCode'),
      'error',
    );
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
    notice.value = String($t('page.mail.deleted'));
    if (editingId.value === account.id) resetForm();
    await load();
  } catch {
    error.value = String($t('page.mail.deleteError'));
  } finally {
    testingId.value = '';
  }
}

function statusLabel(status: string) {
  if (status === 'sent') return String($t('page.mail.statusSent'));
  if (status === 'failed') return String($t('page.mail.statusFailed'));
  return status;
}

onMounted(load);
</script>

<template>
  <ManagementPage
    class="mail-page"
    :aria-busy="loading || saving"
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
        <button class="secondary" type="button" :disabled="loading" @click="load">
          {{ $t('page.mail.refresh') }}
        </button>
      </div>
    </header>

    <aside v-if="guideOpen" ref="guideDrawer" class="guide-drawer" role="dialog" aria-modal="true" aria-labelledby="mail-guide-title" @click.self="closeGuide" @keydown.esc="closeGuide" tabindex="-1">
      <div class="guide-panel">
        <div class="section-heading"><div><p class="eyebrow">{{ $t('page.mail.guideButton') }}</p><h2 id="mail-guide-title">{{ guide.title }}</h2></div><button class="secondary" type="button" @click="closeGuide">{{ $t('page.mail.guideClose') }}</button></div>
        <p class="description">{{ $t('page.mail.guideAudience') }}</p>
        <h3>{{ $t('page.mail.guideNormal') }}</h3><ol><li v-for="step in guide.steps" :key="step">{{ step }}</li></ol>
        <h3>{{ $t('page.mail.guideDeveloper') }}</h3><ul><li v-for="step in guide.developer" :key="step">{{ step }}</li></ul>
      </div>
    </aside>

    <p v-if="error" class="feedback error" role="alert">{{ error }}</p>
    <p v-if="notice" class="feedback success" role="status">{{ notice }}</p>
    <p v-if="testResult" class="feedback" :class="testResultTone" role="status" aria-live="polite">
      {{ testResult }}
    </p>

    <section
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
        <label><span>{{ $t('page.mail.accountName') }}</span><input v-model="form.name" autocomplete="off" required /></label>
        <label><span>{{ $t('page.mail.smtpHost') }}</span><input v-model="form.host" autocomplete="off" required /></label>
        <label><span>{{ $t('page.mail.smtpPort') }}</span><input
            v-model.number="form.port"
            min="1"
            max="65535"
            type="number"
            required
        /></label>
        <label><span>{{ $t('page.mail.smtpUsername') }}</span><input v-model="form.username" autocomplete="username" /></label>
        <label><span>{{ $t('page.mail.smtpPassword') }}</span><input
            v-model="form.password"
            autocomplete="new-password"
            type="password"
            :placeholder="editingId ? $t('page.mail.passwordPlaceholder') : ''"
        /></label>
        <label><span>{{ $t('page.mail.weight') }}</span><input v-model.number="form.weight" min="1" type="number" required /></label>
        <label><span>{{ $t('page.mail.fromEmail') }}</span><input
            v-model="form.fromEmail"
            autocomplete="email"
            type="email"
            required
        /></label>
        <label><span>{{ $t('page.mail.fromName') }}</span><input v-model="form.fromName" /></label>
        <label class="toggle"><input v-model="form.enabled" type="checkbox" /><span>{{
            $t('page.mail.enableSmtp')
          }}</span></label>
        <label class="toggle"><input v-model="form.implicitTls" type="checkbox" /><span>{{
            $t('page.mail.implicitTls')
          }}</span></label>
        <div class="form-actions">
          <button class="primary" type="submit" :disabled="saving">
            {{ saving ? $t('page.mail.saving') : $t('page.mail.save') }}
          </button>
        </div>
      </form>
    </section>

    <section v-if="canManage" class="capability-grid" :aria-label="$t('page.mail.commonCapabilities')">
      <article class="editor-card capability-card" aria-labelledby="caller-editor-title">
        <div class="section-heading">
          <div>
            <p class="eyebrow">{{ $t('page.mail.callerEyebrow') }}</p>
            <h2 id="caller-editor-title">
              {{ callerEditingId ? $t('page.mail.callerEdit') : $t('page.mail.callerNew') }}
            </h2>
          </div>
          <button v-if="callerEditingId" class="secondary" type="button" @click="resetCallerForm">
            {{ $t('page.mail.cancelEdit') }}
          </button>
        </div>
        <p class="helper">{{ $t('page.mail.callerHelp') }}</p>
        <form class="capability-form" @submit.prevent="saveCaller">
          <label><span>{{ $t('page.mail.callerKey') }}</span><input v-model="callerForm.key" :disabled="Boolean(callerEditingId)" autocomplete="off" required /></label>
          <label><span>{{ $t('page.mail.callerName') }}</span><input v-model="callerForm.name" autocomplete="off" required /></label>
          <label><span>{{ $t('page.mail.callerModule') }}</span><input v-model="callerForm.module" autocomplete="off" /></label>
          <label><span>{{ $t('page.mail.callerAccounts') }}</span><input v-model="callerForm.smtpAccountIds" :placeholder="$t('page.mail.callerAccountsPlaceholder')" autocomplete="off" /></label>
          <label><span>{{ $t('page.mail.callerDefaultAccount') }}</span><input v-model="callerForm.defaultAccountId" autocomplete="off" /></label>
          <label><span>{{ $t('page.mail.callerRouting') }}</span><select v-model="callerForm.routingPolicy"><option value="weighted_random">{{ $t('page.mail.weightedRandom') }}</option><option value="round_robin">{{ $t('page.mail.roundRobin') }}</option></select></label>
          <label><span>{{ $t('page.mail.callerWeights') }}</span><input v-model="callerForm.weights" :placeholder="$t('page.mail.callerWeightsPlaceholder')" autocomplete="off" /></label>
          <label class="toggle"><input v-model="callerForm.enabled" type="checkbox" /><span>{{ $t('page.mail.enabled') }}</span></label>
          <div class="form-actions"><button class="primary" type="submit" :disabled="callerSaving">{{ callerSaving ? $t('page.mail.saving') : $t('page.mail.saveCaller') }}</button></div>
        </form>
        <div v-if="callers.length" class="capability-list">
          <div v-for="caller in callers" :key="caller.id" class="capability-row">
            <div><strong>{{ caller.name }}</strong><small><code>{{ callerKey(caller) }}</code> · {{ caller.routingPolicy || caller.strategy || $t('page.mail.weightedRandom') }}</small></div>
            <div class="actions"><span class="status-pill" :class="[caller.enabled ? 'ok' : 'off']">{{ caller.enabled ? $t('page.mail.enabled') : $t('page.mail.disabled') }}</span><button type="button" @click="editCaller(caller)">{{ $t('page.mail.edit') }}</button><button v-if="!caller.systemOwned" class="danger" type="button" @click="removeCaller(caller)">{{ $t('page.mail.delete') }}</button></div>
          </div>
        </div>
      </article>

      <article class="editor-card capability-card" aria-labelledby="template-editor-title">
        <div class="section-heading">
          <div>
            <p class="eyebrow">{{ $t('page.mail.templateEyebrow') }}</p>
            <h2 id="template-editor-title">
              {{ templateEditingId ? $t('page.mail.templateEdit') : $t('page.mail.templateNew') }}
            </h2>
          </div>
          <button v-if="templateEditingId" class="secondary" type="button" @click="resetTemplateForm">
            {{ $t('page.mail.cancelEdit') }}
          </button>
        </div>
        <p class="helper">{{ $t('page.mail.templateHelp') }}</p>
        <form class="capability-form" @submit.prevent="saveTemplate">
          <label><span>{{ $t('page.mail.templateKey') }}</span><input v-model="templateForm.key" :disabled="Boolean(templateEditingId)" autocomplete="off" required /></label>
          <label><span>{{ $t('page.mail.templatePurpose') }}</span><input v-model="templateForm.purpose" autocomplete="off" /></label>
          <label><span>{{ $t('page.mail.templateDefaultLocale') }}</span><select v-model="templateForm.defaultLocale"><option value="zh-CN">zh-CN</option><option value="en-US">en-US</option></select></label>
          <label><span>{{ $t('page.mail.templateVariables') }}</span><input v-model="templateForm.variables" :placeholder="$t('page.mail.templateVariablesPlaceholder')" autocomplete="off" /></label>
          <label><span>{{ $t('page.mail.templateZhSubject') }}</span><input v-model="templateForm.zhSubject" autocomplete="off" /></label>
          <label><span>{{ $t('page.mail.templateEnSubject') }}</span><input v-model="templateForm.enSubject" autocomplete="off" /></label>
          <label class="wide"><span>{{ $t('page.mail.templateZhBody') }}</span><textarea v-model="templateForm.zhBody" rows="3"></textarea></label>
          <label class="wide"><span>{{ $t('page.mail.templateEnBody') }}</span><textarea v-model="templateForm.enBody" rows="3"></textarea></label>
          <label class="toggle"><input v-model="templateForm.enabled" type="checkbox" /><span>{{ $t('page.mail.enabled') }}</span></label>
          <label class="toggle"><input v-model="templateForm.published" type="checkbox" /><span>{{ $t('page.mail.templatePublishNow') }}</span></label>
          <div class="form-actions"><button class="primary" type="submit" :disabled="templateSaving">{{ templateSaving ? $t('page.mail.saving') : $t('page.mail.saveTemplate') }}</button></div>
        </form>
        <div class="template-test">
          <h3>{{ $t('page.mail.templateTest') }}</h3>
          <div class="capability-form">
            <label><span>{{ $t('page.mail.testRecipient') }}</span><input v-model="templateForm.testRecipient" type="email" autocomplete="email" /></label>
            <label><span>{{ $t('page.mail.testLocale') }}</span><select v-model="templateForm.testLocale"><option value="zh-CN">zh-CN</option><option value="en-US">en-US</option></select></label>
            <label class="wide"><span>{{ $t('page.mail.testVariables') }}</span><input v-model="templateForm.testVariables" :placeholder="$t('page.mail.testVariablesPlaceholder')" autocomplete="off" /></label>
            <p class="helper wide">{{ $t('page.mail.testVariablesHint') }}</p>
          </div>
        </div>
        <div v-if="templates.length" class="capability-list">
          <div v-for="template in templates" :key="template.id" class="capability-row template-row">
            <div><strong><code>{{ templateKey(template) }}</code></strong><small>{{ template.purpose || templateKey(template) }} · {{ template.published ? $t('page.mail.templatePublishedState') : $t('page.mail.templateDraftState') }}</small></div>
            <div class="actions"><button type="button" @click="editTemplate(template)">{{ $t('page.mail.edit') }}</button><button type="button" :disabled="templateTesting === template.id || !templateForm.testRecipient.trim()" @click="testTemplate(template)">{{ templateTesting === template.id ? $t('page.mail.testing') : $t('page.mail.testTemplate') }}</button><button v-if="!template.published" type="button" @click="publishTemplate(template)">{{ $t('page.mail.publishTemplate') }}</button><button class="danger" type="button" @click="removeTemplate(template)">{{ $t('page.mail.delete') }}</button><span v-if="templateTestFeedback[template.id]" class="test-feedback" :class="feedbackTone(templateTestFeedback, template.id)" role="status">{{ feedbackMessage(templateTestFeedback, template.id) }}</span></div>
          </div>
        </div>
      </article>
    </section>

    <section v-if="canManage && policyRows.length" class="table-card policy-card" aria-labelledby="policy-title">
      <div class="section-heading"><div><p class="eyebrow">{{ $t('page.mail.policyEyebrow') }}</p><h2 id="policy-title">{{ $t('page.mail.policyTitle') }}</h2></div><span class="muted">{{ $t('page.mail.policyHelp') }}</span></div>
      <div class="policy-grid">
        <div v-for="row in policyRows" :key="row.key" class="policy-row">
          <div class="policy-heading"><strong><code>{{ row.key }}</code></strong><small>{{ row.policy.purpose || row.key }}</small></div>
          <div class="policy-fields">
            <label><span>{{ $t('page.mail.callerKey') }}</span><input v-model="row.draft.callerKey" /></label>
            <label><span>{{ $t('page.mail.codeLength') }}</span><input v-model.number="row.draft.codeLength" min="4" max="10" type="number" /></label>
            <label><span>{{ $t('page.mail.codeCharset') }}</span><select v-model="row.draft.charset"><option value="numeric">{{ $t('page.mail.numeric') }}</option><option value="alphanumeric">{{ $t('page.mail.alphanumeric') }}</option></select></label>
            <label><span>{{ $t('page.mail.codeTTL') }}</span><input v-model.number="row.draft.ttlSeconds" min="60" max="1800" type="number" /></label>
            <label><span>{{ $t('page.mail.maxFailures') }}</span><input v-model.number="row.draft.maxFailures" min="1" type="number" /></label>
            <label><span>{{ $t('page.mail.resendInterval') }}</span><input v-model.number="row.draft.resendIntervalSeconds" min="1" type="number" /></label>
            <label><span>{{ $t('page.mail.hourlyLimit') }}</span><input v-model.number="row.draft.hourlyLimit" min="1" type="number" /></label>
            <button class="primary" type="button" :disabled="policySaving === row.key" @click="savePolicy(row.policy)">{{ policySaving === row.key ? $t('page.mail.saving') : $t('page.mail.savePolicy') }}</button>
          </div>
        </div>
      </div>
    </section>

    <section class="table-card" aria-labelledby="mail-table-title">
      <div class="section-heading">
        <div>
          <p class="eyebrow">{{ $t('page.mail.poolStatus') }}</p>
          <h2 id="mail-table-title">{{ $t('page.mail.accountPool') }}</h2>
        </div>
        <span class="count">{{
          $t('page.mail.accountCount', { count: accounts.length })
        }}</span>
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
              <th>{{ $t('page.mail.weight') }}</th>
              <th>{{ $t('page.mail.sender') }}</th>
              <th>{{ $t('page.mail.status') }}</th>
              <th>{{ $t('page.mail.security') }}</th>
              <th>{{ $t('page.mail.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="account in accounts" :key="account.id">
              <td>
                <strong>{{ account.name }}</strong><small>{{
                  account.username || $t('page.mail.noAuthUsername')
                }}</small>
              </td>
              <td>{{ account.host }}</td>
              <td>{{ account.port }}</td>
              <td>{{ account.weight }}</td>
              <td>{{ account.fromEmail }}</td>
              <td>
                <span
                  class="status-pill"
                  :class="[account.enabled ? 'ok' : 'off']"
                  >{{
                    account.enabled
                      ? $t('page.mail.enabled')
                      : $t('page.mail.disabled')
                  }}</span>
              </td>
              <td>
                {{
                  account.implicitTls
                    ? $t('page.mail.implicitTlsShort')
                    : $t('page.mail.startTlsPlain')
                }}
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
</button><button v-if="canManage" type="button" @click="edit(account)">
                  {{ $t('page.mail.edit') }}
</button><button
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
    </section>

    <section class="table-card" aria-labelledby="message-table-title">
      <div class="section-heading">
        <div>
          <p class="eyebrow">{{ $t('page.mail.auditTrail') }}</p>
          <h2 id="message-table-title">
            {{ $t('page.mail.deliveryRecords') }}
          </h2>
        </div>
        <span class="muted">{{ $t('page.mail.bodyStoredEncrypted') }}</span>
      </div>
      <p v-if="messages.length === 0" class="empty-state">
        {{ $t('page.mail.emptyMessages') }}
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
              <th>{{ $t('page.mail.status') }}</th>
              <th>{{ $t('page.mail.attempts') }}</th>
              <th>{{ $t('page.mail.createdAt') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="message in messages" :key="message.id">
              <td>{{ message.subject }}</td>
              <td>
                {{
                  message.recipients
                    .map((recipient) => recipient.address)
                    .join(', ')
                }}
              </td>
              <td>
                <span
                  class="status-pill"
                  :class="[message.status === 'sent' ? 'ok' : 'off']"
                  >{{ statusLabel(message.status) }}</span>
              </td>
              <td>{{ message.attemptCount }}</td>
              <td>{{ new Date(message.createdAt).toLocaleString() }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </ManagementPage>
</template>

<style scoped>
.mail-page {
  --ink: #172033;
  --muted: #64748b;
  --line: #dbe3ef;
  --accent: #2563eb;
  --ok: #15803d;
  --danger: #b42318;

  color: var(--ink);
}

.page-heading,
.section-heading {
  display: flex;
  gap: 20px;
  align-items: flex-start;
  justify-content: space-between;
}

.eyebrow {
  margin: 0 0 6px;
  font-size: 0.72rem;
  font-weight: 800;
  color: #5267d9;
  letter-spacing: 0.12em;
}

h1 {
  margin: 0 0 8px;
  font-size: clamp(1.7rem, 4vw, 2.5rem);
}

h2 {
  margin: 0;
  font-size: 1.15rem;
}

.description,
.muted,
small {
  color: var(--muted);
}

.count {
  font-size: 0.9rem;
  color: var(--muted);
}

.editor-card,
.table-card {
  padding: 24px;
  margin-top: 24px;
  background: color-mix(in srgb, white 94%, #dbeafe);
  border: 1px solid var(--line);
  border-radius: 16px;
  box-shadow: 0 10px 28px rgb(30 41 59 / 7%);
}

.capability-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 24px;
  align-items: start;
}

.capability-card {
  min-width: 0;
}

.helper {
  margin: 10px 0 0;
  color: var(--muted);
  font-size: 0.85rem;
  line-height: 1.5;
}

.capability-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-top: 16px;
}

.capability-form label {
  display: grid;
  gap: 6px;
  min-width: 0;
  font-size: 0.82rem;
  font-weight: 700;
}

.capability-form label.wide,
.capability-form .wide,
.capability-form .form-actions {
  grid-column: 1 / -1;
}

.capability-form input,
.capability-form select,
.capability-form textarea {
  box-sizing: border-box;
  width: 100%;
  min-height: 38px;
  padding: 8px 10px;
  color: var(--ink);
  background: white;
  border: 1px solid #cbd5e1;
  border-radius: 8px;
  font: inherit;
}

.capability-form textarea {
  resize: vertical;
}

.capability-list {
  display: grid;
  gap: 8px;
  margin-top: 18px;
}

.capability-row,
.policy-row {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding: 11px 12px;
  background: rgb(248 250 252 / 78%);
  border: 1px solid var(--line);
  border-radius: 10px;
}

.capability-row small,
.policy-heading small {
  display: block;
  margin-top: 3px;
  color: var(--muted);
  font-size: 0.76rem;
}

.capability-row .actions {
  justify-content: flex-end;
}

.template-test {
  padding-top: 18px;
  margin-top: 18px;
  border-top: 1px solid var(--line);
}

.template-test h3 {
  margin: 0;
  font-size: 0.95rem;
}

.policy-card {
  margin-top: 24px;
}

.policy-grid {
  display: grid;
  gap: 12px;
  margin-top: 16px;
}

.policy-row {
  display: block;
}

.policy-fields {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  margin-top: 12px;
}

.policy-fields label {
  display: grid;
  gap: 5px;
  font-size: 0.78rem;
  font-weight: 700;
}

.policy-fields input,
.policy-fields select {
  box-sizing: border-box;
  width: 100%;
  min-height: 36px;
  padding: 6px 8px;
  border: 1px solid #cbd5e1;
  border-radius: 7px;
}

.policy-fields > button {
  align-self: end;
}

.account-form {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
  margin-top: 20px;
}

.account-form label {
  display: grid;
  gap: 7px;
  font-size: 0.85rem;
  font-weight: 700;
}

.account-form input {
  min-height: 42px;
  padding: 0 11px;
  color: var(--ink);
  background: white;
  border: 1px solid #cbd5e1;
  border-radius: 9px;
}

.account-form input:focus,
button:focus-visible {
  outline: 3px solid rgb(37 99 235 / 25%);
  outline-offset: 2px;
}

.toggle {
  display: flex !important;
  grid-template-columns: auto 1fr;
  gap: 9px !important;
  align-items: center;
  padding-top: 27px;
}

.toggle input {
  width: 18px;
  height: 18px;
}

.form-actions {
  display: flex;
  align-items: end;
}

.primary,
.secondary,
.actions button {
  min-height: 40px;
  padding: 0 14px;
  cursor: pointer;
  background: white;
  border: 1px solid #cbd5e1;
  border-radius: 9px;
  transition:
    transform 0.18s ease,
    box-shadow 0.18s ease;
}

.primary {
  font-weight: 700;
  color: white;
  background: var(--accent);
  border-color: var(--accent);
}

.primary:hover,
.secondary:hover,
.actions button:hover {
  box-shadow: 0 5px 14px rgb(15 23 42 / 12%);
  transform: translateY(-1px);
}

.danger {
  color: var(--danger);
}

.feedback {
  padding: 12px 14px;
  margin: 20px 0 0;
  border-radius: 10px;
}

.error {
  color: #8b1e1e;
  background: #fef2f2;
}

.success {
  color: #166534;
  background: #f0fdf4;
}

.info {
  color: #1e40af;
  background: #eff6ff;
}

.test-feedback {
  flex-basis: 100%;
  font-size: 0.78rem;
  line-height: 1.35;
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

.table-scroll {
  margin-top: 18px;
  overflow-x: auto;
}

table {
  width: 100%;
  min-width: 920px;
  border-collapse: collapse;
}

th,
td {
  padding: 13px 10px;
  vertical-align: middle;
  text-align: left;
  border-bottom: 1px solid var(--line);
}

th {
  font-size: 0.76rem;
  color: var(--muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

td small {
  display: block;
  margin-top: 3px;
  font-weight: 400;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}

.status-pill {
  display: inline-flex;
  padding: 4px 9px;
  font-size: 0.75rem;
  font-weight: 800;
  border-radius: 999px;
}

.status-pill.ok {
  color: var(--ok);
  background: #dcfce7;
}

.status-pill.off {
  color: #92400e;
  background: #fef3c7;
}

.empty-state {
  margin: 18px 0 0;
  color: var(--muted);
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

@media (max-width: 900px) {
  .capability-grid {
    grid-template-columns: 1fr;
  }

  .account-form {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .policy-fields {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 560px) {
  .account-form,
  .capability-form,
  .policy-fields {
    grid-template-columns: 1fr;
  }

  .toggle {
    padding-top: 0;
  }

  .page-heading,
  .section-heading {
    flex-direction: column;
  }
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
    animation-duration: 0.01ms !important;
  }
}

.guide-drawer { position: fixed; inset: 0; z-index: 1000; display: flex; justify-content: flex-end; background: rgb(15 23 42 / 38%); }
.guide-panel { width: min(34rem, 100%); height: 100%; overflow: auto; padding: 24px; background: white; box-shadow: -12px 0 32px rgb(15 23 42 / 18%); }
.guide-panel h3 { margin-top: 24px; }
.guide-panel li { margin: 8px 0; line-height: 1.5; }
.heading-actions { display:flex; gap: 8px; flex-wrap: wrap; }
@media (prefers-reduced-motion: reduce) { .guide-drawer * { transition: none !important; } }
</style>
