<script lang="ts" setup>
withDefaults(
  defineProps<{
    variant?: 'neutral' | 'warning' | 'danger' | 'info'
    title: string
  }>(),
  {
    variant: 'neutral',
  },
)
</script>

<template>
  <section
    class="ms-notice"
    :class="`ms-notice--${variant}`"
    :role="variant === 'danger' ? 'alert' : undefined"
  >
    <div>
      <strong>{{ title }}</strong>
      <p><slot /></p>
    </div>
  </section>
</template>
