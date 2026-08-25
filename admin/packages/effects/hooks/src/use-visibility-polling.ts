import { onBeforeUnmount, onMounted } from 'vue';

type PollingCallback = () => Promise<void> | void;

/**
 * Poll only while the document is visible.
 *
 * Returning to a visible tab triggers one immediate refresh before the regular
 * interval resumes. Repeated visibility events for the same state are ignored.
 */
export function useVisibilityPolling(
  callback: PollingCallback,
  intervalMs = 15_000,
) {
  let timer: number | undefined;
  let visible = false;

  function stop() {
    if (timer === undefined) return;
    window.clearInterval(timer);
    timer = undefined;
  }

  function start() {
    stop();
    if (!visible) return;
    timer = window.setInterval(() => void callback(), intervalMs);
  }

  function handleVisibilityChange() {
    const nextVisible = document.visibilityState === 'visible';
    if (nextVisible === visible) return;
    visible = nextVisible;

    if (!visible) {
      stop();
      return;
    }

    void callback();
    start();
  }

  onMounted(() => {
    document.addEventListener('visibilitychange', handleVisibilityChange);
    visible = document.visibilityState === 'visible';
    if (!visible) return;
    void callback();
    start();
  });

  onBeforeUnmount(() => {
    stop();
    document.removeEventListener('visibilitychange', handleVisibilityChange);
  });
}
