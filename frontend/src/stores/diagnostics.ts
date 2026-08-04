import { defineStore } from 'pinia'

import {
  ExportGlobalDiagnostic,
  ExportMeetingDiagnostic,
  GetStorageScan,
  OpenLogDirectory,
  StartStorageScan,
} from '../../wailsjs/go/wails/DiagnosticBinding'
import type { wails } from '../../wailsjs/go/models'

/** useDiagnosticsStore 保存内存扫描结果和诊断导出反馈。 */
export const useDiagnosticsStore = defineStore('diagnostics', {
  state: () => ({
    scan: undefined as wails.StorageScanDTO | undefined,
    loading: false,
    errorMessage: '',
    notice: '',
    timer: undefined as number | undefined,
  }),
  actions: {
    /** startScan 启动扫描并轮询真实阶段，不推算百分比。 */
    async startScan(): Promise<void> {
      const result = await StartStorageScan()
      if (!this.applyScan(result)) return
      this.poll()
    },
    /** refreshScan 读取当前或最近一次结果。 */
    async refreshScan(): Promise<void> {
      const result = await GetStorageScan()
      if (this.applyScan(result) && this.scan?.running) this.poll()
    },
    /** poll 在后台扫描结束前进行低频刷新。 */
    poll(): void {
      if (this.timer !== undefined) window.clearTimeout(this.timer)
      this.timer = window.setTimeout(() => void this.refreshScan(), 700)
    },
    /** exportGlobal 只触发系统保存对话框。 */
    async exportGlobal(): Promise<void> {
      const result = await ExportGlobalDiagnostic()
      this.applyExport(result)
    },
    /** exportMeeting 导出本场白名单摘要。 */
    async exportMeeting(meetingID: string): Promise<void> {
      const result = await ExportMeetingDiagnostic(meetingID)
      this.applyExport(result)
    },
    /** openLogs 只在用户点击后调用系统文件管理器。 */
    async openLogs(): Promise<void> {
      const result = await OpenLogDirectory()
      this.errorMessage = result.code === 200 ? '' : result.message
    },
    /** applyScan 应用版本化扫描投影。 */
    applyScan(result: {
      code: number
      message: string
      data?: wails.StorageScanDTO
    }): boolean {
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      this.errorMessage = ''
      this.scan = result.data
      return true
    },
    /** applyExport 不显示保存路径，只反馈文件名。 */
    applyExport(result: {
      code: number
      message: string
      data?: wails.DiagnosticExportDTO
    }): void {
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return
      }
      this.errorMessage = ''
      this.notice = result.data.cancelled
        ? ''
        : `已导出 ${result.data.file_name}`
    },
  },
})
