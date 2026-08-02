<script lang="ts" setup>
withDefaults(
  defineProps<{
    current?:
      'start' | 'live' | 'interrupted' | 'records' | 'people' | 'settings'
    meetingNo?: string
    localSaveLabel?: string
  }>(),
  {
    current: 'settings',
    meetingNo: '',
    localSaveLabel: '本地数据已验证',
  },
)
defineEmits<{
  navigate: [
    destination: 'start' | 'live' | 'interrupted' | 'people' | 'settings',
  ]
}>()
</script>

<template>
  <div class="ms-app-shell">
    <aside class="ms-sidebar" aria-label="主导航">
      <div>
        <p class="ms-brand">MeetSieve</p>
        <nav class="ms-nav">
          <button
            class="ms-nav__item"
            :class="{ 'is-current': current === 'start' }"
            :aria-current="current === 'start' ? 'page' : undefined"
            @click="$emit('navigate', 'start')"
          >
            开始会议
          </button>
          <button
            v-if="current === 'live' || current === 'interrupted'"
            class="ms-nav__item is-current"
            aria-current="page"
            @click="$emit('navigate', current)"
          >
            {{ current === 'live' ? '会议进行中' : '会议恢复' }}
          </button>
          <span
            class="ms-nav__item"
            :class="{ 'is-current': current === 'records' }"
            :aria-current="current === 'records' ? 'page' : undefined"
            >会议记录</span
          >
          <button
            class="ms-nav__item"
            :class="{ 'is-current': current === 'people' }"
            :aria-current="current === 'people' ? 'page' : undefined"
            @click="$emit('navigate', 'people')"
          >
            常用小组
          </button>
          <button
            class="ms-nav__item"
            :class="{ 'is-current': current === 'settings' }"
            :aria-current="current === 'settings' ? 'page' : undefined"
            @click="$emit('navigate', 'settings')"
          >
            设置
          </button>
        </nav>
      </div>
      <div class="ms-sidebar__foot">
        <p>{{ localSaveLabel }}</p>
        <p v-if="meetingNo" class="ms-input--mono">{{ meetingNo }}</p>
      </div>
    </aside>
    <main class="ms-main">
      <header class="ms-titlebar">
        <span>
          {{
            current === 'people'
              ? '常用小组'
              : current === 'records'
                ? '校对原始记录'
                : current === 'settings'
                  ? '设置'
                  : current === 'start'
                    ? '创建会议'
                    : current === 'interrupted'
                      ? '会议恢复'
                      : 'MeetSieve / 会议进行中'
          }}
        </span>
        <div id="meeting-titlebar-actions" class="ms-titlebar__actions">
          <slot name="titlebar-actions" />
        </div>
      </header>
      <div class="ms-content"><slot /></div>
    </main>
  </div>
</template>
