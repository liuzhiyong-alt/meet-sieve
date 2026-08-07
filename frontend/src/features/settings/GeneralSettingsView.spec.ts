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
      codex_proxy_port: 0,
      wake_word: 'AI 助手',
      availability: {},
    },
    wakeTest: { state: 'idle', matched: 0, required: 3 },
    saving: false,
    loadSettings: vi.fn().mockResolvedValue(undefined),
    saveSettings: vi.fn().mockResolvedValue(true),
    applyWakeTestEvent: vi.fn(),
    stopWakeTest: vi.fn().mockResolvedValue(undefined),
  },
  minutes: {
    settings: {
      prompt: '默认会议纪要要求',
      is_default: true,
      updated_at: 1,
    },
    settingsLoading: false,
    settingsSaving: false,
    settingsError: '',
    settingsNotice: '',
    loadSettings: vi.fn().mockResolvedValue(undefined),
    saveSettings: vi.fn().mockImplementation(async (prompt: string) => {
      stores.minutes.settings.prompt = prompt
      stores.minutes.settings.is_default = false
      stores.minutes.settings.updated_at += 1
      return true
    }),
    restoreDefault: vi.fn().mockImplementation(async () => {
      stores.minutes.settings.prompt = '默认会议纪要要求'
      stores.minutes.settings.is_default = true
      stores.minutes.settings.updated_at += 1
      return true
    }),
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
vi.mock('../../stores/minutes', () => ({
  useMinutesStore: () => stores.minutes,
}))

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
    stores.minutes.settings.prompt = '默认会议纪要要求'
    stores.minutes.settings.is_default = true
    stores.agent.settings.codex_proxy_port = 0
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

  it('五项配置使用统一的标题和说明层级', async () => {
    const wrapper = await mountGeneralSettings('audio')
    const sections = [
      {
        name: 'audio',
        title: '#audio-settings-title',
        lead: '默认麦克风独立保存；测试结果不会修改设置。',
      },
      {
        name: 'asr',
        title: '#asr-settings-title',
        lead: '一个 APP Key 同时用于实时转写与缺口补录。',
      },
      {
        name: 'codex',
        title: '#codex-settings-title',
        lead: 'Codex 使用你本机已有的登录、工具与原生权限配置。',
      },
      {
        name: 'minutes',
        title: '#minutes-settings-title',
        lead: '自定义会议纪要的内容重点、详略程度和表达方式。',
      },
      {
        name: 'voice-model',
        title: '#voice-model-title',
        lead: '管理本机声纹模型的下载、离线导入与完整性校验。',
      },
    ]

    for (const section of sections) {
      await wrapper.setProps({ section: section.name })

      expect(wrapper.get(section.title).element.tagName).toBe('H2')
      expect(
        wrapper.get('.ms-settings-card .ms-card-head .ms-help').text(),
      ).toContain(section.lead)
      expect(wrapper.findAll('h1')).toHaveLength(1)
    }

    wrapper.unmount()
  })

  it('未修改设置时切换所有分类都不产生 dirty 状态', async () => {
    const wrapper = await mountGeneralSettings()

    for (const section of ['audio', 'asr', 'codex', 'minutes', 'voice-model']) {
      await wrapper.setProps({ section })
      expect(dirtyEditRegistry.dirtyEditors()).toHaveLength(0)
    }
    await wrapper.setProps({ section: 'asr' })
    expect(wrapper.get('#asr-api-key').attributes('autocomplete')).toBe(
      'new-password',
    )
    wrapper.unmount()
  })

  it('会议纪要分类回填默认要求并独立保存修改', async () => {
    const wrapper = await mountGeneralSettings('minutes')

    expect(wrapper.text()).toContain(
      '自定义会议纪要的内容重点、详略程度和表达方式。',
    )
    expect(wrapper.text()).not.toContain('MeetSieve 会始终依据会议记录生成')
    expect(wrapper.get('#minutes-prompt').attributes('rows')).toBe('10')
    expect(wrapper.get('#minutes-prompt').element).toHaveProperty(
      'value',
      '默认会议纪要要求',
    )
    await wrapper.get('#minutes-prompt').setValue('突出决策和负责人')
    expect(
      dirtyEditRegistry.dirtyEditors().map((editor) => editor.label),
    ).toEqual(['会议纪要设置'])

    await wrapper.get('button.ms-button--primary').trigger('click')
    await flushPromises()
    expect(stores.minutes.saveSettings).toHaveBeenCalledWith('突出决策和负责人')
    wrapper.unmount()
  })

  it('Codex 代理端口使用已有设置字段保存，并允许 0 表示直连', async () => {
    const wrapper = await mountGeneralSettings('codex')
    const proxyPort = wrapper.get('#codex-proxy-port')

    expect(proxyPort.attributes('min')).toBe('0')
    expect(proxyPort.attributes('max')).toBe('65535')
    expect(wrapper.text()).toContain('填写 0 表示关闭代理')
    expect(wrapper.text()).toContain('供 Codex 与下载官方声纹模型使用')

    await proxyPort.setValue('65400')
    expect(
      dirtyEditRegistry.dirtyEditors().map((editor) => editor.label),
    ).toEqual(['Codex 设置'])

    const saveButton = wrapper
      .findAll('button.ms-button--primary')
      .find((button) => button.text() === '保存更改')
    expect(saveButton).toBeDefined()
    await saveButton?.trigger('click')
    await flushPromises()
    expect(stores.agent.saveSettings).toHaveBeenCalledWith('AI 助手', '', 65400)
    wrapper.unmount()
  })

  it('自定义会议纪要要求可以恢复为当前默认值', async () => {
    stores.minutes.settings.prompt = '突出风险'
    stores.minutes.settings.is_default = false
    const wrapper = await mountGeneralSettings('minutes')

    expect(wrapper.text()).toContain('已自定义')
    const restoreButton = wrapper
      .findAll('button')
      .find((button) => button.text() === '恢复默认要求')
    expect(restoreButton).toBeDefined()
    await restoreButton?.trigger('click')
    await flushPromises()

    expect(stores.minutes.restoreDefault).toHaveBeenCalled()
    expect(wrapper.get('#minutes-prompt').element).toHaveProperty(
      'value',
      '默认会议纪要要求',
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
