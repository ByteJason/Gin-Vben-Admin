<script setup lang="ts">
import { computed } from 'vue';

import { ProfileNotificationSetting } from '@vben/common-ui';
import { $t } from '@vben/locales';

const formSchema = computed(() => {
  return [
    {
      value: true,
      fieldName: 'accountPassword',
      label: $t('profile.accountPassword'),
      description: $t('profile.notificationDescription'),
    },
    {
      value: true,
      fieldName: 'systemMessage',
      label: $t('profile.systemMessage'),
      description: $t('profile.notificationDescription'),
    },
    {
      value: true,
      fieldName: 'todoTask',
      label: $t('profile.todoTask'),
      description: $t('profile.notificationDescription'),
    },
  ];
});
</script>
<template>
  <ProfileNotificationSetting :form-schema="formSchema" />
</template>
