<script lang="ts" setup>
import { ref } from 'vue'

import BaseButton from '../../components/base/BaseButton.vue'
import AppNotice from '../../components/base/AppNotice.vue'
import { useBootstrapStore } from '../../stores/bootstrap'
import { useWorkspaceStore } from '../../stores/workspace'

const bootstrap = useBootstrapStore()
const workspace = useWorkspaceStore()
const path = ref('')
const emit = defineEmits<{ used: [] }>()

/** choosePath 调用系统选择器；取消保留用户已输入内容。 */
async function choosePath(): Promise<void> {
  const chosen = await workspace.chooseDirectory()
  if (chosen) {
    path.value = chosen
    await workspace.inspect(path.value)
  }
}

/** start 使用真实 Wails binding 初始化或接入路径。 */
async function start(): Promise<void> {
  if (!path.value.trim()) {
    workspace.fieldError = '请输入会议工作目录的绝对路径'
    return
  }
  await bootstrap.useWorkspace(path.value)
  if (bootstrap.view === 'ready') {
    emit('used')
  } else if (bootstrap.errorMessage) {
    workspace.fieldError = bootstrap.errorMessage
  }
}
</script>

<template>
  <main class="ms-onboarding">
    <section class="ms-onboarding__panel" aria-labelledby="onboarding-title">
      <p class="ms-eyebrow">MeetSieve</p>
      <h1 id="onboarding-title">首次启动</h1>
      <p class="ms-lead">
        选择一个会议工作目录。MeetSieve 会在其中安全保存本地会议数据。
      </p>

      <div class="ms-field">
        <label for="workspace-path">会议工作目录</label>
        <div class="ms-field__row">
          <input
            id="workspace-path"
            v-model="path"
            class="ms-input ms-input--mono"
            autocomplete="off"
            placeholder="/Users/name/ai-meetings"
            :aria-describedby="
              workspace.fieldError ? 'workspace-path-error' : undefined
            "
            :aria-invalid="Boolean(workspace.fieldError)"
            :disabled="bootstrap.loading"
            @blur="workspace.inspect(path)"
          />
          <BaseButton
            :disabled="bootstrap.loading"
            variant="quiet"
            @click="choosePath"
            >选择…</BaseButton
          >
        </div>
        <p
          v-if="workspace.fieldError"
          id="workspace-path-error"
          class="ms-field__error"
          role="alert"
        >
          {{ workspace.fieldError }}
        </p>
      </div>

      <AppNotice variant="neutral" title="即将创建的内容">
        <code>ai-meetings/data/meetings.db</code>、<code>backups/</code>、<code
          >voice-samples/</code
        >
        和 <code>meetings/</code>。
      </AppNotice>
      <AppNotice v-if="workspace.inspecting" variant="info" title="正在检查目录"
        >不会修改目录或数据库。</AppNotice
      >
      <BaseButton
        class="ms-onboarding__action"
        variant="primary"
        :busy="bootstrap.loading"
        @click="start"
        >开始使用</BaseButton
      >
    </section>
  </main>
</template>
