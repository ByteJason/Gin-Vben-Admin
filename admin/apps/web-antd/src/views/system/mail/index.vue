<script setup lang="ts">
import type {
  EmailMessage,
  SMTPAccount,
  SMTPAccountInput,
} from '#/api/core/mail';

import { computed, onMounted, reactive, ref } from 'vue';

import { useAccess } from '@vben/access';
import { ManagementPage } from '@vben/common-ui';

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
    error.value = '加载 SMTP 账户或投递记录失败';
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (!canManage.value) return;
  if (!form.name.trim() || !form.host.trim() || !form.fromEmail.trim()) {
    error.value = '账号名称、SMTP 主机和发件人邮箱必填';
    return;
  }
  saving.value = true;
  error.value = '';
  notice.value = '';
  try {
    await saveSMTPAccountApi({ ...form }, editingId.value);
    notice.value = editingId.value ? 'SMTP 账户已更新' : 'SMTP 账户已创建';
    resetForm();
    await load();
  } catch {
    error.value = '保存 SMTP 账户失败，请检查字段或唯一索引';
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
        ? `${account.name} 连接成功`
        : `${account.name} 连接失败：${result.stage ?? 'unknown'} / ${result.code ?? 'provider_unavailable'}`;
  } catch {
    error.value = 'SMTP 连接测试请求失败';
  } finally {
    testingId.value = '';
  }
}

async function remove(account: SMTPAccount) {
  if (!canManage.value) return;
  if (!window.confirm(`确认软删除 SMTP 账户“${account.name}”？`)) return;
  testingId.value = account.id;
  try {
    await deleteSMTPAccountApi(account.id);
    notice.value = 'SMTP 账户已软删除';
    if (editingId.value === account.id) resetForm();
    await load();
  } catch {
    error.value = '删除 SMTP 账户失败';
  } finally {
    testingId.value = '';
  }
}

function statusLabel(status: string) {
  return status === 'sent' ? '已发送' : status === 'failed' ? '失败' : status;
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
        <p class="eyebrow">MAIL / DELIVERY</p>
        <h1 id="mail-title">SMTP 账户与邮件投递</h1>
        <p class="description">
          配置可启停的 SMTP 账户池，按权重轮训，并查看脱敏投递状态。
        </p>
      </div>
      <button class="secondary" type="button" :disabled="loading" @click="load">
        刷新
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
          <p class="eyebrow">ACCOUNT POOL</p>
          <h2 id="mail-editor-title">
            {{ editingId ? '编辑 SMTP 账户' : '新增 SMTP 账户' }}
          </h2>
        </div>
        <button
          v-if="editingId"
          class="secondary"
          type="button"
          @click="resetForm"
        >
          取消编辑
        </button>
      </div>
      <form class="account-form" @submit.prevent="save">
        <label><span>账号名称</span><input v-model="form.name" autocomplete="off" required /></label>
        <label><span>SMTP 主机</span><input v-model="form.host" autocomplete="off" required /></label>
        <label><span>SMTP 端口</span><input
            v-model.number="form.port"
            min="1"
            max="65535"
            type="number"
            required
        /></label>
        <label><span>SMTP 用户名</span><input v-model="form.username" autocomplete="username" /></label>
        <label><span>SMTP 密码</span><input
            v-model="form.password"
            autocomplete="new-password"
            type="password"
            :placeholder="editingId ? '留空保留现有密码' : ''"
        /></label>
        <label><span>权重</span><input v-model.number="form.weight" min="1" type="number" required /></label>
        <label><span>发件人邮箱</span><input
            v-model="form.fromEmail"
            autocomplete="email"
            type="email"
            required
        /></label>
        <label><span>发件人名称</span><input v-model="form.fromName" /></label>
        <label class="toggle"><input v-model="form.enabled" type="checkbox" /><span>启用该 SMTP</span></label>
        <label class="toggle"><input v-model="form.implicitTls" type="checkbox" /><span>隐式 TLS（SMTPS / 465）</span></label>
        <div class="form-actions">
          <button class="primary" type="submit" :disabled="saving">
            {{ saving ? '保存中…' : '保存 SMTP 账户' }}
          </button>
        </div>
      </form>
    </section>

    <section class="table-card" aria-labelledby="mail-table-title">
      <div class="section-heading">
        <div>
          <p class="eyebrow">POOL STATUS</p>
          <h2 id="mail-table-title">SMTP 账户池</h2>
        </div>
        <span class="count">{{ accounts.length }} 个账户</span>
      </div>
      <p v-if="!loading && !hasAccounts" class="empty-state">
        尚未配置 SMTP 账户。
      </p>
      <div v-else class="table-scroll">
        <table>
          <caption class="sr-only">
            SMTP 账户列表
          </caption>
          <thead>
            <tr>
              <th>名称</th>
              <th>主机</th>
              <th>端口</th>
              <th>权重</th>
              <th>发件人</th>
              <th>状态</th>
              <th>安全</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="account in accounts" :key="account.id">
              <td>
                <strong>{{ account.name }}</strong><small>{{ account.username || '无认证用户名' }}</small>
              </td>
              <td>{{ account.host }}</td>
              <td>{{ account.port }}</td>
              <td>{{ account.weight }}</td>
              <td>{{ account.fromEmail }}</td>
              <td>
                <span
                  class="status-pill" :class="[account.enabled ? 'ok' : 'off']"
                  >{{ account.enabled ? '启用' : '关闭' }}</span>
              </td>
              <td>
                {{ account.implicitTls ? '隐式 TLS' : 'STARTTLS / 明文' }}
              </td>
              <td class="actions">
                <button
                  v-if="canManage"
                  type="button"
                  :disabled="testingId === account.id"
                  @click="test(account)"
                >
                  {{
                    testingId === account.id ? '测试中…' : '测试连接'
                  }}
</button><button v-if="canManage" type="button" @click="edit(account)">
                  编辑
</button><button
                  v-if="canManage"
                  class="danger"
                  type="button"
                  :disabled="testingId === account.id"
                  @click="remove(account)"
                >
                  删除
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
          <p class="eyebrow">AUDIT TRAIL</p>
          <h2 id="message-table-title">邮件投递记录</h2>
        </div>
        <span class="muted">正文仅保存密文与摘要</span>
      </div>
      <p v-if="messages.length === 0" class="empty-state">暂无发送记录。</p>
      <div v-else class="table-scroll">
        <table>
          <caption class="sr-only">
            邮件投递记录
          </caption>
          <thead>
            <tr>
              <th>主题</th>
              <th>收件人</th>
              <th>状态</th>
              <th>尝试次数</th>
              <th>创建时间</th>
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
                  class="status-pill" :class="[
                    message.status === 'sent' ? 'ok' : 'off',
                  ]"
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
