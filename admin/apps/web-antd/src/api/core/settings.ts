import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export interface SettingDefinition {
  allowed?: string[];
  default?: string;
  key: string;
  kind: 'bool' | 'json' | 'number' | 'secret' | 'string';
  sensitive: boolean;
}

export interface SettingData {
  key: string;
  sensitive: boolean;
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
