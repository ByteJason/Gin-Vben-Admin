<script lang="ts" setup>
import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { NButton, NCard, useMessage } from 'naive-ui';

import { useVbenForm } from '#/adapter/form';
import { getAllMenusApi } from '#/api';

import modalDemo from './modal.vue';

const message = useMessage();

const [Form, formApi] = useVbenForm({
  commonConfig: {
    // 所有表单项
    componentProps: {
      class: 'w-full',
    },
  },
  layout: 'vertical',
  // 大屏一行显示3个，中屏一行显示2个，小屏一行显示1个
  wrapperClass: 'grid-cols-1 md:grid-cols-2 lg:grid-cols-3',
  handleSubmit: (values) => {
    message.success($t('demos.formData', { data: JSON.stringify(values) }));
  },
  schema: [
    {
      // 组件需要在 #/adapter.ts内注册，并加上类型
      component: 'ApiSelect',
      // 对应组件的参数
      componentProps: {
        // 菜单接口转options格式
        afterFetch: (data: { name: string; path: string }[]) => {
          return data.map((item: any) => ({
            label: item.name,
            value: item.path,
          }));
        },
        // 菜单接口
        api: getAllMenusApi,
      },
      // 字段名
      fieldName: 'api',
      // 界面显示的label
      label: 'ApiSelect',
      rules: 'required',
    },
    {
      component: 'ApiTreeSelect',
      // 对应组件的参数
      componentProps: {
        // 菜单接口
        api: getAllMenusApi,
        childrenField: 'children',
        // 菜单接口转options格式
        labelField: 'name',
        valueField: 'path',
      },
      // 字段名
      fieldName: 'apiTree',
      // 界面显示的label
      label: 'ApiTreeSelect',
      rules: 'required',
    },
    {
      component: 'Input',
      fieldName: 'string',
      label: 'String',
      rules: 'required',
    },
    {
      component: 'InputNumber',
      fieldName: 'number',
      label: 'Number',
      rules: 'required',
    },
    {
      component: 'RadioGroup',
      fieldName: 'radio',
      label: 'Radio',
      componentProps: {
        options: [
          { value: 'A', label: 'A' },
          { value: 'B', label: 'B' },
          { value: 'C', label: 'C' },
          { value: 'D', label: 'D' },
          { value: 'E', label: 'E' },
        ],
      },
      rules: 'selectRequired',
    },
    {
      component: 'RadioGroup',
      fieldName: 'radioButton',
      label: 'RadioButton',
      componentProps: {
        isButton: true,
        class: 'flex flex-wrap', // 如果选项过多，可以添加class来自动折叠
        options: [
          { value: 'A', label: $t('demos.optionPrefix', { value: 'A' }) },
          { value: 'B', label: $t('demos.optionPrefix', { value: 'B' }) },
          { value: 'C', label: $t('demos.optionPrefix', { value: 'C' }) },
          { value: 'D', label: $t('demos.optionPrefix', { value: 'D' }) },
          { value: 'E', label: $t('demos.optionPrefix', { value: 'E' }) },
        ],
      },
      rules: 'selectRequired',
    },
    {
      component: 'CheckboxGroup',
      fieldName: 'checkbox',
      label: 'Checkbox',
      componentProps: {
        options: [
          { value: 'A', label: $t('demos.optionPrefix', { value: 'A' }) },
          { value: 'B', label: $t('demos.optionPrefix', { value: 'B' }) },
          { value: 'C', label: $t('demos.optionPrefix', { value: 'C' }) },
        ],
      },
      rules: 'selectRequired',
    },
    {
      component: 'DatePicker',
      fieldName: 'date',
      label: 'Date',
      rules: 'required',
    },
    {
      component: 'Input',
      fieldName: 'textArea',
      label: 'TextArea',
      componentProps: {
        type: 'textarea',
      },
      rules: 'required',
    },
    {
      component: 'Input',
      fieldName: 'collapsibleTextArea',
      label: $t('demos.collapsibleTextArea'),
      componentProps: {
        type: 'textarea',
      },
      collapsible: true,
    },
  ],
});

function setFormValues() {
  formApi.setValues({
    string: 'string',
    number: 123,
    radio: 'B',
    radioButton: 'C',
    checkbox: ['A', 'C'],
    date: Date.now(),
  });
}

const [Modal, modalApi] = useVbenModal({
  connectedComponent: modalDemo,
});
</script>
<template>
  <Page
    :description="$t('demos.formAdapterDescription')"
    :title="$t('demos.formDemo')"
  >
    <NCard :title="$t('demos.baseForm')" header-extra-class="gap-4">
      <template #header-extra>
        <NButton type="primary" @click="setFormValues">{{
          $t('demos.setFormValues')
        }}</NButton>
        <NButton type="primary" @click="modalApi.open()" class="ml-2">
          {{ $t('demos.openDialog') }}
        </NButton>
      </template>
      <Form />
    </NCard>
    <Modal />
  </Page>
</template>
