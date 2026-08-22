<script setup lang="ts">
import type {
  IAMRole,
  IAMUser,
  IAMUserBatchStatusInput,
  IAMUserCreateInput,
  IAMRoleUsersReplaceInput,
  IAMUserLoginEventPage,
  IAMUserPage,
  IAMUserPasswordResetInput,
  IAMUserStatus,
  IAMUserUpdateInput,
} from '#/api/core/iam';

import { computed, nextTick, onMounted, reactive, ref } from 'vue';

import {
  batchUpdateIAMUserStatusApi,
  createIAMUserApi,
  deleteIAMUserApi,
  getIAMUserApi,
  listIAMRolesApi,
  listIAMUserLoginEventsApi,
  listIAMUsersApi,
  resetIAMUserPasswordApi,
  replaceIAMRoleUsersApi,
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
const roleAssignmentOpen = ref(false);
const roleAssignmentLoading = ref(false);
const roleAssignmentUserId = ref('');
const roleAssignmentUsername = ref('');
const roleAssignmentSelectedRoleIds = ref<string[]>([]);
const roleAssignmentInitialRoleIds = ref<string[]>([]);
const roleAssignmentError = ref('');
const roleAssignmentErrorSummary = ref<HTMLElement | null>(null);
const rolesLoading = ref(false);
const rolesError = ref('');
const error = ref('');
const errorSummary = ref<HTMLElement | null>(null);
const formError = ref('');
const formErrorSummary = ref<HTMLElement | null>(null);
const formMessage = ref('');
const selectedIds = ref<string[]>([]);
const actionLoading = ref(false);
const actionError = ref('');
const actionErrorSummary = ref<HTMLElement | null>(null);
const actionMessage = ref('');
const actionFeedback = ref<HTMLElement | null>(null);
const formOpen = ref(false);
const formLoading = ref(false);
const formMode = ref<'create' | 'edit'>('create');
const editingId = ref('');
const resetOpen = ref(false);
const resetLoading = ref(false);
const resetUserId = ref('');
const resetUsername = ref('');
const resetPassword = ref('');
const resetError = ref('');
const resetErrorSummary = ref<HTMLElement | null>(null);
const loginEventsOpen = ref(false);
const loginEventsLoading = ref(false);
const loginEventsUserId = ref('');
const loginEventsUsername = ref('');
const loginEventsError = ref('');
const loginEventsErrorSummary = ref<HTMLElement | null>(null);
const loginEventsFrom = ref('');
const loginEventsTo = ref('');
const loginEventsOffset = ref(0);
const loginEventsPage = ref<IAMUserLoginEventPage>({
  items: [],
  limit: 50,
  offset: 0,
  total: 0,
});
const loginEventsLimit = 50;
const roleAssignmentLimit = 100;
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
const pageIds = computed(() => page.value.items.map((user) => user.id));
const allPageSelected = computed(
  () =>
    pageIds.value.length > 0 &&
    pageIds.value.every((id) => selectedIds.value.includes(id)),
);
const selectedCount = computed(() => selectedIds.value.length);
const loginEventsTotalPages = computed(() =>
  Math.max(1, Math.ceil(loginEventsPage.value.total / loginEventsLimit)),
);
const loginEventsCurrentPage = computed(
  () => Math.floor(loginEventsOffset.value / loginEventsLimit) + 1,
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

async function focusActionError() {
  await nextTick();
  actionErrorSummary.value?.focus();
}

async function focusActionFeedback() {
  await nextTick();
  actionFeedback.value?.focus();
}

async function focusResetError() {
  await nextTick();
  resetErrorSummary.value?.focus();
}

async function focusLoginEventsError() {
  await nextTick();
  loginEventsErrorSummary.value?.focus();
}

function clearSelection() {
  selectedIds.value = [];
}

function togglePageSelection(checked: boolean) {
  selectedIds.value = checked ? [...pageIds.value] : [];
}

function onPageSelectionChange(event: Event) {
  togglePageSelection((event.target as HTMLInputElement).checked);
}

function onUserSelectionChange(id: string, event: Event) {
  const checked = (event.target as HTMLInputElement).checked;
  if (checked && !selectedIds.value.includes(id)) {
    selectedIds.value = [...selectedIds.value, id];
    return;
  }
  if (!checked) {
    selectedIds.value = selectedIds.value.filter(
      (selectedId) => selectedId !== id,
    );
  }
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

function openResetPassword(user: IAMUser) {
  resetUserId.value = user.id;
  resetUsername.value = user.username;
  resetPassword.value = '';
  resetError.value = '';
  actionError.value = '';
  actionMessage.value = '';
  resetOpen.value = true;
}

function closeResetPassword() {
  if (resetLoading.value) return;
  resetOpen.value = false;
  resetPassword.value = '';
  resetError.value = '';
}

function validateResetPassword() {
  const bytes = new TextEncoder().encode(resetPassword.value).length;
  if (bytes < 8 || bytes > 128) {
    return String($t('page.iam.passwordLength'));
  }
  return '';
}

async function submitResetPassword() {
  resetError.value = '';
  actionError.value = '';
  actionMessage.value = '';
  const validationError = validateResetPassword();
  if (validationError) {
    resetError.value = validationError;
    await focusResetError();
    return;
  }
  if (!window.confirm(String($t('page.iam.confirmResetPassword')))) return;
  resetLoading.value = true;
  try {
    const input: IAMUserPasswordResetInput = {
      password: resetPassword.value,
    };
    await resetIAMUserPasswordApi(resetUserId.value, input);
    resetPassword.value = '';
    resetOpen.value = false;
    actionMessage.value = String($t('page.iam.resetDone'));
    await loadUsers();
    await focusActionFeedback();
  } catch {
    resetError.value = String($t('page.iam.resetError'));
    await focusResetError();
  } finally {
    resetLoading.value = false;
  }
}

function clearLoginEvents() {
  loginEventsPage.value = {
    items: [],
    limit: loginEventsLimit,
    offset: 0,
    total: 0,
  };
}

function loginEventsDate(value: string) {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

function validateLoginEventsFilters() {
  const from = loginEventsDate(loginEventsFrom.value);
  const to = loginEventsDate(loginEventsTo.value);
  if (from === null || to === null) {
    return String($t('page.iam.loginEventsDateError'));
  }
  if (from && to && from > to) {
    return String($t('page.iam.loginEventsDateError'));
  }
  return '';
}

async function loadLoginEvents() {
  loginEventsError.value = '';
  const validationError = validateLoginEventsFilters();
  if (validationError) {
    loginEventsError.value = validationError;
    await focusLoginEventsError();
    return;
  }
  loginEventsLoading.value = true;
  try {
    const from = loginEventsDate(loginEventsFrom.value);
    const to = loginEventsDate(loginEventsTo.value);
    loginEventsPage.value = await listIAMUserLoginEventsApi(
      loginEventsUserId.value,
      {
        from: from ?? undefined,
        to: to ?? undefined,
        limit: loginEventsLimit,
        offset: loginEventsOffset.value,
      },
    );
  } catch {
    loginEventsError.value = String($t('page.iam.loginEventsError'));
    await focusLoginEventsError();
  } finally {
    loginEventsLoading.value = false;
  }
}

function openLoginEvents(user: IAMUser) {
  loginEventsUserId.value = user.id;
  loginEventsUsername.value = user.username;
  loginEventsFrom.value = '';
  loginEventsTo.value = '';
  loginEventsOffset.value = 0;
  loginEventsError.value = '';
  clearLoginEvents();
  actionError.value = '';
  actionMessage.value = '';
  loginEventsOpen.value = true;
  void loadLoginEvents();
}

function closeLoginEvents() {
  if (loginEventsLoading.value) return;
  loginEventsOpen.value = false;
  loginEventsError.value = '';
}

async function applyLoginEventsFilters() {
  loginEventsOffset.value = 0;
  await loadLoginEvents();
}

async function resetLoginEventsFilters() {
  loginEventsFrom.value = '';
  loginEventsTo.value = '';
  loginEventsOffset.value = 0;
  await loadLoginEvents();
}

async function changeLoginEventsPage(nextPage: number) {
  if (
    loginEventsLoading.value ||
    nextPage < 1 ||
    nextPage > loginEventsTotalPages.value
  ) {
    return;
  }
  loginEventsOffset.value = (nextPage - 1) * loginEventsLimit;
  await loadLoginEvents();
}

function roleIdsForUser(userId: string) {
  return roles.value
    .filter((role) => role.userIds?.includes(userId))
    .map((role) => role.id);
}

async function focusRoleAssignmentError() {
  await nextTick();
  roleAssignmentErrorSummary.value?.focus();
}

function openRoleAssignment(user: IAMUser) {
  const currentRoleIds = roleIdsForUser(user.id);
  roleAssignmentUserId.value = user.id;
  roleAssignmentUsername.value = user.username;
  roleAssignmentInitialRoleIds.value = [...currentRoleIds];
  roleAssignmentSelectedRoleIds.value = [...currentRoleIds];
  roleAssignmentError.value = rolesError.value;
  actionError.value = '';
  actionMessage.value = '';
  roleAssignmentOpen.value = true;
}

function closeRoleAssignment() {
  if (roleAssignmentLoading.value) return;
  roleAssignmentOpen.value = false;
  roleAssignmentError.value = '';
}

function roleAssignmentChanges() {
  const selected = new Set(roleAssignmentSelectedRoleIds.value);
  const initial = new Set(roleAssignmentInitialRoleIds.value);
  return roles.value
    .filter((role) => selected.has(role.id) !== initial.has(role.id))
    .map((role) => {
      const members = new Set(
        (role.userIds ?? []).map((userId) => userId.trim()).filter(Boolean),
      );
      if (selected.has(role.id)) {
        members.add(roleAssignmentUserId.value);
      } else {
        members.delete(roleAssignmentUserId.value);
      }
      return { role, userIds: [...members] };
    });
}

async function submitRoleAssignment() {
  roleAssignmentError.value = '';
  if (rolesError.value) {
    roleAssignmentError.value = rolesError.value;
    await focusRoleAssignmentError();
    return;
  }
  const changes = roleAssignmentChanges();
  if (changes.length === 0) {
    actionMessage.value = String($t('page.iam.roleAssignmentNoChanges'));
    roleAssignmentOpen.value = false;
    await focusActionFeedback();
    return;
  }
  const overLimit = changes.find(
    (change) => change.userIds.length > roleAssignmentLimit,
  );
  if (overLimit) {
    roleAssignmentError.value = String($t('page.iam.roleAssignmentLimit'));
    await focusRoleAssignmentError();
    return;
  }
  if (!window.confirm(String($t('page.iam.roleAssignmentConfirm')))) return;
  roleAssignmentLoading.value = true;
  actionError.value = '';
  actionMessage.value = '';
  try {
    for (const change of changes) {
      const input: IAMRoleUsersReplaceInput = { userIds: change.userIds };
      const updated = await replaceIAMRoleUsersApi(change.role.id, input);
      const localRole = roles.value.find((role) => role.id === change.role.id);
      if (localRole) {
        localRole.userIds = [...(updated.userIds ?? change.userIds)];
      }
    }
    roleAssignmentOpen.value = false;
    actionMessage.value = String($t('page.iam.roleAssignmentDone'));
    await Promise.all([loadUsers(), loadRoles()]);
    await focusActionFeedback();
  } catch {
    roleAssignmentError.value = String($t('page.iam.roleAssignmentError'));
    await focusRoleAssignmentError();
  } finally {
    roleAssignmentLoading.value = false;
  }
}

async function deleteUser(user: IAMUser) {
  if (
    actionLoading.value ||
    formLoading.value ||
    resetLoading.value ||
    loginEventsLoading.value ||
    roleAssignmentLoading.value
  ) {
    return;
  }
  if (!window.confirm(String($t('page.iam.confirmDelete')))) return;
  actionLoading.value = true;
  actionError.value = '';
  actionMessage.value = '';
  try {
    await deleteIAMUserApi(user.id);
    actionMessage.value = String($t('page.iam.deleted'));
    await loadUsers();
    await focusActionFeedback();
  } catch {
    actionError.value = String($t('page.iam.deleteError'));
    await focusActionError();
  } finally {
    actionLoading.value = false;
  }
}

async function batchUpdate(active: boolean) {
  if (
    actionLoading.value ||
    resetLoading.value ||
    loginEventsLoading.value ||
    roleAssignmentLoading.value ||
    selectedIds.value.length === 0
  ) {
    return;
  }
  if (!active && !window.confirm(String($t('page.iam.batchConfirmDisable')))) {
    return;
  }
  actionLoading.value = true;
  actionError.value = '';
  actionMessage.value = '';
  const input: IAMUserBatchStatusInput = {
    items: selectedIds.value.map((id) => ({ id, active })),
  };
  try {
    const result = await batchUpdateIAMUserStatusApi(input);
    const hasFailures = result.results.some(
      (item) => item.status !== (active ? 'active' : 'disabled'),
    );
    actionMessage.value = String(
      $t(hasFailures ? 'page.iam.batchPartial' : 'page.iam.batchUpdated'),
    );
    await loadUsers();
    await focusActionFeedback();
  } catch {
    actionError.value = String($t('page.iam.batchError'));
    await focusActionError();
  } finally {
    actionLoading.value = false;
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
    clearSelection();
  } catch {
    error.value = String($t('page.iam.loadError'));
    await focusError();
  } finally {
    loading.value = false;
  }
}

async function loadRoles() {
  rolesLoading.value = true;
  rolesError.value = '';
  try {
    roles.value = await listIAMRolesApi();
  } catch {
    // A role filter is optional; the user list remains useful when roles are unavailable.
    roles.value = [];
    rolesError.value = String($t('page.iam.roleAssignmentLoadError'));
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
        <span class="scope-chip">{{ $t('page.iam.manage') }}</span>
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
    <p
      v-if="actionError"
      ref="actionErrorSummary"
      class="feedback feedback-error"
      role="alert"
      tabindex="-1"
    >
      {{ actionError }}
    </p>
    <p
      v-if="actionMessage"
      ref="actionFeedback"
      class="feedback feedback-success"
      aria-live="polite"
      tabindex="-1"
    >
      {{ actionMessage }}
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
      <div
        class="bulk-toolbar"
        role="toolbar"
        :aria-label="$t('page.iam.bulkActions')"
      >
        <label class="bulk-select" for="iam-users-select-all">
          <input
            id="iam-users-select-all"
            :checked="allPageSelected"
            :disabled="
              loading ||
              actionLoading ||
              resetLoading ||
              loginEventsLoading ||
              roleAssignmentLoading ||
              page.items.length === 0
            "
            type="checkbox"
            @change="onPageSelectionChange"
          />
          <span>{{ $t('page.iam.selectAll') }}</span>
        </label>
        <span class="selected-count" aria-live="polite">
          {{ $t('page.iam.selectedCount') }}: {{ selectedCount }}
        </span>
        <div class="bulk-actions">
          <button
            type="button"
            :disabled="
              loading ||
              actionLoading ||
              resetLoading ||
              loginEventsLoading ||
              roleAssignmentLoading ||
              selectedCount === 0
            "
            @click="batchUpdate(true)"
          >
            {{ $t('page.iam.batchEnable') }}
          </button>
          <button
            type="button"
            :disabled="
              loading ||
              actionLoading ||
              resetLoading ||
              loginEventsLoading ||
              roleAssignmentLoading ||
              selectedCount === 0
            "
            @click="batchUpdate(false)"
          >
            {{ $t('page.iam.batchDisable') }}
          </button>
        </div>
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
              <th scope="col" class="selection-column">
                <span class="sr-only">{{ $t('page.iam.selectAll') }}</span>
              </th>
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
              <td class="table-state" colspan="9">
                {{ $t('page.iam.loading') }}
              </td>
            </tr>
            <tr v-else-if="page.items.length === 0">
              <td class="table-state" colspan="9">
                {{ $t('page.iam.empty') }}
              </td>
            </tr>
            <tr v-for="user in page.items" v-else :key="user.id">
              <td class="selection-column">
                <input
                  :id="`iam-user-select-${user.id}`"
                  :checked="selectedIds.includes(user.id)"
                  :disabled="
                    loading ||
                    actionLoading ||
                    resetLoading ||
                    loginEventsLoading ||
                    roleAssignmentLoading
                  "
                  :aria-label="`${$t('page.iam.selectUser')}: ${user.username}`"
                  type="checkbox"
                  @change="onUserSelectionChange(user.id, $event)"
                />
              </td>
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
                  :disabled="
                    loading ||
                    actionLoading ||
                    formLoading ||
                    resetLoading ||
                    loginEventsLoading ||
                    roleAssignmentLoading
                  "
                  @click="openEdit(user)"
                >
                  {{ $t('page.iam.edit') }}
                </button>
                <button
                  class="link-button"
                  type="button"
                  :disabled="
                    loading ||
                    actionLoading ||
                    formLoading ||
                    resetLoading ||
                    loginEventsLoading ||
                    roleAssignmentLoading ||
                    rolesLoading
                  "
                  @click="openRoleAssignment(user)"
                >
                  {{ $t('page.iam.roleAssignment') }}
                </button>
                <button
                  class="link-button"
                  type="button"
                  :disabled="
                    loading ||
                    actionLoading ||
                    formLoading ||
                    resetLoading ||
                    loginEventsLoading ||
                    roleAssignmentLoading
                  "
                  @click="openLoginEvents(user)"
                >
                  {{ $t('page.iam.loginEvents') }}
                </button>
                <button
                  class="link-button"
                  type="button"
                  :disabled="
                    loading ||
                    actionLoading ||
                    formLoading ||
                    resetLoading ||
                    loginEventsLoading ||
                    roleAssignmentLoading
                  "
                  @click="openResetPassword(user)"
                >
                  {{ $t('page.iam.resetPassword') }}
                </button>
                <button
                  class="link-button danger"
                  type="button"
                  :disabled="
                    loading ||
                    actionLoading ||
                    formLoading ||
                    resetLoading ||
                    loginEventsLoading ||
                    roleAssignmentLoading
                  "
                  @click="deleteUser(user)"
                >
                  {{ $t('page.iam.delete') }}
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

    <section
      v-if="roleAssignmentOpen"
      class="modal-backdrop"
      :aria-label="$t('page.iam.roleAssignmentTitle')"
      @click.self="closeRoleAssignment"
    >
      <div
        class="user-dialog role-assignment-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="iam-user-role-assignment-title"
        aria-describedby="iam-user-role-assignment-description"
      >
        <header class="dialog-heading">
          <div>
            <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
            <h2 id="iam-user-role-assignment-title">
              {{ $t('page.iam.roleAssignmentTitle') }}
            </h2>
          </div>
          <button
            class="icon-button"
            type="button"
            :aria-label="$t('page.iam.cancel')"
            :disabled="roleAssignmentLoading"
            @click="closeRoleAssignment"
          >
            ×
          </button>
        </header>
        <p
          id="iam-user-role-assignment-description"
          class="role-assignment-description"
        >
          {{ $t('page.iam.roleAssignmentDescription') }}
        </p>
        <p class="role-assignment-user">
          {{ $t('page.iam.roleAssignmentUser') }}: {{ roleAssignmentUsername }}
        </p>
        <p
          v-if="roleAssignmentError"
          ref="roleAssignmentErrorSummary"
          class="feedback feedback-error"
          role="alert"
          tabindex="-1"
        >
          {{ roleAssignmentError }}
        </p>
        <p class="sr-status" aria-live="polite">
          {{
            roleAssignmentLoading ? $t('page.iam.roleAssignmentLoading') : ''
          }}
        </p>
        <fieldset class="role-assignment-list">
          <legend>{{ $t('page.iam.roleAssignmentRoles') }}</legend>
          <label
            v-for="role in roles"
            :key="role.id"
            class="role-assignment-option"
          >
            <input
              v-model="roleAssignmentSelectedRoleIds"
              :value="role.id"
              :disabled="roleAssignmentLoading"
              type="checkbox"
            />
            <span>
              <strong>{{ role.name }}</strong>
              <small>{{ role.id }}</small>
            </span>
          </label>
          <p
            v-if="roles.length === 0 && !roleAssignmentError"
            class="table-state"
          >
            {{ $t('page.iam.roleAssignmentEmpty') }}
          </p>
        </fieldset>
        <footer class="dialog-actions">
          <button
            type="button"
            :disabled="roleAssignmentLoading"
            @click="closeRoleAssignment"
          >
            {{ $t('page.iam.cancel') }}
          </button>
          <button
            class="primary"
            type="button"
            :disabled="roleAssignmentLoading"
            @click="submitRoleAssignment"
          >
            {{
              roleAssignmentLoading
                ? $t('page.iam.roleAssignmentSaving')
                : $t('page.iam.roleAssignmentSave')
            }}
          </button>
        </footer>
      </div>
    </section>

    <section
      v-if="resetOpen"
      class="modal-backdrop"
      :aria-label="$t('page.iam.resetTitle')"
      @click.self="closeResetPassword"
    >
      <div
        class="user-dialog reset-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="iam-user-reset-password-title"
        aria-describedby="iam-user-reset-password-description"
      >
        <header class="dialog-heading">
          <div>
            <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
            <h2 id="iam-user-reset-password-title">
              {{ $t('page.iam.resetTitle') }}
            </h2>
          </div>
          <button
            class="icon-button"
            type="button"
            :aria-label="$t('page.iam.cancel')"
            :disabled="resetLoading"
            @click="closeResetPassword"
          >
            ×
          </button>
        </header>
        <p id="iam-user-reset-password-description" class="reset-description">
          {{ $t('page.iam.resetDescription') }}
        </p>
        <p class="reset-user">{{ resetUsername }}</p>
        <p
          v-if="resetError"
          ref="resetErrorSummary"
          class="feedback feedback-error"
          role="alert"
          tabindex="-1"
        >
          {{ resetError }}
        </p>
        <form
          class="user-form reset-form"
          @submit.prevent="submitResetPassword"
        >
          <label class="field field-wide" for="iam-user-reset-password">
            <span>{{ $t('page.iam.resetPasswordLabel') }}</span>
            <input
              id="iam-user-reset-password"
              v-model="resetPassword"
              :disabled="resetLoading"
              autocomplete="new-password"
              minlength="8"
              maxlength="128"
              required
              type="password"
            />
          </label>
          <footer class="dialog-actions">
            <button
              type="button"
              :disabled="resetLoading"
              @click="closeResetPassword"
            >
              {{ $t('page.iam.cancel') }}
            </button>
            <button class="primary" type="submit" :disabled="resetLoading">
              {{
                resetLoading
                  ? $t('page.iam.resetSaving')
                  : $t('page.iam.resetPassword')
              }}
            </button>
          </footer>
        </form>
      </div>
    </section>
    <section
      v-if="loginEventsOpen"
      class="modal-backdrop"
      :aria-label="$t('page.iam.loginEventsTitle')"
      @click.self="closeLoginEvents"
    >
      <div
        class="user-dialog login-events-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="iam-user-login-events-title"
        aria-describedby="iam-user-login-events-description"
      >
        <header class="dialog-heading">
          <div>
            <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
            <h2 id="iam-user-login-events-title">
              {{ $t('page.iam.loginEventsTitle') }}
            </h2>
          </div>
          <button
            class="icon-button"
            type="button"
            :aria-label="$t('page.iam.cancel')"
            :disabled="loginEventsLoading || roleAssignmentLoading"
            @click="closeLoginEvents"
          >
            ×
          </button>
        </header>
        <p
          id="iam-user-login-events-description"
          class="login-events-description"
        >
          {{ $t('page.iam.loginEventsDescription') }}
        </p>
        <p class="login-events-user">{{ loginEventsUsername }}</p>
        <form
          class="login-events-filters"
          @submit.prevent="applyLoginEventsFilters"
        >
          <label class="field" for="iam-user-login-events-from">
            <span>{{ $t('page.iam.loginEventsFrom') }}</span>
            <input
              id="iam-user-login-events-from"
              v-model="loginEventsFrom"
              :disabled="loginEventsLoading || roleAssignmentLoading"
              type="datetime-local"
            />
          </label>
          <label class="field" for="iam-user-login-events-to">
            <span>{{ $t('page.iam.loginEventsTo') }}</span>
            <input
              id="iam-user-login-events-to"
              v-model="loginEventsTo"
              :disabled="loginEventsLoading || roleAssignmentLoading"
              type="datetime-local"
            />
          </label>
          <div class="filter-actions">
            <button
              class="primary"
              type="submit"
              :disabled="loginEventsLoading || roleAssignmentLoading"
            >
              {{ $t('page.iam.loginEventsApply') }}
            </button>
            <button
              type="button"
              :disabled="loginEventsLoading || roleAssignmentLoading"
              @click="resetLoginEventsFilters"
            >
              {{ $t('page.iam.loginEventsReset') }}
            </button>
          </div>
        </form>
        <p
          v-if="loginEventsError"
          ref="loginEventsErrorSummary"
          class="feedback feedback-error"
          role="alert"
          tabindex="-1"
        >
          {{ loginEventsError }}
        </p>
        <p class="sr-status" aria-live="polite">
          {{ loginEventsLoading ? $t('page.iam.loginEventsLoading') : '' }}
        </p>
        <div class="table-wrap login-events-table-wrap">
          <table class="login-events-table">
            <caption class="sr-only">
              {{
                $t('page.iam.loginEventsTable')
              }}
            </caption>
            <thead>
              <tr>
                <th scope="col">{{ $t('page.iam.lastLoginAt') }}</th>
                <th scope="col">{{ $t('page.iam.loginEventsAction') }}</th>
                <th scope="col">{{ $t('page.iam.loginEventsResource') }}</th>
                <th scope="col">{{ $t('page.iam.loginEventsOutcome') }}</th>
                <th scope="col">{{ $t('page.iam.loginEventsRequestId') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="loginEventsLoading">
                <td class="table-state" colspan="5">
                  {{ $t('page.iam.loginEventsLoading') }}
                </td>
              </tr>
              <tr v-else-if="loginEventsPage.items.length === 0">
                <td class="table-state" colspan="5">
                  {{ $t('page.iam.loginEventsEmpty') }}
                </td>
              </tr>
              <tr v-for="event in loginEventsPage.items" v-else :key="event.id">
                <td>{{ formatDate(event.createdAt) }}</td>
                <td>{{ event.action }}</td>
                <td>{{ event.resource }}</td>
                <td>{{ event.outcome }}</td>
                <td>{{ event.requestId || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <footer
          class="pagination"
          :aria-label="$t('page.iam.loginEventsTitle')"
        >
          <button
            type="button"
            :disabled="loginEventsLoading || loginEventsCurrentPage <= 1"
            @click="changeLoginEventsPage(loginEventsCurrentPage - 1)"
          >
            {{ $t('page.iam.loginEventsPrevious') }}
          </button>
          <span aria-live="polite">
            {{ loginEventsCurrentPage }} / {{ loginEventsTotalPages }}
          </span>
          <button
            type="button"
            :disabled="
              loginEventsLoading ||
              roleAssignmentLoading ||
              loginEventsCurrentPage >= loginEventsTotalPages
            "
            @click="changeLoginEventsPage(loginEventsCurrentPage + 1)"
          >
            {{ $t('page.iam.loginEventsNext') }}
          </button>
        </footer>
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
  font-size: 0.75rem;
  font-weight: 700;
  color: hsl(var(--muted-foreground));
  text-transform: uppercase;
  letter-spacing: 0.12em;
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

.reset-description {
  margin: 0 0 12px;
  color: hsl(var(--muted-foreground));
}

.reset-user {
  margin: 0 0 16px;
  font-weight: 700;
}

.reset-dialog {
  width: min(520px, 100%);
}

.role-assignment-dialog {
  width: min(620px, 100%);
}

.role-assignment-description {
  margin: 0 0 8px;
  color: hsl(var(--muted-foreground));
}

.role-assignment-user {
  margin: 0 0 16px;
  font-weight: 700;
}

.role-assignment-list {
  display: grid;
  gap: 8px;
  padding: 0;
  margin: 0;
  border: 0;
}

.role-assignment-list legend {
  margin-bottom: 8px;
  font-size: 0.8rem;
  font-weight: 650;
  color: hsl(var(--muted-foreground));
}

.role-assignment-option {
  display: flex;
  gap: 10px;
  align-items: center;
  min-height: 48px;
  padding: 8px 10px;
  border: 1px solid hsl(var(--border));
  border-radius: 9px;
}

.role-assignment-option input {
  width: 18px;
  min-height: 18px;
  padding: 0;
  accent-color: hsl(var(--primary));
}

.role-assignment-option span {
  display: grid;
  gap: 2px;
}

.role-assignment-option small {
  color: hsl(var(--muted-foreground));
}

.login-events-dialog {
  width: min(960px, 100%);
}

.login-events-description {
  margin: 0 0 8px;
  color: hsl(var(--muted-foreground));
}

.login-events-user {
  margin: 0 0 16px;
  font-weight: 700;
}

.login-events-filters {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr)) auto;
  gap: 12px;
  align-items: end;
  margin-bottom: 16px;
}

.login-events-filters .filter-actions {
  align-self: end;
}

.login-events-table-wrap {
  margin-top: 12px;
}

.login-events-table {
  min-width: 680px;
}

.scope-chip,
.status-pill {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 4px 10px;
  font-size: 0.78rem;
  font-weight: 650;
  color: hsl(var(--muted-foreground));
  white-space: nowrap;
  background: hsl(var(--muted));
  border-radius: 999px;
}

.filters,
.table-card {
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 14px;
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
  font-size: 0.8rem;
  font-weight: 650;
  color: hsl(var(--muted-foreground));
}

input,
select,
button {
  min-height: 40px;
  font: inherit;
  color: inherit;
  background: hsl(var(--background));
  border: 1px solid hsl(var(--border));
  border-radius: 9px;
}

input,
select {
  width: 100%;
  padding: 0 11px;
}

button {
  padding: 0 14px;
  font-weight: 650;
  cursor: pointer;
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
  color: hsl(var(--primary-foreground));
  background: hsl(var(--primary));
  border-color: hsl(var(--primary));
}

.link-button {
  min-height: 32px;
  padding: 0 8px;
  color: hsl(var(--primary));
  background: transparent;
  border-color: transparent;
}

.link-button.danger {
  color: hsl(var(--destructive));
}

.icon-button {
  min-width: 36px;
  min-height: 36px;
  padding: 0;
  font-size: 1.4rem;
  line-height: 1;
  background: transparent;
  border-color: transparent;
}

.filter-actions {
  gap: 8px;
}

.feedback-error {
  padding: 12px 14px;
  color: hsl(var(--destructive));
  background: hsl(var(--destructive) / 8%);
  border: 1px solid hsl(var(--destructive) / 35%);
  border-radius: 10px;
}

.feedback-success {
  padding: 12px 14px;
  color: hsl(142deg 60% 25%);
  background: hsl(142deg 70% 40% / 9%);
  border: 1px solid hsl(142deg 70% 40% / 35%);
  border-radius: 10px;
}

.table-card {
  overflow: hidden;
}

.table-heading {
  justify-content: space-between;
  padding: 16px 18px;
  border-bottom: 1px solid hsl(var(--border));
}

.bulk-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  padding: 12px 16px;
  background: hsl(var(--muted) / 18%);
  border-bottom: 1px solid hsl(var(--border));
}

.bulk-select {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  font-size: 0.82rem;
  font-weight: 650;
  color: hsl(var(--foreground));
}

.bulk-select input,
.selection-column input {
  width: 18px;
  min-height: 18px;
  padding: 0;
  accent-color: hsl(var(--primary));
}

.selected-count {
  font-size: 0.8rem;
  color: hsl(var(--muted-foreground));
}

.bulk-actions {
  display: flex;
  gap: 8px;
  margin-left: auto;
}

.selection-column {
  width: 48px;
  text-align: center;
}

.result-count {
  font-size: 0.8rem;
  color: hsl(var(--muted-foreground));
}

.table-wrap {
  overflow-x: auto;
}

table {
  width: 100%;
  min-width: 860px;
  text-align: left;
  border-collapse: collapse;
}

th,
td {
  padding: 13px 16px;
  font-size: 0.88rem;
  vertical-align: middle;
  border-bottom: 1px solid hsl(var(--border));
}

thead th {
  font-size: 0.75rem;
  color: hsl(var(--muted-foreground));
  text-transform: uppercase;
  letter-spacing: 0.04em;
  background: hsl(var(--muted) / 45%);
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
  font-size: 0.72rem;
  font-weight: 400;
  color: hsl(var(--muted-foreground));
}

.role-list {
  color: hsl(var(--muted-foreground));
}

.status-pill[data-status='active'] {
  color: hsl(142deg 60% 28%);
  background: hsl(142deg 70% 45% / 14%);
}

.status-pill[data-status='disabled'] {
  color: hsl(var(--muted-foreground));
  background: hsl(var(--muted));
}

.table-state {
  height: 160px;
  color: hsl(var(--muted-foreground));
  text-align: center;
}

.pagination {
  gap: 10px;
  justify-content: flex-end;
  padding: 14px 16px;
}

.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 20;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgb(15 23 42 / 48%);
}

.user-dialog {
  width: min(680px, 100%);
  max-height: min(720px, calc(100vh - 40px));
  padding: 22px;
  overflow: auto;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 16px;
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
  font-size: 0.8rem;
  color: hsl(var(--muted-foreground));
}

.switch-row span {
  display: grid;
  gap: 4px;
}

.switch-row strong {
  font-size: 0.9rem;
  color: hsl(var(--foreground));
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
  gap: 10px;
  justify-content: flex-end;
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
  white-space: nowrap;
  border: 0;
  clip: rect(0, 0, 0, 0);
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

  .bulk-actions {
    width: 100%;
    margin-left: 0;
  }

  .bulk-actions > * {
    flex: 1;
  }

  .user-form,
  .login-events-filters {
    grid-template-columns: 1fr;
  }

  .field-wide {
    grid-column: auto;
  }
}

@media (max-width: 480px) {
  .bulk-toolbar {
    align-items: flex-start;
  }

  .pagination {
    flex-wrap: wrap;
    justify-content: space-between;
  }

  .page-size {
    width: 100%;
    margin-bottom: 4px;
  }
}
@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
  }
}
</style>
