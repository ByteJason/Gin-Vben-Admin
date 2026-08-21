<script setup lang="ts">
import type { ImageCaptchaChallenge, ImageCaptchaProps } from '../types';

import { computed, onMounted, ref } from 'vue';

const props = withDefaults(defineProps<ImageCaptchaProps>(), {
  alt: 'Captcha image',
  disabled: false,
  inputPlaceholder: 'Enter the characters in the image',
  onChallengeId: undefined,
  refreshText: 'Refresh captcha',
});

const modelValue = defineModel<string>({ default: '' });
const challenge = ref<ImageCaptchaChallenge | null>(null);
const loading = ref(false);
const failed = ref(false);
const requestVersion = ref(0);

const expiresInLabel = computed(() => {
  const seconds = challenge.value?.expiresIn;
  return typeof seconds === 'number' && seconds > 0 ? `${seconds}s` : '';
});

async function refresh() {
  const version = ++requestVersion.value;
  loading.value = true;
  failed.value = false;
  challenge.value = null;
  modelValue.value = '';
  props.onChallengeId?.('');

  try {
    const nextChallenge = await props.request();
    if (version !== requestVersion.value) {
      return;
    }
    if (!nextChallenge?.id) {
      throw new Error('captcha challenge is missing an id');
    }
    challenge.value = nextChallenge;
    props.onChallengeId?.(nextChallenge.id);
  } catch {
    if (version === requestVersion.value) {
      failed.value = true;
      props.onChallengeId?.('');
    }
  } finally {
    if (version === requestVersion.value) {
      loading.value = false;
    }
  }
}

onMounted(() => {
  void refresh();
});
</script>

<template>
  <div
    aria-live="polite"
    class="flex w-full flex-col gap-2"
    data-testid="image-captcha"
  >
    <div class="flex items-center gap-2">
      <button
        :aria-label="alt"
        class="border-input bg-background focus-visible:ring-ring h-14 w-40 overflow-hidden rounded-md border focus-visible:outline-none focus-visible:ring-2"
        :disabled="disabled || loading"
        type="button"
        @click="refresh"
      >
        <img
          v-if="challenge?.payload"
          :alt="alt"
          class="h-full w-full object-cover"
          draggable="false"
          :src="challenge.payload"
        />
        <span v-else class="text-muted-foreground px-2 text-xs">
          {{ failed ? refreshText : '…' }}
        </span>
      </button>
      <button
        class="text-foreground text-sm underline-offset-4 hover:underline disabled:cursor-not-allowed disabled:opacity-50"
        :disabled="disabled || loading"
        type="button"
        @click="refresh"
      >
        {{ refreshText }}
      </button>
    </div>
    <input
      v-model="modelValue"
      :aria-label="inputPlaceholder"
      autocomplete="off"
      class="border-input bg-background focus-visible:ring-ring h-9 w-full rounded-md border px-3 text-sm focus-visible:outline-none focus-visible:ring-2"
      data-testid="image-captcha-input"
      :disabled="disabled || loading || !challenge"
      :name="name"
      :placeholder="inputPlaceholder"
      spellcheck="false"
      type="text"
    />
    <span v-if="expiresInLabel" class="text-muted-foreground text-xs">
      {{ expiresInLabel }}
    </span>
  </div>
</template>
