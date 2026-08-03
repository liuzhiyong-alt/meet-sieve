<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

import { useGapStore, type GapResolution } from '../../stores/gap'
import type { Step8EventEnvelope } from '../../stores/finalization'

const props = defineProps<{
  meetingId: string
  meetingNo: string
  gapId: string
}>()
const emit = defineEmits<{ back: [] }>()
const gaps = useGapStore()
const resolution = ref<GapResolution>('keep_existing')
const texts = ref<Record<string, string>>({})
let stopListener: (() => void) | undefined

const conflict = computed(() => gaps.conflict)
const canSubmit = computed(() => {
  if (!conflict.value || gaps.submitting) return false
  return (
    resolution.value === 'keep_existing' ||
    conflict.value.existing.every((item) =>
      Boolean(texts.value[item.id]?.trim()),
    )
  )
})

onMounted(async () => {
  await gaps.loadConflict(props.meetingId, props.gapId)
  resetTexts()
  stopListener = EventsOn(
    'meeting.gap.changed',
    (event: Step8EventEnvelope) => {
      if (gaps.applyEvent(event))
        void gaps.loadConflict(props.meetingId, props.gapId)
    },
  )
})
onBeforeUnmount(() => stopListener?.())

watch(resolution, resetTexts)

/** overlaps 判断两个半开样本范围是否存在正长度交集。 */
function overlaps(
  startA: number,
  endA: number,
  startB: number,
  endB: number,
): boolean {
  return Math.max(startA, startB) < Math.min(endA, endB)
}

/** fileTextFor 按后端同一规则为每条现有分句拼接重叠候选。 */
function fileTextFor(item: {
  start_sample: number
  end_sample: number
}): string {
  return (conflict.value?.candidates ?? [])
    .filter((candidate) =>
      overlaps(
        item.start_sample,
        item.end_sample,
        candidate.start_sample,
        candidate.end_sample,
      ),
    )
    .map((candidate) => candidate.text)
    .join('\n')
}

/** resetTexts 切换选择时恢复对应证据，不静默改正式记录。 */
function resetTexts(): void {
  const next: Record<string, string> = {}
  for (const item of conflict.value?.existing ?? []) {
    next[item.id] =
      resolution.value === 'use_file_text'
        ? fileTextFor(item)
        : item.current_text
  }
  texts.value = next
}

/** sampleTime 把 16kHz 绝对样本点转换为会议时间。 */
function sampleTime(value: number): string {
  const seconds = Math.max(0, Math.floor(value / 16000))
  return `${String(Math.floor(seconds / 60)).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`
}

/** submit 提交全部冲突分句，成功后返回会议详情。 */
async function submit(): Promise<void> {
  if (await gaps.resolveConflict(resolution.value, texts.value)) emit('back')
}
</script>

<template>
  <section class="ms-page-head">
    <div>
      <p class="ms-eyebrow">{{ meetingNo }}</p>
      <h1>补转写冲突校对</h1>
      <p class="ms-lead">
        实时转写与文件补转写在同一范围内重叠。确认前，两份结果都不会覆盖对方。
      </p>
    </div>
    <span class="ms-status-pill is-warning">待确认</span>
  </section>
  <p v-if="gaps.errorMessage" class="ms-notice ms-notice--danger" role="alert">
    {{ gaps.errorMessage }}
  </p>
  <section v-if="conflict" class="ms-conflict-layout">
    <article class="ms-card ms-step8-card">
      <div class="ms-audio-strip">
        <div>
          <strong>冲突录音</strong>
          <p class="ms-help">
            {{ sampleTime(conflict.audio_start_sample) }}–{{
              sampleTime(conflict.audio_end_sample)
            }}
          </p>
        </div>
        <audio :src="conflict.audio_clip_url" controls preload="metadata">
          浏览器不支持音频回放。
        </audio>
      </div>
      <div class="ms-notice ms-notice--warning">
        整段文件结果尚未写入正式原始记录。完成校对后，未重叠分句会一并补入。
      </div>
      <div class="ms-comparison-grid" aria-label="转写结果对比">
        <section class="ms-evidence-pane">
          <div class="ms-evidence-pane__head">
            <h2>实时转写 · 当前记录</h2>
            <span class="ms-status-pill is-complete">已保存</span>
          </div>
          <article v-for="item in conflict.existing" :key="item.id">
            <p class="ms-help">
              {{ sampleTime(item.start_sample) }}–{{
                sampleTime(item.end_sample)
              }}
            </p>
            <blockquote>{{ item.current_text }}</blockquote>
            <p v-if="item.original_text !== item.current_text" class="ms-help">
              原始 ASR：{{ item.original_text }}
            </p>
          </article>
        </section>
        <section class="ms-evidence-pane is-file">
          <div class="ms-evidence-pane__head">
            <h2>文件补转写 · 候选结果</h2>
            <span class="ms-status-pill is-warning">有重叠</span>
          </div>
          <article
            v-for="candidate in conflict.candidates"
            :key="`${candidate.start_sample}-${candidate.end_sample}-${candidate.text}`"
          >
            <p class="ms-help">
              {{ sampleTime(candidate.start_sample) }}–{{
                sampleTime(candidate.end_sample)
              }}
            </p>
            <blockquote>{{ candidate.text }}</blockquote>
          </article>
        </section>
      </div>
      <div class="ms-conflict-context">
        <strong>相邻会议内容</strong>
        <p v-for="item in conflict.context" :key="item.id" class="ms-help">
          <span>{{ sampleTime(item.start_sample) }}</span>
          {{ item.current_text }}
        </p>
      </div>
    </article>
    <aside class="ms-card ms-step8-card ms-conflict-panel">
      <h2>选择保留方式</h2>
      <div
        class="ms-conflict-options"
        role="radiogroup"
        aria-label="冲突解决方式"
      >
        <label
          ><input v-model="resolution" type="radio" value="keep_existing" />
          <span
            ><strong>保留当前记录</strong
            ><small>忽略重叠的文件文字</small></span
          ></label
        >
        <label
          ><input v-model="resolution" type="radio" value="use_file_text" />
          <span
            ><strong>使用文件补转写</strong
            ><small>原始实时文字仍保留在修改历史中</small></span
          ></label
        >
        <label
          ><input v-model="resolution" type="radio" value="save_manual_text" />
          <span
            ><strong>手动修改</strong
            ><small>根据录音逐条输入最终文字</small></span
          ></label
        >
      </div>
      <template v-if="resolution !== 'keep_existing'">
        <label v-for="item in conflict.existing" :key="item.id" class="ms-field"
          >最终文字 {{ sampleTime(item.start_sample)
          }}<textarea
            v-model="texts[item.id]"
            class="ms-input ms-textarea"
            :readonly="resolution === 'use_file_text'"
          />
        </label>
      </template>
      <p class="ms-help">保存会创建人工校对审计，不会修改原始 ASR。</p>
      <div class="ms-actions">
        <button
          class="ms-button ms-button--primary"
          type="button"
          :disabled="!canSubmit"
          @click="submit"
        >
          {{ gaps.submitting ? '正在保存…' : '保存并完成本段' }}</button
        ><button
          class="ms-button ms-button--quiet"
          type="button"
          @click="emit('back')"
        >
          稍后处理
        </button>
      </div>
    </aside>
  </section>
  <section v-else class="ms-card ms-step8-card" aria-live="polite">
    <p>{{ gaps.loading ? '正在加载冲突证据…' : '冲突证据不可用。' }}</p>
    <button
      class="ms-button ms-button--quiet"
      type="button"
      @click="emit('back')"
    >
      返回会议详情
    </button>
  </section>
</template>
