<script setup lang="ts">
defineProps<{ label: string; required?: boolean }>();
</script>
<template>
  <label class="pa-field">
    <span class="pa-label" :class="{ 'pa-label--req': required }">{{
      label
    }}</span>
    <slot />
  </label>
</template>
