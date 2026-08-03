<script lang="ts" setup>
import { computed, onMounted, ref, watch } from 'vue'

import { useMeetingStore } from '../../stores/meeting'
import { useASRStore } from '../../stores/asr'
import { useLANStore } from '../../stores/lan'

const meeting = useMeetingStore()
const asr = useASRStore()
const lan = useLANStore()
const selectedGroupID = ref('')
const selectedMemberIDs = ref<string[]>([])
const temporaryNames = ref<string[]>([])
const newTemporaryName = ref('')
const subject = ref('')
const meetingNo = ref('')
const microphoneID = ref('')
const advanced = ref(false)
const asrMode = ref<'realtime' | 'record_only'>('record_only')

const canStart = computed(
  () =>
    !meeting.loading &&
    !meeting.saving &&
    Boolean(microphoneID.value) &&
    (!lan.enabled || Boolean(lan.selectedInterfaceID)) &&
    selectedMemberIDs.value.length + temporaryNames.value.length > 0,
)

onMounted(async () => {
  await Promise.all([
    meeting.loadCreateScreen(),
    asr.loadSettings(),
    lan.loadInterfaces(),
  ])
  subject.value = meeting.draft.subject
  meetingNo.value = meeting.draft.meetingNo
  microphoneID.value =
    meeting.microphones.find((device) => device.is_default)?.id ??
    meeting.microphones[0]?.id ??
    ''
  asrMode.value = asr.legacyReady ? 'realtime' : 'record_only'
})

watch(selectedGroupID, (groupID) => {
  const group = meeting.groups.find((candidate) => candidate.id === groupID)
  selectedMemberIDs.value = group
    ? group.members.map((member) => member.id)
    : []
  if (group) lan.enabled = group.default_lan_enabled
})

/** addTemporaryParticipant 把已去除首尾空白的临时姓名加入当前 UI 顺序。 */
function addTemporaryParticipant(): void {
  const name = newTemporaryName.value.trim()
  if (!name) return
  temporaryNames.value.push(name)
  newTemporaryName.value = ''
}

/** submit 交给后端完成全部校验、预检和首帧提交。 */
async function submit(): Promise<void> {
  if (!canStart.value) return
  await meeting.startMeeting({
    meetingNo: meetingNo.value,
    suggestedMeetingNo: meeting.draft.meetingNo,
    subject: subject.value,
    memberIds: selectedMemberIDs.value,
    temporaryNames: temporaryNames.value,
    microphoneId: microphoneID.value,
    asrMode: asrMode.value,
    lanEnabled: lan.enabled,
    lanInterfaceId: lan.enabled ? lan.selectedInterfaceID : '',
  })
}
</script>

<template>
  <section class="ms-meeting-head">
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
  >
    {{ meeting.errorMessage }}
  </p>

  <section class="ms-meeting-split" :aria-busy="meeting.loading">
    <div class="ms-card ms-meeting-card">
      <div class="ms-card-head">
        <h2>参会人</h2>
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

      <fieldset class="ms-choice-list">
        <legend class="ms-visually-hidden">选择参会成员</legend>
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
      </fieldset>

      <div class="ms-temporary-row">
        <input
          v-model="newTemporaryName"
          class="ms-input"
          placeholder="临时成员姓名"
          @keydown.enter.prevent="addTemporaryParticipant"
        />
        <button
          class="ms-button ms-button--quiet"
          type="button"
          @click="addTemporaryParticipant"
        >
          添加临时成员
        </button>
      </div>
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
      <div v-if="advanced" class="ms-advanced-grid">
        <label class="ms-field ms-field--compact">
          <span>会议主题（可选）</span>
          <input v-model="subject" class="ms-input" maxlength="200" />
        </label>
        <label class="ms-field ms-field--compact">
          <span>会议号</span>
          <input v-model="meetingNo" class="ms-input ms-input--mono" />
        </label>
        <label class="ms-field ms-field--compact">
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
          <div class="ms-card-head">
            <div>
              <h3 id="lan-create-title">局域网访客页</h3>
              <p class="ms-help">允许同一私有网络中的访客发送消息和资料。</p>
            </div>
            <label class="ms-switch-label">
              <input v-model="lan.enabled" type="checkbox" role="switch" />
              <span>{{ lan.enabled ? '已允许' : '未允许' }}</span>
            </label>
          </div>
          <template v-if="lan.enabled">
            <label class="ms-field ms-field--compact">
              <span>使用的网络</span>
              <select
                v-model="lan.selectedInterfaceID"
                class="ms-input"
                :disabled="lan.loading || !lan.interfaces.length"
              >
                <option value="">请选择私有网络</option>
                <option
                  v-for="item in lan.interfaces"
                  :key="item.id"
                  :value="item.id"
                >
                  {{ item.name }} · {{ item.address
                  }}{{ item.id === lan.recommendedID ? '（推荐）' : '' }}
                </option>
              </select>
            </label>
            <div class="ms-notice ms-notice--warning">
              <div>
                <strong>只在可信的私有网络使用</strong>
                <p>
                  局域网页面使用 HTTP。公共 Wi-Fi 中的其他设备可能观察网络流量。
                </p>
              </div>
            </div>
            <p v-if="lan.warning || lan.errorMessage" class="ms-help">
              {{ lan.errorMessage || lan.warning }}
            </p>
          </template>
        </section>
      </div>

      <fieldset class="ms-asr-mode-options">
        <legend>实时转写</legend>
        <label
          class="ms-choice ms-choice--auth"
          :class="{ 'is-disabled': !asr.legacyReady }"
        >
          <input
            v-model="asrMode"
            type="radio"
            value="realtime"
            :disabled="!asr.legacyReady"
          />
          <span
            ><strong>录音并实时转写</strong
            ><small>{{
              asr.legacyReady ? '使用已保存的火山凭证' : '请先在设置中保存凭证'
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
                  : '需要选择网络'
            }}</strong
          >
        </li>
      </ul>
      <button
        class="ms-button ms-button--primary ms-start-button"
        type="button"
        :disabled="!canStart"
        @click="submit"
      >
        {{ meeting.saving ? '正在取得麦克风音频…' : '开始会议' }}
      </button>
      <p class="ms-help">取得真实首帧并安全写入后，会议才会开始。</p>
    </aside>
  </section>
</template>
