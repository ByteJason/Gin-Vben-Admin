<script setup lang="ts">
import { computed } from 'vue';

import { ProfileSecuritySetting } from '@vben/common-ui';
import { $t } from '@vben/locales';

const formSchema = computed(() => {
  return [
    {
      value: true,
      fieldName: 'accountPassword',
      label: $t('profile.accountPassword'),
      description: $t('profile.passwordStrength'),
    },
    {
      value: true,
      fieldName: 'securityPhone',
      label: $t('profile.securityPhone'),
      description: $t('profile.boundPhone'),
    },
    {
      value: true,
      fieldName: 'securityQuestion',
      label: $t('profile.securityQuestion'),
      description: $t('profile.securityQuestionUnbound'),
    },
    {
      value: true,
      fieldName: 'securityEmail',
      label: $t('profile.securityEmail'),
      description: $t('profile.boundEmail'),
    },
    {
      value: false,
      fieldName: 'securityMfa',
      label: $t('profile.securityMfa'),
      description: $t('profile.securityMfaUnbound'),
    },
  ];
});
</script>
<template>
  <ProfileSecuritySetting :form-schema="formSchema" />
</template>
