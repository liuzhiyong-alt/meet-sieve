<script lang="ts" setup>
import { computed, onMounted } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import { RetryMeetingRecovery } from '../../../wailsjs/go/wails/MeetingBinding'
import { useQueryStore } from '../../stores/query'
import { useMeetingStore } from '../../stores/meeting'

const props = defineProps<{ id: string }>()
const router = useRouter()
const query = useQueryStore()
const meeting = useMeetingStore()
const duration = computed(() => {
  const value = query.recovery
  if (!value?.sample_rate) return '—'
  const seconds = Math.floor(value.duration_samples / value.sample_rate)
  return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
})

/** load 只允许 interrupted/unsaved，已保存会议回规范详情页。 */
async function load(): Promise<void> {
  if (
    !(await query.loadRecovery(props.id)) &&
    query.errorCode.includes('RECOVERY_NOT_ALLOWED')
  )
    await router.replace(`/meetings/${props.id}`)
}
/** retry 复用 Step 8 文件恢复命令，不创建 stream/session/token。 */
async function retry(): Promise<void> {
  const result = await RetryMeetingRecovery(props.id)
  if (result.code !== 200) query.errorMessage = result.message
  await load()
}
/** createBasedOnMeeting 只传递主题和登记成员，并重新读取新会议号与会前检查。 */
function createBasedOnMeeting(): void {
  const summary = query.recovery?.meeting
  if (!summary) return
  meeting.prepareFromHistory(
    summary.subject,
    summary.participant_member_ids ?? [],
  )
  void router.push('/meetings/new')
}
onMounted(load)
</script>

<template>
  <section class="ms-page-head">
    <div>
      <p class="ms-eyebrow">异常退出恢复</p>
      <h1>会议录音未正常收尾</h1>
      <p>只修复已落盘的本地文件，不会续录，也不会恢复旧 ASR 会话。</p>
    </div>
  </section>
  <p v-if="query.errorMessage" class="ms-notice ms-notice--danger" role="alert">
    {{ query.errorMessage }}
  </p>
  <section v-if="query.recovery" class="ms-detail-grid">
    <article class="ms-card ms-settings-card">
      <h2>
        {{
          query.recovery.meeting.subject || query.recovery.meeting.meeting_no
        }}
      </h2>
      <dl class="ms-fact-grid">
        <div>
          <dt>可恢复分片</dt>
          <dd>{{ query.recovery.segment_count }}</dd>
        </div>
        <div>
          <dt>录音时长</dt>
          <dd>{{ duration }}</dd>
        </div>
        <div>
          <dt>序号范围</dt>
          <dd>
            {{ query.recovery.first_sequence }}–{{
              query.recovery.last_sequence
            }}
          </dd>
        </div>
        <div>
          <dt>缺口</dt>
          <dd>
            {{ query.recovery.pending_gap_count }}/{{
              query.recovery.gap_count
            }}
            待处理
          </dd>
        </div>
        <div>
          <dt>文件状态</dt>
          <dd>
            {{ query.recovery.ready_file_count }} 可用 /
            {{ query.recovery.failed_file_count }} 失败
          </dd>
        </div>
        <div>
          <dt>失败阶段</dt>
          <dd>{{ query.recovery.failure_stage || '本地安全收尾' }}</dd>
        </div>
      </dl>
      <p v-if="query.recovery.disabled_reason" class="ms-help">
        {{ query.recovery.disabled_reason }}
      </p>
      <div class="ms-actions">
        <button
          class="ms-button ms-button--primary"
          :disabled="!query.recovery.can_retry || query.loading"
          @click="retry"
        >
          重试本地恢复</button
        ><button
          class="ms-button ms-button--quiet"
          @click="createBasedOnMeeting"
        >
          基于本场创建新会议</button
        ><RouterLink
          class="ms-button ms-button--quiet"
          :to="`/meetings/${props.id}`"
          >查看现有记录</RouterLink
        >
      </div>
    </article>
    <aside class="ms-card ms-settings-card">
      <h2>不会发生什么</h2>
      <ul>
        <li>不会继续写入原录音</li>
        <li>不会复用会议号或 UUID</li>
        <li>不会创建新的 ASR session</li>
        <li>不会向 LAN 访客重新开放会议</li>
      </ul>
    </aside>
  </section>
</template>
