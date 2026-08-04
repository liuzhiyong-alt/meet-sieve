<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import {
  DeleteGroup,
  GetGroup,
  ListMembers,
  UpdateGroup,
} from '../../../wailsjs/go/wails/PeopleBinding'
import type { wails } from '../../../wailsjs/go/models'
import { dirtyEditRegistry } from '../../router/dirty'
import { setBreadcrumbTitles } from '../../router/breadcrumb'

const props = defineProps<{ id: string }>()
const route = useRoute()
const router = useRouter()
const group = ref<wails.GroupDTO>()
const members = ref<wails.MemberDTO[]>([])
const name = ref('')
const memberIDs = ref<string[]>([])
const lanEnabled = ref(false)
const initial = ref('')
const error = ref('')
const dirty = computed(
  () =>
    JSON.stringify({
      name: name.value,
      memberIDs: memberIDs.value,
      lanEnabled: lanEnabled.value,
    }) !== initial.value,
)
let unregister: (() => void) | undefined

/** snapshot 保存表单基线用于 dirty guard。 */
function snapshot(): void {
  initial.value = JSON.stringify({
    name: name.value,
    memberIDs: memberIDs.value,
    lanEnabled: lanEnabled.value,
  })
}
/** load 读取小组和活动成员候选。 */
async function load(): Promise<void> {
  const [groupResult, memberResult] = await Promise.all([
    GetGroup(props.id),
    ListMembers(),
  ])
  if (
    groupResult.code !== 200 ||
    !groupResult.data ||
    memberResult.code !== 200 ||
    !memberResult.data
  ) {
    error.value = groupResult.message || memberResult.message
    return
  }
  group.value = groupResult.data
  setBreadcrumbTitles(route.path, { current: groupResult.data.name })
  members.value = memberResult.data
  name.value = group.value.name
  lanEnabled.value = group.value.default_lan_enabled
  memberIDs.value = group.value.members.map((item) => item.member_id)
  snapshot()
}
/** save 完整替换小组当前关系，历史会议快照不变。 */
async function save(): Promise<boolean> {
  const result = await UpdateGroup(props.id, {
    name: name.value,
    default_lan_enabled: lanEnabled.value,
    member_ids: memberIDs.value,
    revision: group.value?.updated_at,
  })
  if (result.code !== 200 || !result.data) {
    error.value = result.message
    return false
  }
  group.value = result.data
  setBreadcrumbTitles(route.path, { current: result.data.name })
  snapshot()
  return true
}
/** remove 删除小组及当前关系，不删除成员。 */
async function remove(): Promise<void> {
  const result = await DeleteGroup(props.id)
  if (result.code === 200) await router.replace('/people?tab=groups')
  else error.value = result.message
}
onMounted(async () => {
  await load()
  unregister = dirtyEditRegistry.register({
    id: `group-${props.id}`,
    label: '小组资料',
    isDirty: () => dirty.value,
    canSave: () => Boolean(name.value.trim()),
    save,
    discard: () => {
      if (group.value) {
        name.value = group.value.name
        memberIDs.value = group.value.members.map((item) => item.member_id)
        lanEnabled.value = group.value.default_lan_enabled
        snapshot()
      }
    },
  })
})
onBeforeUnmount(() => unregister?.())
</script>

<template>
  <section class="ms-page-head">
    <div>
      <RouterLink class="ms-link-button" to="/people?tab=groups"
        >返回常用小组</RouterLink
      >
      <p class="ms-eyebrow">小组详情</p>
      <h1>{{ group?.name || '正在读取…' }}</h1>
      <p>删除小组只移除当前关系，不删除成员或历史会议快照。</p>
    </div>
  </section>
  <p v-if="error" class="ms-notice ms-notice--danger" role="alert">
    {{ error }}
  </p>
  <section v-if="group" class="ms-card ms-settings-card">
    <label class="ms-field"
      ><span>小组名称</span><input v-model="name" class="ms-input" /></label
    ><label class="ms-check"
      ><input v-model="lanEnabled" type="checkbox" />新会议默认开启 LAN
      访客页</label
    >
    <fieldset class="ms-member-picker">
      <legend>成员顺序</legend>
      <label v-for="member in members" :key="member.id"
        ><input v-model="memberIDs" type="checkbox" :value="member.id" />{{
          member.name
        }}</label
      >
    </fieldset>
    <div class="ms-actions">
      <button
        class="ms-button ms-button--primary"
        :disabled="!dirty || !name.trim()"
        @click="save"
      >
        保存更改</button
      ><button class="ms-button ms-button--danger" @click="remove">
        删除小组
      </button>
    </div>
  </section>
</template>
