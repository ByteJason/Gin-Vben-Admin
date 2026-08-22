import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export type SettingCategory =
  | 'basic'
  | 'captcha'
  | 'file'
  | 'i18n'
  | 'mail'
  | 'other'
  | 'security';

export type SettingSource = 'database' | 'default' | 'dotenv' | 'env' | 'yaml';

export interface SettingDefinition {
  allowed?: string[];
  category: SettingCategory;
  default?: string;
  description?: string;
  envKey?: string;
  key: string;
  kind: 'bool' | 'json' | 'number' | 'secret' | 'string';
  restartRequired: boolean;
  sensitive: boolean;
  yamlPath?: string;
}

export interface SettingData {
  category: SettingCategory;
  key: string;
  restartRequired: boolean;
  sensitive: boolean;
  source: SettingSource;
  updatedAt?: string;
  updatedBy?: string;
  value: string;
  version: number;
}

export interface SettingUpdateInput {
  expectedVersion: number;
  value: unknown;
}

export interface SettingRollbackInput {
  expectedVersion: number;
  version: number;
}

export interface SettingConnectionTestResult {
  category: SettingCategory;
  checkedAt: string;
  key: string;
  message?: string;
  requestId: string;
  source: SettingSource;
  status: 'failed' | 'ok';
}

const settingPath = (key: string) =>
  `${ADMIN_ENDPOINTS.getSetting.replace('{key}', encodeURIComponent(key))}`;

export async function listSettingDefinitionsApi() {
  return requestClient.get<SettingDefinition[]>(
    ADMIN_ENDPOINTS.listSettingDefinitions,
  );
}

export async function getSettingApi(key: string) {
  return requestClient.get<SettingData>(settingPath(key));
}

export async function updateSettingApi(key: string, input: SettingUpdateInput) {
  return requestClient.put<SettingData>(settingPath(key), input);
}

export async function listSettingHistoryApi(key: string) {
  const endpoint = ADMIN_ENDPOINTS.listSettingHistory.replace(
    '{key}',
    encodeURIComponent(key),
  );
  return requestClient.get<SettingData[]>(endpoint);
}

export async function rollbackSettingApi(
  key: string,
  input: SettingRollbackInput,
) {
  const endpoint = ADMIN_ENDPOINTS.rollbackSetting.replace(
    '{key}',
    encodeURIComponent(key),
  );
  return requestClient.post<SettingData>(endpoint, input);
}

export async function testSettingConnectionApi(key: string, value?: unknown) {
  const endpoint = `${settingPath(key)}/test`;
  return requestClient.post<SettingConnectionTestResult>(
    endpoint,
    value === undefined ? undefined : { value },
  );
}
