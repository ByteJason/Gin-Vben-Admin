<script setup lang="ts">
import type { ImageCaptchaChallenge, ImageCaptchaProps } from '../types';

import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import { $t } from '@vben/locales';

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
const remainingSeconds = ref(0);
let expiryTimer: ReturnType<typeof setInterval> | undefined;

const expiresInLabel = computed(() => {
  if (!challenge.value) return '';
  if (remainingSeconds.value <= 0) {
    return String($t('authentication.captchaExpired'));
  }
  return String(
    $t('authentication.captchaExpiresIn', {
      seconds: remainingSeconds.value,
    }),
  );
});

function clearExpiryTimer() {
  if (expiryTimer) {
    clearInterval(expiryTimer);
    expiryTimer = undefined;
  }
}

function startExpiryTimer(seconds: number) {
  clearExpiryTimer();
  remainingSeconds.value = Math.max(0, Math.ceil(seconds));
  if (remainingSeconds.value <= 0) return;

  expiryTimer = setInterval(() => {
    if (remainingSeconds.value <= 1) {
      remainingSeconds.value = 0;
      clearExpiryTimer();
      return;
    }
    remainingSeconds.value -= 1;
  }, 1000);
}

async function refresh() {
  const version = ++requestVersion.value;
  loading.value = true;
  failed.value = false;
  clearExpiryTimer();
  remainingSeconds.value = 0;
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
    startExpiryTimer(nextChallenge.expiresIn);
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

onBeforeUnmount(clearExpiryTimer);
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
      :disabled="disabled || loading || !challenge || remainingSeconds <= 0"
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
