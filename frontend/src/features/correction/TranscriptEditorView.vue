<script lang="ts" setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

import { useCorrectionStore } from '../../stores/correction'
import type { CorrectionEntry } from '../../stores/correction'
import { dirtyEditRegistry } from '../../router/dirty'

const props = defineProps<{
  meetingId: string
  meetingNo?: string
  subject?: string
}>()
const emit = defineEmits<{ back: [] }>()
const correction = useCorrectionStore()
const audio = ref<HTMLAudioElement>()
const loadingPlaybackID = ref('')
const playingPlaybackID = ref('')
const textareaElements = new Map<string, HTMLTextAreaElement>()
let stopCorrectionListener: (() => void) | undefined
let stopSpeakerListener: (() => void) | undefined
let unregisterDirty: (() => void) | undefined

interface SpeakerCluster {
  id: string
  displayNo: number
  display: string
  participantID: string
  count: number
}

const speakerClusters = computed<SpeakerCluster[]>(() => {
  const clusters = new Map<string, SpeakerCluster>()
  for (const entry of correction.entries) {
    if (!entry.speaker_cluster_id || clusters.has(entry.speaker_cluster_id))
      continue
    clusters.set(entry.speaker_cluster_id, {
      id: entry.speaker_cluster_id,
      displayNo: entry.cluster_display_no ?? 0,
      display: entry.speaker_display,
      participantID: entry.cluster_participant_id ?? '',
      count: entry.cluster_count ?? 0,
    })
  }
  return [...clusters.values()].sort(
    (left, right) => left.displayNo - right.displayNo,
  )
})

watch(
  () => correction.entries,
  () => void nextTick(resizeAllTextareas),
)

onMounted(async () => {
  await correction.load(props.meetingId)
  stopCorrectionListener = EventsOn('meeting.correction.changed', (event) => {
    if (event?.data?.meeting_id === props.meetingId) refreshFromEvent()
  })
  stopSpeakerListener = EventsOn('meeting.speaker.changed', (event) => {
    if (event?.data?.meeting_id === props.meetingId) refreshFromEvent()
  })
  unregisterDirty = dirtyEditRegistry.register({
    id: `correction-${props.meetingId}`,
    label: '原始记录修改',
    isDirty: () => correction.isDirty,
    canSave: () => correction.isDirty && !correction.saving,
    save: () => correction.saveAll(),
    discard: correction.discardDrafts,
  })
})

onBeforeUnmount(() => {
  stopCorrectionListener?.()
  stopSpeakerListener?.()
  unregisterDirty?.()
  void correction.revokeClip()
})

/** refreshFromEvent 仅在页面没有本地输入时接受其他窗口的刷新事件。 */
function refreshFromEvent(): void {
  if (!correction.isDirty && !correction.saving)
    void correction.load(props.meetingId)
}

/** sampleTime 把 16kHz 全局采样位置格式化为会议内时间。 */
function sampleTime(sample: number): string {
  const seconds = Math.max(0, Math.floor(sample / 16000))
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const values = [hours, minutes, seconds % 60].map((value) =>
    String(value).padStart(2, '0'),
  )
  return values.join(':')
}

/** clusterValue 返回顶部对应关系当前显示的 participant ID。 */
function clusterValue(cluster: SpeakerCluster): string {
  return correction.clusterDrafts[cluster.id]?.value ?? cluster.participantID
}

/** updateClusterDraft 保存顶部对应关系的本地选择，不立即写入 SQLite。 */
function updateClusterDraft(cluster: SpeakerCluster, event: Event): void {
  const entry = correction.entries.find(
    (item) => item.speaker_cluster_id === cluster.id,
  )
  const participantID = (event.target as HTMLSelectElement).value
  if (entry && participantID) correction.setClusterDraft(entry, participantID)
}

/** updateSpeakerDraft 保存单段说话人选择；与 cluster 相同则自动去掉例外。 */
function updateSpeakerDraft(entry: CorrectionEntry, event: Event): void {
  const participantID = (event.target as HTMLSelectElement).value
  if (participantID) correction.setSpeakerDraft(entry, participantID)
}

/** updateTextDraft 记录输入并让 textarea 随内容自然增长。 */
function updateTextDraft(entry: CorrectionEntry, event: Event): void {
  const textarea = event.target as HTMLTextAreaElement
  correction.setTextDraft(entry, textarea.value)
  resizeTextarea(textarea)
}

/** setTextareaElement 保存 DOM 引用，以便加载和窗口变化后重算行高。 */
function setTextareaElement(element: unknown, utteranceID: string): void {
  if (!(element instanceof HTMLTextAreaElement)) {
    textareaElements.delete(utteranceID)
    return
  }
  textareaElements.set(utteranceID, element)
  resizeTextarea(element)
}

/** resizeAllTextareas 同步全部记录的最小紧凑高度。 */
function resizeAllTextareas(): void {
  textareaElements.forEach(resizeTextarea)
}

/** resizeTextarea 用实际 scrollHeight 扩展内容，不预留多行空白。 */
function resizeTextarea(textarea: HTMLTextAreaElement): void {
  textarea.style.height = 'auto'
  textarea.style.height = `${textarea.scrollHeight}px`
}

/** playEntry 创建短期 clip；仅在媒体实际播放后更新为暂停状态。 */
async function playEntry(entry: CorrectionEntry): Promise<void> {
  if (
    loadingPlaybackID.value === entry.utterance_id ||
    playingPlaybackID.value === entry.utterance_id
  ) {
    await stopPlayback()
    return
  }
  loadingPlaybackID.value = entry.utterance_id
  playingPlaybackID.value = ''
  const url = await correction.createClip(entry)
  if (!url) {
    loadingPlaybackID.value = ''
  }
}

/** stopPlayback 停止媒体并回收当前 clip，避免页面持有过期 token。 */
async function stopPlayback(): Promise<void> {
  audio.value?.pause()
  loadingPlaybackID.value = ''
  playingPlaybackID.value = ''
  await correction.revokeClip()
}

/** handlePlaybackStarted 仅在媒体真正播放后切换行内操作文案。 */
function handlePlaybackStarted(): void {
  playingPlaybackID.value = loadingPlaybackID.value
  loadingPlaybackID.value = ''
}

/** handlePlaybackError 仅在媒体资源实际加载失败时显示错误。 */
async function handlePlaybackError(): Promise<void> {
  if (!loadingPlaybackID.value && !playingPlaybackID.value) return
  correction.errorMessage = '无法读取该片段录音，请重试。'
  await stopPlayback()
}
</script>

<template>
  <section class="ms-correction-page">
    <div class="ms-correction-head">
      <div>
        <button class="ms-link-button" type="button" @click="emit('back')">
          返回会议详情
        </button>
        <p v-if="meetingNo" class="ms-eyebrow ms-input--mono">
          {{ meetingNo }}
        </p>
        <h1>{{ subject || '校对原始记录' }}</h1>
      </div>
    </div>

    <p
      v-if="correction.errorMessage"
      class="ms-notice ms-notice--danger"
      role="alert"
    >
      {{ correction.errorMessage }}
    </p>
    <div
      v-if="correction.projectionWarning"
      class="ms-notice ms-notice--warning"
      role="alert"
    >
      <div>
        <strong>{{ correction.projectionWarning }}</strong>
        <button
          class="ms-button ms-button--quiet"
          type="button"
          @click="correction.retryProjection()"
        >
          重试刷新原始记录
        </button>
      </div>
    </div>
    <p
      v-if="correction.notice"
      class="ms-notice ms-notice--info"
      aria-live="polite"
    >
      {{ correction.notice }}
    </p>

    <section
      v-if="speakerClusters.length"
      class="ms-card ms-correction-clusters"
    >
      <div class="ms-card-head">
        <div>
          <h2>本场说话人对应关系</h2>
          <p class="ms-muted">一次指定后会应用到该说话人的所有片段。</p>
        </div>
      </div>
      <div class="ms-correction-cluster-list">
        <article
          v-for="cluster in speakerClusters"
          :key="cluster.id"
          class="ms-correction-cluster"
        >
          <div>
            <strong>{{ cluster.display }}</strong>
            <p>{{ cluster.count }} 段记录</p>
          </div>
          <select
            class="ms-input"
            :value="clusterValue(cluster)"
            :aria-label="`${cluster.display} 对应的参会人`"
            @change="updateClusterDraft(cluster, $event)"
          >
            <option v-if="!clusterValue(cluster)" value="" disabled>
              保持未知
            </option>
            <option
              v-for="participant in correction.participants"
              :key="participant.id"
              :value="participant.id"
            >
              {{ participant.display_name }}
            </option>
          </select>
        </article>
      </div>
    </section>

    <section class="ms-correction-records">
      <header class="ms-correction-records__head">
        <div>
          <h2>原始记录</h2>
          <p>点击文字即可修改，选择本段说话人即可调整。</p>
        </div>
        <button
          class="ms-button ms-button--primary"
          type="button"
          :disabled="!correction.isDirty || correction.saving"
          :aria-busy="correction.saving"
          @click="correction.saveAll()"
        >
          {{ correction.saving ? '正在保存' : '保存修改' }}
        </button>
      </header>

      <div class="ms-correction-record-list" aria-label="可编辑的原始记录">
        <article
          v-for="entry in correction.entries"
          :key="entry.utterance_id"
          class="ms-correction-record"
        >
          <header class="ms-correction-record__meta">
            <time
              >{{ sampleTime(entry.start_sample) }}–{{
                sampleTime(entry.end_sample)
              }}</time
            >
            <div>
              <button
                class="ms-button ms-button--quiet"
                type="button"
                :disabled="!entry.can_play"
                :title="entry.playback_disabled_reason"
                @click="playEntry(entry)"
              >
                {{
                  playingPlaybackID === entry.utterance_id
                    ? '暂停'
                    : loadingPlaybackID === entry.utterance_id
                      ? '正在加载'
                      : '播放'
                }}
              </button>
              <label class="ms-correction-record__speaker">
                <select
                  class="ms-input"
                  :value="correction.speakerValue(entry)"
                  :aria-label="`${sampleTime(entry.start_sample)} 的说话人`"
                  @change="updateSpeakerDraft(entry, $event)"
                >
                  <option
                    v-if="!correction.speakerValue(entry)"
                    value=""
                    disabled
                  >
                    未指定说话人
                  </option>
                  <option
                    v-for="participant in correction.participants"
                    :key="participant.id"
                    :value="participant.id"
                  >
                    {{ participant.display_name }}
                  </option>
                </select>
              </label>
            </div>
          </header>
          <textarea
            :ref="(element) => setTextareaElement(element, entry.utterance_id)"
            class="ms-correction-record__text"
            rows="1"
            :value="correction.textValue(entry)"
            :aria-label="`${sampleTime(entry.start_sample)} 的原始记录文字`"
            @input="updateTextDraft(entry, $event)"
          />
        </article>
        <p
          v-if="!correction.loading && !correction.entries.length"
          class="ms-empty-state"
        >
          本场暂无可修改的原始记录。
        </p>
      </div>
    </section>
    <audio
      v-if="correction.audioURL"
      ref="audio"
      :src="correction.audioURL"
      autoplay
      @play="handlePlaybackStarted"
      @error="handlePlaybackError"
      @ended="stopPlayback"
    />
  </section>
</template>
