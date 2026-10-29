<script setup lang="ts">
const props = defineProps<{
  modelValue: string;
  value: string;
  label?: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: string];
  change: [value: string];
}>();
function pick() {
  emit("update:modelValue", props.value);
  emit("change", props.value);
}
</script>
<template>
  <label class="pa-radio" :class="{ 'pa-radio--disabled': disabled }">
    <input
      type="radio"
      :checked="modelValue === value"
      :disabled="disabled"
      :aria-label="label"
      @change="pick"
    />
    <span v-if="label">{{ label }}</span>
  </label>
</template>
