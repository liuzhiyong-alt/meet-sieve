<script lang="ts" setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

import BaseButton from '../../components/base/BaseButton.vue'
import { usePeopleStore } from '../../stores/people'
import { useVoiceStore } from '../../stores/voice'

const people = usePeopleStore()
const voice = useVoiceStore()
const activeTab = ref<'groups' | 'members'>('groups')
const createKind = ref<'group' | 'member'>('group')
const modalOpen = ref(false)
const name = ref('')
const notes = ref('')
const nameInput = ref<HTMLInputElement>()
const voiceDialog = ref<HTMLElement>()
const confirmationDialog = ref<HTMLElement>()
const editingID = ref('')
const selectedMemberIDs = ref<string[]>([])
const defaultLANEnabled = ref(false)
const voiceMember = ref<{ id: string; name: string }>()
const voiceSource = ref<'record' | 'upload'>('record')
const environment = ref('quiet')
const selectedDevice = ref('')
const confirmation = ref<{
  kind: 'delete_group' | 'archive_member' | 'delete_member' | 'delete_all_voice'
  title: string
  message: string
  actionLabel: string
}>()
let recordingTimer: number | undefined
let stopVoiceModelListener: (() => void) | undefined

const canSubmit = computed(() => name.value.trim().length > 0 && !people.saving)
const recordingLevelText = computed(() => {
  if (!voice.recording) return '当前没有检测到语音'
  if (voice.recordingLevel >= 0.5) return '当前语音音量较高'
  if (voice.recordingLevel >= 0.08) return '当前检测到语音'
  return '当前语音音量较低'
})

/** openCreateModal 重置表单并把焦点送入对话框。 */
async function openCreateModal(): Promise<void> {
  editingID.value = ''
  name.value = ''
  notes.value = ''
  selectedMemberIDs.value = []
  defaultLANEnabled.value = false
  modalOpen.value = true
  await nextTick()
  nameInput.value?.focus()
}

/** openEditMember 使用当前后端投影填充成员编辑表单。 */
async function openEditMember(
  member: (typeof people.members)[number],
): Promise<void> {
  createKind.value = 'member'
  editingID.value = member.id
  name.value = member.name
  notes.value = member.notes ?? ''
  modalOpen.value = true
  await nextTick()
  nameInput.value?.focus()
}

/** openEditGroup 使用显式 sort_order 还原当前小组成员顺序。 */
async function openEditGroup(
  group: (typeof people.groups)[number],
): Promise<void> {
  createKind.value = 'group'
  editingID.value = group.id
  name.value = group.name
  defaultLANEnabled.value = group.default_lan_enabled
  selectedMemberIDs.value = [...group.members]
    .sort((left, right) => left.sort_order - right.sort_order)
    .map((item) => item.member_id)
  modalOpen.value = true
  await nextTick()
  nameInput.value?.focus()
}

/** closeCreateModal 关闭可逆创建流程。 */
function closeCreateModal(): void {
  if (!people.saving) modalOpen.value = false
}

/** submitCreate 调用真实成员或小组 binding，成功后才关闭对话框。 */
async function submitCreate(): Promise<void> {
  if (!canSubmit.value) return
  const succeeded = editingID.value
    ? createKind.value === 'member'
      ? await people.updateMember(editingID.value, name.value, notes.value)
      : await people.updateGroup(
          editingID.value,
          name.value,
          defaultLANEnabled.value,
          selectedMemberIDs.value,
        )
    : createKind.value === 'member'
      ? await people.createMember(name.value, notes.value)
      : await people.createGroup(name.value, selectedMemberIDs.value)
  if (succeeded) modalOpen.value = false
}

/** toggleGroupMember 按用户勾选顺序维护小组显式成员顺序。 */
function toggleGroupMember(memberID: string): void {
  const index = selectedMemberIDs.value.indexOf(memberID)
  if (index >= 0) selectedMemberIDs.value.splice(index, 1)
  else selectedMemberIDs.value.push(memberID)
}

/** requestRemoveCurrent 在执行小组或成员破坏性操作前显示产品内确认。 */
function requestRemoveCurrent(permanent = false): void {
  if (!editingID.value) return
  confirmation.value =
    createKind.value === 'group'
      ? {
          kind: 'delete_group',
          title: '删除这个小组？',
          message: '小组关系会永久删除，其中的成员和声纹资料不会被删除。',
          actionLabel: '删除小组',
        }
      : permanent
        ? {
            kind: 'delete_member',
            title: '永久删除这个成员？',
            message:
              '仅从未被历史会议引用的成员可以永久删除；其声纹样本和向量也会一并删除。',
            actionLabel: '永久删除',
          }
        : {
            kind: 'archive_member',
            title: '归档这个成员？',
            message:
              '成员不会再出现在新会议和小组中，历史会议仍保留原参会者信息。',
            actionLabel: '归档成员',
          }
}

/** requestDeleteAllVoice 确认永久删除当前成员的全部声纹资料。 */
function requestDeleteAllVoice(): void {
  confirmation.value = {
    kind: 'delete_all_voice',
    title: '删除全部声纹？',
    message: '该成员的全部录音样本和声纹向量会永久删除，历史会议不会改变。',
    actionLabel: '删除全部声纹',
  }
}

/** confirmDestructive 执行已明确展示后果的单个破坏性动作。 */
async function confirmDestructive(): Promise<void> {
  const kind = confirmation.value?.kind
  if (!kind) return
  if (kind === 'delete_all_voice') {
    await voice.deleteAll()
    confirmation.value = undefined
    return
  }
  const succeeded =
    kind === 'delete_group'
      ? await people.deleteGroup(editingID.value)
      : kind === 'delete_member'
        ? await people.deleteMember(editingID.value)
        : await people.archiveMember(editingID.value)
  if (succeeded) {
    confirmation.value = undefined
    modalOpen.value = false
  }
}

/** openVoice 打开成员声纹管理，并从数据库和系统设备恢复真实状态。 */
async function openVoice(
  member: (typeof people.members)[number],
): Promise<void> {
  voiceMember.value = { id: member.id, name: member.name }
  await nextTick()
  voiceDialog.value?.focus()
  await voice.open(member.id)
  selectedDevice.value =
    voice.devices.find((item) => item.is_default)?.id ??
    voice.devices[0]?.id ??
    ''
}

/** trapDialogFocus 把 Tab 键限制在当前对话框的可操作元素中。 */
function trapDialogFocus(event: KeyboardEvent): void {
  const dialog = event.currentTarget as HTMLElement
  const focusable = Array.from(
    dialog.querySelectorAll<HTMLElement>(
      'button:not(:disabled), input:not(:disabled), select:not(:disabled), [tabindex="0"]',
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

/** closeVoice 关闭前先取消仍在进行的设备录音。 */
async function closeVoice(): Promise<void> {
  if (voice.startingRecording) return
  if (voice.recording) await voice.cancelRecording()
  stopTimer()
  voiceMember.value = undefined
  voice.reset()
  await people.refresh()
}

/** toggleRecording 启动或停止真实麦克风采集与处理。 */
async function toggleRecording(): Promise<void> {
  if (voice.recording) {
    stopTimer()
    await voice.stopRecording()
    return
  }
  if (!selectedDevice.value) return
  if (await voice.startRecording(selectedDevice.value, environment.value)) {
    scheduleRecordingStateRefresh()
  }
}

/** scheduleRecordingStateRefresh 轮询后端真实 PCM 快照，并在 60 秒硬上限后保存。 */
function scheduleRecordingStateRefresh(): void {
  recordingTimer = window.setTimeout(async () => {
    await voice.refreshRecordingState()
    if (voice.recordingDurationMS >= 60000) {
      await toggleRecording()
      return
    }
    if (voice.recording) scheduleRecordingStateRefresh()
  }, 100)
}

/** stopTimer 清理录音计时器，避免弹窗关闭后继续更新。 */
function stopTimer(): void {
  if (recordingTimer !== undefined) window.clearTimeout(recordingTimer)
  recordingTimer = undefined
}

/** cancelCurrentRecording 丢弃当前录音并同步停止计时。 */
async function cancelCurrentRecording(): Promise<void> {
  stopTimer()
  await voice.cancelRecording()
}

/** formatDuration 以 mm:ss 显示录音或样本时长。 */
function formatDuration(milliseconds: number): string {
  const seconds = Math.floor(milliseconds / 1000)
  return `${String(Math.floor(seconds / 60)).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`
}

/** initials 返回头像使用的首个 Unicode 字符。 */
function initials(displayName: string): string {
  return Array.from(displayName.trim())[0] ?? '人'
}

/** voiceDescription 把后端 readiness 转换为统一中文状态，不推断识别准确性。 */
function voiceDescription(member: {
  accepted_sample_count: number
  voice_readiness: string
}): string {
  switch (member.voice_readiness) {
    case 'processing':
      return '声纹样本正在评估'
    case 'ready':
      return `${member.accepted_sample_count} 个声纹样本 · 可用`
    case 'rebuild_required':
      return `${member.accepted_sample_count} 个声纹样本 · 需要重建`
    case 'unavailable':
      return `${member.accepted_sample_count} 个声纹样本 · 模型不可用`
    default:
      return '未录入声纹'
  }
}

onMounted(() => {
  // 模型在后台初始化；监听完成事件，避免成员页保留启动瞬间的过期状态。
  stopVoiceModelListener = EventsOn('voice.model.changed', () => {
    void people.refresh()
  })
  void people.refresh()
})
watch(confirmation, async (value) => {
  if (!value) return
  await nextTick()
  confirmationDialog.value?.focus()
})
onBeforeUnmount(() => {
  stopVoiceModelListener?.()
  stopTimer()
  if (voice.recording) void voice.cancelRecording()
})
</script>

<template>
  <section class="ms-people" aria-labelledby="people-heading">
    <header class="ms-page-head">
      <div>
        <p class="ms-eyebrow">本地成员库</p>
        <h1 id="people-heading">小组与成员</h1>
        <p>小组加快会前选择；历史会议始终保留当时的参会者快照。</p>
      </div>
      <BaseButton variant="primary" @click="openCreateModal">新建</BaseButton>
    </header>

    <div class="ms-tabs" role="tablist" aria-label="成员库分类">
      <button
        :class="{ 'is-current': activeTab === 'groups' }"
        role="tab"
        :aria-selected="activeTab === 'groups'"
        @click="activeTab = 'groups'"
      >
        小组
      </button>
      <button
        :class="{ 'is-current': activeTab === 'members' }"
        role="tab"
        :aria-selected="activeTab === 'members'"
        @click="activeTab = 'members'"
      >
        成员
      </button>
    </div>

    <p v-if="people.loading" class="ms-progress-label" aria-live="polite">
      <span class="ms-spinner" aria-hidden="true" />正在读取本地成员库…
    </p>
    <div
      v-else-if="people.errorMessage"
      class="ms-notice ms-notice--danger"
      role="alert"
    >
      <div>
        <strong>无法读取成员库</strong>
        <p>{{ people.errorMessage }}</p>
      </div>
    </div>

    <section
      v-else-if="activeTab === 'groups'"
      class="ms-people-grid"
      role="tabpanel"
    >
      <div v-if="people.groups.length === 0" class="ms-empty-state">
        <h2>还没有小组</h2>
        <p>创建常用小组后，可以在会前更快选择参会者。</p>
      </div>
      <article
        v-for="group in people.groups"
        :key="group.id"
        class="ms-people-card"
      >
        <div class="ms-card-head">
          <div>
            <p class="ms-meta">{{ group.members.length }} 位成员</p>
            <h2>{{ group.name }}</h2>
          </div>
          <BaseButton variant="quiet" @click="openEditGroup(group)"
            >编辑</BaseButton
          >
        </div>
        <div
          v-if="group.members.length"
          class="ms-avatar-row"
          aria-label="小组成员"
        >
          <span
            v-for="relation in group.members.slice(0, 5)"
            :key="relation.member_id"
            class="ms-avatar"
            >{{
              initials(
                people.members.find((item) => item.id === relation.member_id)
                  ?.name ?? '',
              )
            }}</span
          >
        </div>
        <p v-else class="ms-muted">该小组暂时没有成员。</p>
        <div class="ms-card-foot">
          <span>默认开启访客页</span
          ><span class="ms-status">{{
            group.default_lan_enabled ? '开启' : '关闭'
          }}</span>
        </div>
      </article>
    </section>

    <section v-else class="ms-people-list" role="tabpanel">
      <div v-if="people.members.length === 0" class="ms-empty-state">
        <h2>还没有成员</h2>
        <p>先创建成员，再录入属于他的声纹样本。</p>
      </div>
      <article
        v-for="member in people.members"
        :key="member.id"
        class="ms-list-item"
      >
        <div class="ms-person">
          <span class="ms-avatar">{{ initials(member.name) }}</span>
          <div>
            <strong>{{ member.name }}</strong>
            <p>
              {{ voiceDescription(member)
              }}<template v-if="member.notes"> · {{ member.notes }}</template>
            </p>
          </div>
        </div>
        <div class="ms-actions-inline">
          <BaseButton variant="quiet" @click="openEditMember(member)"
            >编辑</BaseButton
          >
          <BaseButton variant="quiet" @click="openVoice(member)"
            >管理声纹</BaseButton
          >
        </div>
      </article>
    </section>
  </section>

  <div
    v-if="modalOpen"
    class="ms-modal-backdrop"
    @keydown.esc="closeCreateModal"
  >
    <section
      class="ms-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="create-people-title"
      @keydown.tab="trapDialogFocus"
    >
      <h2 id="create-people-title">
        {{ editingID ? '编辑' : '新建' }}小组或成员
      </h2>
      <div class="ms-tabs" role="tablist" aria-label="新建类型">
        <button
          :class="{ 'is-current': createKind === 'group' }"
          role="tab"
          :aria-selected="createKind === 'group'"
          @click="createKind = 'group'"
        >
          小组
        </button>
        <button
          :class="{ 'is-current': createKind === 'member' }"
          role="tab"
          :aria-selected="createKind === 'member'"
          @click="createKind = 'member'"
        >
          成员
        </button>
      </div>
      <label class="ms-field"
        ><span>名称</span
        ><input
          ref="nameInput"
          v-model="name"
          class="ms-input"
          :placeholder="
            createKind === 'group' ? '例如：客户端评审组' : '例如：王小明'
          "
          @keydown.enter="submitCreate"
      /></label>
      <label v-if="createKind === 'member'" class="ms-field"
        ><span>备注（可选）</span
        ><input
          v-model="notes"
          class="ms-input"
          placeholder="例如：产品负责人"
          @keydown.enter="submitCreate"
      /></label>
      <template v-else>
        <label class="ms-check"
          ><input
            v-model="defaultLANEnabled"
            type="checkbox"
          />默认开启访客页</label
        >
        <fieldset class="ms-member-picker">
          <legend>成员（按勾选顺序保存）</legend>
          <label v-for="member in people.members" :key="member.id">
            <input
              type="checkbox"
              :checked="selectedMemberIDs.includes(member.id)"
              @change="toggleGroupMember(member.id)"
            />{{ member.name }}
          </label>
        </fieldset>
      </template>
      <p v-if="people.errorMessage" class="ms-field__error" role="alert">
        {{ people.errorMessage }}
      </p>
      <div class="ms-modal-actions">
        <template v-if="editingID">
          <BaseButton variant="quiet" @click="requestRemoveCurrent(false)">{{
            createKind === 'group' ? '删除小组' : '归档成员'
          }}</BaseButton>
          <BaseButton
            v-if="createKind === 'member'"
            variant="quiet"
            @click="requestRemoveCurrent(true)"
            >永久删除</BaseButton
          >
        </template>
        <BaseButton variant="quiet" @click="closeCreateModal">取消</BaseButton
        ><BaseButton
          variant="primary"
          :busy="people.saving"
          :disabled="!canSubmit"
          @click="submitCreate"
          >{{ editingID ? '保存' : '创建' }}</BaseButton
        >
      </div>
    </section>
  </div>

  <div v-if="voiceMember" class="ms-modal-backdrop" @keydown.esc="closeVoice">
    <section
      ref="voiceDialog"
      class="ms-modal ms-modal--wide"
      role="dialog"
      aria-modal="true"
      aria-labelledby="voice-title"
      tabindex="-1"
      @keydown.tab="trapDialogFocus"
    >
      <div class="ms-card-head">
        <div>
          <h2 id="voice-title">录入{{ voiceMember.name }}的声纹</h2>
          <p class="ms-muted">
            请选择一种会前采集方式。录音和声纹特征仅保存在本机。
          </p>
        </div>
        <BaseButton
          variant="quiet"
          :disabled="voice.startingRecording"
          @click="closeVoice"
          >关闭</BaseButton
        >
      </div>
      <div
        v-if="voice.errorMessage"
        class="ms-notice ms-notice--danger"
        role="alert"
      >
        <div>
          <strong>声纹操作未完成</strong>
          <p>{{ voice.errorMessage }}</p>
        </div>
      </div>
      <div class="ms-tabs" role="tablist" aria-label="声纹采集方式">
        <button
          :class="{ 'is-current': voiceSource === 'record' }"
          role="tab"
          :aria-selected="voiceSource === 'record'"
          @click="voiceSource = 'record'"
        >
          直接录音
        </button>
        <button
          :class="{ 'is-current': voiceSource === 'upload' }"
          role="tab"
          :aria-selected="voiceSource === 'upload'"
          @click="voiceSource = 'upload'"
        >
          上传 WAV
        </button>
      </div>
      <section v-if="voiceSource === 'record'" class="ms-voice-source">
        <div class="ms-notice">
          <div>
            <strong>自然朗读</strong>
            <p>建议录制 10–30 秒，并与麦克风保持稳定距离。最长可录制 60 秒。</p>
          </div>
        </div>
        <label class="ms-field"
          ><span>麦克风</span
          ><select
            v-model="selectedDevice"
            class="ms-input"
            :disabled="voice.recording || voice.startingRecording"
          >
            <option
              v-for="device in voice.devices"
              :key="device.id"
              :value="device.id"
            >
              {{ device.name }}{{ device.is_default ? '（默认）' : '' }}
            </option>
          </select></label
        >
        <label class="ms-field"
          ><span>录音环境</span
          ><select
            v-model="environment"
            class="ms-input"
            :disabled="voice.recording || voice.startingRecording"
          >
            <option value="quiet">安静环境</option>
            <option value="meeting_room">会议室</option>
            <option value="other">其他环境</option>
          </select></label
        >
        <div class="ms-recording-status">
          <div>
            <strong>{{
              voice.recording
                ? '正在录音'
                : voice.startingRecording
                  ? '正在连接麦克风'
                  : voice.processing
                    ? '正在评估并生成声纹'
                    : '等待录音'
            }}</strong>
            <p class="ms-meta">
              {{ formatDuration(voice.recordingDurationMS) }} / 01:00
            </p>
          </div>
          <span
            class="ms-audio-meter"
            :class="{ 'is-active': voice.recording }"
            :style="{ '--ms-recording-level': String(voice.recordingLevel) }"
            role="img"
            :aria-label="recordingLevelText"
            ><i v-for="index in 5" :key="index"
          /></span>
        </div>
        <div class="ms-modal-actions">
          <BaseButton
            v-if="voice.recording"
            variant="quiet"
            @click="cancelCurrentRecording"
            >取消录音</BaseButton
          ><BaseButton
            variant="primary"
            :busy="voice.processing || voice.startingRecording"
            :disabled="
              !selectedDevice || voice.processing || voice.startingRecording
            "
            @click="toggleRecording"
            >{{
              voice.recording
                ? '停止并保存'
                : voice.startingRecording
                  ? '正在连接麦克风'
                  : '开始录音'
            }}</BaseButton
          >
        </div>
      </section>
      <section v-else class="ms-voice-source">
        <div class="ms-notice">
          <div>
            <strong>选择单人语音文件</strong>
            <p>支持最长 60 秒的 PCM WAV。原始上传文件不会长期保留。</p>
          </div>
        </div>
        <label class="ms-field"
          ><span>WAV 文件</span>
          <div class="ms-field__row">
            <input
              class="ms-input"
              readonly
              :value="voice.selectedFileName || '尚未选择文件'"
            /><BaseButton
              variant="quiet"
              :disabled="voice.processing"
              @click="voice.chooseWAV()"
              >选择文件</BaseButton
            >
          </div></label
        >
        <label class="ms-field"
          ><span>录音环境</span
          ><select v-model="environment" class="ms-input">
            <option value="quiet">安静环境</option>
            <option value="meeting_room">会议室</option>
            <option value="other">其他环境</option>
          </select></label
        >
        <div class="ms-modal-actions">
          <BaseButton
            variant="primary"
            :busy="voice.processing"
            :disabled="!voice.selectedToken || voice.processing"
            @click="voice.processWAV(environment)"
            >处理并保存</BaseButton
          >
        </div>
      </section>
      <section class="ms-sample-list" aria-labelledby="sample-list-title">
        <div class="ms-card-head">
          <h3 id="sample-list-title">已有样本</h3>
          <BaseButton
            v-if="voice.samples.length"
            variant="quiet"
            @click="requestDeleteAllVoice"
            >删除全部声纹</BaseButton
          >
        </div>
        <p v-if="voice.loading" class="ms-progress-label">
          <span class="ms-spinner" />正在读取样本…
        </p>
        <p v-else-if="!voice.samples.length" class="ms-muted">
          还没有声纹样本。
        </p>
        <div
          v-for="sample in voice.samples"
          :key="sample.id"
          class="ms-sample-row"
        >
          <div>
            <strong>{{
              sample.source_kind === 'recorded'
                ? '软件内录音'
                : sample.source_name
            }}</strong>
            <p class="ms-meta">
              {{ formatDuration(sample.duration_ms) }} ·
              {{
                sample.quality_state === 'accepted'
                  ? '质量可用'
                  : sample.quality_state === 'rejected'
                    ? '质量未通过'
                    : '处理中'
              }}
            </p>
          </div>
          <BaseButton variant="quiet" @click="voice.deleteSample(sample.id)"
            >删除</BaseButton
          >
        </div>
      </section>
    </section>
  </div>

  <div
    v-if="confirmation"
    class="ms-modal-backdrop"
    @keydown.esc="confirmation = undefined"
  >
    <section
      ref="confirmationDialog"
      class="ms-modal"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="destructive-title"
      aria-describedby="destructive-description"
      tabindex="-1"
      @keydown.tab="trapDialogFocus"
    >
      <h2 id="destructive-title">{{ confirmation.title }}</h2>
      <p id="destructive-description" class="ms-muted">
        {{ confirmation.message }}
      </p>
      <div class="ms-modal-actions">
        <BaseButton variant="quiet" @click="confirmation = undefined"
          >取消</BaseButton
        >
        <BaseButton
          variant="danger"
          :busy="people.saving || voice.processing"
          @click="confirmDestructive"
          >{{ confirmation.actionLabel }}</BaseButton
        >
      </div>
    </section>
  </div>
</template>
