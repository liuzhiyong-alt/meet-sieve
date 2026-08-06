<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

import { dirtyEditRegistry } from '../../router/dirty'
import type { Step8EventEnvelope } from '../../stores/finalization'
import { useGapStore } from '../../stores/gap'
import { useMinutesStore } from '../../stores/minutes'

const props = defineProps<{ meetingId: string; meetingNo: string }>()
const emit = defineEmits<{ back: [] }>()
const minutes = useMinutesStore()
const gaps = useGapStore()
const current = computed(() => minutes.projection.current)
const generationBlocked = computed(() => gaps.state === 'processing')
let stopListener: (() => void) | undefined
let unregisterDirty: (() => void) | undefined

onMounted(async () => {
  await Promise.all([
    minutes.refresh(props.meetingId),
    gaps.refresh(props.meetingId),
  ])
  stopListener = EventsOn(
    'meeting.minutes.changed',
    (event: Step8EventEnvelope) => {
      if (minutes.applyEvent(event)) void minutes.refresh(props.meetingId)
    },
  )
  unregisterDirty = dirtyEditRegistry.register({
    id: `minutes-${props.meetingId}`,
    label: '会议纪要',
    isDirty: () => minutes.dirty,
    canSave: () => Boolean(minutes.baseVersionID && minutes.draft.trim()),
    save: () => minutes.saveDraft(),
    discard: () => minutes.resetDraft(),
  })
})

onBeforeUnmount(() => {
  stopListener?.()
  unregisterDirty?.()
})
</script>

<template>
  <section class="ms-page-head">
    <div>
      <button class="ms-link-button" type="button" @click="emit('back')">
        返回会议详情
      </button>
      <p class="ms-eyebrow">{{ meetingNo }}</p>
      <h1>编辑会议纪要</h1>
      <p class="ms-lead">直接编辑会议纪要的 Markdown 源码。</p>
    </div>
  </section>

  <p
    v-if="minutes.errorMessage"
    class="ms-notice ms-notice--danger"
    role="alert"
  >
    {{ minutes.errorMessage }}
  </p>
  <p v-if="minutes.notice" class="ms-notice ms-notice--info" aria-live="polite">
    {{ minutes.notice }}
  </p>

  <section v-if="!current" class="ms-card ms-step8-card ms-empty-minutes">
    <h2>尚未生成会议纪要</h2>
    <p class="ms-lead">生成后可在这里直接编辑原始 Markdown 内容。</p>
    <button
      class="ms-button ms-button--primary"
      type="button"
      :disabled="generationBlocked || minutes.processing"
      @click="
        minutes.generate(
          meetingId,
          gaps.state !== 'none' && gaps.state !== 'completed',
        )
      "
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
    <p v-if="generationBlocked" class="ms-help">
      补转写仍在处理，暂时不能生成会议纪要。
    </p>
  </section>

  <section v-else class="ms-card ms-step8-card">
    <label class="ms-field" for="minute-markdown-source"
      >Markdown 源码
      <textarea
        id="minute-markdown-source"
        class="ms-input ms-textarea ms-minute-editor"
        :value="minutes.draft"
        spellcheck="false"
        @input="minutes.setDraft(($event.target as HTMLTextAreaElement).value)"
      />
    </label>
    <p class="ms-help">保存后，会议详情会按 Markdown 语法重新渲染。</p>
    <div class="ms-actions">
      <button
        class="ms-button ms-button--primary"
        type="button"
        :disabled="!minutes.dirty || minutes.saving || !minutes.draft.trim()"
        @click="minutes.saveDraft()"
      >
        {{ minutes.saving ? '正在保存…' : '保存会议纪要' }}</button
      ><button
        class="ms-button ms-button--quiet"
        type="button"
        :disabled="!minutes.dirty"
        @click="minutes.resetDraft()"
      >
        放弃未保存修改
      </button>
    </div>
  </section>
</template>
