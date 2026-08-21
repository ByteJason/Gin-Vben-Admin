<script setup lang="ts">
import { nextTick, onMounted, reactive, ref } from 'vue';

import { getSettingApi, updateSettingApi } from '#/api/core/settings';
import { $t } from '#/locales';

const settingKeys = {
  metricsEnabled: 'observability.metrics.enabled',
  metricsEndpoint: 'observability.metrics.endpoint',
  otlpApiKey: 'observability.otlp.api_key',
  sampleRate: 'observability.tracing.sample_rate',
  tlsVerify: 'observability.tracing.tls_verify',
  tracingEnabled: 'observability.tracing.enabled',
  tracingEndpoint: 'observability.tracing.endpoint',
  tracingProtocol: 'observability.tracing.protocol',
} as const;

type SettingKey = (typeof settingKeys)[keyof typeof settingKeys];

const state = reactive({
  metricsEnabled: false,
  metricsEndpoint: '',
  otlpApiKey: '',
  sampleRate: 0,
  tlsVerify: true,
  tracingEnabled: false,
  tracingEndpoint: '',
  tracingProtocol: 'http/protobuf',
});
const versions = reactive<Record<SettingKey, number>>(
  Object.fromEntries(
    Object.values(settingKeys).map((key) => [key, 0]),
  ) as Record<SettingKey, number>,
);
const loading = ref(true);
const saving = ref(false);
const message = ref('');
const error = ref('');
const errorSummary = ref<HTMLElement>();

function parseValue<T>(value: string, fallback: T): T {
  if (!value || value === '[REDACTED]') return fallback;
  try {
    return JSON.parse(value) as T;
  } catch {
    return fallback;
  }
}

function isHTTPURL(value: string) {
  try {
    const parsed = new URL(value);
    return parsed.protocol === 'http:' || parsed.protocol === 'https:';
  } catch {
    return false;
  }
}

async function load() {
  loading.value = true;
  error.value = '';
  try {
    const entries = await Promise.all(
      Object.values(settingKeys).map(
        async (key) => [key, await getSettingApi(key)] as const,
      ),
    );
    const values = Object.fromEntries(entries) as Record<
      SettingKey,
      Awaited<ReturnType<typeof getSettingApi>>
    >;
    for (const [key, setting] of entries) versions[key] = setting.version;
    state.metricsEnabled = parseValue(
      values[settingKeys.metricsEnabled].value,
      false,
    );
    state.metricsEndpoint = parseValue(
      values[settingKeys.metricsEndpoint].value,
      '',
    );
    state.tracingEnabled = parseValue(
      values[settingKeys.tracingEnabled].value,
      false,
    );
    state.tracingEndpoint = parseValue(
      values[settingKeys.tracingEndpoint].value,
      '',
    );
    state.tracingProtocol = parseValue(
      values[settingKeys.tracingProtocol].value,
      'http/protobuf',
    );
    state.tlsVerify = parseValue(values[settingKeys.tlsVerify].value, true);
    state.sampleRate = parseValue(values[settingKeys.sampleRate].value, 0);
  } catch {
    error.value = String($t('page.observability.loadError'));
  } finally {
    loading.value = false;
  }
}

async function update(key: SettingKey, value: unknown) {
  const setting = await updateSettingApi(key, {
    expectedVersion: versions[key],
    value,
  });
  versions[key] = setting.version;
}

async function save() {
  error.value = '';
  message.value = '';
  if (state.metricsEnabled && !isHTTPURL(state.metricsEndpoint)) {
    error.value = String($t('page.observability.metricsEndpointError'));
  } else if (state.tracingEnabled && !isHTTPURL(state.tracingEndpoint)) {
    error.value = String($t('page.observability.tracingEndpointError'));
  }
  if (error.value) {
    await nextTick();
    errorSummary.value?.focus();
    return;
  }

  saving.value = true;
  try {
    await update(settingKeys.metricsEnabled, state.metricsEnabled);
    await update(settingKeys.metricsEndpoint, state.metricsEndpoint);
    await update(settingKeys.tracingEnabled, state.tracingEnabled);
    await update(settingKeys.tracingEndpoint, state.tracingEndpoint);
    await update(settingKeys.tracingProtocol, state.tracingProtocol);
    await update(settingKeys.tlsVerify, state.tlsVerify);
    await update(settingKeys.sampleRate, Number(state.sampleRate));
    if (state.otlpApiKey.trim()) {
      await update(settingKeys.otlpApiKey, state.otlpApiKey.trim());
      state.otlpApiKey = '';
    }
    message.value = String($t('page.observability.saved'));
  } catch {
    error.value = String($t('page.observability.saveError'));
    await nextTick();
    errorSummary.value?.focus();
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <section
    class="observability-page"
    :aria-busy="loading"
    aria-labelledby="observability-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.observability.eyebrow') }}</p>
        <h1 id="observability-title">{{ $t('page.observability.title') }}</h1>
        <p class="description">{{ $t('page.observability.description') }}</p>
      </div>
      <span class="status-chip">{{ $t('page.observability.external') }}</span>
    </header>

    <p
      v-if="error"
      ref="errorSummary"
      class="feedback feedback-error"
      role="alert"
      tabindex="-1"
    >
      {{ error }}
    </p>
    <p v-if="message" class="feedback feedback-success" aria-live="polite">
      {{ message }}
    </p>
    <p v-else class="sr-status" aria-live="polite">
      {{ loading ? $t('page.observability.loading') : '' }}
    </p>

    <form class="settings-form" @submit.prevent="save">
      <fieldset class="setting-card" :disabled="loading || saving">
        <legend>{{ $t('page.observability.metricsTitle') }}</legend>
        <label class="switch-row" for="metrics-enabled">
          <span>
            <strong>{{ $t('page.observability.metricsSwitch') }}</strong>
            <small>{{ $t('page.observability.metricsHelp') }}</small>
          </span>
          <input
            id="metrics-enabled"
            v-model="state.metricsEnabled"
            type="checkbox"
          />
        </label>
        <label class="field" for="metrics-endpoint">
          <span>{{ $t('page.observability.metricsEndpoint') }}</span>
          <input
            id="metrics-endpoint"
            v-model.trim="state.metricsEndpoint"
            :disabled="!state.metricsEnabled"
            inputmode="url"
            placeholder="https://admin.example.com/metrics"
            type="url"
          />
          <small>{{ $t('page.observability.metricsEndpointHelp') }}</small>
        </label>
      </fieldset>

      <fieldset class="setting-card" :disabled="loading || saving">
        <legend>{{ $t('page.observability.tracingTitle') }}</legend>
        <label class="switch-row" for="tracing-enabled">
          <span>
            <strong>{{ $t('page.observability.tracingSwitch') }}</strong>
            <small>{{ $t('page.observability.tracingHelp') }}</small>
          </span>
          <input
            id="tracing-enabled"
            v-model="state.tracingEnabled"
            type="checkbox"
          />
        </label>
        <div class="field-grid">
          <label class="field field-wide" for="tracing-endpoint">
            <span>{{ $t('page.observability.tracingEndpoint') }}</span>
            <input
              id="tracing-endpoint"
              v-model.trim="state.tracingEndpoint"
              :disabled="!state.tracingEnabled"
              inputmode="url"
              placeholder="https://collector.example.com:4318"
              type="url"
            />
          </label>
          <label class="field" for="tracing-protocol">
            <span>{{ $t('page.observability.protocol') }}</span>
            <select
              id="tracing-protocol"
              v-model="state.tracingProtocol"
              :disabled="!state.tracingEnabled"
            >
              <option value="http/protobuf">HTTP / Protobuf</option>
              <option value="grpc">gRPC</option>
            </select>
          </label>
          <label class="field" for="sample-rate">
            <span>{{ $t('page.observability.sampleRate') }}</span>
            <input
              id="sample-rate"
              v-model.number="state.sampleRate"
              :disabled="!state.tracingEnabled"
              max="1"
              min="0"
              step="0.05"
              type="number"
            />
          </label>
          <label class="field" for="otlp-api-key">
            <span>{{ $t('page.observability.apiKey') }}</span>
            <input
              id="otlp-api-key"
              v-model="state.otlpApiKey"
              :disabled="!state.tracingEnabled"
              autocomplete="new-password"
              type="password"
            />
            <small>{{ $t('page.observability.apiKeyHelp') }}</small>
          </label>
          <label class="switch-row compact" for="tls-verify">
            <span>
              <strong>{{ $t('page.observability.tlsVerify') }}</strong>
              <small>{{ $t('page.observability.tlsHelp') }}</small>
            </span>
            <input
              id="tls-verify"
              v-model="state.tlsVerify"
              :disabled="!state.tracingEnabled"
              type="checkbox"
            />
          </label>
        </div>
      </fieldset>

      <aside class="restart-note" role="note">
        <strong>{{ $t('page.observability.restartTitle') }}</strong>
        <span>{{ $t('page.observability.restartHelp') }}</span>
      </aside>
      <div class="actions">
        <button type="button" :disabled="loading || saving" @click="load">
          {{ $t('page.observability.reload') }}
        </button>
        <button class="primary" type="submit" :disabled="loading || saving">
          {{
            saving
              ? $t('page.observability.saving')
              : $t('page.observability.save')
          }}
        </button>
      </div>
    </form>
  </section>
</template>

<style scoped>
.observability-page {
  max-width: 1080px;
  padding: 24px;
  margin: 0 auto;
  color: hsl(var(--foreground));
}

.page-heading {
  display: flex;
  gap: 24px;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
}

h1 {
  margin: 2px 0 8px;
  font-size: clamp(1.5rem, 3vw, 2rem);
  line-height: 1.2;
}

.eyebrow {
  margin: 0;
  font-size: 0.75rem;
  font-weight: 700;
  color: hsl(var(--primary));
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.description {
  max-width: 720px;
  margin: 0;
  line-height: 1.6;
  color: hsl(var(--muted-foreground));
}

.status-chip {
  flex: none;
  padding: 6px 10px;
  font-size: 0.75rem;
  font-weight: 600;
  background: hsl(var(--muted));
  border: 1px solid hsl(var(--border));
  border-radius: 999px;
}

.settings-form {
  display: grid;
  gap: 16px;
}

.setting-card {
  min-width: 0;
  padding: 18px;
  margin: 0;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 12px;
}

.setting-card legend {
  padding: 0 6px;
  font-size: 1rem;
  font-weight: 700;
}

.switch-row {
  display: flex;
  gap: 20px;
  align-items: center;
  justify-content: space-between;
  min-height: 52px;
}

.switch-row span,
.field {
  display: grid;
  gap: 4px;
}

.switch-row small,
.field small {
  line-height: 1.45;
  color: hsl(var(--muted-foreground));
}

.switch-row input[type='checkbox'] {
  width: 22px;
  height: 22px;
  accent-color: hsl(var(--primary));
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  margin-top: 14px;
}

.field {
  font-weight: 600;
}

.field-wide {
  grid-column: 1 / -1;
}

.field input,
.field select {
  width: 100%;
  min-height: 44px;
  padding: 9px 11px;
  font: inherit;
  font-weight: 400;
  color: hsl(var(--foreground));
  background: hsl(var(--background));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
}

.field input:disabled,
.field select:disabled {
  cursor: not-allowed;
  opacity: 0.58;
}

.compact {
  min-height: 68px;
  padding: 0 2px;
}

.feedback,
.restart-note {
  padding: 12px 14px;
  border-radius: 8px;
}

.feedback:focus {
  outline: 3px solid hsl(var(--ring));
  outline-offset: 2px;
}

.feedback-error {
  color: hsl(var(--destructive));
  border: 1px solid hsl(var(--destructive));
}

.feedback-success {
  border: 1px solid hsl(var(--primary));
}

.restart-note {
  display: grid;
  gap: 4px;
  color: hsl(var(--muted-foreground));
  background: hsl(var(--muted));
  border: 1px solid hsl(var(--border));
}

.restart-note strong {
  color: hsl(var(--foreground));
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: flex-end;
}

button {
  min-height: 44px;
  padding: 9px 18px;
  font-weight: 700;
  color: hsl(var(--foreground));
  cursor: pointer;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
  transition:
    background-color 160ms ease,
    border-color 160ms ease;
}

button.primary {
  color: hsl(var(--primary-foreground));
  background: hsl(var(--primary));
  border-color: hsl(var(--primary));
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

button:focus-visible,
input:focus-visible,
select:focus-visible {
  outline: 3px solid hsl(var(--ring));
  outline-offset: 2px;
}

.sr-status {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

@media (max-width: 700px) {
  .observability-page {
    padding: 16px;
  }

  .page-heading {
    display: grid;
    gap: 12px;
  }

  .status-chip {
    justify-self: start;
  }

  .field-grid {
    grid-template-columns: 1fr;
  }

  .field-wide {
    grid-column: auto;
  }

  .actions button {
    flex: 1 1 140px;
  }
}

@media (prefers-reduced-motion: reduce) {
  button {
    transition: none;
  }
}
</style>
