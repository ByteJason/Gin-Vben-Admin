import type {
  FileCategory,
  FileCategoryInput,
  FileObject,
  MediaPage,
  MediaResource,
} from '@vben/api-client';

import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export type {
  FileCategory,
  FileCategoryInput,
  FileObject,
  MediaPage,
  MediaResource,
} from '@vben/api-client';
export type FileACL = 'private' | 'public-read';
export type MediaURLPurpose = 'download' | 'preview';

export interface FilePage {
  items: FileObject[];
  limit: number;
  offset: number;
  total: number;
}

export interface FileSignedURL {
  expiresIn: number;
  url: string;
}

export interface FileCleanupDryRun {
  bytes: number;
  cutoff: string;
  matchingCount: number;
}

export async function listFilesApi(params?: {
  categoryId?: string;
  limit?: number;
  offset?: number;
  ownerId?: string;
}) {
  return requestClient.get<FilePage>(ADMIN_ENDPOINTS.listFiles, { params });
}

export async function listFileCategoriesApi() {
  return requestClient.get<FileCategory[]>(ADMIN_ENDPOINTS.listFileCategories);
}

export async function createFileCategoryApi(input: FileCategoryInput) {
  return requestClient.post<FileCategory>(
    ADMIN_ENDPOINTS.createFileCategory,
    input,
  );
}

function categoryPath(template: string, id: string) {
  return template.replace('{id}', encodeURIComponent(id));
}

export async function updateFileCategoryApi(
  id: string,
  input: FileCategoryInput,
) {
  return requestClient.put<FileCategory>(
    categoryPath(ADMIN_ENDPOINTS.updateFileCategory, id),
    input,
  );
}

export async function deleteFileCategoryApi(id: string) {
  return requestClient.delete<void>(
    categoryPath(ADMIN_ENDPOINTS.deleteFileCategory, id),
  );
}

export async function uploadFileApi(
  file: File,
  acl: FileACL = 'private',
  categoryId?: string,
) {
  return requestClient.upload<FileObject>(ADMIN_ENDPOINTS.uploadFile, {
    acl,
    categoryId,
    file,
  });
}

function filePath(template: string, id: string) {
  return template.replace('{id}', encodeURIComponent(id));
}

export async function getFileApi(id: string) {
  return requestClient.get<FileObject>(filePath(ADMIN_ENDPOINTS.getFile, id));
}

export async function downloadFileApi(id: string, preview = false) {
  const endpoint = preview
    ? filePath(ADMIN_ENDPOINTS.previewFile, id)
    : filePath(ADMIN_ENDPOINTS.downloadFile, id);
  return requestClient.download<Blob>(endpoint, { responseReturn: 'body' });
}

export async function deleteFileApi(id: string) {
  return requestClient.delete<void>(filePath(ADMIN_ENDPOINTS.deleteFile, id));
}

export async function signedFileUrlApi(id: string, ttlSeconds = 900) {
  return requestClient.post<FileSignedURL>(
    filePath(ADMIN_ENDPOINTS.signFileURL, id),
    { ttlSeconds },
  );
}

export async function cleanupDryRunApi(ageSeconds = 180 * 24 * 60 * 60) {
  return requestClient.get<FileCleanupDryRun>(
    ADMIN_ENDPOINTS.fileCleanupDryRun,
    { params: { ageSeconds } },
  );
}

/** Shared media-library API used by feature modules (legacy /files APIs remain intact). */
export interface MediaListParams {
  categoryId?: string;
  cursor?: string;
  includeDescendants?: boolean;
  limit?: number;
  mimeFamily?: string;
  mimeExact?: string;
  /** Legacy compatibility only; cursor takes precedence when both are sent. */
  offset?: number;
  ownerId?: string;
  scopeType?: string;
  status?: string;
}

export function listMediaResourcesApi(params?: MediaListParams) {
  return requestClient.get<MediaPage>(ADMIN_ENDPOINTS.listMediaLibrary, { params });
}

export function uploadMediaResourceApi(
  file: File,
  acl: FileACL = 'private',
  categoryId?: string,
  metadata?: Record<string, string>,
) {
  return requestClient.upload<MediaResource>(ADMIN_ENDPOINTS.uploadMediaResource, {
    acl,
    categoryId,
    file,
    // The shared uploader appends scalar form values; serialise the object so
    // the multipart handler receives valid JSON rather than "[object Object]".
    metadata: metadata ? JSON.stringify(metadata) : undefined,
  });
}

export function getMediaResourceApi(id: string) {
  return requestClient.get<MediaResource>(ADMIN_ENDPOINTS.getMediaResource.replace('{id}', encodeURIComponent(id)));
}

export function updateMediaResourceApi(id: string, input: Partial<MediaResource>) {
  return requestClient.request<MediaResource>(ADMIN_ENDPOINTS.updateMediaResource.replace('{id}', encodeURIComponent(id)), { method: 'PATCH', data: input });
}

export function deleteMediaResourceApi(id: string, idempotencyKey?: string) {
  return requestClient.delete<void>(
    ADMIN_ENDPOINTS.deleteMediaResource.replace('{id}', encodeURIComponent(id)),
    idempotencyKey ? { headers: { 'Idempotency-Key': idempotencyKey } } : undefined,
  );
}

export function mediaResourceOpenPath(id: string) {
	return ADMIN_ENDPOINTS.openMediaResource.replace('{id}', encodeURIComponent(id));
}

export function openMediaResourceApi(id: string) {
	return requestClient.download<Blob>(mediaResourceOpenPath(id), { responseReturn: 'body' });
}

export function signMediaResourceUrlApi(
	id: string,
	purpose: MediaURLPurpose = 'preview',
	ttlSeconds = 900,
) {
	return requestClient.post<{ expiresAt: string; url: string }>(
		ADMIN_ENDPOINTS.signMediaResourceURL.replace('{id}', encodeURIComponent(id)),
		{ purpose, ttlSeconds },
	);
}

export interface BrandingSettings { logoResourceId?: string }
export interface BrandingSettingResponse {
  key: string;
  value: BrandingSettings;
  version?: number;
  updatedAt?: string;
}

interface RawSettingResponse {
  key: string;
  value: string;
  version?: number;
  updatedAt?: string;
}

export async function getBrandingSettingsApi(): Promise<BrandingSettingResponse> {
  const setting = await requestClient.get<RawSettingResponse>(
    ADMIN_ENDPOINTS.getSetting.replace('{key}', 'branding'),
  );
  let value: BrandingSettings = {};
  try {
    const parsed = JSON.parse(setting.value || '{}') as BrandingSettings;
    if (parsed && typeof parsed === 'object') value = parsed;
  } catch {
    value = {};
  }
  return { ...setting, value };
}

export function updateBrandingSettingsApi(
  value: BrandingSettings,
  expectedVersion = 0,
) {
  return requestClient.put<RawSettingResponse>(
    ADMIN_ENDPOINTS.updateSetting.replace('{key}', 'branding'),
    // The settings endpoint binds `value` as json.RawMessage. Pass the
    // structured branding object directly so the server stores an object
    // rather than a JSON-encoded string (which would be parsed twice on read).
    { value, expectedVersion },
  );
}

export interface MediaUsage {
  id: string;
  resourceId: string;
  module: string;
  entityType: string;
  entityId: string;
  field: string;
}

export function listMediaUsagesApi(resourceId: string) {
  return requestClient.get<MediaUsage[]>(ADMIN_ENDPOINTS.listMediaUsages, {
    params: { resourceId },
  });
}

export function listMediaResourceUsageApi(resourceId: string) {
  return requestClient.get<MediaUsage[]>(
    ADMIN_ENDPOINTS.listMediaResourceUsage.replace(
      '{id}',
      encodeURIComponent(resourceId),
    ),
  );
}

export function attachMediaUsageApi(
  resourceId: string,
  input: Omit<MediaUsage, 'id' | 'resourceId'>,
  idempotencyKey?: string,
) {
  const config = idempotencyKey
    ? { headers: { 'Idempotency-Key': idempotencyKey } }
    : undefined;
  return requestClient.post<MediaUsage>(
    ADMIN_ENDPOINTS.attachMediaUsage.replace('{id}', encodeURIComponent(resourceId)),
    input,
    config,
  );
}

export function detachMediaUsageApi(id: string, idempotencyKey?: string) {
  const config = idempotencyKey
    ? { headers: { 'Idempotency-Key': idempotencyKey } }
    : undefined;
  return requestClient.delete<void>(
    ADMIN_ENDPOINTS.detachMediaUsage.replace('{id}', encodeURIComponent(id)),
    config,
  );
}
