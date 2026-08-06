<script lang="ts" setup>
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import { useMeetingStore } from '../../stores/meeting'
import { useASRStore } from '../../stores/asr'
import { useLANStore } from '../../stores/lan'

const meeting = useMeetingStore()
const asr = useASRStore()
const lan = useLANStore()
const route = useRoute()
const selectedGroupID = ref('')
const selectedMemberIDs = ref<string[]>([])
const temporaryNames = ref<string[]>([])
const newTemporaryName = ref('')
const temporaryDialogOpen = ref(false)
const temporaryDialogInput = ref<HTMLInputElement>()
const temporaryDialogOpener = ref<HTMLButtonElement>()
const subject = ref('')
const meetingNo = ref('')
const microphoneID = ref('')
const advanced = ref(false)
const asrMode = ref<'realtime' | 'record_only'>('record_only')
const lanFeatureAvailable = false

const canStart = computed(
  () =>
    !meeting.loading &&
    !meeting.saving &&
    Boolean(microphoneID.value) &&
    selectedMemberIDs.value.length + temporaryNames.value.length > 0,
)

onMounted(async () => {
  await Promise.all([
    meeting.loadCreateScreen(),
    asr.loadSettings(),
    lan.loadInterfaces(),
  ])
  subject.value = meeting.draft.subject
  if (meeting.prefill) {
    subject.value = meeting.prefill.subject
    selectedMemberIDs.value = meeting.prefill.memberIds.filter((id) =>
      meeting.members.some((member) => member.id === id),
    )
    meeting.prefill = null
  }
  meetingNo.value = meeting.draft.meetingNo
  microphoneID.value =
    meeting.microphones.find((device) => device.is_default)?.id ??
    meeting.microphones[0]?.id ??
    ''
  asrMode.value = asr.apiKeyReady ? 'realtime' : 'record_only'
  const entryGroupID = String(route.query.group ?? '')
  if (meeting.groups.some((group) => group.id === entryGroupID)) {
    selectedGroupID.value = entryGroupID
  }
})

watch(selectedGroupID, (groupID) => {
  const group = meeting.groups.find((candidate) => candidate.id === groupID)
  const activeMemberIDs = new Set(meeting.members.map((member) => member.id))
  selectedMemberIDs.value = group
    ? group.members
        .map((member) => member.id)
        .filter((memberID) => activeMemberIDs.has(memberID))
    : []
})

/** addTemporaryParticipant 把已去除首尾空白的临时姓名加入当前 UI 顺序。 */
function addTemporaryParticipant(): void {
  const name = newTemporaryName.value.trim()
  if (!name) return
  temporaryNames.value.push(name)
  newTemporaryName.value = ''
}

/** openTemporaryDialog 打开临时成员弹窗，并把键盘焦点移到姓名输入框。 */
async function openTemporaryDialog(): Promise<void> {
  temporaryDialogOpen.value = true
  await nextTick()
  temporaryDialogInput.value?.focus()
}

/** closeTemporaryDialog 关闭弹窗并把焦点还给触发按钮。 */
async function closeTemporaryDialog(): Promise<void> {
  temporaryDialogOpen.value = false
  newTemporaryName.value = ''
  await nextTick()
  temporaryDialogOpener.value?.focus()
}

/** trapTemporaryDialog 处理 Escape，并把 Tab 焦点限制在临时成员弹窗内。 */
function trapTemporaryDialog(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.preventDefault()
    void closeTemporaryDialog()
    return
  }
  if (event.key !== 'Tab') return
  const dialog = (
    event.currentTarget as HTMLElement
  ).querySelectorAll<HTMLElement>('input, button:not([disabled])')
  if (!dialog.length) return
  const first = dialog[0]
  const last = dialog[dialog.length - 1]
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

/** submit 交给后端完成全部校验、预检和首帧提交。 */
async function submit(
  mode: 'realtime' | 'record_only' = asrMode.value,
): Promise<void> {
  if (!canStart.value) return
  await meeting.startMeeting({
    meetingNo: meetingNo.value,
    suggestedMeetingNo: meeting.draft.meetingNo,
    subject: subject.value,
    memberIds: selectedMemberIDs.value,
    temporaryNames: temporaryNames.value,
    microphoneId: microphoneID.value,
    asrMode: mode,
    lanEnabled: lanFeatureAvailable,
    lanInterfaceId: '',
  })
}

/** retryWithRecording 使用相同草稿显式改为仅录音重试，不静默改变用户选择。 */
async function retryWithRecording(): Promise<void> {
  asrMode.value = 'record_only'
  await submit('record_only')
}

/** cancelRecordingRetry 只关闭降级选择，保留原错误信息供用户判断。 */
function cancelRecordingRetry(): void {
  meeting.errorCode = ''
}
</script>

<template>
  <section
    class="ms-meeting-head"
    :inert="temporaryDialogOpen"
    :aria-hidden="temporaryDialogOpen || undefined"
  >
    <div>
      <p class="ms-eyebrow">快速开始</p>
      <h1>谁会参加这场会议？</h1>
      <p class="ms-lead">先确定参会人，再检查麦克风和本地录音。</p>
    </div>
  </section>

  <p
    v-if="meeting.errorMessage"
    class="ms-notice ms-notice--danger"
    role="alert"
    :inert="temporaryDialogOpen"
    :aria-hidden="temporaryDialogOpen || undefined"
  >
    {{ meeting.errorMessage }}
  </p>
  <div
    v-if="meeting.canRetryRecordOnly"
    class="ms-notice ms-notice--warning"
    role="group"
    aria-label="实时转写失败处理"
    :inert="temporaryDialogOpen"
    :aria-hidden="temporaryDialogOpen || undefined"
  >
    <div>
      <strong>实时转写未能启动</strong>
      <p>可以保留当前会议草稿，改为仅录音后重试。</p>
    </div>
    <div class="ms-inline-actions">
      <button
        class="ms-button ms-button--quiet"
        type="button"
        @click="cancelRecordingRetry"
      >
        取消
      </button>
      <button
        class="ms-button ms-button--primary"
        type="button"
        @click="retryWithRecording"
      >
        仅录音继续
      </button>
    </div>
  </div>

  <section
    class="ms-meeting-split"
    :aria-busy="meeting.loading"
    :inert="temporaryDialogOpen"
    :aria-hidden="temporaryDialogOpen || undefined"
  >
    <div class="ms-card ms-meeting-card">
      <div class="ms-card-head">
        <h2>参会人</h2>
        <button
          ref="temporaryDialogOpener"
          class="ms-button ms-button--quiet"
          type="button"
          @click="openTemporaryDialog"
        >
          添加临时成员
        </button>
      </div>

      <label class="ms-field ms-field--compact">
        <span>常用小组</span>
        <select v-model="selectedGroupID" class="ms-input">
          <option value="">不使用小组</option>
          <option
            v-for="group in meeting.groups"
            :key="group.id"
            :value="group.id"
          >
            {{ group.name }} · {{ group.members.length }} 人
          </option>
        </select>
      </label>

      <fieldset class="ms-participant-fieldset">
        <legend>选择本场参会成员</legend>
        <div class="ms-choice-list">
          <label
            v-for="member in meeting.members"
            :key="member.id"
            class="ms-choice"
          >
            <input
              v-model="selectedMemberIDs"
              type="checkbox"
              :value="member.id"
            />
            <span class="ms-avatar" aria-hidden="true">{{
              member.name.slice(0, 1)
            }}</span>
            <span>
              <strong>{{ member.name }}</strong>
              <small>
                {{
                  member.voice_readiness === 'ready'
                    ? '声纹可用'
                    : '未录入声纹，仍可参会'
                }}
              </small>
            </span>
          </label>
        </div>
      </fieldset>

      <ul
        v-if="temporaryNames.length"
        class="ms-chip-list"
        aria-label="临时参会者"
      >
        <li v-for="(name, index) in temporaryNames" :key="`${name}-${index}`">
          <span>{{ name }}</span>
          <button
            type="button"
            :aria-label="`移除 ${name}`"
            @click="temporaryNames.splice(index, 1)"
          >
            ×
          </button>
        </li>
      </ul>

      <button
        class="ms-button ms-button--quiet"
        type="button"
        :aria-expanded="advanced"
        @click="advanced = !advanced"
      >
        {{ advanced ? '收起高级设置' : '展开高级设置' }}
      </button>
      <div v-if="advanced" class="ms-advanced-panel">
        <div class="ms-advanced-grid">
          <label class="ms-field ms-advanced-field">
            <span>会议主题（可选）</span>
            <input v-model="subject" class="ms-input" maxlength="200" />
          </label>
          <label class="ms-field ms-advanced-field">
            <span>会议号</span>
            <input v-model="meetingNo" class="ms-input ms-input--mono" />
          </label>
          <label class="ms-field ms-advanced-field">
            <span>麦克风</span>
            <select v-model="microphoneID" class="ms-input">
              <option
                v-for="device in meeting.microphones"
                :key="device.id"
                :value="device.id"
              >
                {{ device.name }}
              </option>
            </select>
          </label>
          <section class="ms-lan-config" aria-labelledby="lan-create-title">
            <span id="lan-create-title">局域网访客页</span>
            <div class="ms-lan-switch-control">
              <span>允许同一私有网络访问</span>
              <label class="ms-switch-label">
                <input
                  class="ms-switch-input"
                  type="checkbox"
                  role="switch"
                  aria-label="允许同一私有网络访问"
                  aria-describedby="lan-create-help"
                  :checked="lanFeatureAvailable"
                  disabled
                />
                <span class="ms-switch-track" aria-hidden="true"></span>
              </label>
            </div>
            <p id="lan-create-help" class="ms-help">
              开启后，同一局域网内的设备可以访问访客页。仅在可信的私有网络使用（暂未开放）。
            </p>
          </section>
        </div>
      </div>

      <fieldset class="ms-asr-mode-options">
        <legend>实时转写</legend>
        <label
          class="ms-choice ms-choice--auth"
          :class="{ 'is-disabled': !asr.apiKeyReady }"
        >
          <input
            v-model="asrMode"
            type="radio"
            value="realtime"
            :disabled="!asr.apiKeyReady"
          />
          <span
            ><strong>录音并实时转写</strong
            ><small>{{
              asr.apiKeyReady
                ? '使用已保存的火山 APP Key'
                : '请先在设置中保存 APP Key'
            }}</small></span
          >
        </label>
        <label class="ms-choice ms-choice--auth">
          <input v-model="asrMode" type="radio" value="record_only" />
          <span
            ><strong>仅录音</strong
            ><small>完整保存本地音频，并把整段标记为待补转写</small></span
          >
        </label>
      </fieldset>
    </div>

    <aside class="ms-card ms-meeting-card ms-preflight-card">
      <p class="ms-eyebrow">开始前检查</p>
      <ul class="ms-status-list">
        <li>
          <span>工作目录</span><strong class="is-ok">开始时检查可写</strong>
        </li>
        <li>
          <span>麦克风</span
          ><strong :class="{ 'is-ok': microphoneID }">{{
            microphoneID ? '已选择' : '没有可用设备'
          }}</strong>
        </li>
        <li>
          <span>本地录音</span><strong class="is-ok">安全分片保存</strong>
        </li>
        <li>
          <span>实时转写</span
          ><strong :class="{ 'is-ok': asrMode === 'realtime' }">{{
            asrMode === 'realtime' ? '会议中实时生成' : '仅录音，稍后补转写'
          }}</strong>
        </li>
        <li><span>Codex</span><strong>本步骤未启用</strong></li>
        <li>
          <span>局域网</span
          ><strong
            :class="{ 'is-ok': lan.enabled && lan.selectedInterfaceID }"
            >{{
              !lan.enabled
                ? '未允许'
                : lan.selectedInterfaceID
                  ? '开始后启动'
                  : '没有可用私有网络'
            }}</strong
          >
        </li>
      </ul>
      <button
        class="ms-button ms-button--primary ms-start-button"
        type="button"
        :disabled="!canStart"
        @click="submit()"
      >
        {{ meeting.saving ? '正在取得麦克风音频…' : '开始会议' }}
      </button>
    </aside>
  </section>

  <div
    v-if="temporaryDialogOpen"
    class="ms-modal-backdrop"
    @mousedown.self="closeTemporaryDialog"
  >
    <section
      class="ms-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="temporary-participant-title"
      @keydown="trapTemporaryDialog"
    >
      <h2 id="temporary-participant-title">添加临时成员</h2>
      <p class="ms-help">临时成员仅用于本场会议，不会加入成员库。</p>
      <label class="ms-field">
        <span>姓名</span>
        <input
          ref="temporaryDialogInput"
          v-model="newTemporaryName"
          class="ms-input"
          placeholder="临时成员姓名"
          @keydown.enter.prevent="addTemporaryParticipant"
        />
      </label>
      <ul
        v-if="temporaryNames.length"
        class="ms-chip-list"
        aria-label="已添加的临时成员"
      >
        <li v-for="(name, index) in temporaryNames" :key="`${name}-${index}`">
          <span>{{ name }}</span>
          <button
            type="button"
            :aria-label="`移除 ${name}`"
            @click="temporaryNames.splice(index, 1)"
          >
            ×
          </button>
        </li>
      </ul>
      <div class="ms-modal-actions">
        <button
          class="ms-button ms-button--quiet"
          type="button"
          @click="closeTemporaryDialog"
        >
          完成
        </button>
        <button
          class="ms-button ms-button--primary"
          type="button"
          :disabled="!newTemporaryName.trim()"
          @click="addTemporaryParticipant"
        >
          添加
        </button>
      </div>
    </section>
  </div>
</template>
