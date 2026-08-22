import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export interface SMTPAccount {
  id: string;
  tenantId?: string;
  orgId?: string;
  name: string;
  enabled: boolean;
  host: string;
  port: number;
  username: string;
  weight: number;
  fromEmail: string;
  fromName?: string;
  implicitTls: boolean;
  passwordConfigured: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface SMTPAccountInput {
  name: string;
  enabled: boolean;
  host: string;
  port: number;
  username?: string;
  password?: string;
  weight: number;
  fromEmail: string;
  fromName?: string;
  implicitTls: boolean;
}

export interface SMTPConnectionTestResult {
  accountId: string;
  requestId: string;
  status: 'ok' | 'failed';
  stage?: string;
  code?: string;
  message?: string;
  checkedAt: string;
}

export interface EmailRecipient { address: string; kind: string }
export interface EmailMessage {
  id: string;
  subject: string;
  recipients: EmailRecipient[];
  body?: string;
  bodyDigest: string;
  status: string;
  attemptCount: number;
  smtpAccountId?: string;
  senderId?: string;
  providerMessageId?: string;
  lastErrorCode?: string;
  createdAt: string;
  updatedAt: string;
}

function accountPath(id: string) {
  return ADMIN_ENDPOINTS.updateSMTPAccount.replace('{id}', encodeURIComponent(id));
}

export function listSMTPAccountsApi() {
  return requestClient.get<SMTPAccount[]>(ADMIN_ENDPOINTS.listSMTPAccounts);
}

export function saveSMTPAccountApi(input: SMTPAccountInput, id?: string) {
  if (id) {
    return requestClient.put<SMTPAccount>(accountPath(id), input);
  }
  return requestClient.post<SMTPAccount>(ADMIN_ENDPOINTS.createSMTPAccount, input);
}

export function testSMTPAccountApi(id: string) {
  return requestClient.post<SMTPConnectionTestResult>(
    ADMIN_ENDPOINTS.testSMTPAccount.replace('{id}', encodeURIComponent(id)),
  );
}

export function deleteSMTPAccountApi(id: string) {
  return requestClient.delete<void>(accountPath(id));
}

export function listEmailMessagesApi(params?: { limit?: number; offset?: number; status?: string }) {
  return requestClient.get<{ items: EmailMessage[]; total: number; limit: number; offset: number }>(
    ADMIN_ENDPOINTS.listEmailMessages,
    { params },
  );
}
