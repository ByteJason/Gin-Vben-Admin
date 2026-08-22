import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export interface DictionaryType {
  id: string;
  tenantId?: string;
  orgId?: string;
  code: string;
  nameZhCN: string;
  nameEnUS: string;
  description?: string;
  status: 'active' | 'disabled';
  sortOrder: number;
  systemOwned: boolean;
  cacheVersion: number;
  createdAt: string;
  updatedAt: string;
}

export interface DictionaryTypeInput {
  code: string;
  nameZhCN?: string;
  nameEnUS?: string;
  description?: string;
  status?: 'active' | 'disabled';
  sortOrder?: number;
}

export interface DictionaryItem {
  id: string;
  tenantId?: string;
  orgId?: string;
  typeCode: string;
  value: string;
  labelZhCN: string;
  labelEnUS: string;
  label: string;
  description?: string;
  tag?: string;
  status: 'active' | 'disabled';
  sortOrder: number;
  systemOwned: boolean;
  cacheVersion: number;
  createdAt: string;
  updatedAt: string;
}

export interface DictionaryItemInput {
  value: string;
  labelZhCN?: string;
  labelEnUS?: string;
  description?: string;
  tag?: string;
  status?: 'active' | 'disabled';
  sortOrder?: number;
  enabled?: boolean;
}

const replace = (template: string, key: string, value: string) =>
  template.replace(`{${key}}`, encodeURIComponent(value));

export function listDictionariesApi(params?: { includeDisabled?: boolean }) {
  return requestClient.get<DictionaryType[]>(ADMIN_ENDPOINTS.listDictionaries, { params });
}

export function saveDictionaryApi(input: DictionaryTypeInput, code?: string) {
  if (code) {
    return requestClient.request<DictionaryType>(
      replace(ADMIN_ENDPOINTS.updateDictionary, 'code', code),
      { data: input, method: 'PATCH' },
    );
  }
  return requestClient.post<DictionaryType>(ADMIN_ENDPOINTS.createDictionary, input);
}

export function deleteDictionaryApi(code: string) {
  return requestClient.delete<void>(replace(ADMIN_ENDPOINTS.deleteDictionary, 'code', code));
}

export function listDictionaryItemsApi(typeCode: string, params?: { locale?: string; includeDisabled?: boolean }) {
  return requestClient.get<DictionaryItem[]>(
    replace(ADMIN_ENDPOINTS.listDictionaryItems, 'type', typeCode),
    { params },
  );
}

export function saveDictionaryItemApi(typeCode: string, input: DictionaryItemInput, id?: string) {
  if (id) {
    return requestClient.request<DictionaryItem>(
      replace(replace(ADMIN_ENDPOINTS.updateDictionaryItem, 'type', typeCode), 'id', id),
      { data: input, method: 'PATCH' },
    );
  }
  return requestClient.post<DictionaryItem>(
    replace(ADMIN_ENDPOINTS.createDictionaryItem, 'type', typeCode),
    input,
  );
}

export function deleteDictionaryItemApi(typeCode: string, id: string) {
  return requestClient.delete<void>(
    replace(replace(ADMIN_ENDPOINTS.deleteDictionaryItem, 'type', typeCode), 'id', id),
  );
}

export function importDictionaryItemsApi(typeCode: string, items: DictionaryItemInput[]) {
  return requestClient.post<{ items: DictionaryItem[] }>(
    replace(ADMIN_ENDPOINTS.importDictionaryItems, 'type', typeCode),
    { items },
  );
}
