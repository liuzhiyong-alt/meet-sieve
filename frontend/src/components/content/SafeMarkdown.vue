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

const rendered = computed(() =>
  DOMPurify.sanitize(markdown.render(props.content), {
    ALLOWED_TAGS: [
      'p',
      'br',
      'strong',
      'em',
      's',
      'blockquote',
      'ul',
      'ol',
      'li',
      'code',
      'pre',
      'a',
      'hr',
      'h1',
      'h2',
      'h3',
      'h4',
      'span',
    ],
    ALLOWED_ATTR: ['href', 'title', 'class'],
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
