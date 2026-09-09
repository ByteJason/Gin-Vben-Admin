/** Shared contracts and endpoint helpers for notification and media capabilities. */
import { ADMIN_ENDPOINTS } from './generated/admin-v1';
export type { MediaPage, MediaResource, MediaSignedURL, MediaURLPurpose } from './generated/admin-v1';

/** The only value a business form persists for a media item. URLs are read-time hints. */
export type MediaSelectionRole = 'cover' | 'gallery';
export interface MediaSelection {
  resourceId: string;
  sortOrder: number;
  role: MediaSelectionRole;
}

/**
 * Canonicalize picker output before it crosses a business API boundary.
 * Stable ordering means concurrent uploads cannot silently change a cover;
 * resource IDs remain unique and the caller must explicitly choose a cover.
 */
export function normalizeMediaSelections(
  input: readonly Partial<MediaSelection>[],
): MediaSelection[] {
  const seen = new Set<string>();
  let coverCount = 0;
  const items = input.map((item, index) => {
    const resourceId = item.resourceId?.trim() ?? '';
    if (!resourceId) throw new Error('media selection resource id is required');
    if (seen.has(resourceId)) throw new Error('media selection resource id must be unique');
    seen.add(resourceId);
    const sortOrder = item.sortOrder ?? index;
    if (!Number.isInteger(sortOrder) || sortOrder < 0) {
      throw new Error('media selection sort order must be non-negative');
    }
    const role = item.role ?? 'gallery';
    if (role !== 'cover' && role !== 'gallery') throw new Error('media selection role is invalid');
    if (role === 'cover') coverCount += 1;
    return { resourceId, sortOrder, role, index };
  });
  if (coverCount > 1) throw new Error('media selection allows only one cover');
  items.sort((a, b) => a.sortOrder - b.sortOrder || a.index - b.index);
  return items.map(({ resourceId, role }, sortOrder) => ({ resourceId, sortOrder, role }));
}

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
