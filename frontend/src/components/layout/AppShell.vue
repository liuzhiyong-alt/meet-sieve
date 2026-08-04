<script lang="ts" setup>
import { RouterLink, useRoute } from 'vue-router'
import PageBreadcrumb from '../navigation/PageBreadcrumb.vue'

const props = defineProps<{
  meetingNo?: string
  localSaveLabel?: string
  activeMeeting?: boolean
}>()
defineEmits<{ endMeeting: [] }>()
const route = useRoute()

/** isCurrent 根据正式路由决定主导航选中态。 */
function isCurrent(prefix: string): boolean {
  return (
    route.path === prefix ||
    (prefix !== '/home' && route.path.startsWith(prefix))
  )
}

/** startMeetingTarget 让“开始会议”在存在活动会议时直接回到会中页。 */
function startMeetingTarget(): string {
  return props.activeMeeting ? '/meetings/live' : '/meetings/new'
}
</script>

<template>
  <div class="ms-app-shell">
    <aside class="ms-sidebar" aria-label="主导航">
      <div>
        <p class="ms-brand">
          <span class="ms-brand-mark" aria-hidden="true">M</span>
          <span>MeetSieve</span>
        </p>
        <nav class="ms-nav">
          <RouterLink
            class="ms-nav__item"
            :class="{ 'is-current': isCurrent('/home') }"
            to="/home"
            >首页</RouterLink
          >
          <RouterLink
            class="ms-nav__item"
            :class="{
              'is-current':
                isCurrent('/meetings/new') || isCurrent('/meetings/live'),
            }"
            :to="startMeetingTarget()"
            >开始会议</RouterLink
          >
          <RouterLink
            class="ms-nav__item"
            :class="{
              'is-current':
                route.path === '/meetings' || route.name === 'meeting-detail',
            }"
            to="/meetings"
            >会议记录</RouterLink
          >
          <RouterLink
            class="ms-nav__item"
            :class="{ 'is-current': isCurrent('/people') }"
            to="/people"
            >小组与成员</RouterLink
          >
          <RouterLink
            class="ms-nav__item"
            :class="{ 'is-current': isCurrent('/settings') }"
            to="/settings/general"
            >设置</RouterLink
          >
        </nav>
      </div>
      <div class="ms-sidebar__foot">
        <p>{{ localSaveLabel || '本地数据已验证' }}</p>
        <p v-if="meetingNo" class="ms-input--mono">{{ meetingNo }}</p>
        <button
          v-if="activeMeeting"
          class="ms-button ms-button--danger"
          type="button"
          @click="$emit('endMeeting')"
        >
          结束会议
        </button>
      </div>
    </aside>
    <main class="ms-main">
      <header class="ms-titlebar">
        <PageBreadcrumb />
        <div id="meeting-titlebar-actions" class="ms-titlebar__actions" />
      </header>
      <div class="ms-content"><slot /></div>
    </main>
  </div>
</template>
