<script lang="ts" setup>
import DOMPurify from 'dompurify'
import MarkdownIt from 'markdown-it'
import { computed } from 'vue'
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime'

const props = withDefaults(
  defineProps<{
    content: string
    externalLinks?: 'desktop' | 'browser'
  }>(),
  { externalLinks: 'desktop' },
)

const markdown = new MarkdownIt({
  html: false,
  linkify: true,
  breaks: true,
  typographer: false,
})

// 图片不在消息正文中直接加载，避免局域网或外部地址泄漏访问信息。
markdown.renderer.rules.image = (tokens, index) => {
  const token = tokens[index]
  const alt = markdown.utils.escapeHtml(token.content || '图片')
  return `<span class="ms-markdown-image-placeholder">[图片：${alt}]</span>`
}

// Markdown 标题嵌入业务页面时从二级开始，避免与页面唯一 H1 冲突。
markdown.renderer.rules.heading_open = (tokens, index) => {
  const sourceLevel = Number(tokens[index].tag.slice(1)) || 1
  const level = Math.min(sourceLevel + 1, 6)
  return `<div class="ms-markdown-heading ms-markdown-heading--${level}" role="heading" aria-level="${level}">`
}
markdown.renderer.rules.heading_close = () => '</div>\n'

// 外层容器只为 DOMPurify 提供稳定解析根，返回值仍是内部 Markdown 内容。
const rendered = computed(() =>
  DOMPurify.sanitize(`<div>${markdown.render(props.content)}</div>`, {
    USE_PROFILES: { html: true },
    FORBID_TAGS: [
      'img',
      'audio',
      'video',
      'source',
      'iframe',
      'form',
      'input',
      'button',
      'select',
      'option',
      'textarea',
      'style',
    ],
    FORBID_ATTR: ['style'],
    ALLOW_DATA_ATTR: false,
  }),
)

/** openSafeLink 只允许明确的 HTTP(S) 链接，并交给平台默认浏览器。 */
function openSafeLink(event: MouseEvent): void {
  const target = (event.target as HTMLElement).closest<HTMLAnchorElement>('a')
  if (!target) return
  event.preventDefault()
  const url = new URL(target.href, window.location.href)
  if (!['http:', 'https:'].includes(url.protocol)) return
  if (props.externalLinks === 'browser') {
    window.open(url.toString(), '_blank', 'noopener,noreferrer')
    return
  }
  BrowserOpenURL(url.toString())
}
</script>

<template>
  <!-- 内容已经经过关闭 HTML 的 Markdown 解析与 DOMPurify 白名单过滤。 -->
  <!-- eslint-disable-next-line vue/no-v-html -->
  <div class="ms-markdown" @click="openSafeLink" v-html="rendered" />
</template>
