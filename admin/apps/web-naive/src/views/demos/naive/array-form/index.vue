<script lang="ts" setup>
import { Page } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { NButton, NCard, useMessage } from 'naive-ui';

import { useVbenForm } from '#/adapter/form';

const message = useMessage();

const [Form, formApi] = useVbenForm({
  layout: 'vertical',
  wrapperClass: 'grid-cols-1',
  handleSubmit: (values) => {
    message.success(
      $t('demos.submittedWithData', { data: JSON.stringify(values) }),
    );
  },
  schema: [
    {
      component: 'Input',
      fieldName: 'projectName',
      label: $t('demos.projectName'),
      rules: 'required',
    },
    {
      component: 'VbenFormFieldArray',
      fieldName: 'members',
      label: $t('demos.projectMembers'),
      // 初始化为空数组，供数组编辑器使用
      defaultValue: [],
      componentProps: {
        min: 1,
        max: 5,
        createRow: () => ({
          name: null,
          age: null,
          role: null,
          joinDate: null,
          active: true,
        }),
        // 每一列就是一个子字段，复用 vbenForm 的所有编辑组件
        schema: [
          {
            component: 'Input',
            fieldName: 'name',
            label: $t('demos.memberName'),
            rules: 'required',
            componentProps: { placeholder: $t('demos.memberName') },
          },
          {
            component: 'InputNumber',
            fieldName: 'age',
            label: $t('demos.memberAge'),
            componentProps: { min: 0, max: 150 },
          },
          {
            component: 'Select',
            fieldName: 'role',
            label: $t('demos.memberRole'),
            rules: 'selectRequired',
            componentProps: {
              placeholder: $t('demos.pleaseSelect'),
              options: [
                { label: $t('demos.frontend'), value: 'fe' },
                { label: $t('demos.backend'), value: 'be' },
                { label: $t('demos.testing'), value: 'qa' },
                { label: $t('demos.product'), value: 'pm' },
              ],
            },
          },
          {
            component: 'DatePicker',
            fieldName: 'joinDate',
            label: $t('demos.joinDate'),
          },
          {
            component: 'Switch',
            fieldName: 'active',
            label: $t('demos.onDuty'),
          },
        ],
      },
    },
  ],
});

function setFormValues() {
  formApi.setValues({
    projectName: 'Gin Vben Admin',
    members: [
      {
        name: $t('demos.memberOne'),
        age: 28,
        role: 'fe',
        joinDate: Date.now(),
        active: true,
      },
      {
        name: $t('demos.memberTwo'),
        age: 32,
        role: 'be',
        joinDate: Date.now(),
        active: false,
      },
    ],
  });
}

async function getFormValues() {
  const values = await formApi.getValues();
  message.info(JSON.stringify(values));
}
</script>

<template>
  <Page
    :description="$t('demos.arrayFormDescription')"
    :title="$t('demos.arrayForm')"
  >
    <NCard :title="$t('demos.arrayEditor')">
      <template #header-extra>
        <NButton class="mr-2" @click="setFormValues">{{
          $t('demos.setFormValues')
        }}</NButton>
        <NButton class="mr-2" @click="getFormValues">{{
          $t('demos.getFormValues')
        }}</NButton>
        <NButton type="primary" @click="formApi.submit()">{{
          $t('demos.submitValidate')
        }}</NButton>
      </template>
      <Form />
    </NCard>
  </Page>
</template>
