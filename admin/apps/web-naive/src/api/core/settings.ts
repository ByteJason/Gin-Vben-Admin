import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

/** The active System Settings schema. Mail transport is owned by the
 * independent mail module and is deliberately absent from this contract. */
export type SettingCategory =
  | 'basic'
  | 'captcha'
  | 'file'
  | 'i18n'
  | 'observability'
  | 'other'
  | 'runtime'
  | 'security';

export type SettingSource = 'database' | 'default' | 'dotenv' | 'env' | 'yaml';
export type SettingApplyMode =
  | 'component_reload'
  | 'deployment'
  | 'immediate'
  | 'migration'
  | 'restart';
export type SettingScope = 'organization' | 'system' | 'tenant';
export type ModuleStatus =
  | 'save_failed'
  | 'saved_and_applied'
  | 'saved_apply_failed'
  | 'saved_pending_migration'
  | 'saved_pending_reload'
  | 'saved_pending_restart'
  | 'unchanged';

export interface SettingDefinition {
  allowed?: string[];
  allowedValues?: string[];
  applyMode: SettingApplyMode;
  category: SettingCategory;
  default?: string;
  deprecated?: boolean;
  description?: string;
  displayName: string;
  editable: boolean;
  envKey?: string;
  group: string;
  inputHint?: string;
  key: string;
  label?: string;
  kind: 'bool' | 'json' | 'number' | 'secret' | 'string';
  valueKind?: 'bool' | 'json' | 'number' | 'secret' | 'string';
  placeholder?: string;
  restartRequired?: boolean;
  scope?: SettingScope;
  scopePolicy?: SettingScope[];
  sensitive: boolean;
  sourcePolicy?: SettingSource[];
  unit?: string;
  yamlPath?: string;
}

export interface SettingData {
  category: SettingCategory;
  displayName?: string;
  editable?: boolean;
  group?: string;
  applyMode?: SettingApplyMode;
  key: string;
  restartRequired?: boolean;
  scope?: SettingScope;
  sensitive: boolean;
  source: SettingSource;
  updatedAt?: string;
  updatedBy?: string;
  value: string;
  version: number;
}

export interface SettingModuleDefinition {
  applyMode: SettingApplyMode;
  category: SettingCategory;
  description?: string;
  displayName: string;
  editable: boolean;
  group: string;
  id: string;
  keys: string[];
  name: string;
  scope: SettingScope;
  scopePolicy?: SettingScope[];
}

export interface SettingModuleView {
  applyMode: SettingApplyMode;
  category: SettingCategory;
  definitions: SettingDefinition[];
  description?: string;
  displayName: string;
  group: string;
  id: string;
  module: string;
  name: string;
  requiresRestart: boolean;
  revision: number;
  settings: SettingData[];
  scopePolicy?: SettingScope[];
  source?: SettingSource;
  updatedBy?: string;
  applyError?: string;
  otherNodesPending: boolean;
  status: ModuleStatus;
  updatedAt?: string;
}

export interface SettingModuleUpdateInput {
  expectedRevision: number;
  requestId?: string;
  values: Record<string, unknown>;
}

/** Explicit credential removal; blank secret inputs remain a no-op. */
export interface SettingModuleClearCredentialsInput {
  expectedRevision: number;
  keys: string[];
  requestId?: string;
}

export interface SettingModuleValidationInput {
  expectedRevision?: number;
  requestId?: string;
  values?: Record<string, unknown>;
}

export interface SettingModuleSaveResult {
  applyError?: string;
  applyMode: SettingApplyMode;
  applied: boolean;
  auditRecorded: boolean;
  cacheSynced: boolean;
  changedKeys: string[];
  id?: string;
  module: string;
  otherNodesPending: boolean;
  persisted: boolean;
  previousRevision: number;
  requestId?: string;
  requiresRestart: boolean;
  revision: number;
  settings: SettingData[];
  status: ModuleStatus;
  updatedAt: string;
}

export interface SettingModuleValidationResult {
  applyMode: SettingApplyMode;
  checkedAt: string;
  errors?: Record<string, string>;
  module: string;
  valid: boolean;
  values: Record<string, unknown>;
}

const modulePath = (module: string) =>
  ADMIN_ENDPOINTS.getSettingModule.replace(
    '{module}',
    encodeURIComponent(module),
  );

export async function listSettingModulesApi() {
  return requestClient.get<SettingModuleDefinition[]>(
    ADMIN_ENDPOINTS.listSettingModules,
  );
}

/** Compatibility schema reader for older embedded clients. New screens use
 * the module endpoints above; this alias remains read-only and carries the
 * same canonical definition metadata. */
export async function listSettingDefinitionsApi() {
  return requestClient.get<SettingDefinition[]>(
    ADMIN_ENDPOINTS.listSettingDefinitions,
  );
}

export async function getSettingModuleApi(module: string) {
  return requestClient.get<SettingModuleView>(modulePath(module));
}

export async function updateSettingModuleApi(
  module: string,
  input: SettingModuleUpdateInput,
) {
  return requestClient.put<SettingModuleSaveResult>(modulePath(module), input);
}

export async function validateSettingModuleApi(
  module: string,
  input?: SettingModuleValidationInput,
) {
  return requestClient.post<SettingModuleValidationResult>(
    `${modulePath(module)}/validate`,
    input,
  );
}

export async function resetSettingModuleApi(
  module: string,
  input: Pick<SettingModuleUpdateInput, 'expectedRevision' | 'requestId'>,
) {
  return requestClient.post<SettingModuleSaveResult>(
    `${modulePath(module)}/reset`,
    input,
  );
}

export async function clearSettingModuleCredentialsApi(
  module: string,
  input: SettingModuleClearCredentialsInput,
) {
  return requestClient.post<SettingModuleSaveResult>(
    `${modulePath(module)}/clear-credentials`,
    input,
  );
}

// Compatibility read helpers remain for branding and dictionary consumers;
// the System Settings page itself only uses the module APIs above.
const settingPath = (key: string) =>
  ADMIN_ENDPOINTS.getSetting.replace('{key}', encodeURIComponent(key));

export async function getSettingApi(key: string) {
  return requestClient.get<SettingData>(settingPath(key));
}

export interface SettingUpdateInput {
  expectedVersion: number;
  value: unknown;
}

export async function updateSettingApi(key: string, input: SettingUpdateInput) {
  return requestClient.put<SettingData>(settingPath(key), input);
}

// The standalone observability page is retained as a compatibility consumer;
// new System Settings edits use the observability module above.
const observabilitySettingPath = (key: string) =>
  ADMIN_ENDPOINTS.getObservabilitySetting.replace(
    '{key}',
    encodeURIComponent(key),
  );

export async function getObservabilitySettingApi(key: string) {
  return requestClient.get<SettingData>(observabilitySettingPath(key));
}

export async function updateObservabilitySettingApi(
  key: string,
  input: SettingUpdateInput,
) {
  return requestClient.put<SettingData>(observabilitySettingPath(key), input);
}
