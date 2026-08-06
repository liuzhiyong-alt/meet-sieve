<script lang="ts" setup>
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import type { MeetingSummary } from '../../stores/query'
import { useQueryStore } from '../../stores/query'
import MeetingDeleteDialog from '../deletion/MeetingDeleteDialog.vue'

const route = useRoute()
const router = useRouter()
const query = useQueryStore()
const search = ref(String(route.query.search ?? ''))
const status = ref(String(route.query.status ?? ''))
const deleteOpen = ref(false)
const selectedMeeting = ref<MeetingSummary>()
const pageNumber = computed(() =>
  Math.max(1, Number(route.query.page ?? 1) || 1),
)

const statusLabels: Record<string, string> = {
  deleting: '删除处理中',
  recovery_required: '需要恢复',
  gap_conflict: '缺口冲突',
  gap_pending: '补转写处理中',
  minute_candidate: '纪要已生成',
  agent_unsynced: 'Codex 未同步',
  minute_confirmed: '纪要已生成',
  saved: '本地已保存',
}

/** reload 从 URL 重读筛选和不透明游标。 */
async function reload(): Promise<void> {
  search.value = String(route.query.search ?? '')
  status.value = String(route.query.status ?? '')
  const ok = await query.loadMeetings(
    search.value,
    status.value,
    String(route.query.cursor ?? ''),
  )
  if (!ok && query.errorCode.includes('CURSOR')) {
    await router.replace({
      path: '/meetings',
      query: {
        search: route.query.search,
        status: route.query.status,
        notice: 'cursor-reset',
      },
    })
  }
}

/** applyFilter 只在提交时写入规范筛选，并清除旧游标。 */
function applyFilter(): void {
  void router.push({
    path: '/meetings',
    query: {
      search: search.value.trim() || undefined,
      status: status.value || undefined,
    },
  })
}

/** turnPage 将后端签发游标和仅用于显示的页码写入 URL。 */
function turnPage(cursor: string, direction: -1 | 1): void {
  if (!cursor) return
  void router.push({
    path: '/meetings',
    query: {
      ...route.query,
      cursor,
      page: Math.max(1, pageNumber.value + direction),
    },
  })
}

/** openMeeting 打开会议内容；删除任务统一进入恢复页。 */
function openMeeting(item: MeetingSummary): void {
  const meeting = `/meetings/${item.id}`
  void router.push(
    item.highest_status === 'deleting' ? `${meeting}/delete-recovery` : meeting,
  )
}

/** openDelete 打开当前行的整场会议删除确认。 */
function openDelete(item: MeetingSummary): void {
  if (!item.can_delete_meeting) return
  selectedMeeting.value = item
  deleteOpen.value = true
}

/** handleDeleted 删除完成后保留筛选条件刷新当前页。 */
async function handleDeleted(): Promise<void> {
  const previousCursor = query.previousCursor
  deleteOpen.value = false
  selectedMeeting.value = undefined
  await reload()
  if (!query.meetings.length && pageNumber.value > 1 && previousCursor) {
    turnPage(previousCursor, -1)
  }
}

/** handleDeleteFailed 跳转到当前会议的删除恢复页。 */
function handleDeleteFailed(): void {
  const meetingID = selectedMeeting.value?.id
  deleteOpen.value = false
  selectedMeeting.value = undefined
  if (meetingID) void router.push(`/meetings/${meetingID}/delete-recovery`)
}

/** statusClass 返回已确认状态色，不承担动作决策。 */
function statusClass(value: string): string {
  if (value === 'deleting' || value === 'recovery_required') return 'is-danger'
  if (value === 'minute_confirmed' || value === 'saved') return 'is-complete'
  return 'is-warning'
}

/** meetingMeta 生成不泄漏路径的列表辅助信息。 */
function meetingMeta(item: MeetingSummary): string {
  const participants = item.participants.join('、') || '未登记参会人'
  return `${item.meeting_no} · ${participants}`
}

onMounted(reload)
watch(() => route.fullPath, reload)
</script>

<template>
  <div class="ms-records-page">
    <section class="ms-page-head">
      <div>
        <p class="ms-eyebrow">本地会议</p>
        <h1>会议记录</h1>
        <p>搜索主题、会议号或参会人，并按当前最高处理状态筛选。</p>
      </div>
      <RouterLink class="ms-button ms-button--primary" to="/meetings/new"
        >创建会议</RouterLink
      >
    </section>
    <p
      v-if="route.query.notice === 'cursor-reset'"
      class="ms-notice ms-notice--warning"
    >
      页面位置已过期，已返回第一页。
    </p>
    <p
      v-if="route.query.notice === 'meeting-not-found'"
      class="ms-notice ms-notice--warning"
    >
      会议不存在或已被删除，已返回会议记录。
    </p>
    <p
      v-if="query.errorMessage"
      class="ms-notice ms-notice--danger"
      role="alert"
    >
      {{ query.errorMessage }}
    </p>

    <form class="ms-card ms-records-toolbar" @submit.prevent="applyFilter">
      <label class="ms-field ms-records-filter-field"
        ><span>搜索</span
        ><input
          v-model="search"
          class="ms-input"
          placeholder="主题、会议号或参会人"
      /></label>
      <label class="ms-field ms-records-filter-field"
        ><span>状态</span
        ><select v-model="status" class="ms-input">
          <option value="">全部状态</option>
          <option value="recovery_required">需要恢复</option>
          <option value="gap_conflict">缺口冲突</option>
          <option value="gap_pending">补转写处理中</option>
          <option value="agent_unsynced">Codex 未同步</option>
          <option value="saved">本地已保存</option>
          <option value="deleting">删除处理中</option>
        </select></label
      >
      <button
        class="ms-button ms-button--quiet ms-records-filter-submit"
        type="submit"
      >
        查询
      </button>
    </form>

    <section class="ms-card ms-records-card" :aria-busy="query.loading">
      <div class="ms-records-viewport">
        <p v-if="query.loading" class="ms-progress-label" aria-live="polite">
          <span class="ms-spinner" />正在读取…
        </p>
        <ul
          v-else-if="query.meetings.length"
          class="ms-records-list"
          aria-label="会议记录列表，每页 10 场"
          tabindex="0"
        >
          <li
            v-for="item in query.meetings"
            :key="item.id"
            class="ms-record-row"
          >
            <div class="ms-record-main">
              <strong>{{ item.subject || item.meeting_no }}</strong
              ><span class="ms-muted ms-input--mono">{{
                meetingMeta(item)
              }}</span>
            </div>
            <span
              class="ms-status-pill ms-record-status"
              :class="statusClass(item.highest_status)"
              >{{ statusLabels[item.highest_status] || '状态未知' }}</span
            >
            <div class="ms-record-actions">
              <button
                class="ms-button ms-button--quiet"
                type="button"
                @click="openMeeting(item)"
              >
                打开
              </button>
              <button
                class="ms-button ms-button--danger"
                type="button"
                :disabled="!item.can_delete_meeting"
                :title="item.delete_disabled_reason"
                @click="openDelete(item)"
              >
                删除
              </button>
            </div>
          </li>
        </ul>
        <div v-else class="ms-empty-state">
          <h2>没有符合条件的会议</h2>
          <p>调整搜索或状态筛选后再试。</p>
        </div>
      </div>
      <div class="ms-records-pagination ms-cursor-pagination">
        <button
          class="ms-button ms-button--quiet"
          type="button"
          :disabled="!query.previousCursor || query.loading"
          @click="turnPage(query.previousCursor, -1)"
        >
          上一页
        </button>
        <span class="ms-meta"
          >第 {{ pageNumber }} 页 · 当前 {{ query.meetings.length }} 场</span
        >
        <button
          class="ms-button ms-button--quiet"
          type="button"
          :disabled="!query.nextCursor || query.loading"
          @click="turnPage(query.nextCursor, 1)"
        >
          下一页
        </button>
      </div>
    </section>
    <MeetingDeleteDialog
      v-if="selectedMeeting"
      v-model:open="deleteOpen"
      :meeting-id="selectedMeeting.id"
      :meeting-no="selectedMeeting.meeting_no"
      :subject="selectedMeeting.subject"
      @deleted="handleDeleted"
      @failed="handleDeleteFailed"
    />
  </div>
</template>
