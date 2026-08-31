<script setup lang="ts">
interface Props {
  busy?: boolean;
  labelledby?: string;
}

defineOptions({ name: 'ManagementPage' });
defineProps<Props>();
</script>

<template>
  <div
    class="management-page"
    :aria-busy="busy ? 'true' : undefined"
    :aria-labelledby="labelledby"
  >
    <slot></slot>
  </div>
</template>

<style scoped>
.management-page {
  --management-page-gutter: clamp(1rem, calc(0.4rem + 1.2vw), 2rem);

  box-sizing: border-box;
  display: block;
  inline-size: 100%;
  min-inline-size: 0;
  min-block-size: 100%;
  padding: var(--management-page-gutter);
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.management-page :deep(.page-heading) {
  min-inline-size: 0;
  padding: clamp(0.875rem, calc(0.65rem + 0.45vw), 1.25rem)
    var(--management-page-gutter);
  margin-block-start: calc(var(--management-page-gutter) * -1);
  margin-inline: calc(var(--management-page-gutter) * -1);
  background: hsl(var(--background));
  border-block-end: 1px solid hsl(var(--border));
}

.management-page :deep(.page-heading h1) {
  line-height: 1.2;
  text-wrap: balance;
}

@media (width <= 480px) {
  .management-page {
    --management-page-gutter: 1rem;
  }
}
</style>
