import type {
  FileCategory,
  FileCategoryInput,
  FileObject,
} from '@vben/api-client';

import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export type {
  FileCategory,
  FileCategoryInput,
  FileObject,
} from '@vben/api-client';
export type FileACL = 'private' | 'public-read';

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
