import { defineStore } from 'pinia'

import {
  CreateGroup,
  CreateMember,
  ArchiveMember,
  DeleteGroup,
  DeleteMember,
  ListGroups,
  ListMembers,
  UpdateGroup,
  UpdateMember,
} from '../../wailsjs/go/wails/PeopleBinding'

export interface MemberProjection {
  id: string
  name: string
  notes?: string
  accepted_sample_count: number
  rejected_sample_count: number
  voice_readiness: string
  created_at: number
  updated_at: number
}

export interface GroupProjection {
  id: string
  name: string
  default_lan_enabled: boolean
  members: Array<{ member_id: string; sort_order: number }>
  created_at: number
  updated_at: number
}

/** usePeopleStore 保存后端当前投影，不把前端状态当作成员或小组事实源。 */
export const usePeopleStore = defineStore('people', {
  state: () => ({
    members: [] as MemberProjection[],
    groups: [] as GroupProjection[],
    loading: false,
    saving: false,
    errorMessage: '',
  }),
  actions: {
    /** refresh 并行读取成员与小组，在任一失败时保留安全错误语义。 */
    async refresh(): Promise<void> {
      this.loading = true
      this.errorMessage = ''
      const [members, groups] = await Promise.all([ListMembers(), ListGroups()])
      this.loading = false
      if (members.code !== 200 || !members.data) {
        this.errorMessage = members.message || '无法读取成员'
        return
      }
      if (groups.code !== 200 || !groups.data) {
        this.errorMessage = groups.message || '无法读取小组'
        return
      }
      this.members = members.data
      this.groups = groups.data
    },
    /** createMember 创建成功后重新读取后端排序投影。 */
    async createMember(name: string, notes: string): Promise<boolean> {
      this.saving = true
      this.errorMessage = ''
      const result = await CreateMember({ name, notes })
      this.saving = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      await this.refresh()
      return true
    },
    /** createGroup 保存当前勾选成员顺序，并重新读取后端投影。 */
    async createGroup(name: string, memberIds: string[]): Promise<boolean> {
      this.saving = true
      this.errorMessage = ''
      const result = await CreateGroup({
        name,
        default_lan_enabled: false,
        member_ids: memberIds,
      })
      this.saving = false
      if (result.code !== 200 || !result.data) {
        this.errorMessage = result.message
        return false
      }
      await this.refresh()
      return true
    },
    /** updateMember 保存成员名称和备注。 */
    async updateMember(
      id: string,
      name: string,
      notes: string,
    ): Promise<boolean> {
      return this.mutate(UpdateMember(id, { name, notes }))
    },
    /** archiveMember 归档成员并保留历史会议引用。 */
    async archiveMember(id: string): Promise<boolean> {
      return this.mutate(ArchiveMember(id))
    },
    /** deleteMember 永久删除后端确认从未被历史引用的成员。 */
    async deleteMember(id: string): Promise<boolean> {
      return this.mutate(DeleteMember(id))
    },
    /** updateGroup 完整保存小组设置和显式成员顺序。 */
    async updateGroup(
      id: string,
      name: string,
      defaultLANEnabled: boolean,
      memberIds: string[],
    ): Promise<boolean> {
      return this.mutate(
        UpdateGroup(id, {
          name,
          default_lan_enabled: defaultLANEnabled,
          member_ids: memberIds,
        }),
      )
    },
    /** deleteGroup 删除小组关系但不删除成员。 */
    async deleteGroup(id: string): Promise<boolean> {
      return this.mutate(DeleteGroup(id))
    },
    /** mutate 统一处理修改结果，并始终重新读取后端投影。 */
    async mutate(
      request: Promise<{ code: number; message: string }>,
    ): Promise<boolean> {
      this.saving = true
      this.errorMessage = ''
      const result = await request
      this.saving = false
      if (result.code !== 200) {
        this.errorMessage = result.message
        return false
      }
      await this.refresh()
      return true
    },
  },
})
