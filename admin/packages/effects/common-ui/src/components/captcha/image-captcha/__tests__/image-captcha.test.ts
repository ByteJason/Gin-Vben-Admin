import type { ImageCaptchaChallenge } from '../../types';

import { flushPromises, mount } from '@vue/test-utils';

import { describe, expect, it, vi } from 'vitest';

import ImageCaptcha from '../index.vue';

describe('ImageCaptcha', () => {
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
