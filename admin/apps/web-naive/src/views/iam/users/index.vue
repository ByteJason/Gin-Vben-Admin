<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue';

import type {
  IAMRole,
  IAMUserCreateInput,
  IAMUser,
  IAMUserPage,
  IAMUserStatus,
  IAMUserUpdateInput,
} from '#/api/core/iam';

import {
  createIAMUserApi,
  getIAMUserApi,
  listIAMRolesApi,
  listIAMUsersApi,
  updateIAMUserApi,
} from '#/api/core/iam';
import { $t } from '#/locales';

const state = reactive({
  keyword: '',
  orgId: '',
  page: 1,
  pageSize: 20,
  roleId: '',
  status: 'all' as IAMUserStatus,
});

const page = ref<IAMUserPage>({ items: [], page: 1, pageSize: 20, total: 0 });
const roles = ref<IAMRole[]>([]);
const loading = ref(false);
const rolesLoading = ref(false);
const error = ref('');
const errorSummary = ref<HTMLElement | null>(null);
const formError = ref('');
const formErrorSummary = ref<HTMLElement | null>(null);
const formMessage = ref('');
const formOpen = ref(false);
const formLoading = ref(false);
const formMode = ref<'create' | 'edit'>('create');
const editingId = ref('');
const userForm = reactive({
  active: true,
  email: '',
  nickname: '',
  orgId: '',
  password: '',
  phone: '',
  username: '',
});

const totalPages = computed(() =>
  Math.max(1, Math.ceil(page.value.total / state.pageSize)),
);

function roleName(id: string) {
  return roles.value.find((role) => role.id === id)?.name ?? id;
}

function statusOf(user: IAMUser) {
  return user.status ?? (user.active ? 'active' : 'disabled');
}

function formatDate(value?: string) {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date);
}

async function focusError() {
  await nextTick();
  errorSummary.value?.focus();
}

async function focusFormError() {
  await nextTick();
  formErrorSummary.value?.focus();
}

function clearUserForm() {
  editingId.value = '';
  userForm.active = true;
  userForm.email = '';
  userForm.nickname = '';
  userForm.orgId = '';
  userForm.password = '';
  userForm.phone = '';
  userForm.username = '';
  formError.value = '';
}

function openCreate() {
  formMode.value = 'create';
  clearUserForm();
  formMessage.value = '';
  formOpen.value = true;
}

async function openEdit(user: IAMUser) {
  formMode.value = 'edit';
  clearUserForm();
  editingId.value = user.id;
  userForm.username = user.username;
  userForm.nickname = user.nickname ?? user.displayName ?? '';
  userForm.email = user.email ?? '';
  userForm.phone = user.phone ?? '';
  userForm.orgId = user.orgId ?? '';
  userForm.active = user.active;
  formMessage.value = '';
  formOpen.value = true;
  formLoading.value = true;
  try {
    const current = await getIAMUserApi(user.id);
    userForm.username = current.username;
    userForm.nickname = current.nickname ?? current.displayName ?? '';
    userForm.email = current.email ?? '';
    userForm.phone = current.phone ?? '';
    userForm.orgId = current.orgId ?? '';
    userForm.active = current.active;
  } catch {
    formError.value = String($t('page.iam.loadFormError'));
    await focusFormError();
  } finally {
    formLoading.value = false;
  }
}

function closeForm() {
  if (formLoading.value) return;
  formOpen.value = false;
  formError.value = '';
}

function validateUserForm() {
  const username = userForm.username.trim();
  if (!username) return String($t('page.iam.required'));
  if (formMode.value === 'create') {
    const bytes = new TextEncoder().encode(userForm.password).length;
    if (bytes < 8 || bytes > 128) return String($t('page.iam.passwordLength'));
  }
  if (
    userForm.email.trim() &&
    !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(userForm.email.trim())
  ) {
    return String($t('page.iam.invalidEmail'));
  }
  if (
    userForm.phone.trim() &&
    !/^\+[1-9][0-9]{7,14}$/.test(userForm.phone.trim())
  ) {
    return String($t('page.iam.invalidPhone'));
  }
  return '';
}

async function submitUserForm() {
  formError.value = '';
  const validationError = validateUserForm();
  if (validationError) {
    formError.value = validationError;
    await focusFormError();
    return;
  }
  formLoading.value = true;
  const common = {
    active: userForm.active,
    email: userForm.email.trim() || undefined,
    nickname: userForm.nickname.trim() || undefined,
    orgId: userForm.orgId.trim() || undefined,
    phone: userForm.phone.trim() || undefined,
    username: userForm.username.trim(),
  };
  try {
    if (formMode.value === 'create') {
      const input: IAMUserCreateInput = {
        ...common,
        password: userForm.password,
      };
      await createIAMUserApi(input);
    } else {
      const input: IAMUserUpdateInput = common;
      await updateIAMUserApi(editingId.value, input);
    }
    formOpen.value = false;
    formMessage.value = String(
      $t(formMode.value === 'create' ? 'page.iam.created' : 'page.iam.updated'),
    );
    await loadUsers();
  } catch {
    formError.value = String($t('page.iam.saveError'));
    await focusFormError();
  } finally {
    formLoading.value = false;
  }
}

async function loadUsers() {
  loading.value = true;
  error.value = '';
  try {
    page.value = await listIAMUsersApi({
      keyword: state.keyword,
      orgId: state.orgId,
      page: state.page,
      pageSize: state.pageSize,
      roleId: state.roleId,
      status: state.status,
      sort: 'username',
    });
  } catch {
    error.value = String($t('page.iam.loadError'));
    await focusError();
  } finally {
    loading.value = false;
  }
}

async function loadRoles() {
  rolesLoading.value = true;
  try {
    roles.value = await listIAMRolesApi();
  } catch {
    // A role filter is optional; the user list remains useful when roles are unavailable.
    roles.value = [];
  } finally {
    rolesLoading.value = false;
  }
}

async function search() {
  state.page = 1;
  await loadUsers();
}

async function resetFilters() {
  state.keyword = '';
  state.orgId = '';
  state.roleId = '';
  state.status = 'all';
  state.page = 1;
  await loadUsers();
}

async function changePage(nextPage: number) {
  if (loading.value || nextPage < 1 || nextPage > totalPages.value) return;
  state.page = nextPage;
  await loadUsers();
}

async function changePageSize() {
  state.page = 1;
  await loadUsers();
}

onMounted(async () => {
  await Promise.all([loadUsers(), loadRoles()]);
});
</script>

<template>
  <main
    class="iam-users-page"
    :aria-busy="loading"
    aria-labelledby="iam-users-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
        <h1 id="iam-users-title">{{ $t('page.iam.users') }}</h1>
        <p class="description">{{ $t('page.iam.description') }}</p>
      </div>
      <div class="heading-actions">
        <span class="scope-chip">{{ $t('page.iam.readOnly') }}</span>
        <button class="primary" type="button" @click="openCreate">
          {{ $t('page.iam.create') }}
        </button>
      </div>
    </header>

    <p
      v-if="error"
      ref="errorSummary"
      class="feedback feedback-error"
      role="alert"
      tabindex="-1"
    >
      {{ error }}
    </p>
    <p class="sr-status" aria-live="polite">
      {{ loading ? $t('page.iam.loading') : '' }}
    </p>
    <p v-if="formMessage" class="feedback feedback-success" aria-live="polite">
      {{ formMessage }}
    </p>

    <form class="filters" role="search" @submit.prevent="search">
      <label class="field field-keyword" for="iam-users-keyword">
        <span>{{ $t('page.iam.keyword') }}</span>
        <input
          id="iam-users-keyword"
          v-model.trim="state.keyword"
          :placeholder="$t('page.iam.keywordPlaceholder')"
          type="search"
        />
      </label>
      <label class="field" for="iam-users-status">
        <span>{{ $t('page.iam.status') }}</span>
        <select id="iam-users-status" v-model="state.status">
          <option value="all">{{ $t('page.iam.all') }}</option>
          <option value="active">{{ $t('page.iam.active') }}</option>
          <option value="disabled">{{ $t('page.iam.disabled') }}</option>
        </select>
      </label>
      <label class="field" for="iam-users-role">
        <span>{{ $t('page.iam.role') }}</span>
        <select
          id="iam-users-role"
          v-model="state.roleId"
          :aria-busy="rolesLoading"
        >
          <option value="">{{ $t('page.iam.allRoles') }}</option>
          <option v-for="role in roles" :key="role.id" :value="role.id">
            {{ role.name }}
          </option>
        </select>
      </label>
      <label class="field" for="iam-users-org">
        <span>{{ $t('page.iam.orgId') }}</span>
        <input id="iam-users-org" v-model.trim="state.orgId" type="text" />
      </label>
      <div class="filter-actions">
        <button class="primary" type="submit" :disabled="loading">
          {{ $t('page.iam.search') }}
        </button>
        <button type="button" :disabled="loading" @click="resetFilters">
          {{ $t('page.iam.reset') }}
        </button>
      </div>
    </form>

    <section class="table-card" aria-labelledby="iam-users-table-title">
      <div class="table-heading">
        <h2 id="iam-users-table-title">{{ $t('page.iam.users') }}</h2>
        <span class="result-count"
          >{{ page.total }} {{ $t('page.iam.rows') }}</span
        >
      </div>
      <div class="table-wrap">
        <table>
          <caption class="sr-only">
            {{
              $t('page.iam.tableLabel')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('page.iam.username') }}</th>
              <th scope="col">{{ $t('page.iam.displayName') }}</th>
              <th scope="col">{{ $t('page.iam.email') }}</th>
              <th scope="col">{{ $t('page.iam.organization') }}</th>
              <th scope="col">{{ $t('page.iam.role') }}</th>
              <th scope="col">{{ $t('page.iam.status') }}</th>
              <th scope="col">{{ $t('page.iam.lastLoginAt') }}</th>
              <th scope="col">{{ $t('page.iam.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td class="table-state" colspan="8">
                {{ $t('page.iam.loading') }}
              </td>
            </tr>
            <tr v-else-if="page.items.length === 0">
              <td class="table-state" colspan="8">
                {{ $t('page.iam.empty') }}
              </td>
            </tr>
            <tr v-for="user in page.items" v-else :key="user.id">
              <th scope="row">
                <span class="primary-text">{{ user.username }}</span>
                <small>{{ user.id }}</small>
              </th>
              <td>{{ user.displayName || user.nickname || '—' }}</td>
              <td>{{ user.email || '—' }}</td>
              <td>{{ user.orgId || '—' }}</td>
              <td>
                <span v-if="user.roleIds.length" class="role-list">
                  {{ user.roleIds.map(roleName).join(', ') }}
                </span>
                <span v-else>—</span>
              </td>
              <td>
                <span class="status-pill" :data-status="statusOf(user)">
                  {{
                    statusOf(user) === 'active'
                      ? $t('page.iam.active')
                      : $t('page.iam.disabled')
                  }}
                </span>
              </td>
              <td>{{ formatDate(user.lastLoginAt) }}</td>
              <td>
                <button
                  class="link-button"
                  type="button"
                  @click="openEdit(user)"
                >
                  {{ $t('page.iam.edit') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <footer class="pagination" aria-label="Pagination">
        <label class="page-size" for="iam-users-page-size">
          <span>{{ $t('page.iam.pageSize') }}</span>
          <select
            id="iam-users-page-size"
            v-model.number="state.pageSize"
            @change="changePageSize"
          >
            <option :value="20">20</option>
            <option :value="50">50</option>
            <option :value="100">100</option>
          </select>
        </label>
        <button
          type="button"
          :disabled="loading || state.page <= 1"
          @click="changePage(state.page - 1)"
        >
          {{ $t('page.iam.previous') }}
        </button>
        <span aria-live="polite"> {{ state.page }} / {{ totalPages }} </span>
        <button
          type="button"
          :disabled="loading || state.page >= totalPages"
          @click="changePage(state.page + 1)"
        >
          {{ $t('page.iam.next') }}
        </button>
      </footer>
    </section>

    <section
      v-if="formOpen"
      class="modal-backdrop"
      :aria-label="$t('page.iam.form')"
      @click.self="closeForm"
    >
      <div
        class="user-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="iam-user-form-title"
      >
        <header class="dialog-heading">
          <div>
            <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
            <h2 id="iam-user-form-title">
              {{
                formMode === 'create'
                  ? $t('page.iam.create')
                  : $t('page.iam.edit')
              }}
            </h2>
          </div>
          <button
            class="icon-button"
            type="button"
            :aria-label="$t('page.iam.cancel')"
            :disabled="formLoading"
            @click="closeForm"
          >
            ×
          </button>
        </header>
        <p
          v-if="formError"
          ref="formErrorSummary"
          class="feedback feedback-error"
          role="alert"
          tabindex="-1"
        >
          {{ formError }}
        </p>
        <form class="user-form" @submit.prevent="submitUserForm">
          <label class="field" for="iam-user-username">
            <span>{{ $t('page.iam.username') }}</span>
            <input
              id="iam-user-username"
              v-model.trim="userForm.username"
              :disabled="formLoading"
              autocomplete="username"
              maxlength="191"
              required
              type="text"
            />
          </label>
          <label class="field" for="iam-user-nickname">
            <span>{{ $t('page.iam.nickname') }}</span>
            <input
              id="iam-user-nickname"
              v-model.trim="userForm.nickname"
              :disabled="formLoading"
              maxlength="191"
              type="text"
            />
          </label>
          <label class="field" for="iam-user-email">
            <span>{{ $t('page.iam.email') }}</span>
            <input
              id="iam-user-email"
              v-model.trim="userForm.email"
              :disabled="formLoading"
              autocomplete="email"
              type="email"
            />
          </label>
          <label class="field" for="iam-user-phone">
            <span>{{ $t('page.iam.phone') }}</span>
            <input
              id="iam-user-phone"
              v-model.trim="userForm.phone"
              :disabled="formLoading"
              inputmode="tel"
              placeholder="+8613800000000"
              type="tel"
            />
          </label>
          <label class="field" for="iam-user-orgId">
            <span>{{ $t('page.iam.orgId') }}</span>
            <input
              id="iam-user-orgId"
              v-model.trim="userForm.orgId"
              :disabled="formLoading"
              maxlength="128"
              type="text"
            />
          </label>
          <label
            v-if="formMode === 'create'"
            class="field field-wide"
            for="iam-user-password"
          >
            <span>{{ $t('page.iam.initialPassword') }}</span>
            <input
              id="iam-user-password"
              v-model="userForm.password"
              :disabled="formLoading"
              autocomplete="new-password"
              minlength="8"
              maxlength="128"
              required
              type="password"
            />
          </label>
          <label class="switch-row field-wide" for="iam-user-active">
            <span>
              <strong>{{ $t('page.iam.active') }}</strong>
              <small>{{ $t('page.iam.activeHelp') }}</small>
            </span>
            <input
              id="iam-user-active"
              v-model="userForm.active"
              :disabled="formLoading"
              type="checkbox"
            />
          </label>
          <footer class="dialog-actions">
            <button type="button" :disabled="formLoading" @click="closeForm">
              {{ $t('page.iam.cancel') }}
            </button>
            <button class="primary" type="submit" :disabled="formLoading">
              {{ formLoading ? $t('page.iam.saving') : $t('page.iam.save') }}
            </button>
          </footer>
        </form>
      </div>
    </section>
  </main>
</template>

<style scoped>
.iam-users-page {
  max-width: 1440px;
  padding: 24px;
  margin: 0 auto;
  color: hsl(var(--foreground));
}

.page-heading,
.table-heading,
.pagination,
.filter-actions {
  display: flex;
  align-items: center;
}

.page-heading {
  gap: 24px;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
}

.heading-actions {
  display: flex;
  gap: 10px;
  align-items: center;
}

.eyebrow {
  margin: 0;
  color: hsl(var(--muted-foreground));
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}

h1,
h2 {
  margin: 4px 0 8px;
}

h1 {
  font-size: clamp(1.5rem, 2vw, 2rem);
}

h2 {
  font-size: 1.05rem;
}

.description {
  max-width: 720px;
  margin: 0;
  color: hsl(var(--muted-foreground));
}

.scope-chip,
.status-pill {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 4px 10px;
  border-radius: 999px;
  background: hsl(var(--muted));
  color: hsl(var(--muted-foreground));
  font-size: 0.78rem;
  font-weight: 650;
  white-space: nowrap;
}

.filters,
.table-card {
  border: 1px solid hsl(var(--border));
  border-radius: 14px;
  background: hsl(var(--card));
  box-shadow: 0 10px 30px rgb(15 23 42 / 4%);
}

.filters {
  display: grid;
  grid-template-columns: minmax(180px, 2fr) repeat(3, minmax(130px, 1fr)) auto;
  gap: 12px;
  align-items: end;
  padding: 16px;
  margin-bottom: 16px;
}

.field,
.page-size {
  display: grid;
  gap: 6px;
  color: hsl(var(--muted-foreground));
  font-size: 0.8rem;
  font-weight: 650;
}

input,
select,
button {
  min-height: 40px;
  border: 1px solid hsl(var(--border));
  border-radius: 9px;
  background: hsl(var(--background));
  color: inherit;
  font: inherit;
}

input,
select {
  width: 100%;
  padding: 0 11px;
}

button {
  padding: 0 14px;
  cursor: pointer;
  font-weight: 650;
}

button:hover:not(:disabled) {
  border-color: hsl(var(--primary));
}

button:focus-visible,
input:focus-visible,
select:focus-visible {
  outline: 3px solid hsl(var(--ring) / 35%);
  outline-offset: 2px;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

button.primary {
  border-color: hsl(var(--primary));
  background: hsl(var(--primary));
  color: hsl(var(--primary-foreground));
}

.link-button {
  min-height: 32px;
  padding: 0 8px;
  border-color: transparent;
  background: transparent;
  color: hsl(var(--primary));
}

.icon-button {
  min-width: 36px;
  min-height: 36px;
  padding: 0;
  border-color: transparent;
  background: transparent;
  font-size: 1.4rem;
  line-height: 1;
}

.filter-actions {
  gap: 8px;
}

.feedback-error {
  padding: 12px 14px;
  border: 1px solid hsl(var(--destructive) / 35%);
  border-radius: 10px;
  background: hsl(var(--destructive) / 8%);
  color: hsl(var(--destructive));
}

.feedback-success {
  padding: 12px 14px;
  border: 1px solid hsl(142 70% 40% / 35%);
  border-radius: 10px;
  background: hsl(142 70% 40% / 9%);
  color: hsl(142 60% 25%);
}

.table-card {
  overflow: hidden;
}

.table-heading {
  justify-content: space-between;
  padding: 16px 18px;
  border-bottom: 1px solid hsl(var(--border));
}

.result-count {
  color: hsl(var(--muted-foreground));
  font-size: 0.8rem;
}

.table-wrap {
  overflow-x: auto;
}

table {
  width: 100%;
  min-width: 860px;
  border-collapse: collapse;
  text-align: left;
}

th,
td {
  padding: 13px 16px;
  border-bottom: 1px solid hsl(var(--border));
  vertical-align: middle;
  font-size: 0.88rem;
}

thead th {
  background: hsl(var(--muted) / 45%);
  color: hsl(var(--muted-foreground));
  font-size: 0.75rem;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

tbody tr:hover {
  background: hsl(var(--muted) / 25%);
}

.primary-text {
  display: block;
  font-weight: 700;
}

th small {
  display: block;
  margin-top: 3px;
  color: hsl(var(--muted-foreground));
  font-size: 0.72rem;
  font-weight: 400;
}

.role-list {
  color: hsl(var(--muted-foreground));
}

.status-pill[data-status='active'] {
  background: hsl(142 70% 45% / 14%);
  color: hsl(142 60% 28%);
}

.status-pill[data-status='disabled'] {
  background: hsl(var(--muted));
  color: hsl(var(--muted-foreground));
}

.table-state {
  height: 160px;
  color: hsl(var(--muted-foreground));
  text-align: center;
}

.pagination {
  justify-content: flex-end;
  gap: 10px;
  padding: 14px 16px;
}

.modal-backdrop {
  position: fixed;
  z-index: 20;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgb(15 23 42 / 48%);
}

.user-dialog {
  width: min(680px, 100%);
  max-height: min(720px, calc(100vh - 40px));
  overflow: auto;
  padding: 22px;
  border: 1px solid hsl(var(--border));
  border-radius: 16px;
  background: hsl(var(--card));
  box-shadow: 0 24px 70px rgb(15 23 42 / 24%);
}

.dialog-heading,
.dialog-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.dialog-heading {
  margin-bottom: 18px;
}

.dialog-heading h2 {
  margin-bottom: 0;
}

.user-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.field-wide {
  grid-column: 1 / -1;
}

.switch-row {
  display: flex;
  gap: 16px;
  align-items: center;
  justify-content: space-between;
  min-height: 44px;
  color: hsl(var(--muted-foreground));
  font-size: 0.8rem;
}

.switch-row span {
  display: grid;
  gap: 4px;
}

.switch-row strong {
  color: hsl(var(--foreground));
  font-size: 0.9rem;
}

.switch-row small {
  font-weight: 400;
}

.switch-row input[type='checkbox'] {
  width: 20px;
  min-height: 20px;
  accent-color: hsl(var(--primary));
}

.dialog-actions {
  grid-column: 1 / -1;
  justify-content: flex-end;
  gap: 10px;
  padding-top: 6px;
}

.page-size {
  grid-template-columns: auto 78px;
  align-items: center;
  margin-right: auto;
}

.page-size select {
  min-height: 36px;
}

.sr-status,
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

@media (max-width: 1100px) {
  .filters {
    grid-template-columns: repeat(2, minmax(160px, 1fr));
  }

  .field-keyword,
  .filter-actions {
    grid-column: span 2;
  }
}

@media (max-width: 768px) {
  .iam-users-page {
    padding: 16px;
  }

  .page-heading {
    display: block;
  }

  .scope-chip {
    margin-top: 12px;
  }

  .filters {
    grid-template-columns: 1fr;
  }

  .field-keyword,
  .filter-actions {
    grid-column: auto;
  }

  .filter-actions > * {
    flex: 1;
  }

  .heading-actions {
    justify-content: space-between;
    margin-top: 12px;
  }

  .user-form {
    grid-template-columns: 1fr;
  }

  .field-wide {
    grid-column: auto;
  }
}

@media (max-width: 480px) {
  .pagination {
    flex-wrap: wrap;
    justify-content: space-between;
  }

  .page-size {
    width: 100%;
    margin-bottom: 4px;
  }
}
</style>
