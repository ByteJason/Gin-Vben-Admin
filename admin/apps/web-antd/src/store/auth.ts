import type { Recordable, UserInfo } from '@vben/types';

import { ref } from 'vue';
import { useRouter } from 'vue-router';

import { LOGIN_PATH } from '@vben/constants';
import { preferences } from '@vben/preferences';
import {
  authenticateThenNavigate,
  bootstrapAuthenticatedSession,
  resetAllStores,
  useAccessStore,
  useUserStore,
} from '@vben/stores';
import { resolveAuthRedirect } from '@vben/utils';

import { notification } from 'ant-design-vue';
import { defineStore } from 'pinia';

import { getAccessCodesApi, getUserInfoApi, loginApi, logoutApi } from '#/api';
import { $t } from '#/locales';

export const useAuthStore = defineStore('auth', () => {
  const accessStore = useAccessStore();
  const userStore = useUserStore();
  const router = useRouter();

  const loginLoading = ref(false);
  const loginError = ref<null | string>(null);
  const loginSuccess = ref(false);

  /**
   * 异步处理登录操作
   * Asynchronously handle the login process
   * @param params 登录表单数据
   */
  async function authLogin(
    params: Recordable<any>,
    onSuccess?: () => Promise<void> | void,
  ) {
    // 异步处理用户登录操作并获取 accessToken
    let userInfo: UserInfo;
    loginLoading.value = true;
    loginError.value = null;
    loginSuccess.value = false;
    try {
      userInfo = await authenticateThenNavigate({
        authenticate: async () => {
          const { accessToken } = await loginApi({
            captcha:
              typeof params.captcha === 'string' ? params.captcha : undefined,
            captchaId:
              typeof params.captchaId === 'string'
                ? params.captchaId
                : undefined,
            password: params.password,
            identifier: params.identifier ?? params.username,
          });
          if (!accessToken) {
            throw new Error('Login response did not include an access token');
          }
          return bootstrapAuthenticatedSession({
            accessToken,
            commit: ({ accessCodes, userInfo: resolvedUserInfo }) => {
              userStore.setUserInfo(resolvedUserInfo);
              accessStore.setAccessCodes(accessCodes);
            },
            fetchLegacyAccessCodes: getAccessCodesApi,
            fetchUserInfo: getUserInfoApi,
            rollback: async () => {
              try {
                await logoutApi();
              } catch {
                // Cookie/session cleanup is best-effort; local state must reset.
              }
              accessStore.setAccessToken(null);
              accessStore.setAccessCodes([]);
              userStore.setUserInfo(null);
            },
            stageAccessToken: (token) => accessStore.setAccessToken(token),
          });
        },
        navigate: async (authenticatedUser) => {
          loginSuccess.value = true;
          if (accessStore.loginExpired) {
            accessStore.setLoginExpired(false);
          } else {
            onSuccess
              ? await onSuccess()
              : await router.push(
                  authenticatedUser.homePath || preferences.app.defaultHomePath,
                );
          }
          if (authenticatedUser.realName) {
            notification.success({
              description: `${$t('authentication.loginSuccessDesc')}:${authenticatedUser.realName}`,
              duration: 3,
              message: $t('authentication.loginSuccess'),
              placement: 'topRight',
            });
          }
        },
        onAuthenticationFailure: (error) => {
          loginSuccess.value = false;
          const responseData =
            (error as any)?.response?.data ?? (error as any)?.data ?? error;
          loginError.value =
            (responseData as any)?.message ??
            (responseData as any)?.error ??
            'Login failed. Check your credentials and try again.';
        },
      });
    } finally {
      loginLoading.value = false;
    }

    return {
      userInfo,
    };
  }

  async function logout(redirect: boolean = true) {
    const currentRoute = router.currentRoute.value;
    const redirectTarget = resolveAuthRedirect(
      currentRoute.path === LOGIN_PATH
        ? currentRoute.query.redirect
        : currentRoute.fullPath,
      {
        fallback: preferences.app.defaultHomePath,
        loginPath: LOGIN_PATH,
      },
    );

    try {
      await logoutApi();
    } catch {
      // 不做任何处理
    }
    resetAllStores();
    accessStore.setLoginExpired(false);

    // Vue Router owns query encoding. Reuse an existing login redirect instead
    // of wrapping the login page again when multiple expired requests race.
    await router.replace({
      path: LOGIN_PATH,
      query: redirect ? { redirect: redirectTarget } : {},
    });
  }

  async function fetchUserInfo() {
    const userInfo = await getUserInfoApi();
    userStore.setUserInfo(userInfo);
    return userInfo;
  }

  function $reset() {
    loginLoading.value = false;
    loginError.value = null;
    loginSuccess.value = false;
  }

  return {
    $reset,
    authLogin,
    fetchUserInfo,
    loginError,
    loginLoading,
    loginSuccess,
    logout,
  };
});
