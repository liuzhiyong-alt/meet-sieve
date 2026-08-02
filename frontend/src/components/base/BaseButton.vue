<script lang="ts" setup>
withDefaults(
  defineProps<{
    variant?: 'default' | 'primary' | 'quiet' | 'danger'
    busy?: boolean
    disabled?: boolean
  }>(),
  { variant: 'default', busy: false, disabled: false },
)

defineEmits<{ click: [event: MouseEvent] }>()
</script>

<template>
  <button
    class="ms-button"
    :class="`ms-button--${variant}`"
    :aria-busy="busy || undefined"
    :disabled="busy || disabled"
    type="button"
    @click="$emit('click', $event)"
  >
    <span v-if="busy" class="ms-spinner" aria-hidden="true" />
    <slot />
  </button>
</template>
