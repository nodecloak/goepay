<script setup lang="ts">
import { nextTick } from "vue";
const props = defineProps<{
  modelValue: boolean;
  label?: string;
  ariaLabel?: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: boolean];
  change: [value: boolean];
}>();
function toggle(event: Event) {
  const input = event.target as HTMLInputElement;
  const checked = input.checked;
  emit("update:modelValue", checked);
  emit("change", checked);
  // A parent may require confirmation before accepting the proposed value.
  // Vue need not patch an unchanged prop, so restore the actual state explicitly.
  void nextTick(() => { input.checked = props.modelValue; });
}
</script>
<template>
  <label class="pa-switch" :class="{ 'pa-switch--disabled': disabled }">
    <input
      type="checkbox"
      role="switch"
      :checked="modelValue"
      :disabled="disabled"
      :aria-label="ariaLabel || label"
      @change="toggle"
    />
    <span v-if="label">{{ label }}</span>
  </label>
</template>
