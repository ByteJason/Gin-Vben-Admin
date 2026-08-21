import {
  AUTH_API_PREFIX,
  AUTH_ENDPOINTS,
  type AuthApi,
} from '@vben/api-client';

import { baseRequestClient, requestClient } from '#/api/request';

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
  const identifier = data.identifier ?? data.username ?? '';
  const payload: AuthApi.LoginParams = {
    password: data.password,
    ...(data.username ? { username: data.username } : {}),
    ...(identifier ? { identifier } : {}),
    ...(data.identifierType
      ? { identifierType: data.identifierType }
      : identifier.includes('@')
        ? { identifierType: 'email' as const }
        : { identifierType: 'username' as const }),
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
  return requestClient.post<void>(
    AUTH_ENDPOINTS.passwordResetRequest,
    { username },
    {
      withCredentials: true,
    },
  );
}

/** 使用一次性令牌提交新密码。 */
export async function resetPasswordApi(
  data: Required<
    Pick<AuthApi.PasswordResetRequestParams, 'password' | 'token'>
  >,
) {
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
  return requestClient.delete<void>(
    `${AUTH_ENDPOINTS.sessions}/${encodeURIComponent(sessionId)}`,
    {
      withCredentials: true,
    },
  );
}

/** 获取当前账号的权限码。 */
export async function getAccessCodesApi() {
  return requestClient.get<string[]>(`${AUTH_API_PREFIX}/codes`);
}
