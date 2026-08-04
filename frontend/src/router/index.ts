import {
  createMemoryHistory,
  createRouter,
  createWebHashHistory,
  type RouterHistory,
} from 'vue-router'

import HomeView from '../features/home/HomeView.vue'
import RecordsView from '../features/records/RecordsView.vue'
import MeetingDetailView from '../features/detail/MeetingDetailView.vue'
import InterruptedRecoveryView from '../features/recovery/InterruptedRecoveryView.vue'
import DeleteRecoveryView from '../features/recovery/DeleteRecoveryView.vue'
import StartMeetingView from '../features/meeting/StartMeetingView.vue'
import LiveMeetingView from '../features/meeting/LiveMeetingView.vue'
import TranscriptEditorView from '../features/correction/TranscriptEditorView.vue'
import GapConflictView from '../features/gap/GapConflictView.vue'
import MinutesWorkspaceView from '../features/minutes/MinutesWorkspaceView.vue'
import MinutesHistoryView from '../features/minutes/MinutesHistoryView.vue'
import PeopleView from '../features/people/PeopleView.vue'
import GeneralSettingsView from '../features/settings/GeneralSettingsView.vue'
import { dirtyEditRegistry } from './dirty'

export const settingsSections = [
  'general',
  'audio',
  'asr',
  'codex',
  'voice-model',
] as const

/** createMeetSieveRouter 创建可由测试注入 memory history 的完整正式路由。 */
export function createMeetSieveRouter(history?: RouterHistory) {
  const router = createRouter({
    history:
      history ??
      (typeof window === 'undefined'
        ? createMemoryHistory()
        : createWebHashHistory()),
    routes: [
      { path: '/', redirect: '/home' },
      {
        path: '/home',
        name: 'home',
        component: HomeView,
        meta: { breadcrumb: [{ label: '首页' }] },
      },
      {
        path: '/meetings/new',
        name: 'meeting-new',
        component: StartMeetingView,
        meta: { breadcrumb: [{ label: '开始会议' }] },
      },
      {
        path: '/meetings/live',
        name: 'meeting-live',
        component: LiveMeetingView,
        meta: { breadcrumb: [{ label: '会议进行中' }] },
      },
      {
        path: '/meetings',
        name: 'records',
        component: RecordsView,
        meta: { breadcrumb: [{ label: '会议记录' }] },
      },
      {
        path: '/meetings/:id',
        name: 'meeting-detail',
        component: MeetingDetailView,
        props: true,
        meta: {
          breadcrumb: [
            { label: '会议记录', to: '/meetings' },
            { dynamic: 'current' },
          ],
        },
      },
      {
        path: '/meetings/:id/transcript',
        name: 'meeting-transcript',
        component: TranscriptEditorView,
        props: (route) => ({
          meetingId: route.params.id,
          meetingNo: String(route.query.no ?? ''),
          subject: String(route.query.subject ?? ''),
        }),
        meta: {
          breadcrumb: [
            { label: '会议记录', to: '/meetings' },
            { dynamic: 'meeting', to: '/meetings/:id' },
            { label: '原始记录' },
          ],
        },
      },
      {
        path: '/meetings/:id/gaps/:gap',
        name: 'meeting-gap',
        component: GapConflictView,
        props: (route) => ({
          meetingId: route.params.id,
          meetingNo: String(route.query.no ?? ''),
          gapId: route.params.gap,
        }),
        meta: {
          breadcrumb: [
            { label: '会议记录', to: '/meetings' },
            { dynamic: 'meeting', to: '/meetings/:id' },
            { label: '缺口处理' },
          ],
        },
      },
      {
        path: '/meetings/:id/minutes',
        name: 'meeting-minutes',
        component: MinutesWorkspaceView,
        props: (route) => ({
          meetingId: route.params.id,
          meetingNo: String(route.query.no ?? ''),
        }),
        meta: {
          breadcrumb: [
            { label: '会议记录', to: '/meetings' },
            { dynamic: 'meeting', to: '/meetings/:id' },
            { label: '会议纪要' },
          ],
        },
      },
      {
        path: '/meetings/:id/minutes/history',
        name: 'meeting-minutes-history',
        component: MinutesHistoryView,
        props: (route) => ({
          meetingId: route.params.id,
          meetingNo: String(route.query.no ?? ''),
        }),
        meta: {
          breadcrumb: [
            { label: '会议记录', to: '/meetings' },
            { dynamic: 'meeting', to: '/meetings/:id' },
            { label: '会议纪要', to: '/meetings/:id/minutes' },
            { label: '版本历史' },
          ],
        },
      },
      {
        path: '/meetings/:id/recovery',
        name: 'meeting-recovery',
        component: InterruptedRecoveryView,
        props: true,
        meta: {
          breadcrumb: [
            { label: '会议记录', to: '/meetings' },
            { dynamic: 'meeting', to: '/meetings/:id' },
            { label: '恢复会议' },
          ],
        },
      },
      {
        path: '/meetings/:id/delete-recovery',
        name: 'delete-recovery',
        component: DeleteRecoveryView,
        props: true,
        meta: {
          breadcrumb: [
            { label: '会议记录', to: '/meetings' },
            { dynamic: 'meeting', to: '/meetings/:id' },
            { label: '删除恢复' },
          ],
        },
      },
      {
        path: '/people',
        name: 'people',
        component: PeopleView,
        meta: { breadcrumb: [{ label: '小组与成员' }] },
      },
      {
        path: '/people/groups/:id',
        name: 'group-detail',
        redirect: (route) => ({
          path: '/people',
          query: { tab: 'groups', edit: String(route.params.id) },
        }),
      },
      {
        path: '/people/members/:id',
        name: 'member-detail',
        redirect: (route) => ({
          path: '/people',
          query: { tab: 'members', edit: String(route.params.id) },
        }),
      },
      { path: '/settings', redirect: '/settings/general' },
      {
        path: '/settings/:section',
        name: 'settings',
        component: GeneralSettingsView,
        props: (route) => ({ section: route.params.section }),
        meta: {
          breadcrumb: [
            { label: '设置', to: '/settings/general' },
            { dynamic: 'current' },
          ],
        },
      },
      { path: '/:pathMatch(.*)*', redirect: '/home' },
    ],
  })
  router.beforeEach(async (to) => {
    if (
      to.name === 'settings' &&
      !settingsSections.includes(
        to.params.section as (typeof settingsSections)[number],
      )
    ) {
      return '/settings/general'
    }
    return dirtyEditRegistry.confirmNavigation()
  })
  return router
}

export const router = createMeetSieveRouter()
