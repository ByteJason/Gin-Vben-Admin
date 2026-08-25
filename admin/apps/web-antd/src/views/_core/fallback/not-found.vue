<script lang="ts" setup>
import { computed } from 'vue';

import { Fallback } from '@vben/common-ui';
import { preferences } from '@vben/preferences';
import { useUserStore } from '@vben/stores';

defineOptions({ name: 'Fallback404' });

const userStore = useUserStore();
const homePath = computed(
  () => userStore.userInfo?.homePath || preferences.app.defaultHomePath,
);
</script>

<template>
  <Fallback :home-path="homePath" status="404" />
</template>
