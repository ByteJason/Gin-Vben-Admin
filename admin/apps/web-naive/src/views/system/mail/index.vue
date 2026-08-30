<script setup lang="ts">
import type {
  EmailMessage,
  SMTPAccount,
  SMTPAccountInput,
} from '#/api/core/mail';

import { computed, onMounted, reactive, ref } from 'vue';

import { useAccess } from '@vben/access';
import { ManagementPage } from '@vben/common-ui';
import { $t } from '#/locales';

import {
  deleteSMTPAccountApi,
  listEmailMessagesApi,
  listSMTPAccountsApi,
  saveSMTPAccountApi,
  testSMTPAccountApi,
} from '#/api/core/mail';

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
const notice = ref('');
const testResult = ref('');

const hasAccounts = computed(() => accounts.value.length > 0);

function resetForm() {
  Object.assign(form, emptyForm());
  editingId.value = undefined;
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
    testResult.value =
      result.status === 'ok'
        ? String($t('page.mail.testSuccess', { name: account.name }))
        : String(
            $t('page.mail.testFailure', {
              code: result.code ?? 'provider_unavailable',
              name: account.name,
              stage: result.stage ?? 'unknown',
            }),
          );
  } catch {
    error.value = String($t('page.mail.testError'));
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
      <button class="secondary" type="button" :disabled="loading" @click="load">
        {{ $t('page.mail.refresh') }}
      </button>
    </header>

    <p v-if="error" class="feedback error" role="alert">{{ error }}</p>
    <p v-if="notice" class="feedback success" role="status">{{ notice }}</p>
    <p v-if="testResult" class="feedback info" role="status">
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
            :placeholder="editingId ? $t('page.mail.passwordPlaceholder') : ''"
        /></label>
        <label
          ><span>{{ $t('page.mail.weight') }}</span
          ><input v-model.number="form.weight" min="1" type="number" required
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
          ><input v-model="form.fromName"
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
                <strong>{{ account.name }}</strong
                ><small>{{
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
                  }}</span
                >
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
                  }}</button
                ><button v-if="canManage" type="button" @click="edit(account)">
                  {{ $t('page.mail.edit') }}</button
                ><button
                  v-if="canManage"
                  class="danger"
                  type="button"
                  :disabled="testingId === account.id"
                  @click="remove(account)"
                >
                  {{ $t('page.mail.delete') }}
                </button>
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
                  >{{ statusLabel(message.status) }}</span
                >
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
  .account-form {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 560px) {
  .account-form {
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
</style>
