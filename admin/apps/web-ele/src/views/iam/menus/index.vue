<script setup lang="ts">
import type { IAMMenu } from '#/api/core/iam';

import { computed, nextTick, onMounted, ref } from 'vue';

import { listIAMMenusApi } from '#/api/core/iam';
import { $t } from '#/locales';

type MenuRow = {
  depth: number;
  menu: IAMMenu;
};

const menus = ref<IAMMenu[]>([]);
const loading = ref(false);
const error = ref('');
const errorSummary = ref<HTMLElement | null>(null);

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
    [...items].sort((left, right) =>
      `${left.name}:${left.id}`.localeCompare(`${right.name}:${right.id}`),
    );
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
  // Cycles or disconnected records remain visible rather than disappearing.
  for (const menu of sortMenus(menus.value)) {
    if (visited.has(menu.id)) continue;
    visited.add(menu.id);
    rows.push({ depth: 0, menu });
    walk(menu.id, 1);
  }
  return rows;
});

async function focusError() {
  await nextTick();
  errorSummary.value?.focus();
}

async function loadMenus() {
  loading.value = true;
  error.value = '';
  try {
    menus.value = await listIAMMenusApi();
  } catch {
    menus.value = [];
    error.value = String($t('page.iam.menusLoadError'));
    await focusError();
  } finally {
    loading.value = false;
  }
}

function parentName(menu: IAMMenu) {
  const parentId = menu.parentId?.trim();
  if (!parentId) return String($t('page.iam.menuRoot'));
  return (
    menus.value.find((candidate) => candidate.id === parentId)?.name ?? parentId
  );
}

onMounted(loadMenus);
</script>

<template>
  <main
    class="iam-menus-page"
    :aria-busy="loading"
    aria-labelledby="iam-menus-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
        <h1 id="iam-menus-title">{{ $t('page.iam.menus') }}</h1>
        <p class="description">{{ $t('page.iam.menusDescription') }}</p>
      </div>
      <span class="scope-chip">{{ $t('page.iam.readOnly') }}</span>
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
      {{ loading ? $t('page.iam.menusLoading') : '' }}
    </p>

    <section class="table-card" aria-labelledby="iam-menus-table-title">
      <div class="table-heading">
        <h2 id="iam-menus-table-title">{{ $t('page.iam.menus') }}</h2>
        <span class="result-count">{{ menuRows.length }}</span>
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
              <th scope="col">{{ $t('page.iam.menuPath') }}</th>
              <th scope="col">{{ $t('page.iam.menuParent') }}</th>
              <th scope="col">{{ $t('page.iam.menuVisible') }}</th>
              <th scope="col">{{ $t('page.iam.menuActive') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td class="table-state" colspan="6">
                {{ $t('page.iam.menusLoading') }}
              </td>
            </tr>
            <tr v-else-if="menuRows.length === 0">
              <td class="table-state" colspan="6">
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
                  {{ row.menu.name }}
                </span>
              </td>
              <td>
                <code>{{ row.menu.path || '—' }}</code>
              </td>
              <td>{{ parentName(row.menu) }}</td>
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
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </main>
</template>

<style scoped>
.iam-menus-page {
  max-width: 1320px;
  padding: 24px;
  margin: 0 auto;
  color: hsl(var(--foreground));
}

.page-heading,
.table-heading {
  display: flex;
  align-items: center;
}

.page-heading {
  gap: 24px;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
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
  max-width: 760px;
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

.feedback {
  padding: 10px 12px;
  margin: 12px 0;
  color: hsl(0 65% 36%);
  background: hsl(0 75% 55% / 0.1);
  border-radius: 8px;
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
  min-width: 820px;
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

@media (max-width: 720px) {
  .iam-menus-page {
    padding: 16px;
  }

  .page-heading {
    display: grid;
  }
}
</style>
