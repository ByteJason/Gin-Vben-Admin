<script lang="ts" setup>
import type { NotificationType } from 'naive-ui';

import { Page } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { NButton, NCard, NSpace, useMessage, useNotification } from 'naive-ui';

const notification = useNotification();

const message = useMessage();
function error() {
  message.error($t('demos.messageError'));
}

function warning() {
  message.warning($t('demos.messageInfo'));
}
function success() {
  message.success($t('demos.messageSuccess'));
}
function loading() {
  message.loading($t('demos.messageLoading'));
}

function notify(type: NotificationType) {
  notification[type]({
    content: $t('demos.notificationMessage'),
    duration: 2500,
    keepAliveOnHover: true,
    meta: $t('demos.notificationMeta'),
  });
}
</script>

<template>
  <Page :description="$t('demos.description')" :title="$t('demos.naiveDemo')">
    <NCard class="mb-5" :title="$t('demos.buttons')">
      <NSpace>
        <NButton>{{ $t('demos.default') }}</NButton>
        <NButton type="tertiary">{{ $t('demos.tertiary') }}</NButton>
        <NButton type="primary">{{ $t('demos.primary') }}</NButton>
        <NButton type="info">{{ $t('demos.info') }}</NButton>
        <NButton type="success">{{ $t('demos.success') }}</NButton>
        <NButton type="warning">{{ $t('demos.warning') }}</NButton>
        <NButton type="error">{{ $t('demos.error') }}</NButton>
      </NSpace>
    </NCard>

    <NCard class="mb-5" :title="$t('demos.message')">
      <NSpace>
        <NButton type="error" @click="error">{{ $t('demos.error') }}</NButton>
        <NButton type="warning" @click="warning">{{
          $t('demos.warning')
        }}</NButton>
        <NButton type="success" @click="success">{{
          $t('demos.success')
        }}</NButton>
        <NButton type="primary" @click="loading">{{
          $t('demos.loading')
        }}</NButton>
      </NSpace>
    </NCard>

    <NCard class="mb-5" :title="$t('demos.notification')">
      <NSpace>
        <NButton type="error" @click="notify('error')">{{
          $t('demos.error')
        }}</NButton>
        <NButton type="warning" @click="notify('warning')">{{
          $t('demos.warning')
        }}</NButton>
        <NButton type="success" @click="notify('success')">{{
          $t('demos.success')
        }}</NButton>
        <NButton type="primary" @click="notify('info')">{{
          $t('demos.loading')
        }}</NButton>
      </NSpace>
    </NCard>
  </Page>
</template>
