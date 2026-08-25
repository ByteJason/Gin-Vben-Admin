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
  box-sizing: border-box;
  inline-size: 100%;
  min-inline-size: 0;
  min-block-size: 100%;
  padding: clamp(1rem, calc(0.4rem + 1.2vw), 2rem);
  overflow-wrap: anywhere;
}

@media (width <= 480px) {
  .management-page {
    padding: 1rem;
  }
}
</style>
