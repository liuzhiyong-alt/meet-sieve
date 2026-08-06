<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { GetMeetingPeopleOptions } from '../../../wailsjs/go/wails/PeopleBinding'
import type { wails } from '../../../wailsjs/go/models'
import { useQueryStore, type MeetingSummary } from '../../stores/query'

const router = useRouter()
const query = useQueryStore()
const groups = ref<wails.MeetingGroupOptionDTO[]>([])
const peopleError = ref('')
const firstGroup = computed(() => groups.value[0])

const statusLabels: Record<string, string> = {
  deleting: '删除处理中',
  recovery_required: '需要恢复',
  gap_conflict: '有转写缺口',
  gap_pending: '补转写处理中',
  minute_candidate: '纪要待确认',
  agent_unsynced: 'Codex 未同步',
  minute_confirmed: '纪要已确认',
  saved: '本地已保存',
}

onMounted(async () => {
  const [, people] = await Promise.all([
    query.loadHome(),
    GetMeetingPeopleOptions(),
  ])
  if (people.code !== 200 || !people.data) {
    peopleError.value = people.message || '无法读取常用小组'
    return
  }
  groups.value = people.data.groups ?? []
})

/** initials 返回首页成员头像的首个 Unicode 字符。 */
function initials(name: string): string {
  return Array.from(name.trim())[0] ?? '人'
}

/** formatTime 将毫秒时间按本机时区展示。 */
function formatTime(value: number): string {
  return new Intl.DateTimeFormat('zh-CN', {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
}

/** runPrimaryAction 只解释后端动作种类，不从状态轴再次推导业务动作。 */
function runPrimaryAction(item: MeetingSummary): void {
  const action = item.primary_action
  if (!action.enabled) return
  const meeting = `/meetings/${item.id}`
  const targets: Record<string, string> = {
    deletion_recovery: `${meeting}/delete-recovery`,
    recover_meeting: `${meeting}/recovery`,
    resolve_gap: `${meeting}/gaps/${action.target_id}`,
    open_gap: `${meeting}/gaps/${action.target_id}`,
    open_meeting: meeting,
  }
  const target = targets[action.kind]
  if (target) void router.push(target)
}
</script>

<template>
  <section class="ms-page-head ms-home-head">
    <div>
      <p class="ms-eyebrow">准备会议</p>
      <h1>让讨论自然发生，<br />记录可靠落盘。</h1>
      <p>先选择参会人即可开始；设备、局域网与会议号按需调整。</p>
    </div>
    <RouterLink class="ms-button ms-button--primary" to="/meetings/new"
      >创建会议</RouterLink
    >
  </section>

  <p
    v-if="query.errorMessage || peopleError"
    class="ms-notice ms-notice--danger"
    role="alert"
  >
    {{ query.errorMessage || peopleError }}
  </p>
  <section v-if="query.loading" class="ms-card ms-empty-state" aria-busy="true">
    <h2>正在读取本地会议…</h2>
  </section>

  <template v-else-if="query.home">
    <section class="ms-home-actions" aria-label="首页主要任务">
      <article class="ms-card ms-home-action-card">
        <template v-if="firstGroup">
          <div class="ms-card-head">
            <div>
              <p class="ms-meta">快速开始</p>
              <h2>{{ firstGroup.name }}</h2>
            </div>
            <span class="ms-status-pill is-complete"
              >{{ firstGroup.members.length }} 位成员</span
            >
          </div>
          <p class="ms-muted">
            使用常用小组预选
            {{ firstGroup.members.length }}
            位成员；进入创建会议页后确认其他设置。
          </p>
          <div class="ms-home-action-foot">
            <div class="ms-avatar-row" aria-label="小组成员">
              <span
                v-for="member in firstGroup.members.slice(0, 4)"
                :key="member.id"
                class="ms-avatar"
                >{{ initials(member.name) }}</span
              >
              <span v-if="firstGroup.members.length > 4" class="ms-avatar"
                >+{{ firstGroup.members.length - 4 }}</span
              >
            </div>
            <RouterLink
              class="ms-button ms-button--quiet"
              :to="`/meetings/new?group=${firstGroup.id}`"
              >选择此小组</RouterLink
            >
          </div>
        </template>
        <template v-else>
          <p class="ms-meta">快速开始</p>
          <h2>先创建常用小组</h2>
          <p class="ms-muted">建立小组后，可以在会前一次预选多位成员。</p>
          <div class="ms-home-empty-action">
            <RouterLink
              class="ms-button ms-button--quiet"
              to="/people?tab=groups"
              >创建小组</RouterLink
            >
          </div>
        </template>
      </article>

      <article class="ms-card ms-home-action-card">
        <template v-if="query.home.continuation">
          <div class="ms-card-head">
            <div>
              <p class="ms-meta">继续处理</p>
              <h2>
                {{
                  query.home.continuation.subject ||
                  query.home.continuation.meeting_no
                }}
              </h2>
            </div>
            <span class="ms-status-pill is-warning">{{
              statusLabels[query.home.continuation.highest_status] ||
              query.home.continuation.highest_status
            }}</span>
          </div>
          <p class="ms-muted ms-input--mono">
            {{ query.home.continuation.meeting_no }}
          </p>
          <div class="ms-home-action-foot">
            <span class="ms-meta"
              >另有 {{ query.home.remaining }} 场需要处理</span
            >
            <button
              class="ms-button ms-button--quiet"
              type="button"
              data-home-primary-action
              :disabled="!query.home.continuation.primary_action.enabled"
              :title="
                query.home.continuation.primary_action.disabled_reason || ''
              "
              @click="runPrimaryAction(query.home.continuation)"
            >
              {{ query.home.continuation.primary_action.label }}
            </button>
          </div>
        </template>
        <template v-else>
          <p class="ms-meta">继续处理</p>
          <h2>当前没有待办会议</h2>
          <p class="ms-muted">需要恢复、补转写或查看纪要的会议会显示在这里。</p>
        </template>
      </article>
    </section>

    <section class="ms-card ms-home-recent">
      <div class="ms-card-head">
        <h2>最近会议</h2>
        <RouterLink class="ms-button ms-button--quiet" to="/meetings"
          >查看全部</RouterLink
        >
      </div>
      <ul v-if="query.home.recent_meetings.length" class="ms-home-recent-list">
        <li v-for="item in query.home.recent_meetings" :key="item.id">
          <RouterLink :to="`/meetings/${item.id}`">
            <strong>{{ item.subject || item.meeting_no }}</strong>
            <span class="ms-meta ms-input--mono">{{
              formatTime(item.started_at)
            }}</span>
          </RouterLink>
          <span class="ms-status-pill">{{
            statusLabels[item.highest_status] || item.highest_status
          }}</span>
        </li>
      </ul>
      <div v-else class="ms-empty-state">
        <h2>还没有会议记录</h2>
        <p>会议结束并安全保存后会显示在这里。</p>
      </div>
    </section>
  </template>
</template>
