<script lang="ts" setup>
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import type { wails } from '../../../wailsjs/go/models'
import { GetAgentRecoveryCommands } from '../../../wailsjs/go/wails/AgentBinding'

const props = defineProps<{
  open: boolean
  meetingId: string
}>()
const emit = defineEmits<{
  'update:open': [value: boolean]
}>()
const dialog = ref<HTMLElement>()
const commandInput = ref<HTMLTextAreaElement>()
const directoryInput = ref<HTMLTextAreaElement>()
const promptInput = ref<HTMLTextAreaElement>()
const handoff = ref<wails.AgentRecoveryCommandsDTO>()
const loading = ref(false)
const errorMessage = ref('')
const copyNotice = ref('')
const copyError = ref('')
const fileMode = ref<FileContinuationMode>('prompt')
let restoreTarget: HTMLElement | null = null
let backgroundInertByDialog = false
let requestVersion = 0

type FileContinuationMode = 'prompt' | 'command'

/** closeDialog 关闭接续面板并恢复触发按钮的焦点。 */
function closeDialog(): void {
  emit('update:open', false)
}

/** restoreFocus 在弹窗关闭或卸载后恢复用户原来的操作位置。 */
function restoreFocus(): void {
  restoreTarget?.focus()
  restoreTarget = null
}

/** setBackgroundInert 在弹窗打开期间隔离应用主界面，并保留其他弹窗已有的 inert 状态。 */
function setBackgroundInert(active: boolean): void {
  const shell = document.querySelector<HTMLElement>('.ms-app-shell')
  if (!shell) return
  if (active && !shell.hasAttribute('inert')) {
    shell.setAttribute('inert', '')
    backgroundInertByDialog = true
    return
  }
  if (!active && backgroundInertByDialog) {
    shell.removeAttribute('inert')
    backgroundInertByDialog = false
  }
}

/** loadHandoff 按需读取本场可信命令与文件接续提示词。 */
async function loadHandoff(version: number): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  copyNotice.value = ''
  copyError.value = ''
  handoff.value = undefined
  try {
    const result = await GetAgentRecoveryCommands(props.meetingId)
    if (!isCurrentRequest(version)) return
    if (result.code !== 200 || !result.data) {
      errorMessage.value =
        result.message || '无法准备 Codex 接续信息，请稍后重试。'
      return
    }
    handoff.value = result.data
  } catch {
    if (!isCurrentRequest(version)) return
    errorMessage.value = '无法准备 Codex 接续信息，请稍后重试。'
  } finally {
    if (isCurrentRequest(version)) loading.value = false
  }
}

/** reloadHandoff 主动重试读取当前弹窗的接续信息。 */
function reloadHandoff(): void {
  if (loading.value) return
  fileMode.value = 'prompt'
  void loadHandoff(++requestVersion)
}

/** isCurrentRequest 防止关闭或再次打开后的旧请求覆盖当前状态。 */
function isCurrentRequest(version: number): boolean {
  return props.open && version === requestVersion
}

/** selectedFileContent 返回当前文件接续方式的复制内容和目标文本框。 */
function selectedFileContent(): {
  value: string
  label: string
  target: HTMLTextAreaElement | undefined
} {
  if (fileMode.value === 'command') {
    return {
      value: handoff.value?.directory_command ?? '',
      label: '从文件启动命令',
      target: directoryInput.value,
    }
  }
  return {
    value: handoff.value?.recovery_prompt ?? '',
    label: '提示词',
    target: promptInput.value,
  }
}

/** copySelectedFileContent 复制当前选中的文件接续方式。 */
async function copySelectedFileContent(): Promise<void> {
  const content = selectedFileContent()
  await copyText(content.value, content.label, content.target)
}

/** copyText 仅在用户点击后写入剪贴板，失败时保留可选择文本。 */
async function copyText(
  value: string,
  label: string,
  target?: HTMLTextAreaElement,
): Promise<void> {
  copyNotice.value = ''
  copyError.value = ''
  try {
    await navigator.clipboard.writeText(value)
    copyNotice.value = `已复制${label}`
  } catch {
    copyError.value = `无法写入剪贴板，请手动复制${label}。`
    await nextTick()
    target?.focus()
    target?.select()
  }
}

/** trapFocus 将键盘焦点保持在短任务弹窗内。 */
function trapFocus(event: KeyboardEvent): void {
  if (event.key !== 'Tab' || !dialog.value) return
  const controls = Array.from(
    dialog.value.querySelectorAll<HTMLElement>(
      'button:not([disabled]), textarea:not([disabled]), [href], [tabindex]:not([tabindex="-1"])',
    ),
  )
  if (!controls.length) return
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

watch(
  () => props.open,
  async (open) => {
    if (!open) {
      requestVersion++
      setBackgroundInert(false)
      restoreFocus()
      return
    }
    restoreTarget = document.activeElement as HTMLElement | null
    setBackgroundInert(true)
    fileMode.value = 'prompt'
    const version = ++requestVersion
    await loadHandoff(version)
    if (!isCurrentRequest(version)) return
    await nextTick()
    dialog.value
      ?.querySelector<HTMLElement>('[data-handoff-autofocus]')
      ?.focus()
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  setBackgroundInert(false)
  restoreFocus()
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="ms-modal-backdrop"
      @click.self="closeDialog"
      @keydown.esc="closeDialog"
    >
      <section
        ref="dialog"
        class="ms-modal ms-handoff-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="codex-handoff-title"
        aria-describedby="codex-handoff-description"
        tabindex="-1"
        @keydown="trapFocus"
      >
        <header class="ms-handoff-dialog__header">
          <h2 id="codex-handoff-title">继续处理这场会议</h2>
          <p id="codex-handoff-description" class="ms-muted">
            恢复原对话，或从本地会议文件继续。
          </p>
        </header>
        <div class="ms-handoff-dialog__body">
          <p v-if="loading" class="ms-progress-label" aria-live="polite">
            正在准备 Codex 接续信息…
          </p>
          <div v-else-if="errorMessage" class="ms-handoff-error">
            <p class="ms-notice ms-notice--danger" role="alert">
              {{ errorMessage }}
            </p>
            <button
              class="ms-button ms-button--primary"
              type="button"
              data-handoff-autofocus
              @click="reloadHandoff"
            >
              重试
            </button>
          </div>
          <template v-else-if="handoff">
            <section
              v-if="handoff.thread_available"
              class="ms-handoff-section"
              aria-labelledby="codex-handoff-thread-title"
            >
              <h3 id="codex-handoff-thread-title">恢复原对话</h3>
              <p class="ms-help">
                原 Codex 会话仍在本机时，可保留此前的问答上下文。
              </p>
              <label class="ms-field ms-handoff-field">
                <span>恢复命令</span>
                <textarea
                  ref="commandInput"
                  class="ms-textarea ms-input--mono ms-handoff-input--command"
                  readonly
                  rows="3"
                  :value="handoff.thread_command"
                />
              </label>
              <button
                class="ms-button ms-button--primary"
                type="button"
                data-handoff-autofocus
                @click="
                  copyText(handoff.thread_command, '恢复命令', commandInput)
                "
              >
                复制恢复命令
              </button>
            </section>
            <p
              v-else
              class="ms-notice ms-notice--warning ms-handoff-unavailable"
            >
              本场没有可恢复的原 Codex 对话，仍可从本地会议文件继续。
            </p>
            <section
              class="ms-handoff-section"
              aria-labelledby="codex-handoff-file-title"
            >
              <h3 id="codex-handoff-file-title">从会议文件继续</h3>
              <p class="ms-help">
                在能访问本机会议目录的 Codex 任务中使用提示词或终端命令。
              </p>
              <div
                class="ms-handoff-mode-switch"
                role="tablist"
                aria-label="文件接续方式"
              >
                <button
                  id="codex-handoff-prompt-tab"
                  type="button"
                  role="tab"
                  :aria-selected="fileMode === 'prompt'"
                  :class="{ 'is-current': fileMode === 'prompt' }"
                  @click="fileMode = 'prompt'"
                >
                  接续提示词
                </button>
                <button
                  id="codex-handoff-command-tab"
                  type="button"
                  role="tab"
                  :aria-selected="fileMode === 'command'"
                  :class="{ 'is-current': fileMode === 'command' }"
                  @click="fileMode = 'command'"
                >
                  终端命令
                </button>
              </div>
              <div
                v-if="fileMode === 'prompt'"
                id="codex-handoff-prompt-panel"
                role="tabpanel"
                aria-labelledby="codex-handoff-prompt-tab"
              >
                <label class="ms-field ms-handoff-field">
                  <span>接续提示词</span>
                  <textarea
                    ref="promptInput"
                    class="ms-textarea ms-handoff-input--prompt"
                    readonly
                    rows="6"
                    :value="handoff.recovery_prompt"
                  />
                </label>
              </div>
              <div
                v-else
                id="codex-handoff-command-panel"
                role="tabpanel"
                aria-labelledby="codex-handoff-command-tab"
              >
                <label class="ms-field ms-handoff-field">
                  <span>从文件启动命令</span>
                  <textarea
                    ref="directoryInput"
                    class="ms-textarea ms-input--mono ms-handoff-input--command"
                    readonly
                    rows="3"
                    :value="handoff.directory_command"
                  />
                </label>
              </div>
              <button
                class="ms-button ms-button--primary"
                type="button"
                :data-handoff-autofocus="
                  handoff.thread_available ? undefined : ''
                "
                @click="copySelectedFileContent"
              >
                {{ fileMode === 'prompt' ? '复制提示词' : '复制终端命令' }}
              </button>
            </section>
          </template>
          <p v-if="copyNotice" class="ms-help" aria-live="polite">
            {{ copyNotice }}
          </p>
          <p
            v-if="copyError"
            class="ms-notice ms-notice--warning"
            role="status"
          >
            {{ copyError }}
          </p>
        </div>
        <footer class="ms-handoff-dialog__footer">
          <button
            class="ms-button ms-button--quiet"
            type="button"
            data-handoff-autofocus
            @click="closeDialog"
          >
            关闭
          </button>
        </footer>
      </section>
    </div>
  </Teleport>
</template>
