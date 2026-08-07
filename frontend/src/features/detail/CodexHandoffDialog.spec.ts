// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

const bindings = vi.hoisted(() => ({
  GetAgentRecoveryCommands: vi.fn(),
}))

vi.mock('../../../wailsjs/go/wails/AgentBinding', () => ({
  GetAgentRecoveryCommands: bindings.GetAgentRecoveryCommands,
}))

import CodexHandoffDialog from './CodexHandoffDialog.vue'

/** mountDialog 用受控 Wails 返回值挂载 Codex 接续弹窗。 */
async function mountDialog(data: Record<string, unknown>) {
  bindings.GetAgentRecoveryCommands.mockResolvedValue({
    code: 200,
    message: 'ok',
    data,
  })
  const wrapper = mount(CodexHandoffDialog, {
    props: { open: true, meetingId: 'meeting-1' },
    global: { stubs: { Teleport: true } },
  })
  await flushPromises()
  return wrapper
}

describe('CodexHandoffDialog', () => {
  it('同时展示恢复原对话和文件接续方式', async () => {
    const wrapper = await mountDialog({
      thread_available: true,
      thread_command: "codex resume -C '/tmp/meeting' thread-1",
      directory_command: "codex -C '/tmp/meeting' '请读取会议原始记录.md'",
      recovery_prompt: '请读取会议原始记录.md',
    })

    expect(bindings.GetAgentRecoveryCommands).toHaveBeenCalledWith('meeting-1')
    expect(wrapper.text()).toContain('恢复原对话')
    expect(wrapper.text()).toContain('从会议文件继续')
    expect(wrapper.get('textarea').element.value).toContain('codex resume')
    const copyButtons = wrapper
      .findAll('.ms-handoff-section > button')
      .filter((button) => button.text().startsWith('复制'))
    expect(copyButtons).toHaveLength(2)
    expect(
      copyButtons.every((button) =>
        button.classes().includes('ms-button--primary'),
      ),
    ).toBe(true)
  })

  it('没有历史 thread 时仍提供文件接续提示词', async () => {
    const wrapper = await mountDialog({
      thread_available: false,
      thread_command: '',
      directory_command: "codex -C '/tmp/meeting' '请读取会议原始记录.md'",
      recovery_prompt: '请读取会议原始记录.md',
    })

    expect(wrapper.text()).toContain('本场没有可恢复的原 Codex 对话')
    expect(wrapper.text()).toContain('复制提示词')
    expect(wrapper.text()).not.toContain('复制恢复命令')
  })

  it('在提示词和终端命令之间只展示当前选中的接续内容', async () => {
    const wrapper = await mountDialog({
      thread_available: false,
      thread_command: '',
      directory_command: "codex -C '/tmp/meeting' '请读取会议原始记录.md'",
      recovery_prompt: '请读取会议原始记录.md',
    })

    expect(wrapper.findAll('#codex-handoff-prompt-panel')).toHaveLength(1)
    expect(wrapper.findAll('#codex-handoff-command-panel')).toHaveLength(0)

    await wrapper.get('#codex-handoff-command-tab').trigger('click')

    expect(wrapper.findAll('#codex-handoff-prompt-panel')).toHaveLength(0)
    expect(
      (
        wrapper.get('#codex-handoff-command-panel textarea')
          .element as HTMLTextAreaElement
      ).value,
    ).toContain('codex -C')
    expect(wrapper.text()).toContain('复制终端命令')
  })

  it('加载接续信息时仍可从固定底部操作区关闭', async () => {
    let resolveRequest: ((value: unknown) => void) | undefined
    bindings.GetAgentRecoveryCommands.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveRequest = resolve
        }),
    )
    const wrapper = mount(CodexHandoffDialog, {
      props: { open: true, meetingId: 'meeting-1' },
      global: { stubs: { Teleport: true } },
    })

    await wrapper.get('.ms-handoff-dialog__footer button').trigger('click')

    expect(wrapper.emitted('update:open')).toEqual([[false]])
    await wrapper.setProps({ open: false })
    resolveRequest?.({ code: 200, message: 'ok', data: {} })
    await flushPromises()
    expect(wrapper.findAll('.ms-handoff-dialog')).toHaveLength(0)
  })
})
