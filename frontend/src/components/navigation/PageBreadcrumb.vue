<script lang="ts" setup>
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import {
  getBreadcrumbTitles,
  type BreadcrumbDefinition,
} from '../../router/breadcrumb'

const route = useRoute()

const items = computed(() => {
  const definitions =
    (route.meta.breadcrumb as BreadcrumbDefinition[] | undefined) ?? []
  const dynamic = getBreadcrumbTitles(route.path)
  return definitions
    .map((definition) => ({
      label:
        definition.label ??
        (definition.dynamic === 'meeting'
          ? dynamic.meeting ||
            String(route.query.subject ?? route.query.no ?? '')
          : dynamic.current || ''),
      to: definition.to?.replace(':id', String(route.params.id ?? '')),
    }))
    .filter((item) => item.label)
})
</script>

<template>
  <nav class="ms-breadcrumb" aria-label="面包屑">
    <template v-for="(item, index) in items" :key="`${item.label}-${index}`">
      <span v-if="index" aria-hidden="true">/</span>
      <RouterLink v-if="item.to" :to="item.to">{{ item.label }}</RouterLink>
      <span v-else aria-current="page">{{ item.label }}</span>
    </template>
  </nav>
</template>
