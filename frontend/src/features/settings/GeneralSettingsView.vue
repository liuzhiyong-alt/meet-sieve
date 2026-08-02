<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

import BaseButton from '../../components/base/BaseButton.vue'
import AppNotice from '../../components/base/AppNotice.vue'
import { useWorkspaceStore } from '../../stores/workspace'
import { useVoiceModelStore } from '../../stores/voiceModel'
import { useASRStore } from '../../stores/asr'
import { useMeetingStore } from '../../stores/meeting'

const workspace = useWorkspaceStore()
const voiceModel = useVoiceModelStore()
const asr = useASRStore()
const meeting = useMeetingStore()
const path = ref('')
const appID = ref('')
const accessToken = ref('')
const confirmClearASR = ref(false)
const activeSection = ref<'general' | 'asr' | 'voice-model'>('general')
let stopVoiceModelListener: (() => void) | undefined
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
  await Promise.all([
    workspace.loadSettings(),
    voiceModel.refresh(),
    asr.loadSettings(),
  ])
  path.value = workspace.settings.savedPath
})

onBeforeUnmount(() => stopVoiceModelListener?.())

watch(
  () => workspace.settings.savedPath,
  (savedPath) => {
    if (!workspace.saving) path.value = savedPath
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

/** saveASR 保存后清空明文草稿，页面只继续展示后端掩码。 */
async function saveASR(): Promise<void> {
  if (await asr.saveLegacy(appID.value, accessToken.value)) {
    appID.value = ''
    accessToken.value = ''
  }
}

/** testASRConnection 使用当前未保存草稿探测连接，不发送会议音频。 */
async function testASRConnection(): Promise<void> {
  await asr.testLegacyConnection(appID.value, accessToken.value)
}

/** clearASRCredentials 在用户二次确认后删除两项 legacy 凭据。 */
async function clearASRCredentials(): Promise<void> {
  confirmClearASR.value = false
  await asr.clearLegacy()
}
</script>

<template>
  <div class="ms-settings-layout">
    <nav class="ms-settings-nav" aria-label="设置分类">
      <button
        :class="{ 'is-current': activeSection === 'general' }"
        @click="activeSection = 'general'"
      >
        通用
      </button>
      <button
        :class="{ 'is-current': activeSection === 'asr' }"
        @click="activeSection = 'asr'"
      >
        实时转写
      </button>
      <button
        :class="{ 'is-current': activeSection === 'voice-model' }"
        @click="activeSection = 'voice-model'"
      >
        声纹模型
      </button>
    </nav>
    <section
      v-if="activeSection === 'general'"
      class="ms-card ms-settings-card"
      aria-labelledby="general-settings-title"
    >
      <p class="ms-eyebrow">设置</p>
      <h1 id="general-settings-title">General</h1>
      <p class="ms-lead">修改会议工作目录将在下次启动时生效。</p>

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
              workspace.fieldError ? 'settings-workspace-path-error' : undefined
            "
            :aria-invalid="Boolean(workspace.fieldError)"
            @blur="workspace.inspect(path)"
          />
          <BaseButton
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

      <div class="ms-path-summary">
        <p>
          <span>当前仍在使用</span
          ><code>{{ workspace.settings.activePath || '—' }}</code>
        </p>
        <p>
          <span>将在下次启动时使用</span
          ><code>{{ workspace.settings.savedPath || '—' }}</code>
        </p>
      </div>

      <p class="ms-help">
        复制会议工作目录前请先退出
        MeetSieve。复制完成后重新打开应用，并在这里选择复制后的目录。
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
      v-else-if="activeSection === 'asr'"
      class="ms-card ms-settings-card"
      aria-labelledby="asr-settings-title"
      :aria-busy="asr.loading || asr.saving || asr.probing"
    >
      <div class="ms-card-head">
        <div>
          <p class="ms-eyebrow">本机配置</p>
          <h1 id="asr-settings-title">火山引擎实时转写</h1>
        </div>
        <span class="ms-status">{{
          asr.legacyReady ? '凭证已保存' : '未配置'
        }}</span>
      </div>
      <p class="ms-lead">
        凭证保存在会议工作目录的数据库中，不会写入日志或原始记录。
      </p>

      <AppNotice
        v-if="
          meeting.current &&
          !['ended', 'interrupted'].includes(meeting.current.lifecycle_state)
        "
        variant="warning"
        title="会议进行中不能修改凭证"
        >请先结束会议。当前录音和实时转写不会被设置页打断。</AppNotice
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

      <fieldset class="ms-auth-options">
        <legend>鉴权方式</legend>
        <label class="ms-choice ms-choice--auth">
          <input type="radio" name="asr-auth" checked />
          <span
            ><strong>App ID + Access Token</strong
            ><small>已支持实时转写</small></span
          >
        </label>
        <label class="ms-choice ms-choice--auth is-disabled">
          <input type="radio" name="asr-auth" disabled />
          <span
            ><strong>API Key</strong
            ><small>官方实时协议暂未开放，不能用于当前连接</small></span
          >
        </label>
      </fieldset>

      <div class="ms-field">
        <label for="asr-app-id">App ID</label>
        <input
          id="asr-app-id"
          v-model="appID"
          class="ms-input ms-input--mono"
          type="password"
          autocomplete="off"
          :placeholder="asr.settings.app_id_mask || '输入 App ID'"
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
            asr.settings.app_id_configured
              ? '已保存；留空会保留当前值。'
              : '尚未保存。'
          }}
        </p>
      </div>
      <div class="ms-field">
        <label for="asr-access-token">Access Token</label>
        <input
          id="asr-access-token"
          v-model="accessToken"
          class="ms-input ms-input--mono"
          type="password"
          autocomplete="off"
          :placeholder="asr.settings.access_token_mask || '输入 Access Token'"
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
            asr.settings.access_token_configured
              ? '已保存；留空会保留当前值。'
              : '尚未保存。'
          }}
        </p>
      </div>

      <AppNotice variant="info" title="连接测试不发送真实音频">
        测试只验证 WebSocket
        连接和初始化。请在两个输入框中填写本次要测试的凭证；真实转写需要在会议中验证。
      </AppNotice>
      <div class="ms-actions">
        <BaseButton
          variant="primary"
          :busy="asr.saving"
          :disabled="
            asr.saving ||
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
          :disabled="asr.probing || !appID.trim() || !accessToken.trim()"
          @click="testASRConnection"
          >测试连接</BaseButton
        >
        <BaseButton
          variant="quiet"
          :disabled="
            asr.saving ||
            !asr.legacyReady ||
            Boolean(
              meeting.current &&
              !['ended', 'interrupted'].includes(
                meeting.current.lifecycle_state,
              ),
            )
          "
          @click="confirmClearASR = true"
          >清除已保存凭证</BaseButton
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
          <h1 id="voice-model-title">声纹模型</h1>
        </div>
        <span class="ms-status">{{ modelStateText }}</span>
      </div>
      <AppNotice
        v-if="!voiceModel.model.usable"
        :variant="
          voiceModel.model.state === 'verification_failed' ? 'danger' : 'info'
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
        <p><span>模型来源</span><strong>MeetSieve GitHub Releases</strong></p>
        <p>
          <span>版本</span
          ><strong>{{ voiceModel.model.modelVersion || '—' }}</strong>
        </p>
        <p>
          <span>安装位置</span><strong>{{ voiceModel.model.location }}</strong>
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
        <h2 id="clear-asr-title">清除实时转写凭证？</h2>
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
