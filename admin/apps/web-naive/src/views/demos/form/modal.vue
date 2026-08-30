<script lang="ts" setup>
import { useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { useVbenForm } from '#/adapter/form';

defineOptions({
  name: 'FormModelDemo',
});

interface FormModalData {
  values?: Record<string, unknown>;
}

const [Form, formApi] = useVbenForm({
  schema: [
    {
      component: 'Input',
      componentProps: {
        placeholder: $t('demos.pleaseEnter'),
      },
      fieldName: 'field1',
      label: $t('demos.field1'),
      rules: 'required',
    },
    {
      component: 'Input',
      componentProps: {
        placeholder: $t('demos.pleaseEnter'),
      },
      fieldName: 'field2',
      label: $t('demos.field2'),
      rules: 'required',
    },
    {
      component: 'Select',
      componentProps: {
        options: [
          { label: $t('demos.option1'), value: '1' },
          { label: $t('demos.option2'), value: '2' },
        ],
        placeholder: $t('demos.pleaseEnter'),
      },
      fieldName: 'field3',
      label: $t('demos.field3'),
      rules: 'required',
    },
  ],
  showDefaultActions: false,
});

const [Modal, modalApi] = useVbenModal<FormModalData>({
  fullscreenButton: false,
  onCancel() {
    modalApi.close();
  },
  onConfirm: async () => {
    await formApi.validateAndSubmit();
    // modalApi.close();
  },
  onOpenChange(isOpen: boolean) {
    if (isOpen) {
      const data = modalApi.getData();
      if (data?.values) {
        formApi.setValues(data.values);
      }
    }
  },
  title: $t('demos.embeddedFormExample'),
});

defineExpose({ modalApi });
</script>
<template>
  <Modal>
    <Form />
  </Modal>
</template>
