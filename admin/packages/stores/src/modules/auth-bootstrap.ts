interface AuthenticatedUserInfo {
  accessCodes?: string[];
}

interface AuthenticatedSessionCommit<UserInfo> {
  accessCodes: string[];
  userInfo: UserInfo;
}

interface BootstrapAuthenticatedSessionOptions<
  UserInfo extends AuthenticatedUserInfo,
> {
  accessToken: string;
  commit: (session: AuthenticatedSessionCommit<UserInfo>) => void;
  fetchLegacyAccessCodes: () => Promise<string[]>;
  fetchUserInfo: () => Promise<UserInfo>;
  rollback: () => Promise<void> | void;
  stageAccessToken: (accessToken: string) => void;
}

interface AuthenticateThenNavigateOptions<Result> {
  authenticate: () => Promise<Result>;
  navigate: (result: Result) => Promise<void> | void;
  onAuthenticationFailure: (error: unknown) => void;
}

function responseStatus(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined;
  const response = Reflect.get(error, 'response');
  if (response && typeof response === 'object') {
    const status = Reflect.get(response, 'status');
    if (typeof status === 'number') return status;
  }
  const status = Reflect.get(error, 'status');
  return typeof status === 'number' ? status : undefined;
}

async function resolveAccessCodes<UserInfo extends AuthenticatedUserInfo>(
  userInfo: UserInfo,
  fetchLegacyAccessCodes: () => Promise<string[]>,
): Promise<string[]> {
  if (Object.prototype.hasOwnProperty.call(userInfo, 'accessCodes')) {
    if (!Array.isArray(userInfo.accessCodes)) {
      throw new TypeError('iam.me accessCodes must be an array');
    }
    return [...userInfo.accessCodes];
  }

  try {
    return [...(await fetchLegacyAccessCodes())];
  } catch (error) {
    if (responseStatus(error) === 404) return [];
    throw error;
  }
}

async function bootstrapAuthenticatedSession<
  UserInfo extends AuthenticatedUserInfo,
>(options: BootstrapAuthenticatedSessionOptions<UserInfo>): Promise<UserInfo> {
  options.stageAccessToken(options.accessToken);
  try {
    const userInfo = await options.fetchUserInfo();
    const accessCodes = await resolveAccessCodes(
      userInfo,
      options.fetchLegacyAccessCodes,
    );
    options.commit({ accessCodes, userInfo });
    return userInfo;
  } catch (error) {
    try {
      await options.rollback();
    } catch {
      // Rollback is best-effort and must never replace the bootstrap failure.
    }
    throw error;
  }
}

async function authenticateThenNavigate<Result>(
  options: AuthenticateThenNavigateOptions<Result>,
): Promise<Result> {
  let result: Result;
  try {
    result = await options.authenticate();
  } catch (error) {
    options.onAuthenticationFailure(error);
    throw error;
  }

  await options.navigate(result);
  return result;
}

export {
  authenticateThenNavigate,
  bootstrapAuthenticatedSession,
  resolveAccessCodes,
};
export type {
  AuthenticateThenNavigateOptions,
  AuthenticatedSessionCommit,
  AuthenticatedUserInfo,
  BootstrapAuthenticatedSessionOptions,
};
