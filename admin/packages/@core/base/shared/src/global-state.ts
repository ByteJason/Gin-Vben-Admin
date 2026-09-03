/**
 * 全局复用的变量、组件、配置，各个模块之间共享
 * 通过单例模式实现,单例必须注意不受请求影响，例如用户信息这些需要根据请求获取的。后续如果有ssr需求，也不会影响
 */

interface ComponentsState {
  [key: string]: any;
}

export type NotificationType = 'error' | 'info' | 'success' | 'warning';

export interface NotificationOptions {
  title: string;
  description?: string;
  /** Display duration in milliseconds. The adapter converts it if needed. */
  duration?: number;
}

export type NotificationHandler = (
  type: NotificationType,
  options: NotificationOptions,
) => void;

interface MessageState {
  copyPreferencesSuccess?: (title: string, content?: string) => void;
  notify?: NotificationHandler;
}

export interface IGlobalSharedState {
  components: ComponentsState;
  message: MessageState;
}

class GlobalShareState {
  #components: ComponentsState = {};
  #message: MessageState = {};

  /**
   * 定义框架内部各个场景的消息提示
   */
  public defineMessage({ copyPreferencesSuccess, notify }: MessageState) {
    this.#message = {
      copyPreferencesSuccess,
      notify,
    };
  }

  /**
   * Emit a framework-neutral notification. UI adapters register the concrete
   * implementation during bootstrap, so feature views never import a UI
   * library just to show a transient result.
   */
  public notify(
    type: NotificationType,
    title: string,
    description?: string,
    duration?: number,
  ) {
    this.#message.notify?.(type, { title, description, duration });
  }

  public getComponents(): ComponentsState {
    return this.#components;
  }

  public getMessage(): MessageState {
    return this.#message;
  }

  public setComponents(value: ComponentsState) {
    this.#components = value;
  }
}

export const globalShareState = new GlobalShareState();

export function notify(
  type: NotificationType,
  title: string,
  description?: string,
  duration?: number,
) {
  globalShareState.notify(type, title, description, duration);
}
