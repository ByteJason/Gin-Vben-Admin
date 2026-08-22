<script setup lang="ts">
import type {
  IAMPermission,
  IAMRole,
  IAMRoleCreateInput,
  IAMRoleDataScope,
} from '#/api/core/iam';

import { computed, nextTick, onMounted, reactive, ref } from 'vue';

import {
  createIAMRoleApi,
  listIAMPermissionsApi,
  listIAMRolesApi,
  replaceIAMRolePermissionsApi,
} from '#/api/core/iam';
import { $t } from '#/locales';

const roles = ref<IAMRole[]>([]);
const loading = ref(false);
const error = ref('');
const errorSummary = ref<HTMLElement | null>(null);
const feedback = ref('');
const feedbackSummary = ref<HTMLElement | null>(null);
const roleFormOpen = ref(false);
const roleLoading = ref(false);
const roleError = ref('');
const roleErrorSummary = ref<HTMLElement | null>(null);
const permissionEditorOpen = ref(false);
const permissionRole = ref<IAMRole | null>(null);
const permissions = ref<IAMPermission[]>([]);
const permissionSelection = ref<string[]>([]);
const permissionLoading = ref(false);
const permissionSaving = ref(false);
const permissionError = ref('');
const permissionErrorSummary = ref<HTMLElement | null>(null);
const roleForm = reactive<{
  active: boolean;
  dataScope: IAMRoleDataScope;
  id: string;
  name: string;
}>({
  active: true,
  dataScope: 'own',
  id: '',
  name: '',
});

const roleCount = computed(() => roles.value.length);

async function focus(target: typeof errorSummary) {
  await nextTick();
  target.value?.focus();
}

function clearRoleForm() {
  roleForm.active = true;
  roleForm.dataScope = 'own';
  roleForm.id = '';
  roleForm.name = '';
  roleError.value = '';
}

function openCreateRole() {
  clearRoleForm();
  feedback.value = '';
  roleFormOpen.value = true;
}

function closeRoleForm() {
  if (roleLoading.value) return;
  roleFormOpen.value = false;
  roleError.value = '';
}

async function loadPermissions() {
  permissionLoading.value = true;
  permissionError.value = '';
  try {
    permissions.value = (await listIAMPermissionsApi())
      .slice()
      .sort((a, b) => a.id.localeCompare(b.id));
  } catch {
    permissions.value = [];
    permissionError.value = String($t('page.iam.rolePermissionsLoadError'));
    await focus(permissionErrorSummary);
  } finally {
    permissionLoading.value = false;
  }
}

async function openPermissionEditor(role: IAMRole) {
  permissionRole.value = role;
  permissionSelection.value = Array.from(
    new Set(
      (role.permissionIds ?? [])
        .map((permissionId) => permissionId.trim())
        .filter(Boolean),
    ),
  );
  permissionError.value = '';
  feedback.value = '';
  permissionEditorOpen.value = true;
  await loadPermissions();
}

function closePermissionEditor() {
  if (permissionSaving.value) return;
  permissionEditorOpen.value = false;
  permissionRole.value = null;
  permissionError.value = '';
}

async function submitPermissions() {
  if (!permissionRole.value) return;
  permissionError.value = '';
  if (permissionSelection.value.length > 200) {
    permissionError.value = String($t('page.iam.rolePermissionLimit'));
    await focus(permissionErrorSummary);
    return;
  }
  permissionSaving.value = true;
  try {
    const updated = await replaceIAMRolePermissionsApi(
      permissionRole.value.id,
      {
        permissionIds: permissionSelection.value,
      },
    );
    const index = roles.value.findIndex(
      (role) => role.id === permissionRole.value?.id,
    );
    if (index >= 0) {
      roles.value[index] = {
        ...roles.value[index],
        ...updated,
        permissionIds: [...permissionSelection.value],
      };
    }
    permissionEditorOpen.value = false;
    permissionRole.value = null;
    feedback.value = String($t('page.iam.rolePermissionDone'));
    await focus(feedbackSummary);
  } catch {
    permissionError.value = String($t('page.iam.rolePermissionSaveError'));
    await focus(permissionErrorSummary);
  } finally {
    permissionSaving.value = false;
  }
}

function validateRoleForm() {
  if (!roleForm.id.trim() || !roleForm.name.trim()) {
    return String($t('page.iam.roleRequired'));
  }
  if (roleForm.id.trim().length > 128 || roleForm.name.trim().length > 191) {
    return String($t('page.iam.roleRequired'));
  }
  return '';
}

async function loadRoles() {
  loading.value = true;
  error.value = '';
  try {
    roles.value = await listIAMRolesApi();
  } catch {
    roles.value = [];
    error.value = String($t('page.iam.rolesLoadError'));
    await focus(errorSummary);
  } finally {
    loading.value = false;
  }
}

async function submitRole() {
  roleError.value = '';
  const validationError = validateRoleForm();
  if (validationError) {
    roleError.value = validationError;
    await focus(roleErrorSummary);
    return;
  }
  roleLoading.value = true;
  try {
    const input: IAMRoleCreateInput = {
      active: roleForm.active,
      dataScope: roleForm.dataScope,
      id: roleForm.id.trim(),
      name: roleForm.name.trim(),
    };
    await createIAMRoleApi(input);
    roleFormOpen.value = false;
    feedback.value = String($t('page.iam.roleCreated'));
    await loadRoles();
    await focus(feedbackSummary);
  } catch {
    roleError.value = String($t('page.iam.roleSaveError'));
    await focus(roleErrorSummary);
  } finally {
    roleLoading.value = false;
  }
}

function roleScopeLabel(scope?: string) {
  switch (scope) {
    case 'all':
      return String($t('page.iam.roleScopeAll'));
    case 'org':
      return String($t('page.iam.roleScopeOrg'));
    case 'custom':
      return String($t('page.iam.roleScopeCustom'));
    default:
      return String($t('page.iam.roleScopeOwn'));
  }
}

onMounted(loadRoles);
</script>

<template>
  <main
    class="iam-roles-page"
    :aria-busy="loading"
    aria-labelledby="iam-roles-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
        <h1 id="iam-roles-title">{{ $t('page.iam.roles') }}</h1>
        <p class="description">{{ $t('page.iam.rolesDescription') }}</p>
      </div>
      <div class="heading-actions">
        <span class="scope-chip">{{ $t('page.iam.manage') }}</span>
        <button class="primary" type="button" @click="openCreateRole">
          {{ $t('page.iam.roleCreate') }}
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
      {{ loading ? $t('page.iam.rolesLoading') : '' }}
    </p>
    <p
      v-if="feedback"
      ref="feedbackSummary"
      class="feedback feedback-success"
      role="status"
      tabindex="-1"
    >
      {{ feedback }}
    </p>

    <section class="table-card" aria-labelledby="iam-roles-table-title">
      <div class="table-heading">
        <h2 id="iam-roles-table-title">{{ $t('page.iam.roles') }}</h2>
        <span class="result-count">{{ roleCount }}</span>
      </div>
      <div class="table-wrap">
        <table>
          <caption class="sr-only">
            {{
              $t('page.iam.rolesTable')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('page.iam.roleId') }}</th>
              <th scope="col">{{ $t('page.iam.roleName') }}</th>
              <th scope="col">{{ $t('page.iam.roleDataScope') }}</th>
              <th scope="col">{{ $t('page.iam.roleActive') }}</th>
              <th scope="col">{{ $t('page.iam.rolePermissions') }}</th>
              <th scope="col">{{ $t('page.iam.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td class="table-state" colspan="6">
                {{ $t('page.iam.rolesLoading') }}
              </td>
            </tr>
            <tr v-else-if="roles.length === 0">
              <td class="table-state" colspan="6">
                {{ $t('page.iam.rolesEmpty') }}
              </td>
            </tr>
            <tr v-for="role in roles" v-else :key="role.id">
              <th scope="row">
                <span class="primary-text">{{ role.id }}</span>
              </th>
              <td>{{ role.name }}</td>
              <td>{{ roleScopeLabel(role.dataScope) }}</td>
              <td>
                <span
                  class="status-pill"
                  :data-status="role.active ? 'active' : 'disabled'"
                >
                  {{
                    role.active
                      ? $t('page.iam.active')
                      : $t('page.iam.disabled')
                  }}
                </span>
              </td>
              <td>{{ (role.permissionIds ?? []).length }}</td>
              <td>
                <button
                  class="secondary compact"
                  type="button"
                  @click="openPermissionEditor(role)"
                >
                  {{ $t('page.iam.rolePermissionEdit') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section
      v-if="roleFormOpen"
      id="iam-role-form"
      class="modal-backdrop"
      :aria-label="$t('page.iam.roleCreateTitle')"
      @click.self="closeRoleForm"
    >
      <div
        id="iam-role-dialog"
        class="role-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="iam-role-form-title"
      >
        <header class="dialog-heading">
          <div>
            <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
            <h2 id="iam-role-form-title">
              {{ $t('page.iam.roleCreateTitle') }}
            </h2>
          </div>
          <button
            class="icon-button"
            type="button"
            :aria-label="$t('page.iam.roleCancel')"
            :disabled="roleLoading"
            @click="closeRoleForm"
          >
            ×
          </button>
        </header>
        <p
          v-if="roleError"
          ref="roleErrorSummary"
          class="feedback feedback-error"
          role="alert"
          tabindex="-1"
        >
          {{ roleError }}
        </p>
        <form class="role-form" @submit.prevent="submitRole">
          <label class="field" for="iam-role-id">
            <span>{{ $t('page.iam.roleId') }}</span>
            <input
              id="iam-role-id"
              v-model.trim="roleForm.id"
              :disabled="roleLoading"
              maxlength="128"
              required
              type="text"
            />
          </label>
          <label class="field" for="iam-role-name">
            <span>{{ $t('page.iam.roleName') }}</span>
            <input
              id="iam-role-name"
              v-model.trim="roleForm.name"
              :disabled="roleLoading"
              maxlength="191"
              required
              type="text"
            />
          </label>
          <label class="field" for="iam-role-data-scope">
            <span>{{ $t('page.iam.roleDataScope') }}</span>
            <select
              id="iam-role-data-scope"
              v-model="roleForm.dataScope"
              :disabled="roleLoading"
            >
              <option value="all">{{ $t('page.iam.roleScopeAll') }}</option>
              <option value="own">{{ $t('page.iam.roleScopeOwn') }}</option>
              <option value="org">{{ $t('page.iam.roleScopeOrg') }}</option>
              <option value="custom">
                {{ $t('page.iam.roleScopeCustom') }}
              </option>
            </select>
          </label>
          <label class="switch-row field-wide" for="iam-role-active">
            <span>
              <strong>{{ $t('page.iam.roleActive') }}</strong>
              <small>{{ $t('page.iam.roleActiveHelp') }}</small>
            </span>
            <input
              id="iam-role-active"
              v-model="roleForm.active"
              :disabled="roleLoading"
              type="checkbox"
            />
          </label>
          <footer class="dialog-actions">
            <button
              type="button"
              :disabled="roleLoading"
              @click="closeRoleForm"
            >
              {{ $t('page.iam.roleCancel') }}
            </button>
            <button class="primary" type="submit" :disabled="roleLoading">
              {{
                roleLoading
                  ? $t('page.iam.roleSaving')
                  : $t('page.iam.roleSave')
              }}
            </button>
          </footer>
        </form>
      </div>
    </section>

    <section
      v-if="permissionEditorOpen"
      id="iam-role-permission-editor"
      class="modal-backdrop rolePermissionEditor"
      :aria-label="$t('page.iam.rolePermissionTitle')"
      @click.self="closePermissionEditor"
    >
      <div
        class="role-dialog permission-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="iam-role-permission-title"
        aria-describedby="iam-role-permission-description"
      >
        <header class="dialog-heading">
          <div>
            <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
            <h2 id="iam-role-permission-title">
              {{ $t('page.iam.rolePermissionTitle') }}
            </h2>
            <p id="iam-role-permission-description" class="description">
              {{ permissionRole?.name }} ·
              {{ $t('page.iam.rolePermissionDescription') }}
            </p>
          </div>
          <button
            class="icon-button"
            type="button"
            :aria-label="$t('page.iam.roleCancel')"
            :disabled="permissionSaving"
            @click="closePermissionEditor"
          >
            ×
          </button>
        </header>
        <p
          v-if="permissionError"
          ref="permissionErrorSummary"
          class="feedback feedback-error"
          role="alert"
          tabindex="-1"
        >
          {{ permissionError }}
        </p>
        <p class="sr-status" aria-live="polite">
          {{ permissionLoading ? $t('page.iam.rolePermissionsLoading') : '' }}
        </p>
        <div v-if="permissionLoading" class="permission-state">
          {{ $t('page.iam.rolePermissionsLoading') }}
        </div>
        <div v-else-if="permissions.length === 0" class="permission-state">
          {{ $t('page.iam.rolePermissionsEmpty') }}
        </div>
        <fieldset v-else class="permission-list" :disabled="permissionSaving">
          <legend class="sr-only">{{ $t('page.iam.rolePermissions') }}</legend>
          <label
            v-for="permission in permissions"
            :key="permission.id"
            class="permission-option"
          >
            <input
              v-model="permissionSelection"
              :value="permission.id"
              type="checkbox"
            />
            <span>
              <strong>{{ permission.name }}</strong>
              <small
                >{{ permission.id }} · {{ permission.method }}
                {{ permission.path }}</small
              >
            </span>
          </label>
        </fieldset>
        <p class="permission-count" aria-live="polite">
          {{ permissionSelection.length }}/200
          {{ $t('page.iam.rolePermissions') }}
        </p>
        <footer class="dialog-actions">
          <button
            type="button"
            :disabled="permissionSaving"
            @click="closePermissionEditor"
          >
            {{ $t('page.iam.roleCancel') }}
          </button>
          <button
            class="primary"
            type="button"
            :disabled="permissionSaving || permissionLoading"
            @click="submitPermissions"
          >
            {{
              permissionSaving
                ? $t('page.iam.rolePermissionSaving')
                : $t('page.iam.rolePermissionSave')
            }}
          </button>
        </footer>
      </div>
    </section>
  </main>
</template>

<style scoped>
.iam-roles-page {
  max-width: 1180px;
  padding: 24px;
  margin: 0 auto;
  color: hsl(var(--foreground));
}

.page-heading,
.table-heading,
.heading-actions,
.dialog-heading,
.dialog-actions {
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
  gap: 10px;
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

.scope-chip,
.status-pill,
.secondary {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 0.78rem;
  font-weight: 650;
}

.scope-chip {
  color: hsl(var(--primary));
  background: hsl(var(--primary) / 0.1);
}

.status-pill[data-status='active'] {
  color: hsl(142 70% 30%);
  background: hsl(142 70% 45% / 0.14);
}

.status-pill[data-status='disabled'] {
  color: hsl(var(--muted-foreground));
  background: hsl(var(--muted));
}

.secondary {
  color: hsl(var(--foreground));
  background: hsl(var(--muted));
  border: 1px solid hsl(var(--border));
}

.secondary.compact {
  min-height: 30px;
  padding: 4px 9px;
  font-size: 0.76rem;
}

.permission-dialog {
  max-width: 720px;
}

.permission-list {
  display: grid;
  gap: 8px;
  max-height: 360px;
  padding: 0;
  margin: 18px 0 0;
  overflow: auto;
  border: 0;
}

.permission-option {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  padding: 10px 12px;
  border: 1px solid hsl(var(--border));
  border-radius: 10px;
  cursor: pointer;
}

.permission-option input {
  width: 18px;
  height: 18px;
  margin-top: 2px;
  accent-color: hsl(var(--primary));
}

.permission-option span {
  display: grid;
  gap: 3px;
}

.permission-option small,
.permission-count {
  color: hsl(var(--muted-foreground));
}

.permission-option small {
  overflow-wrap: anywhere;
}

.permission-state {
  padding: 28px 0;
  color: hsl(var(--muted-foreground));
  text-align: center;
}

.permission-count {
  margin: 10px 0 0;
  font-size: 0.82rem;
}

button:focus-visible,
input:focus-visible,
select:focus-visible {
  outline: 3px solid hsl(var(--ring));
  outline-offset: 2px;
}

@media (max-width: 720px) {
  .iam-roles-page {
    padding: 16px;
  }

  .page-heading {
    flex-direction: column;
  }

  .table-wrap {
    overflow-x: auto;
  }

  table {
    min-width: 680px;
  }
}

.primary {
  color: hsl(var(--primary-foreground));
  background: hsl(var(--primary));
}

button,
input,
select {
  min-height: 38px;
  font: inherit;
}

button {
  padding: 7px 12px;
  color: hsl(var(--foreground));
  cursor: pointer;
  background: hsl(var(--background));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
}

button:hover:not(:disabled) {
  border-color: hsl(var(--primary));
}

button:disabled,
input:disabled,
select:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.feedback {
  padding: 10px 12px;
  margin: 12px 0;
  border-radius: 8px;
}

.feedback-error {
  color: hsl(0 65% 36%);
  background: hsl(0 75% 55% / 0.1);
}

.feedback-success {
  color: hsl(142 70% 28%);
  background: hsl(142 70% 45% / 0.13);
}

.sr-status,
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

.table-card {
  overflow: hidden;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 14px;
}

.table-heading {
  justify-content: space-between;
  padding: 16px 18px;
  border-bottom: 1px solid hsl(var(--border));
}

.result-count {
  color: hsl(var(--muted-foreground));
  font-size: 0.85rem;
}

.table-wrap {
  overflow-x: auto;
}

table {
  width: 100%;
  min-width: 620px;
  border-collapse: collapse;
}

th,
td {
  padding: 13px 16px;
  text-align: left;
  border-bottom: 1px solid hsl(var(--border));
}

th {
  font-size: 0.78rem;
  font-weight: 700;
  color: hsl(var(--muted-foreground));
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

tbody th {
  color: hsl(var(--foreground));
  text-transform: none;
  letter-spacing: normal;
}

tr:last-child th,
tr:last-child td {
  border-bottom: 0;
}

.primary-text {
  font-weight: 650;
}

.table-state {
  padding: 28px;
  color: hsl(var(--muted-foreground));
  text-align: center;
}

.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 30;
  display: grid;
  place-items: center;
  padding: 20px;
  background: hsl(222 47% 11% / 0.46);
}

.role-dialog {
  width: min(520px, 100%);
  max-height: min(720px, 100%);
  padding: 22px;
  overflow-y: auto;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 14px;
  box-shadow: 0 24px 80px hsl(222 47% 11% / 0.28);
}

.dialog-heading {
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 14px;
}

.dialog-heading h2 {
  margin-bottom: 0;
}

.icon-button {
  min-width: 38px;
  padding: 0;
  font-size: 1.25rem;
}

.role-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.field {
  display: grid;
  gap: 6px;
  color: hsl(var(--muted-foreground));
  font-size: 0.82rem;
  font-weight: 650;
}

.field input,
.field select {
  width: 100%;
  padding: 7px 10px;
  color: hsl(var(--foreground));
  background: hsl(var(--background));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
}

.field-wide {
  grid-column: 1 / -1;
}

.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 48px;
  padding: 10px 12px;
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
}

.switch-row span {
  display: grid;
  gap: 3px;
}

.switch-row small {
  color: hsl(var(--muted-foreground));
  font-weight: 400;
}

.switch-row input {
  width: 18px;
  min-height: 18px;
  accent-color: hsl(var(--primary));
}

.dialog-actions {
  grid-column: 1 / -1;
  justify-content: flex-end;
  gap: 10px;
  padding-top: 4px;
}

@media (max-width: 720px) {
  .iam-roles-page {
    padding: 16px;
  }

  .page-heading {
    display: grid;
  }

  .heading-actions {
    justify-content: space-between;
  }

  .role-form {
    grid-template-columns: 1fr;
  }
}
</style>
