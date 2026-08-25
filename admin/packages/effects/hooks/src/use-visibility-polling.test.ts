import { createApp, defineComponent } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useVisibilityPolling } from './use-visibility-polling';

describe('useVisibilityPolling', () => {
  let visibility: DocumentVisibilityState;

  beforeEach(() => {
    vi.useFakeTimers();
    visibility = 'visible';
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => visibility,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    delete (document as any).visibilityState;
  });

  it('pauses in background and refreshes exactly once when visible again', async () => {
    const refresh = vi.fn();
    const host = document.createElement('div');
    const app = createApp(
      defineComponent({
        setup() {
          useVisibilityPolling(refresh, 15_000);
          return () => null;
        },
      }),
    );
    document.body.append(host);
    app.mount(host);

    expect(refresh).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(15_000);
    expect(refresh).toHaveBeenCalledTimes(2);

    visibility = 'hidden';
    document.dispatchEvent(new Event('visibilitychange'));
    await vi.advanceTimersByTimeAsync(45_000);
    expect(refresh).toHaveBeenCalledTimes(2);

    visibility = 'visible';
    document.dispatchEvent(new Event('visibilitychange'));
    document.dispatchEvent(new Event('visibilitychange'));
    expect(refresh).toHaveBeenCalledTimes(3);
    await vi.advanceTimersByTimeAsync(15_000);
    expect(refresh).toHaveBeenCalledTimes(4);

    app.unmount();
    host.remove();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(refresh).toHaveBeenCalledTimes(4);
  });
});
