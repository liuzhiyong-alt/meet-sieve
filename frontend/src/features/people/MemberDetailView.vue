<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import {
  DeleteAllMemberVoiceSamples,
  GetMemberDetail,
  UpdateMember,
} from '../../../wailsjs/go/wails/PeopleBinding'
import type { wails } from '../../../wailsjs/go/models'
import { dirtyEditRegistry } from '../../router/dirty'
import { setBreadcrumbTitles } from '../../router/breadcrumb'

const props = defineProps<{ id: string }>()
const route = useRoute()
const detail = ref<wails.MemberDetailDTO>()
const name = ref('')
const notes = ref('')
const initial = ref('')
const error = ref('')
const notice = ref('')
const dirty = computed(
  () => JSON.stringify([name.value, notes.value]) !== initial.value,
)
let unregister: (() => void) | undefined
/** snapshot 保存当前 revision 对应的编辑基线。 */
function snapshot(): void {
  initial.value = JSON.stringify([name.value, notes.value])
}
/** load 读取活动或归档成员详情与引用摘要。 */
async function load(): Promise<void> {
  const result = await GetMemberDetail(props.id)
  if (result.code !== 200 || !result.data) {
    error.value = result.message
    return
  }
  detail.value = result.data
  setBreadcrumbTitles(route.path, { current: result.data.member.name })
  name.value = result.data.member.name
  notes.value = result.data.member.notes ?? ''
  snapshot()
}
/** save 保存成员当前资料。 */
async function save(): Promise<boolean> {
  const result = await UpdateMember(props.id, {
    name: name.value,
    notes: notes.value,
    revision: detail.value?.revision,
  })
  if (result.code !== 200) {
    error.value = result.message
    return false
  }
  notice.value = '成员资料已保存'
  await load()
  return true
}
/** deleteVoice 删除全部声纹但保留成员和历史。 */
async function deleteVoice(): Promise<void> {
  const result = await DeleteAllMemberVoiceSamples(props.id)
  if (result.code !== 200) error.value = result.message
  else {
    notice.value = '声纹样本已删除，成员和历史会议已保留'
    await load()
  }
}
onMounted(async () => {
  await load()
  unregister = dirtyEditRegistry.register({
    id: `member-${props.id}`,
    label: '成员资料',
    isDirty: () => dirty.value,
    canSave: () => Boolean(name.value.trim()),
    save,
    discard: () => {
      if (detail.value) {
        name.value = detail.value.member.name
        notes.value = detail.value.member.notes ?? ''
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
      <RouterLink class="ms-link-button" to="/people?tab=members"
        >返回成员列表</RouterLink
      >
      <p class="ms-eyebrow">成员详情</p>
      <h1>{{ detail?.member.name || '正在读取…' }}</h1>
      <p>历史会议始终显示当时的姓名快照。</p>
    </div>
  </section>
  <p v-if="error" class="ms-notice ms-notice--danger" role="alert">
    {{ error }}
  </p>
  <p v-if="notice" class="ms-notice ms-notice--info" aria-live="polite">
    {{ notice }}
  </p>
  <section v-if="detail" class="ms-detail-grid">
    <article class="ms-card ms-settings-card">
      <label class="ms-field"
        ><span>姓名</span><input v-model="name" class="ms-input" /></label
      ><label class="ms-field"
        ><span>备注</span
        ><textarea v-model="notes" class="ms-input ms-textarea" />
      </label>
      <p class="ms-help">资料版本 {{ detail.revision }}</p>
      <div class="ms-actions">
        <button
          class="ms-button ms-button--primary"
          :disabled="!dirty || !name.trim()"
          @click="save"
        >
          保存更改
        </button>
      </div>
    </article>
    <aside class="ms-card ms-settings-card">
      <h2>引用与声纹</h2>
      <dl class="ms-fact-grid">
        <div>
          <dt>当前小组</dt>
          <dd>{{ detail.group_count }}</dd>
        </div>
        <div>
          <dt>历史会议</dt>
          <dd>{{ detail.historical_meetings }}</dd>
        </div>
        <div>
          <dt>可用声纹</dt>
          <dd>{{ detail.member.accepted_sample_count }}</dd>
        </div>
      </dl>
      <div class="ms-actions">
        <button
          class="ms-button ms-button--quiet"
          :disabled="!detail.member.accepted_sample_count"
          @click="deleteVoice"
        >
          删除全部声纹
        </button>
      </div>
    </aside>
  </section>
</template>
