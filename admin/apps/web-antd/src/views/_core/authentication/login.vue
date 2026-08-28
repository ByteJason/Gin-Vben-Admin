<script lang="ts" setup>
import type { VbenFormSchema } from '@vben/common-ui';
import type { Recordable } from '@vben/types';

import { computed, markRaw, ref } from 'vue';

import { AuthenticationLogin, ImageCaptcha, z } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { getCaptchaApi } from '#/api';
import { useAuthStore } from '#/store';

defineOptions({ name: 'Login' });

const authStore = useAuthStore();
const captchaId = ref('');

function handleCaptchaId(id: string) {
  captchaId.value = id;
}

async function handleSubmit(values: Recordable<any>) {
  return authStore.authLogin({
    ...values,
    captchaId: captchaId.value,
  });
}

const formSchema = computed((): VbenFormSchema[] => {
  return [
    {
      component: 'VbenInput',
      componentProps: {
        autocomplete: 'username',
        autocapitalize: 'none',
        placeholder: $t('authentication.identifierTip'),
        spellcheck: false,
      },
      fieldName: 'identifier',
      label: $t('authentication.identifier'),
      rules: z.string().min(1, { message: $t('authentication.identifierTip') }),
    },
    {
      component: 'VbenInputPassword',
      componentProps: {
        autocomplete: 'current-password',
        placeholder: $t('authentication.password'),
      },
      fieldName: 'password',
      label: $t('authentication.password'),
      rules: z.string().min(1, { message: $t('authentication.passwordTip') }),
    },
    {
      component: markRaw(ImageCaptcha),
      componentProps: {
        alt: $t('authentication.captchaImageAlt'),
        inputPlaceholder: $t('authentication.captchaTip'),
        onChallengeId: handleCaptchaId,
        refreshText: $t('authentication.captchaRefresh'),
        request: getCaptchaApi,
      },
      fieldName: 'captcha',
      // The server decides whether the challenge is required (risk/default-off policy).
      rules: z.string().optional(),
    },
  ];
});
</script>

<template>
  <AuthenticationLogin
    :form-schema="formSchema"
    :loading="authStore.loginLoading"
    :show-code-login="false"
    :show-forget-password="false"
    :show-qrcode-login="false"
    :show-register="false"
    :show-third-party-login="false"
    @submit="handleSubmit"
  />
  <p
    v-if="authStore.loginError"
    aria-live="assertive"
    class="login-error"
    data-testid="login-error"
    role="alert"
  >
    {{ authStore.loginError }}
  </p>
  <p
    v-if="authStore.loginSuccess"
    aria-live="polite"
    class="login-success"
    data-testid="login-success"
    role="status"
  >
    {{ $t('authentication.loginSuccess') }}
  </p>
</template>
