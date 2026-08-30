<script lang="ts" setup>
import { ref } from 'vue';

import { Page } from '@vben/common-ui';
import { $t } from '@vben/locales';

import {
  ElButton,
  ElCard,
  ElMessage,
  ElNotification,
  ElSegmented,
  ElSpace,
  ElTable,
} from 'element-plus';

type NotificationType = 'error' | 'info' | 'success' | 'warning';

function info() {
  ElMessage.info($t('demos.messageInfo'));
}

function error() {
  ElMessage.error({
    duration: 2500,
    message: $t('demos.messageError'),
  });
}

function warning() {
  ElMessage.warning($t('demos.messageInfo'));
}
function success() {
  ElMessage.success($t('demos.messageSuccess'));
}

function notify(type: NotificationType) {
  ElNotification({
    duration: 2500,
    message: $t('demos.notificationMessage'),
    type,
  });
}
const tableData = [
  { prop1: '1', prop2: 'A' },
  { prop1: '2', prop2: 'B' },
  { prop1: '3', prop2: 'C' },
  { prop1: '4', prop2: 'D' },
  { prop1: '5', prop2: 'E' },
  { prop1: '6', prop2: 'F' },
];

const segmentedValue = ref('Mon');

const segmentedOptions = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
</script>

<template>
  <Page :description="$t('demos.description')" :title="$t('demos.elementDemo')">
    <div class="flex flex-wrap gap-5">
      <ElCard class="mb-5 w-auto">
        <template #header>{{ $t('demos.buttons') }}</template>
        <ElSpace>
          <ElButton text>{{ $t('demos.text') }}</ElButton>
          <ElButton>{{ $t('demos.default') }}</ElButton>
          <ElButton type="primary">{{ $t('demos.primary') }}</ElButton>
          <ElButton type="info">{{ $t('demos.info') }}</ElButton>
          <ElButton type="success">{{ $t('demos.success') }}</ElButton>
          <ElButton type="warning">{{ $t('demos.warning') }}</ElButton>
          <ElButton type="danger">{{ $t('demos.error') }}</ElButton>
        </ElSpace>
      </ElCard>
      <ElCard class="mb-5 w-80">
        <template #header>{{ $t('demos.message') }}</template>
        <ElSpace>
          <ElButton type="info" @click="info">{{ $t('demos.info') }}</ElButton>
          <ElButton type="danger" @click="error">{{
            $t('demos.error')
          }}</ElButton>
          <ElButton type="warning" @click="warning">{{
            $t('demos.warning')
          }}</ElButton>
          <ElButton type="success" @click="success">{{
            $t('demos.success')
          }}</ElButton>
        </ElSpace>
      </ElCard>
      <ElCard class="mb-5 w-80">
        <template #header>{{ $t('demos.notification') }}</template>
        <ElSpace>
          <ElButton type="info" @click="notify('info')">{{
            $t('demos.info')
          }}</ElButton>
          <ElButton type="danger" @click="notify('error')">{{
            $t('demos.error')
          }}</ElButton>
          <ElButton type="warning" @click="notify('warning')">{{
            $t('demos.warning')
          }}</ElButton>
          <ElButton type="success" @click="notify('success')">{{
            $t('demos.success')
          }}</ElButton>
        </ElSpace>
      </ElCard>
      <ElCard class="mb-5 w-auto">
        <template #header>{{ $t('demos.segmented') }}</template>
        <ElSegmented
          v-model="segmentedValue"
          :options="segmentedOptions"
          size="large"
        />
      </ElCard>
      <ElCard class="mb-5 w-80">
        <template #header>{{ $t('demos.vLoading') }}</template>
        <div class="flex-center size-72" v-loading="true">
          {{ $t('demos.loadingContent') }}
        </div>
      </ElCard>
      <ElCard class="mb-5 w-80">
        <ElTable :data="tableData" stripe>
          <ElTable.TableColumn :label="$t('demos.testColumn1')" prop="prop1" />
          <ElTable.TableColumn :label="$t('demos.testColumn2')" prop="prop2" />
        </ElTable>
      </ElCard>
    </div>
  </Page>
</template>
