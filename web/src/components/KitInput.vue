<script setup lang="ts">
defineProps<{
  modelValue: string;
  placeholder?: string;
  type?: string;
  inputmode?:
    "text" | "numeric" | "decimal" | "email" | "url" | "tel" | "search";
  maxlength?: number;
  disabled?: boolean;
  error?: boolean;
  ariaLabel?: string;
  autocomplete?: string;
  name?: string;
  required?: boolean;
  pattern?: string;
}>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
</script>
<template>
  <span
    class="pa-input"
    :class="{ 'pa-input--error': error, 'pa-input--disabled': disabled }"
  >
    <input
      :value="modelValue"
      :type="type || 'text'"
      :placeholder="placeholder"
      :inputmode="inputmode"
      :maxlength="maxlength"
      :disabled="disabled"
      :aria-label="ariaLabel"
      :autocomplete="autocomplete"
      :name="name"
      :required="required"
      :pattern="pattern"
      :aria-invalid="error || undefined"
      @input="
        emit('update:modelValue', ($event.target as HTMLInputElement).value)
      "
    />
  </span>
</template>
