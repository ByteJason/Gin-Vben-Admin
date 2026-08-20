import { baseRequestClient, requestClient } from '#/api/request';

/** Versioned management authentication path. */
export const AUTH_API_PREFIX = '/admin/v1/auth';
export const AUTH_ENDPOINTS = {
  login: '/admin/v1/auth/login',
  logout: '/admin/v1/auth/logout',
  passwordReset: '/admin/v1/auth/password/reset',
  passwordResetRequest: '/admin/v1/auth/password/reset/request',
  register: '/admin/v1/auth/register',
  refresh: '/admin/v1/auth/refresh',
  sessions: '/admin/v1/auth/sessions',
} as const;

export namespace AuthApi {
  /** 登录接口参数 */
  export interface LoginParams {
    captcha?: string;
    captchaId?: string;
    password: string;
    username: string;
  }

  export interface RegisterParams {
    password: string;
    username: string;
  }

  export interface PasswordResetRequestParams {
    password?: string;
    token?: string;
    username?: string;
  }

  export interface SessionInfo {
    createdAt: string;
    deviceId: string;
    deviceName: string;
    expiresAt: string;
    id: string;
    ipAddress: string;
    lastSeenAt: string;
    revoked: boolean;
    userAgent: string;
  }

  /** 登录、刷新接口返回值；refresh token 永不进入此对象 */
  export interface LoginResult {
    accessToken: string;
    expiresIn: number;
    tokenType: 'Bearer';
  }

  export type RefreshTokenResult = LoginResult;

  export interface ApiEnvelope<T> {
    code: number;
    data: T;
    message: string;
    meta?: { requestId?: string };
    traceId?: string;
  }

  /** 当前 Go domain 的 wire 兼容形态；HTTP client 归一化为 camelCase。 */
  export interface WireTokenData {
    accessToken?: string;
    access_token?: string;
    expiresIn?: number;
    expires_in?: number;
    tokenType?: 'Bearer' | string;
    token_type?: 'Bearer' | string;
  }
}

function normalizeTokenData(
  value: AuthApi.LoginResult | AuthApi.WireTokenData,
): AuthApi.LoginResult {
  const data = value as AuthApi.WireTokenData;
  const accessToken = data.accessToken ?? data.access_token ?? '';
  const expiresIn = data.expiresIn ?? data.expires_in ?? 0;
  const tokenType = data.tokenType ?? data.token_type ?? 'Bearer';

  return {
    accessToken,
    expiresIn,
    tokenType: tokenType === 'Bearer' ? 'Bearer' : 'Bearer',
  };
}

/** 获取一次性验证码挑战；答案由 provider 私下校验。 */
export async function getCaptchaApi() {
  return requestClient.get<{
    expiresIn: number;
    id: string;
    kind: string;
    payload?: string;
  }>(`${AUTH_API_PREFIX}/captcha`);
}

/** 登录；服务端在响应头设置 HttpOnly refresh cookie。 */
export async function loginApi(data: AuthApi.LoginParams) {
  const payload: AuthApi.LoginParams = {
    password: data.password,
    username: data.username,
  };
  if (typeof data.captcha === 'string' && data.captcha.length > 0) {
    payload.captcha = data.captcha;
  }
  if (typeof data.captchaId === 'string' && data.captchaId.length > 0) {
    payload.captchaId = data.captchaId;
  }
  const result = await requestClient.post<
    AuthApi.LoginResult | AuthApi.WireTokenData
  >(AUTH_ENDPOINTS.login, payload, {
    withCredentials: true,
  });
  return normalizeTokenData(result);
}

/**
 * 刷新 access token。refresh token 由浏览器 cookie 发送，不能放进 JSON。
 */
export async function refreshTokenApi() {
  const response = await baseRequestClient.post<
    AuthApi.ApiEnvelope<AuthApi.LoginResult | AuthApi.WireTokenData>
  >(AUTH_ENDPOINTS.refresh, undefined, {
    withCredentials: true,
  });
  const body = (response as any)?.data ?? response;
  return normalizeTokenData(body?.data ?? body);
}

/** 退出登录并让服务端清除 refresh cookie。 */
export async function logoutApi() {
  return baseRequestClient.post<AuthApi.ApiEnvelope<null>>(
    AUTH_ENDPOINTS.logout,
    undefined,
    {
      withCredentials: true,
    },
  );
}

/** 创建管理端账号；服务端只返回统一成功包，不返回凭据。 */
export async function registerApi(data: AuthApi.RegisterParams) {
  return requestClient.post<void>(AUTH_ENDPOINTS.register, data, {
    withCredentials: true,
  });
}

/** 请求密码重置；未知账号也返回相同结果以避免账号枚举。 */
export async function requestPasswordResetApi(username: string) {
  return requestClient.post<void>(AUTH_ENDPOINTS.passwordResetRequest, { username }, {
    withCredentials: true,
  });
}

/** 使用一次性令牌提交新密码。 */
export async function resetPasswordApi(data: Required<Pick<AuthApi.PasswordResetRequestParams, 'token' | 'password'>>) {
  return requestClient.post<void>(AUTH_ENDPOINTS.passwordReset, data, {
    withCredentials: true,
  });
}

/** 获取当前账号的设备会话列表。 */
export async function listSessionsApi() {
  return requestClient.get<AuthApi.SessionInfo[]>(AUTH_ENDPOINTS.sessions, {
    withCredentials: true,
  });
}

/** 撤销当前账号名下的指定设备会话。 */
export async function revokeSessionApi(sessionId: string) {
  return requestClient.delete<void>(`${AUTH_ENDPOINTS.sessions}/${encodeURIComponent(sessionId)}`, {
    withCredentials: true,
  });
}

/** 获取当前账号的权限码。 */
export async function getAccessCodesApi() {
  return requestClient.get<string[]>(`${AUTH_API_PREFIX}/codes`);
}
