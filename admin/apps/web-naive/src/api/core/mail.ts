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
  status: 'failed' | 'ok';
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
  return requestClient.get<{ items: EmailMessage[]; limit: number; offset: number; total: number; }>(
    ADMIN_ENDPOINTS.listEmailMessages,
    { params },
  );
}


export interface NotificationCaller { id: string; key?: string; callerKey?: string; name: string; module?: string; enabled: boolean; systemOwned?: boolean; smtpAccountIds?: string[]; accountIds?: string[]; defaultAccountId?: string; routingPolicy?: string; strategy?: string; weights?: Record<string, number> }
export interface NotificationTemplate { id: string; key?: string; templateKey?: string; name?: string; purpose?: string; subject?: string; body?: string; enabled?: boolean; published?: boolean; defaultLocale?: string; locales?: Record<string, { body: string; locale?: string; subject: string; }>; variables?: string[] }
export interface VerificationPolicy { key?: string; policyKey?: string; purpose?: string; callerKey?: string; codeLength?: number; length?: number; charset?: string; ttlSeconds?: number; maxFailures?: number; resendIntervalSeconds?: number; resendAfterSeconds?: number; hourlyLimit?: number; maxSendsPerHour?: number }
export interface VerificationChallenge { id: string; status: string; expiresAt: string; remainingAttempts?: number; resendAvailableAt?: string }

function notificationPath(template: string, id: string) { return template.replace('{id}', encodeURIComponent(id)); }
export function listNotificationCallersApi() { return requestClient.get<NotificationCaller[]>(ADMIN_ENDPOINTS.listNotificationCallers); }
export function saveNotificationCallerApi(input: Partial<NotificationCaller>, id?: string) { return id ? requestClient.put<NotificationCaller>(notificationPath(ADMIN_ENDPOINTS.updateNotificationCaller, id), input) : requestClient.post<NotificationCaller>(ADMIN_ENDPOINTS.createNotificationCaller, input); }
export function deleteNotificationCallerApi(id: string) { return requestClient.delete<void>(notificationPath(ADMIN_ENDPOINTS.deleteNotificationCaller, id)); }
export function listNotificationTemplatesApi() { return requestClient.get<NotificationTemplate[]>(ADMIN_ENDPOINTS.listNotificationTemplates); }
export function saveNotificationTemplateApi(input: Partial<NotificationTemplate>, id?: string) { return id ? requestClient.put<NotificationTemplate>(notificationPath(ADMIN_ENDPOINTS.updateNotificationTemplate, id), input) : requestClient.post<NotificationTemplate>(ADMIN_ENDPOINTS.createNotificationTemplate, input); }
export function deleteNotificationTemplateApi(id: string) { return requestClient.delete<void>(notificationPath(ADMIN_ENDPOINTS.deleteNotificationTemplate, id)); }
export function publishNotificationTemplateApi(id: string) { return requestClient.post<NotificationTemplate>(notificationPath(ADMIN_ENDPOINTS.publishNotificationTemplate, id)); }
export function testNotificationTemplateApi(id: string, input?: { locale?: string; recipient?: string; variables?: Record<string, string> }) { return requestClient.post<{ isTest: boolean; messageId: string; status: string; }>(notificationPath(ADMIN_ENDPOINTS.testNotificationTemplate, id), input); }
export function listVerificationPoliciesApi() { return requestClient.get<VerificationPolicy[]>(ADMIN_ENDPOINTS.listVerificationPolicies); }
export function updateVerificationPolicyApi(key: string, input: Partial<VerificationPolicy>) { return requestClient.request<VerificationPolicy>(ADMIN_ENDPOINTS.updateVerificationPolicy.replace('{policy_key}', encodeURIComponent(key)), { method: 'PATCH', data: input }); }
export function issueVerificationChallengeApi(input: { callerKey?: string; idempotencyKey?: string; locale?: string; purpose: string; recipient: string; }) { return requestClient.post<VerificationChallenge>(ADMIN_ENDPOINTS.issueVerificationChallenge, input); }
export function getVerificationChallengeApi(id: string) { return requestClient.get<VerificationChallenge>(notificationPath(ADMIN_ENDPOINTS.getVerificationChallenge, id)); }
export function verifyVerificationChallengeApi(id: string, input: { code: string; idempotencyKey?: string }) { return requestClient.post<{ status: string; verified: boolean; }>(notificationPath(ADMIN_ENDPOINTS.verifyVerificationChallenge, id), input); }
