import { defineStore } from 'pinia'

import { ChooseWorkspaceDirectory } from '../../wailsjs/go/wails/DirectoryDialogBinding'
import {
  GetWorkspaceSettings,
  InspectWorkspaceCandidate,
  SaveWorkspacePath,
} from '../../wailsjs/go/wails/WorkspaceBinding'

export interface WorkspaceSettings {
  activePath: string
  savedPath: string
  restartRequired: boolean
  editable: boolean
  disabledReason: string
}

/** useWorkspaceStore 管理输入过程状态；目录和 SQLite 状态始终重新由 Go 校验。 */
export const useWorkspaceStore = defineStore('workspace', {
  state: () => ({
    settings: {
      activePath: '',
      savedPath: '',
      restartRequired: false,
      editable: true,
      disabledReason: '',
    } as WorkspaceSettings,
    inspecting: false,
    saving: false,
    fieldError: '',
    notice: '',
  }),
  actions: {
    /** loadSettings 读取当前与下次启动路径的真实投影。 */
    async loadSettings(): Promise<void> {
      const result = await GetWorkspaceSettings()
      if (result.code !== 200 || !result.data) {
        this.fieldError = result.message
        return
      }
      this.applySettings(result.data)
    },
    /** chooseDirectory 仅调用系统对话框；取消不修改输入和目录。 */
    async chooseDirectory(): Promise<string> {
      const result = await ChooseWorkspaceDirectory()
      if (result.code !== 200) {
        this.fieldError = result.message
        return ''
      }
      return result.data ?? ''
    },
    /** inspect 只提供输入即时反馈，不创建目录或保存 locator。 */
    async inspect(path: string): Promise<void> {
      if (!path.trim()) {
        this.fieldError = ''
        return
      }
      this.inspecting = true
      const result = await InspectWorkspaceCandidate(path)
      this.inspecting = false
      if (result.code !== 200 || !result.data) {
        this.fieldError = result.message
        return
      }
      this.fieldError =
        result.data.reason === 'none'
          ? ''
          : candidateMessage(result.data.reason)
    },
    /** save 保存新路径供下次启动使用，不要求也不触发当前数据库切换。 */
    async save(path: string): Promise<boolean> {
      this.saving = true
      this.fieldError = ''
      this.notice = ''
      const result = await SaveWorkspacePath(path)
      this.saving = false
      if (result.code !== 200 || !result.data) {
        this.fieldError = result.message
        return false
      }
      this.applySettings(result.data)
      this.notice = result.data.restart_required
        ? '工作目录已保存，将在下次启动时使用'
        : '工作目录已保存'
      return true
    },
    /** applySettings 显式映射 Wails snake_case DTO。 */
    applySettings(settings: {
      active_path: string
      saved_path: string
      restart_required: boolean
      editable: boolean
      disabled_reason: string
    }): void {
      this.settings = {
        activePath: settings.active_path,
        savedPath: settings.saved_path,
        restartRequired: settings.restart_required,
        editable: settings.editable,
        disabledReason: settings.disabled_reason,
      }
    },
  },
})

/** candidateMessage 将稳定后端原因映射为产品已确认的字段错误文案。 */
function candidateMessage(reason: string): string {
  const messages: Record<string, string> = {
    not_empty: '目录不为空，且不是有效的 MeetSieve 工作目录',
    database_missing: '无法找到 data/meetings.db',
    database_invalid: '无法打开工作目录中的数据库',
    not_writable: '目录没有写入权限',
    unsupported_volume: '工作目录不能使用网络共享位置',
    install_path_forbidden:
      '会议工作目录不能放在 MeetSieve 安装目录中，请选择其他位置',
    schema_newer: '该工作目录由更高版本的 MeetSieve 创建，请升级应用后重试',
  }
  return messages[reason] ?? '工作目录路径无效'
}
