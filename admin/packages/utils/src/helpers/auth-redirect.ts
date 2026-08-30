export interface AuthRedirectOptions {
  fallback: string;
  loginPath: string;
  maxDepth?: number;
}

function firstValue(value: unknown): string {
  if (Array.isArray(value)) return firstValue(value[0]);
  return typeof value === 'string' ? value.trim() : '';
}

function decodeRepeatedly(value: string, maxDepth: number): null | string {
  let decoded = value;
  for (let depth = 0; depth < maxDepth; depth += 1) {
    try {
      const next = decodeURIComponent(decoded);
      if (next === decoded) return decoded;
      decoded = next;
    } catch {
      return null;
    }
  }
  return decoded;
}

function isLocalPath(value: string): boolean {
  return value.startsWith('/') && !value.startsWith('//');
}

/**
 * Resolves an authentication redirect to one local application path.
 *
 * Old clients encoded the query before giving it to Vue Router, while Router
 * encoded it again. Repeated expiry handling could additionally wrap the
 * login page inside its own redirect. This function accepts those legacy
 * values, unwraps nested login redirects, and rejects external destinations.
 */
export function resolveAuthRedirect(
  value: unknown,
  options: AuthRedirectOptions,
): string {
  const maxDepth = Math.max(1, Math.min(options.maxDepth ?? 5, 10));
  const fallback = isLocalPath(options.fallback) ? options.fallback : '/';
  let candidate = firstValue(value);

  for (let depth = 0; depth < maxDepth; depth += 1) {
    const decoded = decodeRepeatedly(candidate, maxDepth);
    if (!decoded || !isLocalPath(decoded)) return fallback;

    let url: URL;
    try {
      url = new URL(decoded, 'https://local.invalid');
    } catch {
      return fallback;
    }
    if (url.origin !== 'https://local.invalid') return fallback;

    if (url.pathname !== options.loginPath) return decoded;

    const nested = url.searchParams.get('redirect');
    if (!nested) return fallback;
    candidate = nested;
  }

  return fallback;
}
