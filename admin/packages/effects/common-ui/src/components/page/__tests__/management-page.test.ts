import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import ManagementPage from '../management-page.vue';

describe('ManagementPage', () => {
  it('provides one full-width non-main page region with accessible state', () => {
    const wrapper = mount(ManagementPage, {
      props: {
        busy: true,
        labelledby: 'page-title',
      },
      slots: {
        default: '<h1 id="page-title">Users</h1>',
      },
    });

    expect(wrapper.element.tagName).toBe('DIV');
    expect(wrapper.classes()).toContain('management-page');
    expect(wrapper.attributes('aria-busy')).toBe('true');
    expect(wrapper.attributes('aria-labelledby')).toBe('page-title');
    expect(wrapper.find('main').exists()).toBe(false);
    expect(wrapper.text()).toContain('Users');
  });
});
