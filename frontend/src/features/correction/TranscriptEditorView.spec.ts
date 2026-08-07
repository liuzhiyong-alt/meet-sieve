// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const bindings = vi.hoisted(() => ({
  CorrectSpeakerCluster: vi.fn(),
  CorrectUtteranceSpeaker: vi.fn(),
  CorrectUtteranceText: vi.fn(),
  CreateUtteranceAudioClip: vi.fn(),
  ListCorrectionEntries: vi.fn(),
  RetryRawRecordProjection: vi.fn(),
  RevokeAudioClip: vi.fn(),
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => vi.fn()),
}))
vi.mock('../../../wailsjs/go/wails/CorrectionBinding', () => bindings)

import TranscriptEditorView from './TranscriptEditorView.vue'

function correctionPage() {
  return {
    code: 200,
    message: 'ok',
    data: {
      entries: [
        {
          seq: 1,
          utterance_id: 'utterance-1',
          start_sample: 0,
          end_sample: 16000,
          original_text: '原始文字',
          current_text: '第一段文字',
          speaker_display: '未知说话人 1',
          speaker_cluster_id: 'cluster-1',
          cluster_display_no: 1,
          cluster_count: 2,
          cluster_revision: 1,
          assignment_source: 'automatic_cluster',
          text_revision: 1,
          speaker_revision: 1,
          can_play: true,
        },
        {
          seq: 2,
          utterance_id: 'utterance-2',
          start_sample: 16000,
          end_sample: 32000,
          original_text: '原始文字二',
          current_text: '第二段文字',
          speaker_display: '未知说话人 1',
          speaker_cluster_id: 'cluster-1',
          cluster_display_no: 1,
          cluster_count: 2,
          cluster_revision: 1,
          assignment_source: 'automatic_cluster',
          text_revision: 1,
          speaker_revision: 1,
          can_play: false,
          playback_disabled_reason: '对应录音不可回放',
        },
      ],
      participants: [
        {
          id: 'participant-1',
          display_name: '张三',
          kind: 'member',
          is_member: true,
        },
        {
          id: 'participant-2',
          display_name: '李四',
          kind: 'member',
          is_member: true,
        },
      ],
      next_seq: 2,
    },
  }
}

/** mountEditor 使用真实 Pinia 挂载已结束会议的原始记录编辑页。 */
async function mountEditor() {
  const wrapper = mount(TranscriptEditorView, {
    props: {
      meetingId: 'meeting-1',
      meetingNo: '20260805-HAQR-01',
      subject: '未命名会议',
    },
    global: { plugins: [createPinia()] },
  })
  await flushPromises()
  return wrapper
}

describe('TranscriptEditorView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    bindings.ListCorrectionEntries.mockResolvedValue(correctionPage())
    bindings.CorrectSpeakerCluster.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: { saved: true, no_op: false, projection_state: 'completed' },
    })
    bindings.CorrectUtteranceSpeaker.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: { saved: true, no_op: false, projection_state: 'completed' },
    })
    bindings.CorrectUtteranceText.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: { saved: true, no_op: false, projection_state: 'completed' },
    })
    bindings.CreateUtteranceAudioClip.mockResolvedValue({
      code: 200,
      message: 'ok',
      data: { url: '/media/audio-clips/clip-token' },
    })
    bindings.RevokeAudioClip.mockResolvedValue({ code: 200, message: 'ok' })
  })

  it('每段记录直接提供文字、说话人和播放入口，不显示旧校对面板', async () => {
    const wrapper = await mountEditor()

    expect(wrapper.findAll('.ms-correction-record')).toHaveLength(2)
    expect(wrapper.findAll('.ms-correction-record textarea')).toHaveLength(2)
    expect(wrapper.findAll('.ms-correction-record select')).toHaveLength(2)
    expect(wrapper.text()).toContain('本场说话人对应关系')
    expect(wrapper.text()).not.toContain('校对选中片段')
    expect(wrapper.text()).not.toContain('单个片段')
    expect(wrapper.text()).not.toContain('原始 ASR')
    expect(wrapper.text()).not.toContain('加入声纹样本')
    expect(wrapper.text()).not.toContain('查看原始文字与修改记录')
    expect(wrapper.text()).not.toContain('处修改')
    expect(wrapper.find('.ms-correction-record__speaker > span').exists()).toBe(
      false,
    )

    wrapper.unmount()
  })

  it('仅在 audio 实际播放后显示暂停，并在停止后撤销 clip', async () => {
    const wrapper = await mountEditor()

    await wrapper
      .get('.ms-correction-record .ms-button--quiet')
      .trigger('click')
    await flushPromises()

    expect(bindings.CreateUtteranceAudioClip).toHaveBeenCalledWith(
      'utterance-1',
    )
    expect(wrapper.get('audio').attributes('autoplay')).toBeDefined()
    expect(wrapper.get('.ms-correction-record .ms-button--quiet').text()).toBe(
      '正在加载',
    )

    await wrapper.get('audio').trigger('play')
    expect(wrapper.get('.ms-correction-record .ms-button--quiet').text()).toBe(
      '暂停',
    )

    await wrapper
      .get('.ms-correction-record .ms-button--quiet')
      .trigger('click')
    await flushPromises()
    expect(bindings.RevokeAudioClip).toHaveBeenCalledWith(
      '/media/audio-clips/clip-token',
    )
    expect(wrapper.find('audio').exists()).toBe(false)

    wrapper.unmount()
  })

  it('音频加载失败后显示错误并撤销 clip', async () => {
    const wrapper = await mountEditor()

    await wrapper
      .get('.ms-correction-record .ms-button--quiet')
      .trigger('click')
    await flushPromises()
    await wrapper.get('audio').trigger('error')
    await flushPromises()

    expect(wrapper.text()).toContain('无法读取该片段录音，请重试。')
    expect(bindings.RevokeAudioClip).toHaveBeenCalledWith(
      '/media/audio-clips/clip-token',
    )

    wrapper.unmount()
  })

  it('输入文字仅留下草稿，并通过标题行的保存修改统一提交', async () => {
    const wrapper = await mountEditor()
    const saveButton = wrapper.get(
      '.ms-correction-records__head .ms-button--primary',
    )
    expect(saveButton.attributes('disabled')).toBeDefined()

    await wrapper.find('.ms-correction-record textarea').setValue('已直接修改')
    expect(bindings.CorrectUtteranceText).not.toHaveBeenCalled()
    expect(saveButton.attributes('disabled')).toBeUndefined()

    await saveButton.trigger('click')
    await flushPromises()
    expect(bindings.CorrectUtteranceText).toHaveBeenCalledWith(
      expect.objectContaining({ value: '已直接修改' }),
    )

    wrapper.unmount()
  })
})
