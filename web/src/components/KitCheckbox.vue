<script setup lang="ts">
import { ref, watch } from "vue";
const props = defineProps<{
  modelValue: boolean;
  label?: string;
  disabled?: boolean;
  indeterminate?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: boolean];
  change: [value: boolean];
}>();
const input = ref<HTMLInputElement>();
watch(
  () => props.indeterminate,
  (value) => {
    if (input.value) input.value.indeterminate = !!value;
  },
  { immediate: true, flush: "post" },
);
function toggle(event: Event) {
  const checked = (event.target as HTMLInputElement).checked;
  emit("update:modelValue", checked);
  emit("change", checked);
}
</script>
<template>
  <label class="pa-check" :class="{ 'pa-check--disabled': disabled }">
    <input
      ref="input"
      type="checkbox"
      :checked="modelValue"
      :disabled="disabled"
      :aria-label="label"
      @change="toggle"
    />
    <span v-if="label">{{ label }}</span>
  </label>
</template>
