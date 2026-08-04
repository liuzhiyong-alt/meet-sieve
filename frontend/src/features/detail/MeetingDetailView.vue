<script lang="ts" setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import {
  OpenExternalLink,
  OpenResource,
  RevealResource,
} from '../../../wailsjs/go/wails/ResourceBinding'
import { useQueryStore } from '../../stores/query'
import { useDeletionStore } from '../../stores/deletion'
import { setBreadcrumbTitles } from '../../router/breadcrumb'

const props = defineProps<{ id: string }>()
const route = useRoute()
const router = useRouter()
const query = useQueryStore()
const deletion = useDeletionStore()
const deleteKind = ref<'recording' | 'meeting'>('recording')
const deleteOpen = ref(false)
const confirmation = ref('')
const actionError = ref('')
const tab = computed(() => String(route.query.tab ?? 'overview'))

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
    query: value === 'overview' ? {} : { tab: value },
  })
}
/** previewDelete 从后端获取不可扩大的真实清单摘要。 */
async function previewDelete(kind: 'recording' | 'meeting'): Promise<void> {
  deleteKind.value = kind
  confirmation.value = ''
  if (
    await (kind === 'recording'
      ? deletion.previewRecording(props.id)
      : deletion.previewMeeting(props.id))
  )
    deleteOpen.value = true
}
/** confirmDelete 二次确认后执行删除；整场必须手工输入会议号。 */
async function confirmDelete(): Promise<void> {
  const ok =
    deleteKind.value === 'recording'
      ? await deletion.deleteRecording()
      : await deletion.deleteMeeting(confirmation.value.trim())
  if (!ok) return
  deleteOpen.value = false
  if (deletion.job?.state === 'failed')
    await router.push(`/meetings/${props.id}/delete-recovery`)
  else if (deleteKind.value === 'meeting') await router.replace('/meetings')
  else await query.loadDetail(props.id)
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
/** turnSeq 使用真实 seq 边界，不在前端猜测 offset。 */
function turnSeq(after: number, before: number): void {
  void router.push({
    path: route.path,
    query: {
      tab: tab.value,
      after: after || undefined,
      before: before || undefined,
    },
  })
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
      <span class="ms-status-pill">{{
        query.detail.summary.highest_status
      }}</span>
    </section>
    <nav class="ms-tabs" aria-label="会议详情页签">
      <button
        v-for="item in [
          ['overview', '概览'],
          ['transcript', '原始记录'],
          ['messages', '消息与资料'],
          ['minutes', '会议纪要'],
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
    <section v-if="tab === 'overview'" class="ms-detail-grid">
      <article class="ms-card ms-settings-card">
        <h2>状态</h2>
        <dl class="ms-fact-grid">
          <div>
            <dt>本地保存</dt>
            <dd>{{ query.detail.summary.local_save_state }}</dd>
          </div>
          <div>
            <dt>实时转写</dt>
            <dd>{{ query.detail.summary.realtime_asr_state }}</dd>
          </div>
          <div>
            <dt>缺口</dt>
            <dd>{{ query.detail.summary.gap_state }}</dd>
          </div>
          <div>
            <dt>Codex</dt>
            <dd>{{ query.detail.summary.agent_state }}</dd>
          </div>
          <div>
            <dt>纪要</dt>
            <dd>{{ query.detail.summary.minute_state }}</dd>
          </div>
          <div>
            <dt>LAN</dt>
            <dd>{{ query.detail.summary.lan_state }}</dd>
          </div>
        </dl>
        <p v-if="query.detail.disabled_reason" class="ms-help">
          {{ query.detail.disabled_reason }}
        </p>
      </article>
      <article class="ms-card ms-settings-card">
        <h2>会议内容</h2>
        <div class="ms-actions">
          <RouterLink
            class="ms-button ms-button--quiet"
            :to="{ path: route.path, query: { tab: 'transcript' } }"
            >查看原始记录</RouterLink
          ><RouterLink
            class="ms-button ms-button--quiet"
            :to="`/meetings/${props.id}/minutes?no=${encodeURIComponent(query.detail.summary.meeting_no)}`"
            >打开会议纪要</RouterLink
          >
        </div>
      </article>
      <article class="ms-card ms-settings-card ms-danger-zone">
        <h2>危险操作</h2>
        <p class="ms-help">
          删除不会进入废纸篓。录音删除会保留逐字稿、纪要和资料。
        </p>
        <div class="ms-actions">
          <button
            class="ms-button ms-button--quiet"
            :disabled="!query.detail.can_delete_recording"
            @click="previewDelete('recording')"
          >
            删除录音</button
          ><button
            class="ms-button ms-button--danger"
            :disabled="!query.detail.can_delete_meeting"
            @click="previewDelete('meeting')"
          >
            永久删除整场会议
          </button>
        </div>
      </article>
    </section>
    <section v-else-if="tab === 'transcript'" class="ms-card ms-settings-card">
      <div v-if="query.transcript?.items.length" class="ms-transcript-list">
        <article
          v-for="item in query.transcript.items"
          :key="item.seq"
          class="ms-list-item"
        >
          <span
            ><strong>{{ item.speaker_name || item.kind }}</strong
            ><small class="ms-muted">序号 {{ item.seq }}</small></span
          >
          <p>{{ item.text || '无文字事件' }}</p>
        </article>
      </div>
      <div v-else class="ms-empty-state">
        <h2>没有原始记录</h2>
        <p>录音仍可能已保存在本地。</p>
      </div>
      <div class="ms-card-foot">
        <button
          class="ms-button ms-button--quiet"
          :disabled="!query.transcript?.before_seq"
          @click="turnSeq(0, query.transcript?.before_seq || 0)"
        >
          较新</button
        ><button
          class="ms-button ms-button--quiet"
          :disabled="!query.transcript?.has_more"
          @click="turnSeq(query.transcript?.after_seq || 0, 0)"
        >
          更早
        </button>
      </div>
    </section>
    <section v-else-if="tab === 'messages'" class="ms-card ms-settings-card">
      <div v-if="query.content?.items.length" class="ms-people-list">
        <article
          v-for="item in query.content.items"
          :key="`${item.kind}-${item.entity_id}`"
          class="ms-list-item"
        >
          <span
            ><strong>{{
              item.display_name || item.resource_name || item.kind
            }}</strong
            ><small class="ms-muted">{{
              item.text || item.display_url || item.resource_state
            }}</small></span
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
      <div v-else class="ms-empty-state"><h2>没有消息或资料</h2></div>
    </section>
    <section v-else class="ms-card ms-empty-state">
      <h2>会议纪要</h2>
      <p>纪要版本在独立工作区中编辑和确认。</p>
      <RouterLink
        class="ms-button ms-button--primary"
        :to="`/meetings/${props.id}/minutes?no=${encodeURIComponent(query.detail.summary.meeting_no)}`"
        >打开纪要</RouterLink
      >
    </section>
  </template>

  <Teleport to="body"
    ><div
      v-if="deleteOpen && deletion.preview"
      class="ms-modal-backdrop"
      @click.self="deleteOpen = false"
    >
      <section
        class="ms-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="delete-title"
      >
        <h2 id="delete-title">
          {{ deleteKind === 'meeting' ? '永久删除整场会议' : '删除本地录音' }}
        </h2>
        <p>
          将删除 {{ deletion.preview.file_count }} 个文件，共
          {{ (deletion.preview.size_bytes / 1048576).toFixed(1) }} MB<span
            v-if="deletion.preview.unknown_count"
            >，其中 {{ deletion.preview.unknown_count }} 项不是已登记资产</span
          >。
        </p>
        <label v-if="deleteKind === 'meeting'" class="ms-field"
          ><span>输入会议号 {{ deletion.preview.meeting_no }} 确认</span
          ><input
            v-model="confirmation"
            class="ms-input ms-input--mono"
            autocomplete="off"
        /></label>
        <p
          v-if="deletion.errorMessage"
          class="ms-notice ms-notice--danger"
          role="alert"
        >
          {{ deletion.errorMessage }}
        </p>
        <div class="ms-modal-actions">
          <button
            class="ms-button ms-button--quiet"
            autofocus
            @click="deleteOpen = false"
          >
            取消</button
          ><button
            class="ms-button ms-button--danger"
            :disabled="
              deletion.loading ||
              (deleteKind === 'meeting' &&
                confirmation.trim() !== deletion.preview.meeting_no)
            "
            @click="confirmDelete"
          >
            确认删除
          </button>
        </div>
      </section>
    </div></Teleport
  >
</template>
