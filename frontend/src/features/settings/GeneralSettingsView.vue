<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { EventsOn } from '../../../wailsjs/runtime/runtime'
import { useRoute, useRouter } from 'vue-router'
import {
  GetAudioSettings,
  SaveAudioSettings,
  TestAudioDevice,
} from '../../../wailsjs/go/wails/AudioSettingsBinding'
import type { wails } from '../../../wailsjs/go/models'

import BaseButton from '../../components/base/BaseButton.vue'
import AppNotice from '../../components/base/AppNotice.vue'
import { useWorkspaceStore } from '../../stores/workspace'
import { useVoiceModelStore } from '../../stores/voiceModel'
import { useASRStore } from '../../stores/asr'
import { useMeetingStore } from '../../stores/meeting'
import { useAgentStore } from '../../stores/agent'
import { useMinutesStore } from '../../stores/minutes'
import { useDiagnosticsStore } from '../../stores/diagnostics'
import { dirtyEditRegistry } from '../../router/dirty'
import { setBreadcrumbTitles } from '../../router/breadcrumb'

const props = defineProps<{ section?: string }>()
const route = useRoute()
const router = useRouter()

const workspace = useWorkspaceStore()
const voiceModel = useVoiceModelStore()
const asr = useASRStore()
const meeting = useMeetingStore()
const agent = useAgentStore()
const minutes = useMinutesStore()
const diagnostics = useDiagnosticsStore()
const audio = ref<wails.AudioSettingsDTO>()
const audioDeviceID = ref('')
const audioBaselineDeviceID = ref('')
const audioError = ref('')
const audioNotice = ref('')
const path = ref('')
const apiKey = ref('')
const confirmClearASR = ref(false)
const codexPath = ref('')
const wakeWord = ref('AI 助手')
const minutePrompt = ref('')
const activeSection = ref<
  'general' | 'audio' | 'asr' | 'codex' | 'minutes' | 'voice-model'
>('general')
const sectionLabels = {
  general: '通用',
  audio: '录音',
  asr: '实时转写',
  codex: 'Codex',
  minutes: '会议纪要',
  'voice-model': '声纹模型',
} as const
let stopVoiceModelListener: (() => void) | undefined
let stopWakeTestListener: (() => void) | undefined
const unregisterDirty: Array<() => void> = []
const modelStateText = computed(() => {
  if (voiceModel.model.usable) return '已安装并可用'
  if (voiceModel.model.state === 'initializing') return '正在初始化'
  if (voiceModel.model.state === 'verification_failed') return '校验失败'
  if (voiceModel.model.state === 'ready') return '运行时不可用'
  return '未安装'
})

onMounted(async () => {
  // 后台模型初始化结束后立即同步设置页，不要求用户重新进入页面。
  stopVoiceModelListener = EventsOn('voice.model.changed', () => {
    void voiceModel.refresh()
  })
  stopWakeTestListener = EventsOn(
    'settings.wake_word_test.changed',
    (event) => {
      agent.applyWakeTestEvent(event)
    },
  )
  await Promise.all([
    workspace.loadSettings(),
    voiceModel.refresh(),
    asr.loadSettings(),
    agent.loadSettings(),
    minutes.loadSettings(),
    diagnostics.refreshScan(),
    loadAudioSettings(),
  ])
  path.value = workspace.settings.savedPath
  codexPath.value = agent.settings.codex_executable_path
  wakeWord.value = agent.settings.wake_word
  minutePrompt.value = minutes.settings.prompt
  registerDirtyEditors()
})

watch(
  () => props.section,
  (section) => {
    if (
      ['general', 'audio', 'asr', 'codex', 'minutes', 'voice-model'].includes(
        section ?? '',
      )
    ) {
      activeSection.value = section as typeof activeSection.value
      setBreadcrumbTitles(route.path, {
        current: sectionLabels[activeSection.value],
      })
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  stopVoiceModelListener?.()
  stopWakeTestListener?.()
  if (agent.wakeTest.state === 'running') void agent.stopWakeTest()
  unregisterDirty.splice(0).forEach((unregister) => unregister())
})

watch(
  () => workspace.settings.savedPath,
  (savedPath) => {
    if (!workspace.saving) path.value = savedPath
  },
)

watch(
  () => agent.settings.updated_at,
  () => {
    if (!agent.saving) {
      codexPath.value = agent.settings.codex_executable_path
      wakeWord.value = agent.settings.wake_word
    }
  },
)

watch(
  () => minutes.settings.updated_at,
  () => {
    if (!minutes.settingsSaving) minutePrompt.value = minutes.settings.prompt
  },
)

/** choosePath 仅把系统对话框选择结果带回输入框。 */
async function choosePath(): Promise<void> {
  const chosen = await workspace.chooseDirectory()
  if (chosen) {
    path.value = chosen
    await workspace.inspect(path.value)
  }
}

/** saveASR 保存后清空明文草稿，避免已保存凭证继续触发 dirty guard。 */
async function saveASR(): Promise<boolean> {
  const saved = await asr.saveAPIKey(apiKey.value)
  if (saved) apiKey.value = ''
  return saved
}

/** testASRConnection 使用当前未保存草稿探测连接，不发送会议音频。 */
async function testASRConnection(): Promise<void> {
  await asr.testAPIKeyConnection(apiKey.value)
}

/** clearASRCredentials 在用户二次确认后删除已保存的 APP Key。 */
async function clearASRCredentials(): Promise<void> {
  confirmClearASR.value = false
  await asr.clearAPIKey()
}

/** saveAgentSettings 保存后继续由后端规范化值回填表单。 */
async function saveAgentSettings(): Promise<void> {
  await agent.saveSettings(wakeWord.value, codexPath.value)
}

/** saveMinutesSettings 保存生成会议纪要时使用的业务要求。 */
async function saveMinutesSettings(): Promise<boolean> {
  const saved = await minutes.saveSettings(minutePrompt.value)
  if (saved) minutePrompt.value = minutes.settings.prompt
  return saved
}

/** restoreDefaultMinutesSettings 清除自定义内容并回填当前内置默认要求。 */
async function restoreDefaultMinutesSettings(): Promise<void> {
  const restored = await minutes.restoreDefault()
  if (restored) minutePrompt.value = minutes.settings.prompt
}

/** startWakeTest 使用已保存 ASR 凭据和系统当前麦克风。 */
async function startWakeTest(): Promise<void> {
  await agent.startWakeTest()
}

/** selectSection 使用正式路由保存设置分类。 */
function selectSection(section: typeof activeSection.value): void {
  void router.push(`/settings/${section}`)
}

/** loadAudioSettings 独立读取默认麦克风和实时设备列表。 */
async function loadAudioSettings(): Promise<void> {
  const result = await GetAudioSettings()
  if (result.code !== 200 || !result.data) {
    audioError.value = result.message
    return
  }
  audio.value = result.data
  audioDeviceID.value =
    result.data.default_microphone_id ||
    result.data.devices.find((device) => device.is_default)?.id ||
    result.data.devices[0]?.id ||
    ''
  audioBaselineDeviceID.value = audioDeviceID.value
}

/** saveAudioSettings 只保存录音分类，不影响其他设置。 */
async function saveAudioSettings(): Promise<void> {
  const result = await SaveAudioSettings(audioDeviceID.value)
  if (result.code !== 200 || !result.data) {
    audioError.value = result.message
    return
  }
  audio.value = result.data
  audioBaselineDeviceID.value = audioDeviceID.value
  audioError.value = ''
  audioNotice.value = '默认麦克风已保存'
}

/** testAudioDevice 执行真实设备探测，结果不参与保存事务。 */
async function testAudioDevice(): Promise<void> {
  const result = await TestAudioDevice(audioDeviceID.value)
  audioError.value = result.code === 200 ? '' : result.message
  audioNotice.value = result.code === 200 ? '麦克风测试通过' : ''
}

/** formatBytes 以易读单位展示真实扫描字节。 */
function formatBytes(value = 0): string {
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  if (value < 1024 * 1024 * 1024)
    return `${(value / 1024 / 1024).toFixed(1)} MB`
  return `${(value / 1024 / 1024 / 1024).toFixed(1)} GB`
}

/** formatScanStage 将内部扫描阶段转换为用户可理解的状态。 */
function formatScanStage(stage?: string): string {
  switch (stage) {
    case 'checking-volume':
      return '正在检查磁盘'
    case 'scanning-meetings':
      return '正在扫描会议'
    case 'scanning-database-backups':
      return '正在扫描备份与日志'
    case 'scanning-derived-temp':
      return '正在扫描临时文件'
    case 'finalizing-summary':
      return '正在汇总结果'
    case 'completed':
      return '扫描完成'
    case 'failed':
      return '扫描失败'
    default:
      return '尚未扫描'
  }
}

/** registerDirtyEditors 分别保护各设置分类，不把测试和扫描命令登记为 dirty。 */
function registerDirtyEditors(): void {
  unregisterDirty.push(
    dirtyEditRegistry.register({
      id: 'settings-general',
      label: '通用设置',
      isDirty: () =>
        activeSection.value === 'general' &&
        path.value !== workspace.settings.savedPath,
      canSave: () => Boolean(path.value && workspace.settings.editable),
      save: () => workspace.save(path.value),
      discard: () => {
        path.value = workspace.settings.savedPath
      },
    }),
    dirtyEditRegistry.register({
      id: 'settings-audio',
      label: '录音设置',
      isDirty: () =>
        activeSection.value === 'audio' &&
        audioDeviceID.value !== audioBaselineDeviceID.value,
      canSave: () => Boolean(audioDeviceID.value),
      save: async () => {
        await saveAudioSettings()
        return !audioError.value
      },
      discard: () => {
        audioDeviceID.value = audioBaselineDeviceID.value
      },
    }),
    dirtyEditRegistry.register({
      id: 'settings-asr',
      label: '实时转写凭证',
      isDirty: () =>
        activeSection.value === 'asr' && Boolean(apiKey.value.trim()),
      canSave: () => Boolean(apiKey.value.trim()),
      save: saveASR,
      discard: () => {
        apiKey.value = ''
      },
    }),
    dirtyEditRegistry.register({
      id: 'settings-codex',
      label: 'Codex 设置',
      isDirty: () =>
        activeSection.value === 'codex' &&
        (codexPath.value !== agent.settings.codex_executable_path ||
          wakeWord.value !== agent.settings.wake_word),
      canSave: () => Boolean(wakeWord.value.trim()),
      save: () => agent.saveSettings(wakeWord.value, codexPath.value),
      discard: () => {
        codexPath.value = agent.settings.codex_executable_path
        wakeWord.value = agent.settings.wake_word
      },
    }),
    dirtyEditRegistry.register({
      id: 'settings-minutes',
      label: '会议纪要设置',
      isDirty: () =>
        activeSection.value === 'minutes' &&
        minutePrompt.value !== minutes.settings.prompt,
      canSave: () => Boolean(minutePrompt.value.trim()),
      save: saveMinutesSettings,
      discard: () => {
        minutePrompt.value = minutes.settings.prompt
      },
    }),
  )
}
</script>

<template>
  <div class="ms-settings-page">
    <header class="ms-settings-page-head">
      <p class="ms-eyebrow">本机配置</p>
      <h1>设置</h1>
      <p class="ms-lead">每个分类独立保存。配置内容与当前客户端保持一致。</p>
    </header>
    <div class="ms-settings-layout">
      <nav class="ms-settings-nav" aria-label="设置分类">
        <button
          :class="{ 'is-current': activeSection === 'general' }"
          @click="selectSection('general')"
        >
          通用
        </button>
        <button
          :class="{ 'is-current': activeSection === 'audio' }"
          @click="selectSection('audio')"
        >
          录音
        </button>
        <button
          :class="{ 'is-current': activeSection === 'asr' }"
          @click="selectSection('asr')"
        >
          实时转写
        </button>
        <button
          :class="{ 'is-current': activeSection === 'codex' }"
          @click="selectSection('codex')"
        >
          Codex
        </button>
        <button
          :class="{ 'is-current': activeSection === 'minutes' }"
          @click="selectSection('minutes')"
        >
          会议纪要
        </button>
        <button
          :class="{ 'is-current': activeSection === 'voice-model' }"
          @click="selectSection('voice-model')"
        >
          声纹模型
        </button>
      </nav>
      <div class="ms-settings-content">
        <template v-if="activeSection === 'general'">
          <section
            class="ms-card ms-settings-card ms-settings-section-card"
            aria-labelledby="general-settings-title"
          >
            <div class="ms-card-head">
              <div>
                <p class="ms-eyebrow">通用设置</p>
                <h2 id="general-settings-title">会议工作目录</h2>
                <p class="ms-help">修改后的目录将在下次启动时使用。</p>
              </div>
              <span class="ms-status ms-status--success">{{
                workspace.settings.restartRequired ? '重启后生效' : '目录可写'
              }}</span>
            </div>

            <AppNotice
              v-if="workspace.settings.disabledReason === 'meeting_in_progress'"
              variant="warning"
              title="暂时不能修改工作目录"
              >请先结束会议再修改工作目录。</AppNotice
            >
            <AppNotice
              v-if="workspace.notice"
              variant="info"
              title="已保存"
              aria-live="polite"
              >{{ workspace.notice }}</AppNotice
            >

            <div class="ms-field">
              <label for="settings-workspace-path">会议工作目录</label>
              <div class="ms-field__row">
                <input
                  id="settings-workspace-path"
                  v-model="path"
                  class="ms-input ms-input--mono"
                  autocomplete="off"
                  :disabled="!workspace.settings.editable || workspace.saving"
                  :aria-describedby="
                    workspace.fieldError
                      ? 'settings-workspace-path-error'
                      : undefined
                  "
                  :aria-invalid="Boolean(workspace.fieldError)"
                  @blur="workspace.inspect(path)"
                />
                <BaseButton
                  class="ms-workspace-choose"
                  :disabled="!workspace.settings.editable || workspace.saving"
                  variant="quiet"
                  @click="choosePath"
                  >选择…</BaseButton
                >
              </div>
              <p
                v-if="workspace.fieldError"
                id="settings-workspace-path-error"
                class="ms-field__error"
                role="alert"
              >
                {{ workspace.fieldError }}
              </p>
            </div>

            <p class="ms-help">
              如需搬移会议数据，请先退出
              MeetSieve，复制完整工作目录后再选择新目录。
            </p>
            <BaseButton
              variant="primary"
              :busy="workspace.saving"
              :disabled="
                !workspace.settings.editable ||
                !path ||
                path === workspace.settings.savedPath
              "
              @click="workspace.save(path)"
              >保存</BaseButton
            >
          </section>
          <section
            class="ms-card ms-settings-card ms-settings-section-card"
            aria-labelledby="storage-diagnostics-title"
          >
            <div class="ms-card-head">
              <div>
                <p class="ms-eyebrow">存储与诊断</p>
                <h2 id="storage-diagnostics-title">工作目录占用</h2>
                <p class="ms-help">扫描只统计真实占用，不会删除或修复文件。</p>
              </div>
              <span class="ms-status">{{
                formatScanStage(diagnostics.scan?.stage)
              }}</span>
            </div>
            <AppNotice
              v-if="diagnostics.errorMessage"
              variant="danger"
              title="诊断操作未完成"
              >{{ diagnostics.errorMessage }}</AppNotice
            >
            <AppNotice
              v-if="diagnostics.notice"
              variant="info"
              title="诊断包已导出"
              >{{ diagnostics.notice }}</AppNotice
            >
            <dl v-if="diagnostics.scan?.scanned_at" class="ms-fact-grid">
              <div>
                <dt>工作目录</dt>
                <dd>{{ formatBytes(diagnostics.scan.workspace_bytes) }}</dd>
              </div>
              <div>
                <dt>录音</dt>
                <dd>
                  {{ formatBytes(diagnostics.scan.categories.Recordings) }}
                </dd>
              </div>
              <div>
                <dt>附件</dt>
                <dd>
                  {{ formatBytes(diagnostics.scan.categories.Attachments) }}
                </dd>
              </div>
              <div>
                <dt>可用磁盘</dt>
                <dd>{{ formatBytes(diagnostics.scan.available_bytes) }}</dd>
              </div>
            </dl>
            <div class="ms-actions">
              <BaseButton
                variant="quiet"
                :disabled="diagnostics.scan?.running"
                @click="diagnostics.startScan()"
                >{{
                  diagnostics.scan?.running ? '正在扫描…' : '扫描存储占用'
                }}</BaseButton
              >
              <BaseButton variant="quiet" @click="diagnostics.exportGlobal()"
                >导出诊断包</BaseButton
              >
              <BaseButton variant="quiet" @click="diagnostics.openLogs()"
                >打开日志目录</BaseButton
              >
            </div>
          </section>
        </template>
        <section
          v-else-if="activeSection === 'audio'"
          class="ms-card ms-settings-card"
          aria-labelledby="audio-settings-title"
        >
          <div class="ms-card-head">
            <div>
              <p class="ms-eyebrow">本机配置</p>
              <h2 id="audio-settings-title">录音</h2>
              <p class="ms-help">默认麦克风独立保存；测试结果不会修改设置。</p>
            </div>
          </div>
          <AppNotice
            v-if="audioError"
            variant="danger"
            title="录音设置未完成"
            >{{ audioError }}</AppNotice
          >
          <AppNotice v-if="audioNotice" variant="info" title="操作完成">{{
            audioNotice
          }}</AppNotice>
          <label class="ms-field"
            ><span>默认麦克风</span
            ><select v-model="audioDeviceID" class="ms-input">
              <option
                v-for="device in audio?.devices || []"
                :key="device.id"
                :value="device.id"
              >
                {{ device.name }} · {{ device.channel_count }} 声道
              </option>
            </select></label
          >
          <p class="ms-help">
            设置版本
            {{ audio?.revision ?? 0 }}。活动会议、收尾或删除期间后端会拒绝修改。
          </p>
          <div class="ms-actions">
            <BaseButton
              variant="primary"
              :disabled="!audioDeviceID"
              @click="saveAudioSettings"
              >保存录音设置</BaseButton
            ><BaseButton
              variant="quiet"
              :disabled="!audioDeviceID"
              @click="testAudioDevice"
              >测试麦克风</BaseButton
            >
          </div>
        </section>
        <section
          v-else-if="activeSection === 'asr'"
          class="ms-card ms-settings-card"
          aria-labelledby="asr-settings-title"
          :aria-busy="asr.loading || asr.saving || asr.probing"
        >
          <div class="ms-card-head">
            <div>
              <p class="ms-eyebrow">本机配置</p>
              <h2 id="asr-settings-title">火山引擎实时转写</h2>
              <p class="ms-help">
                一个 APP Key 同时用于实时转写与缺口补录。凭证保存在会议工作目录的数据库中，不会写入日志或原始记录。
              </p>
            </div>
            <span class="ms-status">{{
              asr.settings.requires_api_key_upgrade
                ? '需要更新'
                : asr.apiKeyReady
                  ? 'APP Key 已保存'
                  : '未配置'
            }}</span>
          </div>
          <AppNotice
            v-if="
              meeting.current &&
              !['ended', 'interrupted'].includes(
                meeting.current.lifecycle_state,
              )
            "
            variant="warning"
            title="会议进行中不能修改凭证"
            >请先结束会议。当前录音和实时转写不会被设置页打断。</AppNotice
          >
          <AppNotice
            v-if="asr.settings.requires_api_key_upgrade"
            variant="warning"
            title="旧版凭证已停用"
            >请在火山引擎控制台复制新版 APP Key。保存后会清除本机旧版 App ID 与
            Access Token。</AppNotice
          >
          <AppNotice
            v-if="asr.errorMessage"
            variant="danger"
            title="实时转写设置未完成"
            role="alert"
            >{{ asr.errorMessage }}</AppNotice
          >
          <AppNotice
            v-if="asr.notice"
            variant="info"
            title="操作完成"
            aria-live="polite"
            >{{ asr.notice
            }}<span v-if="asr.probeLatencyMS"
              >，耗时 {{ asr.probeLatencyMS }} ms</span
            >。</AppNotice
          >

          <div class="ms-field">
            <label for="asr-api-key">APP Key</label>
            <input
              id="asr-api-key"
              v-model="apiKey"
              class="ms-input ms-input--mono"
              type="password"
              name="meetsieve-asr-api-key"
              autocomplete="new-password"
              :placeholder="asr.settings.api_key_mask || '输入 APP Key'"
              :disabled="
                asr.saving ||
                Boolean(
                  meeting.current &&
                  !['ended', 'interrupted'].includes(
                    meeting.current.lifecycle_state,
                  ),
                )
              "
            />
            <p class="ms-help">
              {{
                asr.settings.api_key_configured
                  ? '已保存；留空会保留当前值。'
                  : '尚未保存。'
              }}
            </p>
          </div>

          <AppNotice variant="info" title="连接测试不发送真实音频">
            测试只验证 WebSocket 连接和初始化。请填写本次要测试的 APP
            Key；真实转写需要在会议中验证。
          </AppNotice>
          <div class="ms-actions">
            <BaseButton
              variant="primary"
              :busy="asr.saving"
              :disabled="
                asr.saving ||
                (!apiKey.trim() && !asr.apiKeyReady) ||
                Boolean(
                  meeting.current &&
                  !['ended', 'interrupted'].includes(
                    meeting.current.lifecycle_state,
                  ),
                )
              "
              @click="saveASR"
              >保存更改</BaseButton
            >
            <BaseButton
              variant="quiet"
              :busy="asr.probing"
              :disabled="asr.probing || !apiKey.trim()"
              @click="testASRConnection"
              >测试连接</BaseButton
            >
            <BaseButton
              variant="quiet"
              :disabled="
                asr.saving ||
                !asr.apiKeyReady ||
                Boolean(
                  meeting.current &&
                  !['ended', 'interrupted'].includes(
                    meeting.current.lifecycle_state,
                  ),
                )
              "
              @click="confirmClearASR = true"
              >清除已保存 APP Key</BaseButton
            >
          </div>
        </section>
        <section
          v-else-if="activeSection === 'codex'"
          class="ms-card ms-settings-card"
          aria-labelledby="codex-settings-title"
          :aria-busy="agent.loading || agent.saving || agent.probing"
        >
          <div class="ms-card-head">
            <div>
              <p class="ms-eyebrow">本机配置</p>
              <h2 id="codex-settings-title">Codex</h2>
              <p class="ms-help">
                Codex 使用你本机已有的登录、工具与原生权限配置。
              </p>
            </div>
            <span class="ms-status">{{
              agent.settings.availability.message
            }}</span>
          </div>
          <AppNotice
            v-if="agent.notice"
            variant="info"
            title="已保存"
            aria-live="polite"
          >
            {{ agent.notice }}
          </AppNotice>
          <AppNotice
            v-if="agent.errorMessage"
            variant="danger"
            title="Codex 操作未完成"
            role="alert"
          >
            {{ agent.errorMessage }}
          </AppNotice>

          <div class="ms-field">
            <label for="codex-executable-path">可执行文件路径</label>
            <input
              id="codex-executable-path"
              v-model="codexPath"
              class="ms-input ms-input--mono"
              autocomplete="off"
              placeholder="codex"
              :disabled="agent.saving"
            />
            <p class="ms-help">
              留空时使用系统 PATH 中的 codex；不支持在这里附加命令参数。
            </p>
          </div>

          <div class="ms-model-facts">
            <p>
              <span>登录状态</span
              ><strong>{{
                agent.settings.availability.account_state === 'logged_in'
                  ? '已登录，由 Codex 管理'
                  : agent.settings.availability.account_state === 'logged_out'
                    ? '尚未登录'
                    : '尚未检测'
              }}</strong>
            </p>
            <p>
              <span>协议状态</span
              ><strong>{{
                agent.settings.availability.protocol_state === 'compatible'
                  ? `兼容 ${agent.settings.availability.version || '当前版本'}`
                  : agent.settings.availability.protocol_state ===
                      'incompatible'
                    ? '不兼容'
                    : '尚未检测'
              }}</strong>
            </p>
            <p><span>工具能力</span><strong>MCP、Apps 与本机配置</strong></p>
          </div>
          <AppNotice variant="info" title="权限与审批">
            MeetSieve 沿用 Codex 原生
            sandbox、审批频率和工具权限。需要审批时，只在主持人桌面端显示本次请求。
          </AppNotice>

          <div class="ms-field">
            <label for="agent-wake-word">AI 唤醒词</label>
            <input
              id="agent-wake-word"
              v-model="wakeWord"
              class="ms-input"
              maxlength="16"
              aria-describedby="wake-word-help"
            />
            <p id="wake-word-help" class="ms-help">
              只在主持人机器的 ASR final 句首生效；建议使用 3 至 8 个中文字符。
            </p>
          </div>
          <div class="ms-wake-test" aria-live="polite">
            <div class="ms-card-head">
              <strong>真实唤醒测试</strong>
              <span class="ms-status"
                >{{ agent.wakeTest.matched }}/{{
                  agent.wakeTest.required
                }}</span
              >
            </div>
            <div
              class="ms-wake-progress"
              :aria-label="`三次唤醒测试已通过 ${agent.wakeTest.matched} 次`"
            >
              <span
                v-for="index in 3"
                :key="index"
                :class="{ 'is-ok': index <= agent.wakeTest.matched }"
                >第 {{ index }} 次{{
                  index <= agent.wakeTest.matched ? '通过' : '待测试'
                }}</span
              >
            </div>
            <p class="ms-help">
              {{
                agent.wakeTest.state === 'passed'
                  ? '三次完整口令测试通过。'
                  : agent.wakeTest.state === 'running'
                    ? '每次请先说唤醒词，短暂停顿后说出一条测试指令，指令结束后保持安静 3 秒。'
                    : '测试会使用真实麦克风和已保存的实时转写凭据，验证三次完整口令，不写入录音文件。'
              }}
            </p>
          </div>
          <AppNotice
            v-if="agent.wakeTestError"
            variant="danger"
            title="唤醒测试未启动"
            role="alert"
          >
            {{ agent.wakeTestError }}
          </AppNotice>
          <div class="ms-actions">
            <BaseButton
              variant="primary"
              :busy="agent.saving"
              :disabled="agent.saving"
              @click="saveAgentSettings"
              >保存更改</BaseButton
            >
            <BaseButton
              variant="quiet"
              :busy="agent.probing"
              :disabled="agent.probing"
              @click="agent.probe()"
              >重新检测</BaseButton
            >
            <BaseButton
              v-if="agent.wakeTest.state !== 'running'"
              variant="quiet"
              :disabled="
                Boolean(
                  meeting.current &&
                  !['ended', 'interrupted'].includes(
                    meeting.current.lifecycle_state,
                  ),
                )
              "
              @click="startWakeTest"
              >开始 3 次唤醒测试</BaseButton
            >
            <BaseButton v-else variant="quiet" @click="agent.stopWakeTest()"
              >停止测试</BaseButton
            >
          </div>
        </section>
        <section
          v-else-if="activeSection === 'minutes'"
          class="ms-card ms-settings-card"
          aria-labelledby="minutes-settings-title"
          :aria-busy="minutes.settingsLoading || minutes.settingsSaving"
        >
          <div class="ms-card-head">
            <div>
              <p class="ms-eyebrow">本机配置</p>
              <h2 id="minutes-settings-title">会议纪要</h2>
              <p class="ms-help">
                自定义会议纪要的内容重点、详略程度和表达方式。
              </p>
            </div>
            <span class="ms-status">{{
              minutes.settings.is_default ? '使用默认要求' : '已自定义'
            }}</span>
          </div>
          <AppNotice
            v-if="minutes.settingsError"
            variant="danger"
            title="会议纪要设置未完成"
            role="alert"
            >{{ minutes.settingsError }}</AppNotice
          >
          <AppNotice
            v-if="minutes.settingsNotice"
            variant="info"
            title="已保存"
            aria-live="polite"
            >{{ minutes.settingsNotice }}</AppNotice
          >
          <div class="ms-field">
            <label for="minutes-prompt">会议纪要要求</label>
            <textarea
              id="minutes-prompt"
              v-model="minutePrompt"
              class="ms-input ms-textarea ms-minute-prompt"
              rows="10"
              :disabled="minutes.settingsLoading || minutes.settingsSaving"
              aria-describedby="minutes-prompt-help"
            />
            <p id="minutes-prompt-help" class="ms-help">
              未自定义时使用 MeetSieve
              默认要求。修改后只影响后续生成的会议纪要。
            </p>
          </div>
          <div class="ms-actions">
            <BaseButton
              variant="primary"
              :busy="minutes.settingsSaving"
              :disabled="
                minutes.settingsSaving ||
                !minutePrompt.trim() ||
                minutePrompt === minutes.settings.prompt
              "
              @click="saveMinutesSettings"
              >保存会议纪要要求</BaseButton
            >
            <BaseButton
              v-if="!minutes.settings.is_default"
              variant="quiet"
              :disabled="
                minutes.settingsSaving ||
                minutePrompt !== minutes.settings.prompt
              "
              @click="restoreDefaultMinutesSettings"
              >恢复默认要求</BaseButton
            >
          </div>
        </section>
        <section
          v-else
          class="ms-card ms-settings-card"
          aria-labelledby="voice-model-title"
        >
          <div class="ms-card-head">
            <div>
              <p class="ms-eyebrow">本机配置</p>
              <h2 id="voice-model-title">声纹模型</h2>
              <p class="ms-help">
                管理本机声纹模型的下载、离线导入与完整性校验。
              </p>
            </div>
            <span class="ms-status">{{ modelStateText }}</span>
          </div>
          <AppNotice
            v-if="!voiceModel.model.usable"
            :variant="
              voiceModel.model.state === 'verification_failed'
                ? 'danger'
                : 'info'
            "
            title="安装模型后才能录入声纹"
            >成员和小组仍可正常管理。模型只在本机离线运行，会议中不会下载模型。</AppNotice
          >
          <AppNotice
            v-if="voiceModel.errorMessage"
            variant="danger"
            title="模型操作未完成"
            role="alert"
            >{{ voiceModel.errorMessage }}</AppNotice
          >
          <div class="ms-model-facts">
            <p>
              <span>模型</span><strong>{{ voiceModel.model.modelName }}</strong>
            </p>
            <p>
              <span>模型来源</span><strong>MeetSieve GitHub Releases</strong>
            </p>
            <p>
              <span>版本</span
              ><strong>{{ voiceModel.model.modelVersion || '—' }}</strong>
            </p>
            <p>
              <span>安装位置</span
              ><strong>{{ voiceModel.model.location }}</strong>
            </p>
            <p><span>完整性校验</span><strong>版本与 SHA-256</strong></p>
          </div>
          <p class="ms-help">
            下载和离线导入接受同一个官方模型包。校验失败的文件不会被安装。
          </p>
          <div class="ms-actions">
            <BaseButton
              variant="primary"
              :busy="voiceModel.loading"
              :disabled="voiceModel.loading"
              @click="voiceModel.download()"
              >{{
                voiceModel.model.usable ? '重新下载官方模型' : '下载官方模型'
              }}</BaseButton
            >
            <BaseButton
              variant="quiet"
              :disabled="voiceModel.loading"
              @click="voiceModel.importOffline()"
              >导入离线模型包</BaseButton
            >
          </div>
        </section>
      </div>
    </div>
  </div>

  <Teleport to="body">
    <div
      v-if="confirmClearASR"
      class="ms-modal-backdrop"
      @click.self="confirmClearASR = false"
    >
      <section
        class="ms-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="clear-asr-title"
      >
        <h2 id="clear-asr-title">清除已保存 APP Key？</h2>
        <p class="ms-lead">
          清除后仍可仅录音，但新会议不能使用实时转写，直到重新保存凭证。
        </p>
        <div class="ms-actions ms-modal-actions">
          <BaseButton variant="quiet" autofocus @click="confirmClearASR = false"
            >取消</BaseButton
          >
          <BaseButton variant="primary" @click="clearASRCredentials"
            >确认清除</BaseButton
          >
        </div>
      </section>
    </div>
  </Teleport>
</template>
