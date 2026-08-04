export interface DirtyEditor {
  id: string
  label: string
  isDirty: () => boolean
  canSave: () => boolean
  save: () => Promise<boolean>
  discard: () => void
}

export type DirtyDecision = 'stay' | 'discard' | 'save'

/** DirtyEditRegistry 只登记本机尚未保存的编辑器，不登记后台任务。 */
export class DirtyEditRegistry {
  private readonly editors = new Map<string, DirtyEditor>()
  private prompt?: (editors: DirtyEditor[]) => Promise<DirtyDecision>

  /** register 登记编辑器并返回幂等注销函数。 */
  register(editor: DirtyEditor): () => void {
    this.editors.set(editor.id, editor)
    return () => this.editors.delete(editor.id)
  }

  /** setPrompt 安装 App 级可访问性确认对话框。 */
  setPrompt(prompt: (editors: DirtyEditor[]) => Promise<DirtyDecision>): void {
    this.prompt = prompt
  }

  /** dirtyEditors 返回当前实际 dirty 的编辑器副本。 */
  dirtyEditors(): DirtyEditor[] {
    return [...this.editors.values()].filter((editor) => editor.isDirty())
  }

  /** confirmNavigation 只在存在本地未保存编辑时拦截路由离开。 */
  async confirmNavigation(): Promise<boolean> {
    const editors = this.dirtyEditors()
    if (!editors.length) return true
    if (!this.prompt) return false
    const decision = await this.prompt(editors)
    if (decision === 'stay') return false
    if (decision === 'discard') {
      editors.forEach((editor) => editor.discard())
      return true
    }
    if (editors.some((editor) => !editor.canSave())) return false
    for (const editor of editors) {
      if (!(await editor.save())) return false
    }
    return true
  }
}

export const dirtyEditRegistry = new DirtyEditRegistry()
