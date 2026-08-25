import type { BasicUserInfo } from '@vben-core/typings';

/** 用户信息 */
interface UserInfo extends BasicUserInfo {
  /** 当前身份可使用的 API/按钮权限码；旧服务端可能不返回。 */
  accessCodes?: string[];
  /**
   * 用户描述
   */
  desc: string;
  /**
   * 首页地址
   */
  homePath: string;

  /**
   * accessToken
   */
  token: string;
}

export type { UserInfo };
