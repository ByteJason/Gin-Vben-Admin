import type { ImageCaptchaChallenge } from '../../types';

import { flushPromises, mount } from '@vue/test-utils';

import { describe, expect, it, vi } from 'vitest';

import { i18n } from '@vben/locales';

import ImageCaptcha from '../index.vue';

describe('ImageCaptcha', () => {
  it('shows a localized expiry countdown and disables an expired challenge', async () => {
    vi.useFakeTimers();
    i18n.global.locale.value = 'en-US';
    i18n.global.setLocaleMessage('en-US', {
      authentication: {
        captchaExpiresIn: 'Expires in {seconds} seconds',
        captchaExpired: 'Captcha expired. Refresh to continue.',
      },
    });
    const request = vi
      .fn<() => Promise<ImageCaptchaChallenge>>()
      .mockResolvedValue({
        expiresIn: 2,
        id: 'challenge-expiring',
        kind: 'image',
        payload: 'data:image/svg+xml;base64,fixture',
      });

    const wrapper = mount(ImageCaptcha, { props: { request } });
    await flushPromises();

    expect(wrapper.text()).toContain('Expires in 2 seconds');
    await vi.advanceTimersByTimeAsync(1000);
    expect(wrapper.text()).toContain('Expires in 1 seconds');
    await vi.advanceTimersByTimeAsync(1000);
    expect(wrapper.text()).toContain('Captcha expired. Refresh to continue.');
    expect(
      wrapper.get('[data-testid="image-captcha-input"]').attributes(),
    ).toHaveProperty('disabled');

    wrapper.unmount();
    vi.useRealTimers();
  });

  it('loads a challenge, reports its id, and binds the answer', async () => {
    const request = vi
      .fn<() => Promise<ImageCaptchaChallenge>>()
      .mockResolvedValue({
        expiresIn: 120,
        id: 'challenge-1',
        kind: 'image',
        payload: 'data:image/svg+xml;base64,fixture',
      });
    const onChallengeId = vi.fn<(id: string) => void>();
    const wrapper = mount(ImageCaptcha, {
      props: {
        onChallengeId,
        request,
      },
    });

    await flushPromises();

    expect(request).toHaveBeenCalledTimes(1);
    expect(onChallengeId).toHaveBeenLastCalledWith('challenge-1');
    expect(wrapper.get('img').attributes('src')).toContain(
      'data:image/svg+xml',
    );

    const input = wrapper.get('[data-testid="image-captcha-input"]');
    await input.setValue('123456');
    expect((input.element as HTMLInputElement).value).toBe('123456');
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['123456']);
  });

  it('clears the previous id before refreshing', async () => {
    const request = vi
      .fn<() => Promise<ImageCaptchaChallenge>>()
      .mockResolvedValueOnce({
        expiresIn: 120,
        id: 'challenge-1',
        kind: 'image',
        payload: 'data:image/svg+xml;base64,fixture',
      })
      .mockResolvedValueOnce({
        expiresIn: 120,
        id: 'challenge-2',
        kind: 'image',
        payload: 'data:image/svg+xml;base64,fixture',
      });
    const onChallengeId = vi.fn<(id: string) => void>();
    const wrapper = mount(ImageCaptcha, {
      props: {
        onChallengeId,
        request,
      },
    });

    await flushPromises();
    await wrapper.get('button').trigger('click');
    await flushPromises();

    expect(onChallengeId).toHaveBeenNthCalledWith(3, '');
    expect(onChallengeId).toHaveBeenLastCalledWith('challenge-2');
    expect(request).toHaveBeenCalledTimes(2);
  });
});
