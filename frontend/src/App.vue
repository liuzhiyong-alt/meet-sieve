<script lang="ts" setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import { EventsOn, Quit } from '../wailsjs/runtime/runtime'

import AppShell from './components/layout/AppShell.vue'
import DatabaseUpgradeView from './features/bootstrap/DatabaseUpgradeView.vue'
import WorkspaceUnavailableView from './features/bootstrap/WorkspaceUnavailableView.vue'
import OnboardingView from './features/workspace/OnboardingView.vue'
import {
  dirtyEditRegistry,
  type DirtyDecision,
  type DirtyEditor,
} from './router/dirty'
import { useBootstrapStore } from './stores/bootstrap'
import { useSystemStore } from './stores/system'
import { useMeetingStore } from './stores/meeting'

const bootstrap = useBootstrapStore()
const system = useSystemStore()
const meeting = useMeetingStore()
const route = useRoute()
const router = useRouter()
const selectingWorkspace = ref(false)
const closeRequested = ref(false)
const dirtyOpen = ref(false)
const dirtyEditors = ref<DirtyEditor[]>([])
const dirtyDialog = ref<HTMLElement>()
let resolveDirty: ((decision: DirtyDecision) => void) | undefined
let stopCloseListener: (() => void) | undefined

const activeView = computed(() =>
  selectingWorkspace.value ? 'onboarding' : bootstrap.view,
)
const activeMeeting = computed(() =>
  Boolean(
    meeting.current &&
    !['ended', 'interrupted'].includes(meeting.current.lifecycle_state),
  ),
)
const localSaveLabel = computed(() => {
  if (!meeting.current) return '可以开始会议'
  if (meeting.current.local_save_state === 'saved') return '本地录音已保存'
  if (meeting.current.local_save_state === 'failed') return '本地保存需要处理'
  return meeting.saving ? '正在安全保存录音' : '正在本地保存'
})

onMounted(async () => {
  await Promise.all([system.refresh(), bootstrap.refresh()])
  if (bootstrap.view === 'ready') await meeting.refreshCurrentMeeting()
  stopCloseListener = EventsOn('meeting.close.requested', () => {
    closeRequested.value = true
  })
  dirtyEditRegistry.setPrompt(promptDirty)
  window.addEventListener('beforeunload', preventDirtyReload)
})
onBeforeUnmount(() => {
  stopCloseListener?.()
  window.removeEventListener('beforeunload', preventDirtyReload)
})
watch(
  () => meeting.screen,
  (screen) => {
    if (screen === 'live' && route.name === 'meeting-new')
      void router.replace('/meetings/live')
  },
)

/** promptDirty 打开 App 级 dirty 对话框并等待明确决策。 */
function promptDirty(editors: DirtyEditor[]): Promise<DirtyDecision> {
  dirtyEditors.value = editors
  dirtyOpen.value = true
  void nextTick(() =>
    dirtyDialog.value?.querySelector<HTMLElement>('button')?.focus(),
  )
  return new Promise((resolve) => {
    resolveDirty = resolve
  })
}
/** decideDirty 完成 dirty 路由决策并关闭对话框。 */
function decideDirty(decision: DirtyDecision): void {
  dirtyOpen.value = false
  resolveDirty?.(decision)
  resolveDirty = undefined
}
/** handleDirtyKeydown 限制焦点在 dirty 对话框内，并支持 Escape 返回编辑。 */
function handleDirtyKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    decideDirty('stay')
    return
  }
  if (event.key !== 'Tab' || !dirtyDialog.value) return
  const controls = Array.from(
    dirtyDialog.value.querySelectorAll<HTMLElement>('button:not([disabled])'),
  )
  const first = controls[0]
  const last = controls[controls.length - 1]
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last?.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first?.focus()
  }
}
/** preventDirtyReload 使用浏览器原生确认保护重载，后台任务不会触发。 */
function preventDirtyReload(event: BeforeUnloadEvent): void {
  if (dirtyEditRegistry.dirtyEditors().length) event.preventDefault()
}
/** endAndQuit 等待安全收尾成功后再允许 Wails 销毁窗口。 */
async function endAndQuit(): Promise<void> {
  if (await meeting.endMeeting()) Quit()
}
/** endFromShell 安全结束会议后进入其详情页。 */
async function endFromShell(): Promise<void> {
  const id = meeting.current?.id
  if (id && (await meeting.endMeeting())) await router.push(`/meetings/${id}`)
}
/** openCorrection 进入当前会议原始记录校对。 */
function openCorrection(): void {
  if (meeting.current)
    void router.push(
      `/meetings/${meeting.current.id}/transcript?no=${encodeURIComponent(meeting.current.meeting_no)}&subject=${encodeURIComponent(meeting.current.subject)}`,
    )
}
/** openGap 进入指定缺口冲突页。 */
function openGap(gapID: string): void {
  if (meeting.current)
    void router.push(
      `/meetings/${meeting.current.id}/gaps/${gapID}?no=${encodeURIComponent(meeting.current.meeting_no)}`,
    )
}
/** openMinutes 进入当前会议纪要。 */
function openMinutes(): void {
  if (meeting.current)
    void router.push(
      `/meetings/${meeting.current.id}/minutes?no=${encodeURIComponent(meeting.current.meeting_no)}`,
    )
}
/** backFromFeature 返回会议详情或纪要工作区。 */
function backFromFeature(): void {
  const id = String(route.params.id ?? meeting.current?.id ?? '')
  if (route.name === 'meeting-minutes-history')
    void router.push(`/meetings/${id}/minutes?no=${route.query.no ?? ''}`)
  else void router.push(id ? `/meetings/${id}` : '/meetings')
}
/** openHistory 进入不可变纪要版本历史。 */
function openHistory(): void {
  const id = String(route.params.id)
  void router.push(`/meetings/${id}/minutes/history?no=${route.query.no ?? ''}`)
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
    :inert="dirtyOpen || closeRequested ? '' : undefined"
    :active-meeting="activeMeeting"
    :meeting-no="meeting.current?.meeting_no"
    :local-save-label="localSaveLabel"
    @end-meeting="endFromShell"
  >
    <RouterView v-slot="{ Component }"
      ><component
        :is="Component"
        @open-correction="openCorrection"
        @open-gap="openGap"
        @open-minutes="openMinutes"
        @back="backFromFeature"
        @history="openHistory"
    /></RouterView>
  </AppShell>
  <main v-else class="ms-blocking-view">
    <section class="ms-blocking-view__panel">
      <h1>正在检查工作目录…</h1>
      <p class="ms-progress-label" aria-live="polite">
        <span class="ms-spinner" /> 请稍候
      </p>
    </section>
  </main>

  <Teleport to="body"
    ><div
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
        <div class="ms-modal-actions">
          <button
            class="ms-button ms-button--quiet"
            autofocus
            @click="closeRequested = false"
          >
            继续会议</button
          ><button
            class="ms-button ms-button--primary"
            :disabled="meeting.saving"
            @click="endAndQuit"
          >
            {{ meeting.saving ? '正在安全保存…' : '结束并退出' }}
          </button>
        </div>
      </section>
    </div></Teleport
  >
  <Teleport to="body"
    ><div
      v-if="dirtyOpen"
      class="ms-modal-backdrop"
      @click.self="decideDirty('stay')"
    >
      <section
        ref="dirtyDialog"
        class="ms-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="dirty-title"
        @keydown="handleDirtyKeydown"
      >
        <h2 id="dirty-title">有尚未保存的更改</h2>
        <p>
          {{
            dirtyEditors.map((editor) => editor.label).join('、')
          }}尚未保存。保存失败或发生冲突时会留在当前页面。
        </p>
        <div class="ms-modal-actions">
          <button
            class="ms-button ms-button--quiet"
            @click="decideDirty('stay')"
          >
            继续编辑</button
          ><button
            class="ms-button ms-button--quiet"
            @click="decideDirty('discard')"
          >
            放弃更改</button
          ><button
            class="ms-button ms-button--primary"
            :disabled="dirtyEditors.some((editor) => !editor.canSave())"
            @click="decideDirty('save')"
          >
            保存并离开
          </button>
        </div>
      </section>
    </div></Teleport
  >
</template>
