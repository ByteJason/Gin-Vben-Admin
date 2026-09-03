/** Shared contracts and endpoint helpers for notification and media capabilities. */
import { ADMIN_ENDPOINTS } from './generated/admin-v1';
export type { MediaPage, MediaResource, MediaSignedURL, MediaURLPurpose } from './generated/admin-v1';

export interface NotificationCaller { accountIds?: string[]; callerKey?: string; defaultAccountId?: string; enabled: boolean; id: string; key?: string; module?: string; name: string; routingPolicy?: string; smtpAccountIds?: string[]; strategy?: string; systemOwned?: boolean; weights?: Record<string, number> }
export interface NotificationTemplate { body?: string; defaultLocale?: string; enabled?: boolean; id: string; key?: string; locales?: Record<string, { body: string; locale?: string; subject: string; }>; name?: string; published?: boolean; purpose?: string; subject?: string; templateKey?: string; variables?: string[] }
export interface VerificationPolicy { callerKey?: string; charset?: string; codeLength?: number; hourlyLimit?: number; key?: string; length?: number; maxFailures?: number; maxSendsPerHour?: number; policyKey?: string; purpose?: string; resendAfterSeconds?: number; resendIntervalSeconds?: number; ttlSeconds?: number; }
export interface VerificationChallenge { expiresAt: string; id: string; remainingAttempts?: number; resendAvailableAt?: string; status: string; }
export interface VerificationIssueRequest { callerKey?: string; idempotencyKey?: string; locale?: string; purpose: string; recipient: string; }
export interface VerificationVerifyRequest { code: string; idempotencyKey?: string }

export const COMMON_CAPABILITY_ENDPOINTS = {
  ...ADMIN_ENDPOINTS,
} as const;

export function commonCapabilityPath(template: string, id: string) {
  return template.replace('{id}', encodeURIComponent(id));
}
