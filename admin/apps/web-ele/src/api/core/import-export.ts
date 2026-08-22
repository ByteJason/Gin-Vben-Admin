import { requestClient } from '#/api/request';

export type ImportRowError = {
  row: number;
  column?: string;
  code: string;
  messageKey: string;
};

export type ImportPreview = {
  id: string;
  format: string;
  headers: string[];
  mappedColumns: Record<string, string>;
  previewRows: Array<Record<string, string>>;
  totalRows: number;
  errors: ImportRowError[];
  sizeBytes: number;
  sha256: string;
  createdAt: string;
  expiresAt: string;
};

export type ImportExportJob = {
  id: string;
  kind: 'import' | 'export';
  status: string;
  totalRows: number;
  processedRows: number;
  errorCount: number;
  previewId?: string;
  downloadUrl?: string;
  expiresAt?: string;
  lastErrorCode?: string;
  createdAt: string;
  updatedAt: string;
};

const BASE = '/api/admin/v1/import-export';
const jobPath = (id: string) => `${BASE}/jobs/${encodeURIComponent(id)}`;

export function downloadImportTemplateApi(format: 'csv' | 'xlsx' = 'csv') {
  return requestClient.get<Blob>(`${BASE}/templates/${format}`, { responseType: 'blob' });
}

export function previewImportApi(form: FormData) {
  return requestClient.post<ImportPreview>(`${BASE}/imports/preview`, form);
}

export function commitImportApi(input: { previewId: string; idempotencyKey?: string }) {
  return requestClient.post<ImportExportJob>(`${BASE}/imports/commit`, input);
}

export function startExportApi(input: {
  fields: string[];
  allowlist: Record<string, boolean>;
  rows?: Array<Record<string, string>>;
  idempotencyKey?: string;
}) {
  return requestClient.post<ImportExportJob>(`${BASE}/exports`, input);
}

export function listImportExportJobsApi(kind?: 'import' | 'export') {
  return requestClient.get<{ items: ImportExportJob[]; total: number }>(
    `${BASE}/jobs${kind ? `?kind=${kind}` : ''}`,
  );
}

export function getImportExportJobApi(id: string) {
  return requestClient.get<ImportExportJob>(jobPath(id));
}

export function listImportErrorsApi(id: string) {
  return requestClient.get<{ items: ImportRowError[]; total: number }>(`${jobPath(id)}/errors`);
}

export function downloadImportErrorsApi(id: string) {
  return requestClient.get<Blob>(`${jobPath(id)}/errors?format=csv`, { responseType: 'blob' });
}

export function downloadExportApi(id: string) {
  return requestClient.get<Blob>(`${jobPath(id)}/download`, { responseType: 'blob' });
}

export function cancelImportExportJobApi(id: string) {
  return requestClient.post<ImportExportJob>(`${jobPath(id)}/cancel`);
}

export function retryImportExportJobApi(id: string) {
  return requestClient.post<ImportExportJob>(`${jobPath(id)}/retry`);
}

// Contract-friendly alias used by the error-row download control.
export const downloadErrorRowsApi = downloadImportErrorsApi;
