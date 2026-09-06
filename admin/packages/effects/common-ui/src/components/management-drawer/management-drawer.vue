<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue';

import { useVbenDrawer } from '@vben-core/popup-ui';

import { useFocusTrap } from '@vueuse/integrations/useFocusTrap';

const props = withDefaults(
  defineProps<{
    busy?: boolean;
    open: boolean;
    title: string;
    wide?: boolean;
  }>(),
  { busy: false, wide: false },
);
const emit = defineEmits<{ close: [] }>();
const content = ref<HTMLElement>();
const panel = ref<HTMLElement>();
let returnFocus: HTMLElement | null = null;

// The base drawer owns positioning, transitions and scroll locking. Resource
// editors add a focus boundary because the base Sheet is intentionally non-modal.
const { activate, deactivate } = useFocusTrap(panel, {
  allowOutsideClick: true,
  escapeDeactivates: false,
  fallbackFocus: () => panel.value!,
  initialFocus: () =>
    content.value?.querySelector<HTMLElement>(
      'input:not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled])',
    ) ?? panel.value!,
  returnFocusOnDeactivate: false,
});

function releaseFocus() {
  deactivate();
  const target = returnFocus;
  returnFocus = null;
  if (target?.isConnected) target.focus({ preventScroll: true });
}

const [Drawer, drawerApi] = useVbenDrawer({
  closeOnClickModal: false,
  closeOnPressEscape: true,
  destroyOnClose: true,
  openAutoFocus: false,
  placement: 'right',
  onBeforeClose: () => !props.open || !props.busy,
  onClosed: releaseFocus,
  onOpenChange(open) {
    if (!open && props.open) emit('close');
  },
});

watch(
  () => props.open,
  async (open) => {
    if (!open) {
      deactivate();
      await drawerApi.close();
      return;
    }
    returnFocus =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    drawerApi.open();
    await nextTick();
    if (!props.open) return;
    panel.value =
      content.value?.closest<HTMLElement>('[role="dialog"]') ?? undefined;
    panel.value?.setAttribute('aria-modal', 'true');
    await nextTick();
    if (props.open && panel.value) activate();
  },
  { immediate: true },
);

onBeforeUnmount(releaseFocus);
</script>

<template>
  <Drawer
    :class="['management-drawer', { 'management-drawer-wide': wide }]"
    :title="title"
    :submitting="busy"
    :footer="Boolean($slots.footer)"
    :closable="false"
    content-class="management-drawer-content"
    header-class="management-drawer-header"
    footer-class="management-drawer-footer"
  >
    <template #extra>
      <button
        class="management-drawer-close"
        type="button"
        :aria-label="title + ' — ×'"
        :disabled="busy"
        @click="drawerApi.close()"
      >
        <svg aria-hidden="true" viewBox="0 0 24 24" width="20" height="20">
          <path
            d="m6 6 12 12M18 6 6 18"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          />
        </svg>
      </button>
    </template>
    <div ref="content" class="management-drawer-body" :aria-busy="busy">
      <slot></slot>
    </div>
    <template v-if="$slots.footer" #footer>
      <slot name="footer"></slot>
    </template>
  </Drawer>
</template>

<style>
.management-drawer {
  width: min(640px, 100vw) !important;
  max-width: 100vw;
  height: 100vh;
  height: 100dvh;
}

.management-drawer-wide {
  width: min(800px, 100vw) !important;
}

.management-drawer-header,
.management-drawer-footer {
  flex-shrink: 0;
  gap: 12px;
}

.management-drawer-header {
  padding-top: max(16px, env(safe-area-inset-top));
}

.management-drawer-header > div:first-child {
  min-width: 0;
  overflow-wrap: anywhere;
}

.management-drawer-content {
  min-height: 0;
  overscroll-behavior: contain;
  padding: 24px;
}

.management-drawer-body {
  min-width: 0;
  overflow-wrap: anywhere;
}

/* Resource pages used to provide their own centred dialog/card chrome. Keep
 * their fields and validation, but let the shared drawer own the surface. */
.management-drawer .user-dialog,
.management-drawer .role-dialog,
.management-drawer .permission-dialog,
.management-drawer .data-scope-dialog,
.management-drawer .menu-dialog,
.management-drawer .editor-card,
.management-drawer .panel {
  width: auto;
  max-width: none;
  max-height: none;
  margin: 0;
  padding: 0;
  overflow: visible;
  background: transparent;
  border: 0;
  border-radius: 0;
  box-shadow: none;
}

.management-drawer .dialog-heading {
  display: none;
}

.management-drawer-footer {
  flex-wrap: wrap;
  padding: 16px 24px max(16px, env(safe-area-inset-bottom));
}

.management-drawer-close {
  display: grid;
  width: 44px;
  height: 44px;
  flex-shrink: 0;
  place-items: center;
  border-radius: 6px;
}

.management-drawer button:focus-visible,
.management-drawer input:focus-visible,
.management-drawer textarea:focus-visible,
.management-drawer select:focus-visible {
  outline: 2px solid hsl(var(--primary));
  outline-offset: 2px;
}

.management-drawer button:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

@media (max-width: 767px) {
  .management-drawer,
  .management-drawer-wide {
    width: 100vw !important;
  }

  .management-drawer-content,
  .management-drawer-footer {
    padding-right: max(16px, env(safe-area-inset-right));
    padding-left: max(16px, env(safe-area-inset-left));
  }

  .management-drawer input:not([type='checkbox'], [type='radio']),
  .management-drawer select,
  .management-drawer button {
    min-height: 44px;
  }

  .management-drawer input,
  .management-drawer textarea,
  .management-drawer select {
    font-size: 16px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .management-drawer {
    animation-duration: 1ms !important;
    transition-duration: 1ms !important;
  }
}
</style>
