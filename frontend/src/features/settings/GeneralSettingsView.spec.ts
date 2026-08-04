// @vitest-environment happy-dom

import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const stores = vi.hoisted(() => ({
  workspace: {
    settings: {
      activePath: '/Users/liu/develop/meetsieve',
      savedPath: '/Users/liu/develop/meetsieve',
      restartRequired: false,
      editable: true,
      disabledReason: '',
    },
    saving: false,
    fieldError: '',
    notice: '',
    loadSettings: vi.fn().mockResolvedValue(undefined),
    chooseDirectory: vi.fn().mockResolvedValue(''),
    inspect: vi.fn().mockResolvedValue(undefined),
    save: vi.fn().mockResolvedValue(true),
  },
  diagnostics: {
    scan: {
      stage: 'completed',
      scanned_at: 1,
      running: false,
      workspace_bytes: 1024,
      available_bytes: 1024 * 1024,
      categories: { Recordings: 0, Attachments: 0 },
    },
    errorMessage: '',
    notice: '',
    refreshScan: vi.fn().mockResolvedValue(undefined),
    startScan: vi.fn().mockResolvedValue(undefined),
    exportGlobal: vi.fn().mockResolvedValue(undefined),
    openLogs: vi.fn().mockResolvedValue(undefined),
  },
  voiceModel: {
    model: {
      usable: false,
      state: 'missing',
      modelName: '',
      modelVersion: '',
      location: '',
    },
    loading: false,
    errorMessage: '',
    refresh: vi.fn().mockResolvedValue(undefined),
  },
  asr: {
    settings: {
      api_key_configured: false,
      api_key_mask: '',
      requires_api_key_upgrade: false,
    },
    loading: false,
    saving: false,
    probing: false,
    apiKeyReady: false,
    errorMessage: '',
    notice: '',
    probeLatencyMS: 0,
    loadSettings: vi.fn().mockResolvedValue(undefined),
    saveAPIKey: vi.fn().mockResolvedValue(true),
    clearAPIKey: vi.fn().mockResolvedValue(true),
    testAPIKeyConnection: vi.fn().mockResolvedValue(true),
  },
  meeting: { current: undefined },
  agent: {
    settings: {
      updated_at: 0,
      codex_executable_path: '',
      wake_word: 'AI 助手',
      availability: {},
    },
    wakeTest: { state: 'idle', matched: 0, required: 3 },
    saving: false,
    loadSettings: vi.fn().mockResolvedValue(undefined),
    applyWakeTestEvent: vi.fn(),
    stopWakeTest: vi.fn().mockResolvedValue(undefined),
  },
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => vi.fn()),
}))
vi.mock('../../../wailsjs/go/wails/AudioSettingsBinding', () => ({
  GetAudioSettings: vi.fn().mockResolvedValue({
    code: 200,
    data: { default_microphone_id: '', devices: [] },
  }),
  SaveAudioSettings: vi.fn(),
  TestAudioDevice: vi.fn(),
}))
vi.mock('../../stores/workspace', () => ({
  useWorkspaceStore: () => stores.workspace,
}))
vi.mock('../../stores/diagnostics', () => ({
  useDiagnosticsStore: () => stores.diagnostics,
}))
vi.mock('../../stores/voiceModel', () => ({
  useVoiceModelStore: () => stores.voiceModel,
}))
vi.mock('../../stores/asr', () => ({ useASRStore: () => stores.asr }))
vi.mock('../../stores/meeting', () => ({
  useMeetingStore: () => stores.meeting,
}))
vi.mock('../../stores/agent', () => ({ useAgentStore: () => stores.agent }))

import GeneralSettingsView from './GeneralSettingsView.vue'
import { dirtyEditRegistry } from '../../router/dirty'

/** mountGeneralSettings 以真实设置路由挂载通用设置。 */
async function mountGeneralSettings(section = 'general') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/settings/:section',
        component: GeneralSettingsView,
        props: true,
      },
    ],
  })
  await router.push(`/settings/${section}`)
  await router.isReady()
  const wrapper = mount(GeneralSettingsView, {
    props: { section },
    global: { plugins: [router] },
  })
  await flushPromises()
  return wrapper
}

describe('GeneralSettingsView 通用设置', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stores.workspace.settings.restartRequired = false
    stores.asr.settings.requires_api_key_upgrade = false
  })

  it('将工作目录和存储诊断作为两个独立配置卡片', async () => {
    const wrapper = await mountGeneralSettings()
    const cards = wrapper.findAll('.ms-settings-section-card')

    expect(cards).toHaveLength(2)
    expect(cards[0].text()).toContain('会议工作目录')
    expect(cards[0].text()).toContain('保存')
    expect(cards[1].text()).toContain('工作目录占用')
    expect(cards[1].text()).not.toContain('保存')
    expect(wrapper.text()).not.toContain('当前仍在使用')
    expect(wrapper.find('.ms-path-summary').exists()).toBe(false)
  })

  it('只在目录变更待重启时显示生效提示', async () => {
    stores.workspace.settings.restartRequired = true
    const wrapper = await mountGeneralSettings()

    expect(wrapper.text()).toContain('重启后生效')
  })

  it('实时转写只展示 APP Key，并提示旧配置需要更新', async () => {
    stores.asr.settings.requires_api_key_upgrade = true
    const wrapper = await mountGeneralSettings('asr')

    expect(wrapper.text()).toContain('APP Key')
    expect(wrapper.text()).toContain('旧版凭证已停用')
    expect(wrapper.find('label[for="asr-app-id"]').exists()).toBe(false)
    expect(wrapper.find('label[for="asr-access-token"]').exists()).toBe(false)
    expect(wrapper.findAll('input[type="radio"]')).toHaveLength(0)
  })

  it('未修改设置时切换所有分类都不产生 dirty 状态', async () => {
    const wrapper = await mountGeneralSettings()

    for (const section of ['audio', 'asr', 'codex', 'voice-model']) {
      await wrapper.setProps({ section })
      expect(dirtyEditRegistry.dirtyEditors()).toHaveLength(0)
    }
    await wrapper.setProps({ section: 'asr' })
    expect(wrapper.get('#asr-api-key').attributes('autocomplete')).toBe(
      'new-password',
    )
    wrapper.unmount()
  })

  it('只保护当前分类，并在弹窗保存 ASR 后清空凭证草稿', async () => {
    const wrapper = await mountGeneralSettings('asr')
    await wrapper.get('#asr-api-key').setValue('draft-app-key')

    expect(
      dirtyEditRegistry.dirtyEditors().map((editor) => editor.label),
    ).toEqual(['实时转写凭证'])

    await wrapper.setProps({ section: 'audio' })
    expect(dirtyEditRegistry.dirtyEditors()).toHaveLength(0)

    await wrapper.setProps({ section: 'asr' })
    const [asrEditor] = dirtyEditRegistry.dirtyEditors()
    expect(await asrEditor?.save()).toBe(true)
    expect(stores.asr.saveAPIKey).toHaveBeenCalledWith('draft-app-key')
    expect(dirtyEditRegistry.dirtyEditors()).toHaveLength(0)
    wrapper.unmount()
  })
})
