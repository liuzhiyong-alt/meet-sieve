<script lang="ts" setup>
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useDeletionStore } from '../../stores/deletion'

const props = defineProps<{
  open: boolean
  meetingId: string
  meetingNo: string
  subject: string
}>()
const emit = defineEmits<{
  'update:open': [value: boolean]
  deleted: []
  failed: []
}>()
const deletion = useDeletionStore()
const confirmation = ref('')
const dialog = ref<HTMLElement>()
let restoreTarget: HTMLElement | null = null

/** closeDialog 关闭弹窗并把焦点还给触发删除的控件。 */
function closeDialog(): void {
  if (deletion.loading) return
  emit('update:open', false)
  restoreFocus()
}

/** restoreFocus 在弹窗关闭或卸载后恢复原操作位置。 */
function restoreFocus(): void {
  restoreTarget?.focus()
  restoreTarget = null
}

/** confirmDelete 使用后端预览事实永久删除整场会议。 */
async function confirmDelete(): Promise<void> {
  if (confirmation.value.trim() !== props.meetingNo) return
  if (!(await deletion.deleteMeeting(confirmation.value.trim()))) return
  emit('update:open', false)
  if (deletion.job?.state === 'completed') emit('deleted')
  else emit('failed')
}

/** trapFocus 把键盘焦点保持在当前危险确认弹窗内。 */
function trapFocus(event: KeyboardEvent): void {
  if (event.key !== 'Tab' || !dialog.value) return
  const controls = Array.from(
    dialog.value.querySelectorAll<HTMLElement>(
      'button:not([disabled]), input:not([disabled]), [href], [tabindex]:not([tabindex="-1"])',
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

/** formatSize 把删除预览字节数转换为稳定的用户可见大小。 */
function formatSize(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

watch(
  () => props.open,
  async (open) => {
    if (!open) {
      restoreFocus()
      return
    }
    restoreTarget = document.activeElement as HTMLElement | null
    confirmation.value = ''
    await deletion.previewMeeting(props.meetingId)
    await nextTick()
    dialog.value?.querySelector<HTMLElement>('button')?.focus()
  },
)
onBeforeUnmount(restoreFocus)
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
        class="ms-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="delete-meeting-title"
        aria-describedby="delete-meeting-description"
        tabindex="-1"
        @keydown="trapFocus"
      >
        <h2 id="delete-meeting-title">永久删除会议？</h2>
        <p id="delete-meeting-description" class="ms-muted">
          将删除会议数据、录音、原始记录、会议纪要、附件和相关文档，且无法恢复。
        </p>
        <div class="ms-delete-summary">
          <strong>{{ subject || '未命名会议' }}</strong>
          <span class="ms-meta ms-input--mono">{{ meetingNo }}</span>
          <span v-if="deletion.preview">
            {{ deletion.preview.file_count }} 个文件，{{
              formatSize(deletion.preview.size_bytes)
            }}
          </span>
          <span v-else-if="deletion.loading">正在读取删除范围…</span>
        </div>
        <label v-if="deletion.preview" class="ms-field">
          <span>输入会议号 {{ meetingNo }} 确认</span>
          <input
            v-model="confirmation"
            class="ms-input ms-input--mono"
            autocomplete="off"
          />
        </label>
        <p
          v-if="deletion.errorMessage"
          class="ms-notice ms-notice--danger"
          role="alert"
        >
          {{ deletion.errorMessage }}
        </p>
        <div class="ms-modal-actions">
          <button
            class="ms-button ms-button--quiet"
            type="button"
            :disabled="deletion.loading"
            @click="closeDialog"
          >
            取消
          </button>
          <button
            class="ms-button ms-button--danger"
            type="button"
            :disabled="
              deletion.loading ||
              !deletion.preview ||
              confirmation.trim() !== meetingNo
            "
            @click="confirmDelete"
          >
            {{ deletion.loading ? '正在删除…' : '永久删除会议' }}
          </button>
        </div>
      </section>
    </div>
  </Teleport>
</template>
