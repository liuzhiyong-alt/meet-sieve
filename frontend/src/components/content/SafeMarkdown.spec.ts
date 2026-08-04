// @vitest-environment happy-dom

import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import SafeMarkdown from './SafeMarkdown.vue'

vi.mock('../../../wailsjs/runtime/runtime', () => ({ BrowserOpenURL: vi.fn() }))

describe('SafeMarkdown', () => {
  it('渲染基础 Markdown，同时阻止 HTML、脚本链接和远程图片', () => {
    const wrapper = mount(SafeMarkdown, {
      props: {
        content:
          '**重点** <img src=x onerror=alert(1)> [危险](javascript:alert(1)) ![远程图](https://example.com/a.png)',
      },
    })

    expect(wrapper.get('strong').text()).toBe('重点')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('<img src=')
    expect(wrapper.html()).not.toContain('href="javascript:')
    expect(wrapper.text()).toContain('[图片：远程图]')
  })
})
