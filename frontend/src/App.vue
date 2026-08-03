<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue'
import { EventsOn, Quit } from '../wailsjs/runtime/runtime'

import AppShell from './components/layout/AppShell.vue'
import DatabaseUpgradeView from './features/bootstrap/DatabaseUpgradeView.vue'
import WorkspaceUnavailableView from './features/bootstrap/WorkspaceUnavailableView.vue'
import GeneralSettingsView from './features/settings/GeneralSettingsView.vue'
import PeopleView from './features/people/PeopleView.vue'
import LiveMeetingView from './features/meeting/LiveMeetingView.vue'
import TranscriptEditorView from './features/correction/TranscriptEditorView.vue'
import StartMeetingView from './features/meeting/StartMeetingView.vue'
import GapConflictView from './features/gap/GapConflictView.vue'
import MinutesHistoryView from './features/minutes/MinutesHistoryView.vue'
import MinutesWorkspaceView from './features/minutes/MinutesWorkspaceView.vue'
import OnboardingView from './features/workspace/OnboardingView.vue'
import { useBootstrapStore } from './stores/bootstrap'
import { useSystemStore } from './stores/system'
import { useMeetingStore } from './stores/meeting'

const bootstrap = useBootstrapStore()
const system = useSystemStore()
const meeting = useMeetingStore()
const selectingWorkspace = ref(false)
const currentPage = ref<
  | 'meeting'
  | 'correction'
  | 'gap-conflict'
  | 'minutes'
  | 'minutes-history'
  | 'people'
  | 'settings'
>('meeting')
const selectedGapID = ref('')
const closeRequested = ref(false)

const activeView = computed(() =>
  selectingWorkspace.value ? 'onboarding' : bootstrap.view,
)

const shellCurrent = computed<
  'start' | 'live' | 'interrupted' | 'records' | 'people' | 'settings'
>(() => {
  if (currentPage.value === 'meeting') return meeting.screen
  if (currentPage.value === 'people' || currentPage.value === 'settings')
    return currentPage.value
  return 'records'
})
const localSaveLabel = computed(() => {
  if (!meeting.current) return '可以开始会议'
  if (meeting.current.local_save_state === 'saved') return '本地录音已保存'
  if (meeting.current.local_save_state === 'failed') return '本地保存需要处理'
  return meeting.saving ? '正在安全保存录音' : '正在本地保存'
})

onMounted(async () => {
  // 系统健康检查同时承载 smoke 构建的真实 Wails event 往返。
  await Promise.all([system.refresh(), bootstrap.refresh()])
  if (bootstrap.view === 'ready') await meeting.refreshCurrentMeeting()
  EventsOn('meeting.close.requested', () => {
    currentPage.value = 'meeting'
    closeRequested.value = true
  })
})

/** endAndQuit 等待安全收尾成功后再允许 Wails 销毁窗口。 */
async function endAndQuit(): Promise<void> {
  if (await meeting.endMeeting()) Quit()
}

/** navigate 在活动录音期间保留真实会议入口，其余页面按主导航切换。 */
function navigate(
  destination: 'start' | 'live' | 'interrupted' | 'people' | 'settings',
): void {
  if (destination === 'people' || destination === 'settings') {
    currentPage.value = destination
    return
  }
  currentPage.value = 'meeting'
  if (destination === 'start' && !meeting.current) meeting.screen = 'start'
}

/** openGapConflict 只进入主持人本机冲突工作台。 */
function openGapConflict(gapID: string): void {
  selectedGapID.value = gapID
  currentPage.value = 'gap-conflict'
}
</script>

<template>
  <OnboardingView
    v-if="activeView === 'onboarding'"
    @used="selectingWorkspace = false"
  />
  <DatabaseUpgradeView
    v-else-if="activeView === 'upgrade'"
    @select="selectingWorkspace = true"
  />
  <WorkspaceUnavailableView
    v-else-if="activeView === 'unavailable' || activeView === 'fatal'"
    @select="selectingWorkspace = true"
  />
  <AppShell
    v-else-if="activeView === 'ready'"
    :current="shellCurrent"
    :meeting-no="meeting.current?.meeting_no"
    :local-save-label="localSaveLabel"
    @navigate="navigate"
  >
    <PeopleView v-if="currentPage === 'people'" />
    <GeneralSettingsView v-else-if="currentPage === 'settings'" />
    <TranscriptEditorView
      v-else-if="currentPage === 'correction' && meeting.current"
      :meeting-id="meeting.current.id"
      :meeting-no="meeting.current.meeting_no"
      :subject="meeting.current.subject"
      @back="currentPage = 'meeting'"
    />
    <GapConflictView
      v-else-if="
        currentPage === 'gap-conflict' && meeting.current && selectedGapID
      "
      :meeting-id="meeting.current.id"
      :meeting-no="meeting.current.meeting_no"
      :gap-id="selectedGapID"
      @back="currentPage = 'meeting'"
    />
    <MinutesWorkspaceView
      v-else-if="currentPage === 'minutes' && meeting.current"
      :meeting-id="meeting.current.id"
      :meeting-no="meeting.current.meeting_no"
      @back="currentPage = 'meeting'"
      @history="currentPage = 'minutes-history'"
    />
    <MinutesHistoryView
      v-else-if="currentPage === 'minutes-history' && meeting.current"
      :meeting-id="meeting.current.id"
      :meeting-no="meeting.current.meeting_no"
      @back="currentPage = 'minutes'"
    />
    <StartMeetingView v-else-if="meeting.screen === 'start'" />
    <LiveMeetingView
      v-else
      @open-correction="currentPage = 'correction'"
      @open-gap="openGapConflict"
      @open-minutes="currentPage = 'minutes'"
    />
  </AppShell>
  <main v-else class="ms-blocking-view">
    <section class="ms-blocking-view__panel">
      <h1>正在检查工作目录…</h1>
      <p class="ms-progress-label" aria-live="polite">
        <span class="ms-spinner" aria-hidden="true" /> 请稍候
      </p>
    </section>
  </main>

  <Teleport to="body">
    <div
      v-if="closeRequested"
      class="ms-modal-backdrop"
      @click.self="closeRequested = false"
    >
      <section
        class="ms-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="close-meeting-title"
      >
        <h2 id="close-meeting-title">会议仍在录音</h2>
        <p class="ms-lead">
          继续会议会取消关闭；结束并退出会先安全保存本地录音。
        </p>
        <p
          v-if="meeting.errorMessage"
          class="ms-notice ms-notice--danger"
          role="alert"
        >
          {{ meeting.errorMessage }}
        </p>
        <div class="ms-actions ms-modal-actions">
          <button
            class="ms-button ms-button--quiet"
            type="button"
            autofocus
            @click="closeRequested = false"
          >
            继续会议
          </button>
          <button
            class="ms-button ms-button--primary"
            type="button"
            :disabled="meeting.saving"
            @click="endAndQuit"
          >
            {{ meeting.saving ? '正在安全保存…' : '结束并退出' }}
          </button>
        </div>
      </section>
    </div>
  </Teleport>
</template>
