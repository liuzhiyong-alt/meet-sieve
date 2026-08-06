<script lang="ts" setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import type { wails } from '../../../wailsjs/go/models'
import {
  OpenExternalLink,
  OpenResource,
  RevealResource,
} from '../../../wailsjs/go/wails/ResourceBinding'
import { useQueryStore } from '../../stores/query'
import { useDeletionStore } from '../../stores/deletion'
import { useMinutesStore } from '../../stores/minutes'
import { setBreadcrumbTitles } from '../../router/breadcrumb'
import SafeMarkdown from '../../components/content/SafeMarkdown.vue'
import CodexHandoffDialog from './CodexHandoffDialog.vue'
import SeqCursorPagination from './SeqCursorPagination.vue'

const props = defineProps<{ id: string }>()
const route = useRoute()
const router = useRouter()
const query = useQueryStore()
const deletion = useDeletionStore()
const minutes = useMinutesStore()
const actionError = ref('')
const codexHandoffOpen = ref(false)
const detailTabs = ['transcript', 'minutes', 'messages'] as const
const tab = computed(() => {
  const value = String(route.query.tab ?? 'transcript')
  return detailTabs.includes(value as (typeof detailTabs)[number])
    ? value
    : 'transcript'
})
const pageNumber = computed(() =>
  Math.max(1, Number(route.query.page ?? 1) || 1),
)

const transcriptKindLabels: Record<string, string> = {
  'asr.gap': '转写缺口',
  'message.created': '会议消息',
  'resource.created': '会议资料',
  'ai.question': 'AI 问题',
  'ai.answer': 'AI 回答',
  'ai.cancelled': 'AI 回答已取消',
  'ai.failed': 'AI 回答失败',
}

const resourceStateLabels: Record<string, string> = {
  ready: '等待上传',
  uploading: '正在上传',
  processing: '正在校验',
  completed: '可打开',
  verified: '完整性已验证',
  missing: '文件缺失',
  changed: '文件内容已变化',
  outside_workspace: '文件位置异常',
  unavailable: '文件暂不可用',
  cancelled: '上传已取消',
  failed: '上传失败',
}

/** loadTab 按 URL 页签懒加载对应长列表。 */
async function loadTab(): Promise<void> {
  if (tab.value === 'transcript')
    await query.loadTranscript(
      props.id,
      Number(route.query.after ?? 0),
      Number(route.query.before ?? 0),
    )
  if (tab.value === 'messages')
    await query.loadContent(
      props.id,
      Number(route.query.after ?? 0),
      Number(route.query.before ?? 0),
    )
  if (tab.value === 'minutes') await minutes.refresh(props.id)
}
/** load 先读摘要，不存在时回记录页并携带一次性提示。 */
async function load(): Promise<void> {
  if (!(await query.loadDetail(props.id))) {
    if (query.errorCode.includes('NOT_FOUND'))
      await router.replace({
        path: '/meetings',
        query: { notice: 'meeting-not-found' },
      })
    return
  }
  if (query.detail) {
    setBreadcrumbTitles(route.path, {
      current: query.detail.summary.subject || query.detail.summary.meeting_no,
    })
  }
  const active = await deletion.loadJob(props.id)
  if (active && deletion.job?.state === 'failed')
    await router.replace(`/meetings/${props.id}/delete-recovery`)
  await loadTab()
}
/** selectTab 把详情页签写入 URL 并清除翻页边界。 */
function selectTab(value: string): void {
  void router.push({
    path: route.path,
    query: value === 'transcript' ? {} : { tab: value },
  })
}

/** generateMinutes 主动生成首份纪要并刷新详情状态。 */
async function generateMinutes(): Promise<void> {
  const gapState = query.detail?.summary.gap_state ?? 'none'
  if (
    await minutes.generate(
      props.id,
      gapState !== 'none' && gapState !== 'completed',
    )
  ) {
    await query.loadDetail(props.id)
  }
}
/** openResource 只响应用户点击，并由后端重读完整性事实。 */
async function openResource(
  id: string,
  kind: string,
  reveal = false,
): Promise<void> {
  const result =
    kind === 'link'
      ? await OpenExternalLink(id)
      : reveal
        ? await RevealResource(id)
        : await OpenResource(id)
  actionError.value = result.code === 200 ? '' : result.message
  if (result.code !== 200) await query.loadContent(props.id)
}
/** turnSeq 使用真实 seq 边界翻页，并记录仅用于展示的当前页码。 */
function turnSeq(after: number, before: number, direction: -1 | 1): void {
  void router.push({
    path: route.path,
    query: {
      tab: tab.value,
      after: after || undefined,
      before: before || undefined,
      page: Math.max(1, pageNumber.value + direction),
    },
  })
}

/** transcriptItemLabel 返回说话人或中文事件名称，不暴露内部 kind。 */
function transcriptItemLabel(item: wails.TranscriptItemDTO): string {
  if (item.kind === 'utterance.final')
    return item.speaker_display || '未识别说话人'
  return transcriptKindLabels[item.kind] ?? '会议事件'
}

/** meetingOffset 把事件绝对时间转换为会议内 HH:MM:SS。 */
function meetingOffset(occurredAt: number): string {
  const startedAt = query.detail?.summary.started_at ?? occurredAt
  const seconds = Math.max(0, Math.floor((occurredAt - startedAt) / 1000))
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return [hours, minutes, seconds % 60]
    .map((value) => String(value).padStart(2, '0'))
    .join(':')
}

/** contentTitle 根据会议内容类型生成稳定的中文标题。 */
function contentTitle(item: wails.ContentItemDTO): string {
  if (item.kind === 'ai.answer') return 'AI 回答'
  if (item.resource_kind === 'attachment') return item.resource_name || '附件'
  if (item.resource_kind === 'link') return item.hostname || '链接'
  return item.display_name || '会议消息'
}

/** contentDetail 返回正文、脱敏链接或中文资料状态。 */
function contentDetail(item: wails.ContentItemDTO): string {
  return (
    item.text ||
    item.display_url ||
    resourceStateLabels[item.resource_state ?? ''] ||
    '没有可展示的内容'
  )
}

/** contentMeta 组合内容类型、可选发送人和会议内时间。 */
function contentMeta(item: wails.ContentItemDTO): string {
  const type =
    item.kind === 'ai.answer'
      ? 'AI 回答'
      : item.resource_kind === 'attachment'
        ? '附件'
        : item.resource_kind === 'link'
          ? '链接'
          : '会议消息'
  const sender =
    item.resource_kind && item.display_name ? item.display_name : ''
  return [type, sender, meetingOffset(item.occurred_at)]
    .filter(Boolean)
    .join(' · ')
}
onMounted(load)
watch(
  () => route.fullPath,
  () => void loadTab(),
)
</script>

<template>
  <p v-if="query.errorMessage" class="ms-notice ms-notice--danger" role="alert">
    {{ query.errorMessage }}
  </p>
  <section
    v-if="query.loading && !query.detail"
    class="ms-card ms-empty-state"
    aria-busy="true"
  >
    <h2>正在读取会议详情…</h2>
  </section>
  <template v-else-if="query.detail">
    <section class="ms-page-head">
      <div>
        <RouterLink class="ms-link-button" to="/meetings"
          >返回会议记录</RouterLink
        >
        <p class="ms-eyebrow">{{ query.detail.summary.meeting_no }}</p>
        <h1>{{ query.detail.summary.subject || '未命名会议' }}</h1>
        <p>
          {{ query.detail.summary.participants.join('、') || '未登记参会人' }}
        </p>
      </div>
      <div class="ms-page-head__actions">
        <button
          class="ms-button ms-button--quiet"
          type="button"
          @click="codexHandoffOpen = true"
        >
          用 Codex 继续
        </button>
      </div>
    </section>
    <nav class="ms-tabs" aria-label="会议详情页签">
      <button
        v-for="item in [
          ['transcript', '原始记录'],
          ['minutes', '会议纪要'],
          ['messages', '消息与资料'],
        ]"
        :key="item[0]"
        :class="{ 'is-current': tab === item[0] }"
        @click="selectTab(item[0])"
      >
        {{ item[1] }}
      </button>
    </nav>
    <p v-if="actionError" class="ms-notice ms-notice--danger" role="alert">
      {{ actionError }}
    </p>
    <section v-if="tab === 'transcript'" class="ms-detail-panel ms-card">
      <div class="ms-detail-panel__head">
        <div>
          <h2>原始记录</h2>
          <p>按会议时间顺序</p>
        </div>
        <RouterLink
          class="ms-button ms-button--quiet"
          :to="{
            name: 'meeting-transcript',
            params: { id: props.id },
            query: {
              no: query.detail.summary.meeting_no,
              subject: query.detail.summary.subject,
            },
          }"
          >编辑原始记录</RouterLink
        >
      </div>
      <div
        v-if="query.transcript?.items.length"
        class="ms-detail-list ms-transcript-list"
      >
        <article
          v-for="item in query.transcript.items"
          :key="item.seq"
          class="ms-list-item"
        >
          <span>
            <strong>{{ transcriptItemLabel(item) }}</strong>
            <small class="ms-muted">{{
              item.text || '该事件没有可展示的文字内容'
            }}</small>
            <small class="ms-status">{{
              meetingOffset(item.occurred_at)
            }}</small>
          </span>
        </article>
      </div>
      <div v-else class="ms-detail-panel__empty">
        <h2>没有原始记录</h2>
        <p>录音仍可能已保存在本地。</p>
      </div>
      <SeqCursorPagination
        :has-previous="query.transcript?.has_previous ?? false"
        :has-next="query.transcript?.has_next ?? false"
        :page-number="pageNumber"
        :current-count="query.transcript?.items.length ?? 0"
        :loading="query.loading"
        @previous="turnSeq(0, query.transcript?.before_seq || 0, -1)"
        @next="turnSeq(query.transcript?.after_seq || 0, 0, 1)"
      />
    </section>
    <section v-else-if="tab === 'messages'" class="ms-detail-panel ms-card">
      <div class="ms-detail-panel__head">
        <div>
          <h2>消息与资料</h2>
          <p>会议消息、AI 回答、链接与附件</p>
        </div>
      </div>
      <div v-if="query.content?.items.length" class="ms-detail-list">
        <article
          v-for="item in query.content.items"
          :key="`${item.kind}-${item.entity_id}`"
          class="ms-list-item"
        >
          <span
            ><strong>{{ contentTitle(item) }}</strong
            ><small class="ms-muted">{{ contentDetail(item) }}</small
            ><small class="ms-status">{{ contentMeta(item) }}</small></span
          ><span v-if="item.resource_kind" class="ms-actions-inline"
            ><button
              class="ms-button ms-button--quiet"
              @click="openResource(item.entity_id, item.resource_kind)"
            >
              {{ item.resource_kind === 'link' ? '打开链接' : '打开' }}</button
            ><button
              v-if="item.resource_kind === 'attachment'"
              class="ms-button ms-button--quiet"
              @click="openResource(item.entity_id, item.resource_kind, true)"
            >
              在文件夹中显示
            </button></span
          >
        </article>
      </div>
      <div v-else class="ms-detail-panel__empty">
        <h2>没有消息或资料</h2>
      </div>
      <SeqCursorPagination
        :has-previous="query.content?.has_previous ?? false"
        :has-next="query.content?.has_next ?? false"
        :page-number="pageNumber"
        :current-count="query.content?.items.length ?? 0"
        :loading="query.loading"
        @previous="turnSeq(0, query.content?.before_seq || 0, -1)"
        @next="turnSeq(query.content?.after_seq || 0, 0, 1)"
      />
    </section>
    <section v-else class="ms-detail-panel ms-card">
      <div class="ms-detail-panel__head">
        <div>
          <h2>会议纪要</h2>
          <p>Markdown 预览</p>
        </div>
        <RouterLink
          v-if="minutes.projection.current"
          class="ms-button ms-button--quiet"
          :to="`/meetings/${props.id}/minutes?no=${encodeURIComponent(query.detail.summary.meeting_no)}`"
          >编辑</RouterLink
        >
      </div>
      <p
        v-if="minutes.errorMessage"
        class="ms-notice ms-notice--danger"
        role="alert"
      >
        {{ minutes.errorMessage }}
      </p>
      <template v-if="minutes.projection.current">
        <SafeMarkdown :content="minutes.projection.current.content_markdown" />
      </template>
      <div v-else class="ms-detail-panel__empty">
        <h2>尚未生成会议纪要</h2>
        <p>生成时会使用设置中保存的会议纪要要求。</p>
        <div class="ms-detail-panel__empty-actions">
          <button
            class="ms-button ms-button--primary"
            type="button"
            :disabled="
              query.detail.summary.gap_state === 'processing' ||
              minutes.processing
            "
            @click="generateMinutes"
          >
            {{ minutes.processing ? '正在生成…' : '生成会议纪要' }}
          </button>
          <button
            v-if="minutes.processing"
            class="ms-button ms-button--quiet"
            type="button"
            @click="minutes.stop()"
          >
            停止生成
          </button>
        </div>
        <p
          v-if="query.detail.summary.gap_state === 'processing'"
          class="ms-help"
        >
          补转写仍在处理，暂时不能生成会议纪要。
        </p>
      </div>
    </section>
    <CodexHandoffDialog
      v-model:open="codexHandoffOpen"
      :meeting-id="props.id"
    />
  </template>
</template>
