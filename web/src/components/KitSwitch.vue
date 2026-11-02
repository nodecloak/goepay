<script setup lang="ts">
defineProps<{
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
  const checked = (event.target as HTMLInputElement).checked;
  emit("update:modelValue", checked);
  emit("change", checked);
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
