<script lang="ts" setup>
import { Page } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { Button, Card, message, notification, Space } from 'ant-design-vue';

type NotificationType = 'error' | 'info' | 'success' | 'warning';

function info() {
  message.info($t('demos.messageInfo'));
}

function error() {
  message.error({
    content: $t('demos.messageError'),
    // ant-design-vue 的 message.duration 单位是「秒」, 不是毫秒
    duration: 2.5,
  });
}

function warning() {
  message.warning($t('demos.messageInfo'));
}
function success() {
  message.success($t('demos.messageSuccess'));
}

function notify(type: NotificationType) {
  notification[type]({
    // ant-design-vue 的 notification.duration 单位是「秒」, 不是毫秒
    duration: 2.5,
    message: $t('demos.notificationMessage'),
    type,
  });
}
</script>

<template>
  <Page
    :description="$t('demos.description')"
    :title="$t('demos.componentDemo')"
  >
    <Card class="mb-5" :title="$t('demos.buttons')">
      <Space>
        <Button>{{ $t('demos.default') }}</Button>
        <Button type="primary">{{ $t('demos.primary') }}</Button>
        <Button>{{ $t('demos.info') }}</Button>
        <Button danger>{{ $t('demos.error') }}</Button>
      </Space>
    </Card>
    <Card class="mb-5" :title="$t('demos.message')">
      <Space>
        <Button @click="info">{{ $t('demos.info') }}</Button>
        <Button danger @click="error">{{ $t('demos.error') }}</Button>
        <Button @click="warning">{{ $t('demos.warning') }}</Button>
        <Button @click="success">{{ $t('demos.success') }}</Button>
      </Space>
    </Card>

    <Card class="mb-5" :title="$t('demos.notification')">
      <Space>
        <Button @click="notify('info')">{{ $t('demos.info') }}</Button>
        <Button danger @click="notify('error')">{{ $t('demos.error') }}</Button>
        <Button @click="notify('warning')">{{ $t('demos.warning') }}</Button>
        <Button @click="notify('success')">{{ $t('demos.success') }}</Button>
      </Space>
    </Card>
  </Page>
</template>
