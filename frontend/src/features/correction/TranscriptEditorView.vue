<script lang="ts" setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

import { useCorrectionStore } from '../../stores/correction'
import type { CorrectionEntry } from '../../stores/correction'

const props = defineProps<{
  meetingId: string
  meetingNo?: string
  subject?: string
}>()
const emit = defineEmits<{ back: [] }>()
const correction = useCorrectionStore()
const tab = ref<'single' | 'cluster'>('single')
const textDraft = ref('')
const participantDraft = ref('')
const batchParticipant = ref('')
const batchConfirm = ref(false)
const enrollmentConfirm = ref(false)
const modalCancel = ref<HTMLButtonElement>()
const modalRoot = ref<HTMLElement>()
const singleTab = ref<HTMLButtonElement>()
const clusterTab = ref<HTMLButtonElement>()
let modalReturnFocus: HTMLElement | null = null
let stopCorrectionListener: (() => void) | undefined
let stopSpeakerListener: (() => void) | undefined

const selected = computed(() => correction.selected)
const canOpenCluster = computed(
  () => !!selected.value?.speaker_cluster_id && !!selected.value.cluster_count,
)

watch(
  selected,
  async (entry) => {
    await correction.revokeClip()
    textDraft.value = entry?.current_text ?? ''
    participantDraft.value = entry?.current_participant_id ?? ''
    batchParticipant.value = ''
    tab.value = 'single'
  },
  { immediate: true },
)

watch([batchConfirm, enrollmentConfirm], async ([batch, enrollment]) => {
  if (batch || enrollment) {
    modalReturnFocus = document.activeElement as HTMLElement | null
    await nextTick()
    modalCancel.value?.focus()
  } else if (modalReturnFocus) {
    await nextTick()
    modalReturnFocus.focus()
    modalReturnFocus = null
  }
})

onMounted(async () => {
  await correction.load(props.meetingId)
  stopCorrectionListener = EventsOn('meeting.correction.changed', (event) => {
    if (event?.data?.meeting_id === props.meetingId)
      void correction.load(props.meetingId)
  })
  stopSpeakerListener = EventsOn('meeting.speaker.changed', (event) => {
    if (event?.data?.meeting_id === props.meetingId)
      void correction.load(props.meetingId)
  })
})

onBeforeUnmount(() => {
  stopCorrectionListener?.()
  stopSpeakerListener?.()
  void correction.revokeClip()
})

/** selectEntry 切换当前片段并保持列表键盘按钮语义。 */
function selectEntry(entry: CorrectionEntry): void {
  correction.selectedID = entry.utterance_id
}

/** sampleTime 把 16kHz 全局采样位置格式化为 MM:SS。 */
function sampleTime(sample: number): string {
  const seconds = Math.max(0, Math.floor(sample / 16000))
  return `${String(Math.floor(seconds / 60)).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`
}

/** saveSingle 分别保存发生变化的文字和说话人，任一步冲突均保留草稿。 */
async function saveSingle(): Promise<void> {
  const entry = selected.value
  if (!entry || correction.saving) return
  if (textDraft.value.trim() !== entry.current_text) {
    if (!(await correction.saveText(entry, textDraft.value))) return
  }
  const refreshed = correction.selected
  if (
    refreshed &&
    participantDraft.value &&
    participantDraft.value !== refreshed.current_participant_id
  ) {
    await correction.saveSpeaker(refreshed, participantDraft.value)
  }
}

/** playSelected 创建短期 clip 并交给原生 audio 控件播放。 */
async function playSelected(): Promise<void> {
  if (selected.value) await correction.createClip(selected.value)
}

/** confirmBatch 在二次确认后提交绑定 revision/count 的 cluster 命令。 */
async function confirmBatch(): Promise<void> {
  if (!selected.value || !batchParticipant.value) return
  batchConfirm.value = false
  await correction.saveCluster(selected.value, batchParticipant.value)
}

/** confirmEnrollment 独立提交永久声纹请求，不与校对保存绑定。 */
async function confirmEnrollment(): Promise<void> {
  if (!selected.value) return
  enrollmentConfirm.value = false
  await correction.enrollSelected(selected.value)
}

/** closeModal 统一关闭对话框，并由 watcher 把焦点返回触发按钮。 */
function closeModal(): void {
  batchConfirm.value = false
  enrollmentConfirm.value = false
}

/** handleModalKeydown 将 Tab 焦点限制在当前对话框内。 */
function handleModalKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    closeModal()
    return
  }
  if (event.key !== 'Tab' || !modalRoot.value) return
  const focusable = Array.from(
    modalRoot.value.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  )
  if (!focusable.length) return
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

/** handleTabKeydown 支持标准 Tabs 的方向键、Home 和 End 导航。 */
function handleTabKeydown(event: KeyboardEvent): void {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const selectCluster = event.key === 'ArrowRight' || event.key === 'End'
  if (selectCluster && canOpenCluster.value) {
    tab.value = 'cluster'
    clusterTab.value?.focus()
    return
  }
  tab.value = 'single'
  singleTab.value?.focus()
}
</script>

<template>
  <section class="ms-correction-page">
    <div class="ms-correction-head">
      <div>
        <button class="ms-link-button" type="button" @click="emit('back')">
          ← 返回会议详情
        </button>
        <p class="ms-eyebrow ms-input--mono">{{ meetingNo }}</p>
        <h1>校对原始记录</h1>
        <p class="ms-lead">
          修改文字或本场说话人。原始 ASR 与每次修改都会保留在本地数据库中。
        </p>
      </div>
      <span class="ms-status-pill">{{
        correction.loading ? '正在加载' : '可校对'
      }}</span>
    </div>

    <div
      v-if="correction.errorMessage"
      class="ms-notice ms-notice--danger"
      role="alert"
    >
      <div>
        <strong>操作未完成</strong>
        <p>{{ correction.errorMessage }}</p>
      </div>
    </div>
    <div
      v-if="correction.projectionWarning"
      class="ms-notice ms-notice--warning"
      role="alert"
    >
      <div>
        <strong>{{ correction.projectionWarning }}</strong>
        <p>SQLite 中的校对事实已经保存，不会重复创建校对记录。</p>
        <button
          class="ms-button ms-button--quiet"
          type="button"
          @click="correction.retryProjection()"
        >
          重试刷新原始记录
        </button>
      </div>
    </div>
    <div
      v-if="correction.speakerState === 'profile_missing'"
      class="ms-notice ms-notice--warning"
      role="status"
    >
      <div>
        <strong>自动说话人识别尚未启用</strong>
        <p>
          当前模型缺少经过真实校准的匹配档案；文字和人工说话人校对仍可使用。
        </p>
      </div>
    </div>
    <p
      v-if="correction.notice"
      class="ms-notice ms-notice--info"
      aria-live="polite"
    >
      {{ correction.notice }}
    </p>

    <section class="ms-correction-layout">
      <article class="ms-card ms-correction-list-card">
        <div class="ms-card-head">
          <div>
            <p class="ms-eyebrow">原始记录</p>
            <h2>{{ subject || '会议原始记录' }}</h2>
          </div>
          <span>{{ correction.entries.length }} 段</span>
        </div>
        <div class="ms-correction-list" aria-label="可校对的原始记录">
          <button
            v-for="entry in correction.entries"
            :key="entry.utterance_id"
            class="ms-correction-entry"
            :class="{
              'is-selected': entry.utterance_id === correction.selectedID,
            }"
            type="button"
            @click="selectEntry(entry)"
          >
            <span class="ms-correction-entry__meta"
              >{{ sampleTime(entry.start_sample) }} ·
              {{
                entry.assignment_source.startsWith('manual')
                  ? '已人工校对'
                  : '当前记录'
              }}</span
            >
            <span
              ><strong>{{ entry.speaker_display }}</strong
              >：{{ entry.current_text }}</span
            >
          </button>
          <p
            v-if="!correction.loading && !correction.entries.length"
            class="ms-lead"
          >
            本场暂无可校对的最终转写。
          </p>
        </div>
      </article>

      <aside v-if="selected" class="ms-stack">
        <article class="ms-card ms-correction-panel">
          <div class="ms-card-head">
            <div>
              <p class="ms-eyebrow">
                {{ sampleTime(selected.start_sample) }}–{{
                  sampleTime(selected.end_sample)
                }}
              </p>
              <h2>校对选中片段</h2>
            </div>
            <button
              class="ms-button ms-button--quiet"
              type="button"
              :disabled="!selected.can_play"
              :title="selected.playback_disabled_reason"
              @click="playSelected"
            >
              播放片段
            </button>
          </div>
          <audio
            v-if="correction.audioURL"
            class="ms-correction-audio"
            :src="correction.audioURL"
            aria-label="选中转写片段录音"
            controls
            autoplay
          />
          <div
            class="ms-tabs"
            role="tablist"
            aria-label="校对范围"
            @keydown="handleTabKeydown"
          >
            <button
              id="correction-tab-single"
              ref="singleTab"
              class="ms-tab"
              :class="{ 'is-current': tab === 'single' }"
              role="tab"
              :aria-selected="tab === 'single'"
              aria-controls="correction-panel-single"
              :tabindex="tab === 'single' ? 0 : -1"
              @click="tab = 'single'"
            >
              单个片段
            </button>
            <button
              id="correction-tab-cluster"
              ref="clusterTab"
              class="ms-tab"
              :class="{ 'is-current': tab === 'cluster' }"
              role="tab"
              :aria-selected="tab === 'cluster'"
              aria-controls="correction-panel-cluster"
              :tabindex="tab === 'cluster' ? 0 : -1"
              :disabled="!canOpenCluster"
              @click="tab = 'cluster'"
            >
              本场同一说话人
            </button>
          </div>

          <div
            v-if="tab === 'single'"
            id="correction-panel-single"
            role="tabpanel"
            aria-labelledby="correction-tab-single"
          >
            <label class="ms-field"
              ><span>原始 ASR</span
              ><textarea
                class="ms-textarea"
                :value="selected.original_text"
                readonly
              />
            </label>
            <label class="ms-field"
              ><span>当前文字</span
              ><textarea v-model="textDraft" class="ms-textarea" />
            </label>
            <label class="ms-field"
              ><span>当前说话人</span
              ><select v-model="participantDraft" class="ms-input">
                <option value="">请选择本场参会者</option>
                <option
                  v-for="participant in correction.participants"
                  :key="participant.id"
                  :value="participant.id"
                >
                  {{ participant.kind === 'temporary' ? '临时参会者 · ' : ''
                  }}{{ participant.display_name }}
                </option>
              </select></label
            >
            <div class="ms-row-between">
              <button
                class="ms-button ms-button--quiet"
                type="button"
                :disabled="!selected.can_enroll"
                :title="selected.enrollment_disabled_reason"
                @click="enrollmentConfirm = true"
              >
                加入声纹样本
              </button>
              <button
                class="ms-button ms-button--primary"
                type="button"
                :disabled="correction.saving"
                @click="saveSingle"
              >
                {{ correction.saving ? '正在保存…' : '保存片段修改' }}
              </button>
            </div>
          </div>

          <div
            v-else
            id="correction-panel-cluster"
            role="tabpanel"
            aria-labelledby="correction-tab-cluster"
          >
            <div class="ms-notice ms-notice--warning">
              <div>
                <strong
                  >将修改“{{ selected.speaker_display }}”的
                  {{ selected.cluster_count }} 个片段</strong
                >
                <p>
                  包括此前单独修改过的说话人；后续归入该聚类的片段也会使用新身份。
                </p>
              </div>
            </div>
            <label class="ms-field"
              ><span>修改为</span
              ><select v-model="batchParticipant" class="ms-input">
                <option value="">请选择本场参会者</option>
                <option
                  v-for="participant in correction.participants"
                  :key="participant.id"
                  :value="participant.id"
                >
                  {{ participant.display_name }}
                </option>
              </select></label
            >
            <div class="ms-actions">
              <button
                class="ms-button ms-button--primary"
                type="button"
                :disabled="!batchParticipant || correction.saving"
                @click="batchConfirm = true"
              >
                修改本场 {{ selected.cluster_count }} 个片段
              </button>
            </div>
          </div>
        </article>
        <div class="ms-notice ms-notice--info">
          <div>
            <strong>声纹只用于辅助判断</strong>
            <p>
              低置信度、证据过短或重叠语音会保留为未知。校对不会自动加入永久声纹资料。
            </p>
          </div>
        </div>
      </aside>
    </section>
  </section>

  <Teleport to="body">
    <div
      v-if="batchConfirm || enrollmentConfirm"
      class="ms-modal-backdrop"
      @click.self="closeModal"
    >
      <section
        ref="modalRoot"
        class="ms-modal"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="batchConfirm ? 'batch-title' : 'enrollment-title'"
        @keydown="handleModalKeydown"
      >
        <template v-if="batchConfirm">
          <h2 id="batch-title">修改本场同一未知说话人？</h2>
          <p class="ms-lead">
            将修改
            {{ selected?.cluster_count }}
            个片段并覆盖此前说话人校对，原始判断与修改历史仍会保留。
          </p>
        </template>
        <template v-else>
          <h2 id="enrollment-title">加入永久声纹样本？</h2>
          <p class="ms-lead">
            将从原始录音复制此片段，执行真实质量检查后保存。删除本场会议不会删除已加入的样本。
          </p>
          <div class="ms-notice ms-notice--warning">
            <div>
              <strong>这是独立操作</strong>
              <p>保存说话人校对不会自动加入声纹样本。</p>
            </div>
          </div>
        </template>
        <div class="ms-actions ms-modal-actions">
          <button
            ref="modalCancel"
            class="ms-button ms-button--quiet"
            type="button"
            @click="closeModal"
          >
            返回检查
          </button>
          <button
            class="ms-button ms-button--primary"
            type="button"
            :disabled="correction.enrolling"
            @click="batchConfirm ? confirmBatch() : confirmEnrollment()"
          >
            {{
              batchConfirm
                ? `修改本场 ${selected?.cluster_count} 个片段`
                : correction.enrolling
                  ? '正在检查质量…'
                  : '加入声纹样本'
            }}
          </button>
        </div>
      </section>
    </div>
  </Teleport>
</template>
