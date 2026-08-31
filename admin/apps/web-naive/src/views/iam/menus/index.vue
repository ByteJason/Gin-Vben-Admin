<script setup lang="ts">
import type {
  IAMComponent,
  IAMMenu,
  IAMMenuCreateInput,
  IAMMenuType,
} from '#/api/core/iam';

import { computed, nextTick, onMounted, reactive, ref } from 'vue';

import { useAccess } from '@vben/access';
import { ManagementPage } from '@vben/common-ui';

import {
  createIAMMenuApi,
  deleteIAMMenuApi,
  listIAMComponentsApi,
  listIAMMenusApi,
  reorderIAMMenusApi,
  updateIAMMenuApi,
} from '#/api/core/iam';
import { $t } from '#/locales';

const { hasAccessByCodes } = useAccess();
const canManage = computed(() => hasAccessByCodes(['iam:menus:manage']));
const canReadComponents = computed(() =>
  hasAccessByCodes(['iam:components:read']),
);
const canEditMenus = computed(() => canManage.value && canReadComponents.value);

type MenuRow = {
  depth: number;
  menu: IAMMenu;
};

type MenuForm = Omit<IAMMenuCreateInput, 'id'> & { id: string };

const menus = ref<IAMMenu[]>([]);
const components = ref<IAMComponent[]>([]);
const loading = ref(false);
const componentsLoading = ref(false);
const error = ref('');
const feedback = ref('');
const errorSummary = ref<HTMLElement | null>(null);
const feedbackSummary = ref<HTMLElement | null>(null);
const formErrorSummary = ref<HTMLElement | null>(null);
const formOpen = ref(false);
const saving = ref(false);
const deletingId = ref('');
const reordering = ref(false);
const editingId = ref('');

const defaultForm = (): MenuForm => ({
  active: true,
  component: '',
  external: false,
  icon: '',
  id: '',
  keepAlive: false,
  name: '',
  parentId: '',
  path: '',
  permission: '',
  redirect: '',
  sort: 0,
  type: 'menu',
  visible: true,
});

const menuForm = reactive<MenuForm>(defaultForm());
const formError = ref('');

const localizedMenuKeys: Record<string, string> = {
  'menu-overview': 'page.navigation.dashboard',
  'menu-identity': 'page.iam.group',
  'menu-identity-users': 'page.iam.users',
  'menu-identity-roles': 'page.iam.roles',
  'menu-identity-menus': 'page.iam.menus',
  'menu-identity-permissions': 'page.iam.permissions',
  'menu-system-config': 'page.settings.group',
  'menu-system-settings': 'page.settings.title',
  'menu-system-dictionary': 'page.dictionary.title',
  'menu-system-mail': 'page.mail.title',
  'menu-system-files': 'page.files.title',
  'menu-system-observability': 'page.observability.title',
  'menu-operations': 'page.navigation.operations',
  'menu-operations-monitor': 'page.monitor.title',
  'menu-operations-audit': 'page.audit.title',
  'menu-operations-tasks': 'page.tasks.title',
  'menu-operations-data-jobs': 'page.navigation.dataJobs',
};

const componentOptions = computed(() =>
  components.value
    .slice()
    .sort((left, right) =>
      `${left.kind}:${left.label}:${left.component}`.localeCompare(
        `${right.kind}:${right.label}:${right.component}`,
      ),
    ),
);

const menuRows = computed<MenuRow[]>(() => {
  const byId = new Map(menus.value.map((menu) => [menu.id, menu]));
  const byParent = new Map<string, IAMMenu[]>();
  for (const menu of menus.value) {
    const parentId = menu.parentId?.trim() ?? '';
    const siblings = byParent.get(parentId) ?? [];
    siblings.push(menu);
    byParent.set(parentId, siblings);
  }
  const sortMenus = (items: IAMMenu[]) =>
    [...items].sort((left, right) => {
      const sortDelta = (left.sort ?? 0) - (right.sort ?? 0);
      return (
        sortDelta ||
        `${left.name}:${left.id}`.localeCompare(`${right.name}:${right.id}`)
      );
    });
  const rows: MenuRow[] = [];
  const visited = new Set<string>();
  const walk = (parentId: string, depth: number) => {
    for (const menu of sortMenus(byParent.get(parentId) ?? [])) {
      if (visited.has(menu.id)) continue;
      visited.add(menu.id);
      rows.push({ depth, menu });
      walk(menu.id, depth + 1);
    }
  };
  const roots = sortMenus(
    menus.value.filter(
      (menu) => !menu.parentId?.trim() || !byId.has(menu.parentId.trim()),
    ),
  );
  for (const menu of roots) {
    if (visited.has(menu.id)) continue;
    visited.add(menu.id);
    rows.push({ depth: 0, menu });
    walk(menu.id, 1);
  }
  for (const menu of sortMenus(menus.value)) {
    if (visited.has(menu.id)) continue;
    visited.add(menu.id);
    rows.push({ depth: 0, menu });
    walk(menu.id, 1);
  }
  return rows;
});

const isEditing = computed(() => Boolean(editingId.value));

async function focus(target: typeof errorSummary) {
  await nextTick();
  target.value?.focus();
}

function normalizeMenu(menu: IAMMenu): IAMMenu {
  return {
    ...menu,
    active: menu.active ?? true,
    component: menu.component ?? '',
    external: menu.external ?? false,
    icon: menu.icon ?? '',
    keepAlive: menu.keepAlive ?? false,
    parentId: menu.parentId ?? '',
    permission: menu.permission ?? '',
    redirect: menu.redirect ?? '',
    sort: menu.sort ?? 0,
    type: menu.type ?? 'menu',
    visible: menu.visible ?? true,
  };
}

async function loadMenus() {
  loading.value = true;
  error.value = '';
  try {
    menus.value = (await listIAMMenusApi()).map(normalizeMenu);
  } catch {
    menus.value = [];
    error.value = String($t('page.iam.menusLoadError'));
    await focus(errorSummary);
  } finally {
    loading.value = false;
  }
}

async function loadComponents() {
  if (!canReadComponents.value) return;
  componentsLoading.value = true;
  try {
    components.value = await listIAMComponentsApi();
  } catch {
    components.value = [];
    error.value = String($t('page.iam.menuComponentsLoadError'));
    await focus(errorSummary);
  } finally {
    componentsLoading.value = false;
  }
}

function parentName(menu: IAMMenu) {
  const parentId = menu.parentId?.trim();
  if (!parentId) return String($t('page.iam.menuRoot'));
  const parent = menus.value.find((candidate) => candidate.id === parentId);
  return parent ? localizedMenuName(parent) : parentId;
}

function localizedMenuName(menu: IAMMenu) {
  const key = localizedMenuKeys[menu.id];
  return key ? String($t(key)) : menu.name;
}

function menuTypeLabel(type?: IAMMenuType) {
  if (type === 'directory') return String($t('page.iam.menuDirectory'));
  if (type === 'button') return String($t('page.iam.menuButton'));
  return String($t('page.iam.menuPage'));
}

function resetForm() {
  Object.assign(menuForm, defaultForm());
  editingId.value = '';
  formError.value = '';
}

function openCreateMenu(parentId = '') {
  if (!canEditMenus.value) return;
  resetForm();
  menuForm.parentId = parentId;
  feedback.value = '';
  formOpen.value = true;
}

function openEditMenu(menu: IAMMenu) {
  if (!canEditMenus.value) return;
  resetForm();
  editingId.value = menu.id;
  Object.assign(menuForm, {
    ...defaultForm(),
    ...normalizeMenu(menu),
    id: menu.id,
  });
  feedback.value = '';
  formOpen.value = true;
}

function closeForm() {
  if (saving.value) return;
  formOpen.value = false;
  formError.value = '';
}

function validateForm() {
  const id = menuForm.id.trim();
  const name = menuForm.name.trim();
  const path = menuForm.path.trim();
  if (!name || !path || (!isEditing.value && !id)) return false;
  if (!['button', 'directory', 'menu'].includes(menuForm.type)) return false;
  if (menuForm.type !== 'directory' && !menuForm.component?.trim())
    return false;
  if (menuForm.parentId?.trim() === menuForm.id.trim()) return false;
  if (!isEditing.value && menus.value.some((menu) => menu.id.trim() === id)) {
    return false;
  }
  return true;
}

async function saveMenu() {
  if (!canEditMenus.value) return;
  formError.value = '';
  if (!validateForm()) {
    formError.value = String($t('page.iam.menuSaveError'));
    await focus(formErrorSummary);
    return;
  }
  saving.value = true;
  const payload: IAMMenuCreateInput = {
    active: menuForm.active,
    component: menuForm.component?.trim() || undefined,
    external: menuForm.external,
    icon: menuForm.icon?.trim() || undefined,
    id: menuForm.id.trim(),
    keepAlive: menuForm.keepAlive,
    name: menuForm.name.trim(),
    parentId: menuForm.parentId?.trim() || undefined,
    path: menuForm.path.trim(),
    permission: menuForm.permission?.trim() || undefined,
    redirect: menuForm.redirect?.trim() || undefined,
    sort: Number(menuForm.sort) || 0,
    type: menuForm.type,
    visible: menuForm.visible,
  };
  const { id: payloadId, ...patchPayload } = payload;
  try {
    if (isEditing.value) {
      const updated = await updateIAMMenuApi(editingId.value, patchPayload);
      const index = menus.value.findIndex(
        (menu) => menu.id === editingId.value,
      );
      if (index >= 0) menus.value[index] = normalizeMenu(updated);
    } else {
      const created = await createIAMMenuApi({
        ...patchPayload,
        id: payloadId,
      });
      menus.value.push(normalizeMenu(created));
    }
    formOpen.value = false;
    feedback.value = String($t('page.iam.menuSaved'));
    await focus(feedbackSummary);
  } catch {
    formError.value = String($t('page.iam.menuSaveError'));
    await focus(formErrorSummary);
  } finally {
    saving.value = false;
  }
}

async function deleteMenu(menu: IAMMenu) {
  if (!canManage.value) return;
  if (deletingId.value) return;
  if (!window.confirm(String($t('page.iam.menuDeleteConfirm')))) return;
  deletingId.value = menu.id;
  error.value = '';
  try {
    await deleteIAMMenuApi(menu.id);
    menus.value = menus.value.filter((candidate) => candidate.id !== menu.id);
    feedback.value = String($t('page.iam.menuDeleted'));
    await focus(feedbackSummary);
  } catch {
    error.value = String($t('page.iam.menuDeleteError'));
    await focus(errorSummary);
  } finally {
    deletingId.value = '';
  }
}

function setSort(menu: IAMMenu, event: Event) {
  if (!canManage.value) return;
  const value = Number((event.target as HTMLInputElement).value);
  menu.sort = Number.isFinite(value) ? Math.trunc(value) : 0;
}

async function saveReorder() {
  if (!canManage.value) return;
  reordering.value = true;
  error.value = '';
  try {
    await reorderIAMMenusApi({
      items: menus.value.map((menu) => ({
        id: menu.id,
        parentId: menu.parentId?.trim() || undefined,
        sort: menu.sort ?? 0,
      })),
    });
    feedback.value = String($t('page.iam.menuReorderSaved'));
    await focus(feedbackSummary);
  } catch {
    error.value = String($t('page.iam.menuReorderError'));
    await focus(errorSummary);
  } finally {
    reordering.value = false;
  }
}

onMounted(async () => {
  await loadMenus();
  await loadComponents();
});
</script>

<template>
  <ManagementPage
    class="iam-menus-page"
    :aria-busy="loading || saving || reordering"
    aria-labelledby="iam-menus-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
        <h1 id="iam-menus-title">{{ $t('page.iam.menus') }}</h1>
        <p class="description">{{ $t('page.iam.menusDescription') }}</p>
      </div>
      <div class="heading-actions">
        <span v-if="canManage" class="scope-chip">{{
          $t('page.iam.manage')
        }}</span>
        <button
          v-if="canEditMenus"
          class="primary-button"
          type="button"
          @click="openCreateMenu()"
        >
          {{ $t('page.iam.menuCreate') }}
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
    <p
      v-if="feedback"
      ref="feedbackSummary"
      class="feedback feedback-success"
      role="status"
      tabindex="-1"
    >
      {{ feedback }}
    </p>
    <p class="sr-status" aria-live="polite">
      {{ loading ? $t('page.iam.menusLoading') : '' }}
    </p>

    <section class="table-card" aria-labelledby="iam-menus-table-title">
      <div class="table-heading">
        <div>
          <h2 id="iam-menus-table-title">{{ $t('page.iam.menus') }}</h2>
          <p class="table-help">{{ $t('page.iam.menuReorderHelp') }}</p>
        </div>
        <div class="table-actions">
          <span class="result-count">{{ menuRows.length }}</span>
          <button
            v-if="canManage"
            class="secondary-button"
            type="button"
            :disabled="reordering || loading || menus.length === 0"
            @click="saveReorder"
          >
            {{
              reordering
                ? $t('page.iam.menuSaving')
                : $t('page.iam.menuReorder')
            }}
          </button>
        </div>
      </div>
      <div class="table-wrap">
        <table>
          <caption class="sr-only">
            {{
              $t('page.iam.menusTable')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('page.iam.menuId') }}</th>
              <th scope="col">{{ $t('page.iam.menuName') }}</th>
              <th scope="col">{{ $t('page.iam.menuType') }}</th>
              <th scope="col">{{ $t('page.iam.menuPath') }}</th>
              <th scope="col">{{ $t('page.iam.menuParent') }}</th>
              <th scope="col">{{ $t('page.iam.menuSort') }}</th>
              <th scope="col">{{ $t('page.iam.menuVisible') }}</th>
              <th scope="col">{{ $t('page.iam.menuActive') }}</th>
              <th scope="col">{{ $t('page.iam.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td class="table-state" colspan="9">
                {{ $t('page.iam.menusLoading') }}
              </td>
            </tr>
            <tr v-else-if="menuRows.length === 0">
              <td class="table-state" colspan="9">
                {{ $t('page.iam.menusEmpty') }}
              </td>
            </tr>
            <tr v-for="row in menuRows" v-else :key="row.menu.id">
              <th scope="row">
                <span class="primary-text">{{ row.menu.id }}</span>
              </th>
              <td>
                <span
                  class="menu-name"
                  :style="{ paddingLeft: `${row.depth * 18}px` }"
                >
                  <span v-if="row.depth" class="tree-branch" aria-hidden="true"
                    >└</span
                  >
                  {{ localizedMenuName(row.menu) }}
                </span>
              </td>
              <td>{{ menuTypeLabel(row.menu.type) }}</td>
              <td>
                <code>{{ row.menu.path || '—' }}</code>
              </td>
              <td>{{ parentName(row.menu) }}</td>
              <td>
                <label class="sr-only" :for="`menu-sort-${row.menu.id}`">{{
                  $t('page.iam.menuSort')
                }}</label>
                <input
                  :id="`menu-sort-${row.menu.id}`"
                  class="sort-input"
                  type="number"
                  :disabled="!canManage"
                  :value="row.menu.sort ?? 0"
                  @input="setSort(row.menu, $event)"
                />
              </td>
              <td>
                {{
                  row.menu.visible
                    ? $t('page.iam.menuVisibleYes')
                    : $t('page.iam.menuVisibleNo')
                }}
              </td>
              <td>
                <span
                  class="status-pill"
                  :data-status="row.menu.active ? 'active' : 'disabled'"
                >
                  {{
                    row.menu.active
                      ? $t('page.iam.active')
                      : $t('page.iam.disabled')
                  }}
                </span>
              </td>
              <td>
                <div class="row-actions">
                  <button
                    v-if="canEditMenus"
                    class="link-button"
                    type="button"
                    @click="openCreateMenu(row.menu.id)"
                  >
                    {{ $t('page.iam.menuCreateChild') }}
                  </button>
                  <button
                    v-if="canEditMenus"
                    class="link-button"
                    type="button"
                    @click="openEditMenu(row.menu)"
                  >
                    {{ $t('page.iam.menuEdit') }}
                  </button>
                  <button
                    v-if="canManage"
                    class="link-button danger-link"
                    type="button"
                    :disabled="deletingId === row.menu.id"
                    @click="deleteMenu(row.menu)"
                  >
                    {{ $t('page.iam.menuDelete') }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section
      v-if="formOpen && canEditMenus"
      class="menu-dialog-backdrop"
      role="presentation"
      @keydown.esc="closeForm"
    >
      <div
        class="menu-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="menu-form-title"
      >
        <div class="dialog-heading">
          <div>
            <h2 id="menu-form-title">
              {{
                isEditing ? $t('page.iam.menuEdit') : $t('page.iam.menuCreate')
              }}
            </h2>
            <p class="description">{{ $t('page.iam.menuFormDescription') }}</p>
          </div>
          <button
            class="icon-button"
            type="button"
            :disabled="saving"
            @click="closeForm"
            aria-label="Close"
          >
            ×
          </button>
        </div>
        <p
          v-if="formError"
          ref="formErrorSummary"
          class="feedback feedback-error"
          role="alert"
          tabindex="-1"
        >
          {{ formError }}
        </p>
        <form class="menu-form" @submit.prevent="saveMenu">
          <label v-if="!isEditing" for="menu-id"
            >{{ $t('page.iam.menuId')
            }}<span aria-hidden="true"> *</span></label
          >
          <input
            v-if="!isEditing"
            id="menu-id"
            v-model="menuForm.id"
            required
            maxlength="64"
            autocomplete="off"
          />
          <label for="menu-name"
            >{{ $t('page.iam.menuName')
            }}<span aria-hidden="true"> *</span></label
          >
          <input
            id="menu-name"
            v-model="menuForm.name"
            required
            maxlength="191"
            autocomplete="off"
          />
          <label for="menu-path"
            >{{ $t('page.iam.menuPath')
            }}<span aria-hidden="true"> *</span></label
          >
          <input
            id="menu-path"
            v-model="menuForm.path"
            required
            maxlength="255"
            autocomplete="off"
          />
          <label for="menu-type"
            >{{ $t('page.iam.menuType')
            }}<span aria-hidden="true"> *</span></label
          >
          <select id="menu-type" v-model="menuForm.type">
            <option value="directory">
              {{ $t('page.iam.menuDirectory') }}
            </option>
            <option value="menu">{{ $t('page.iam.menuPage') }}</option>
            <option value="button">{{ $t('page.iam.menuButton') }}</option>
          </select>
          <label for="menu-parent">{{ $t('page.iam.menuParent') }}</label>
          <select id="menu-parent" v-model="menuForm.parentId">
            <option value="">{{ $t('page.iam.menuRoot') }}</option>
            <option
              v-for="candidate in menuRows"
              :key="candidate.menu.id"
              :value="candidate.menu.id"
              :disabled="candidate.menu.id === editingId"
            >
              {{ '· '.repeat(candidate.depth)
              }}{{ localizedMenuName(candidate.menu) }}
            </option>
          </select>
          <label for="menu-component">{{ $t('page.iam.menuComponent') }}</label>
          <select
            id="menu-component"
            v-model="menuForm.component"
            :disabled="componentsLoading"
          >
            <option value="">
              {{ componentsLoading ? $t('page.iam.menusLoading') : '—' }}
            </option>
            <option
              v-for="component in componentOptions"
              :key="component.id"
              :value="component.component"
            >
              {{ component.label }} · {{ component.component }}
            </option>
          </select>
          <label for="menu-redirect">{{ $t('page.iam.menuRedirect') }}</label>
          <input
            id="menu-redirect"
            v-model="menuForm.redirect"
            maxlength="255"
            autocomplete="off"
          />
          <label for="menu-icon">{{ $t('page.iam.menuIcon') }}</label>
          <input
            id="menu-icon"
            v-model="menuForm.icon"
            maxlength="191"
            autocomplete="off"
          />
          <label for="menu-permission">{{
            $t('page.iam.menuPermission')
          }}</label>
          <input
            id="menu-permission"
            v-model="menuForm.permission"
            maxlength="191"
            autocomplete="off"
          />
          <label for="menu-sort">{{ $t('page.iam.menuSort') }}</label>
          <input
            id="menu-sort"
            v-model.number="menuForm.sort"
            type="number"
            min="-1000000"
            max="1000000"
          />
          <label class="checkbox-field"
            ><input v-model="menuForm.visible" type="checkbox" />
            {{ $t('page.iam.menuVisible') }}</label
          >
          <label class="checkbox-field"
            ><input v-model="menuForm.active" type="checkbox" />
            {{ $t('page.iam.menuActive') }}</label
          >
          <label class="checkbox-field"
            ><input v-model="menuForm.keepAlive" type="checkbox" />
            {{ $t('page.iam.menuKeepAlive') }}</label
          >
          <label class="checkbox-field"
            ><input v-model="menuForm.external" type="checkbox" />
            {{ $t('page.iam.menuExternal') }}</label
          >
          <div class="dialog-actions">
            <button
              class="secondary-button"
              type="button"
              :disabled="saving"
              @click="closeForm"
            >
              {{ $t('page.iam.menuCancel') }}
            </button>
            <button class="primary-button" type="submit" :disabled="saving">
              {{ saving ? $t('page.iam.menuSaving') : $t('page.iam.menuSave') }}
            </button>
          </div>
        </form>
      </div>
    </section>
  </ManagementPage>
</template>

<style scoped>
.iam-menus-page {
  color: hsl(var(--foreground));
}

.page-heading,
.table-heading,
.dialog-heading {
  display: flex;
  align-items: center;
}

.page-heading {
  gap: 24px;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
}

.heading-actions,
.table-actions,
.row-actions,
.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
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

.description,
.table-help {
  max-width: 760px;
  margin: 0;
  color: hsl(var(--muted-foreground));
}

.table-help {
  font-size: 0.85rem;
}

.scope-chip,
.status-pill {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 4px 10px;
  font-size: 0.78rem;
  font-weight: 650;
  border-radius: 999px;
}

.scope-chip {
  color: hsl(var(--primary));
  background: hsl(var(--primary) / 10%);
}

.status-pill[data-status='active'] {
  color: hsl(142deg 70% 30%);
  background: hsl(142deg 70% 45% / 14%);
}

.status-pill[data-status='disabled'] {
  color: hsl(var(--muted-foreground));
  background: hsl(var(--muted));
}

.feedback {
  padding: 10px 12px;
  margin: 12px 0;
  border-radius: 8px;
}

.feedback-error {
  color: hsl(0deg 65% 36%);
  background: hsl(0deg 75% 55% / 10%);
}

.feedback-success {
  color: hsl(142deg 70% 30%);
  background: hsl(142deg 70% 45% / 12%);
}

.sr-status,
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  overflow: hidden;
  white-space: nowrap;
  border: 0;
  clip-path: inset(50%);
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
  font-size: 0.85rem;
  color: hsl(var(--muted-foreground));
}

.table-wrap {
  overflow-x: auto;
}

table {
  width: 100%;
  min-width: 1120px;
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

tbody tr:last-child th,
tbody tr:last-child td {
  border-bottom: 0;
}

.primary-text {
  font-weight: 650;
}

.menu-name {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  font-weight: 600;
}

.tree-branch {
  margin-right: 6px;
  color: hsl(var(--muted-foreground));
}

code {
  padding: 2px 5px;
  color: hsl(var(--muted-foreground));
  background: hsl(var(--muted));
  border-radius: 4px;
}

.table-state {
  padding: 28px;
  color: hsl(var(--muted-foreground));
  text-align: center;
}

button,
input,
select {
  font: inherit;
}

button {
  min-height: 34px;
  cursor: pointer;
  border-radius: 7px;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.primary-button,
.secondary-button {
  padding: 6px 12px;
  border: 1px solid hsl(var(--border));
}

.primary-button {
  color: hsl(var(--primary-foreground));
  background: hsl(var(--primary));
  border-color: hsl(var(--primary));
}

.secondary-button {
  color: hsl(var(--foreground));
  background: hsl(var(--background));
}

.link-button {
  min-height: auto;
  padding: 2px 0;
  color: hsl(var(--primary));
  background: transparent;
  border: 0;
}

.danger-link {
  color: hsl(0deg 65% 42%);
}

.icon-button {
  width: 34px;
  background: transparent;
  border: 1px solid hsl(var(--border));
}

.sort-input {
  width: 76px;
  padding: 5px 7px;
  color: inherit;
  background: hsl(var(--background));
  border: 1px solid hsl(var(--border));
  border-radius: 6px;
}

.menu-dialog-backdrop {
  position: fixed;
  inset: 0;
  z-index: 20;
  display: grid;
  place-items: center;
  padding: 20px;
  background: hsl(220deg 30% 10% / 55%);
}

.menu-dialog {
  width: min(720px, 100%);
  max-height: min(92vh, 900px);
  padding: 22px;
  overflow: auto;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 14px;
  box-shadow: 0 20px 60px hsl(220deg 30% 10% / 25%);
}

.dialog-heading {
  gap: 16px;
  justify-content: space-between;
  margin-bottom: 12px;
}

.menu-form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
  gap: 10px 14px;
  align-items: center;
}

.menu-form > label:not(.checkbox-field) {
  font-size: 0.9rem;
  color: hsl(var(--muted-foreground));
}

.menu-form input:not([type='checkbox']),
.menu-form select {
  width: 100%;
  min-height: 36px;
  padding: 6px 9px;
  color: inherit;
  background: hsl(var(--background));
  border: 1px solid hsl(var(--border));
  border-radius: 6px;
}

.checkbox-field {
  display: flex;
  grid-column: 2;
  gap: 8px;
  align-items: center;
}

.dialog-actions {
  grid-column: 1 / -1;
  justify-content: flex-end;
  margin-top: 8px;
}

@media (max-width: 720px) {
  .page-heading {
    display: grid;
  }

  .menu-form {
    grid-template-columns: 1fr;
  }

  .checkbox-field,
  .dialog-actions {
    grid-column: 1;
  }

  .heading-actions {
    justify-content: flex-start;
  }
}
</style>
