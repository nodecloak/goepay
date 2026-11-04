<script setup lang="ts">
import { computed } from "vue";

const props = withDefaults(
  defineProps<{
    label: string;
    modelValue: string;
    operation: string;
    configured: boolean;
    currentValue?: string;
    secret?: boolean;
    multiline?: boolean;
    options?: { value: string; label: string }[];
    keepAllowed?: boolean;
    clearAllowed?: boolean;
    disabled?: boolean;
    inputLabel?: string;
    optionLabel?: string;
  }>(),
  { keepAllowed: true, clearAllowed: true },
);
const emit = defineEmits<{
  "update:modelValue": [value: string];
  "update:operation": [operation: "keep" | "replace" | "clear"];
}>();

// Existing secrets are write-only. The configured state belongs in the
// placeholder, never in an editable value or in a submitted replacement.
const displayedValue = computed(() =>
  props.operation === "clear"
    ? ""
    : props.operation === "replace"
      ? props.modelValue
      : props.secret
        ? ""
        : (props.currentValue ?? ""),
);
const placeholder = computed(() =>
  props.operation === "clear" && props.configured
    ? "保存后将清除"
    : props.configured &&
        props.keepAllowed &&
        (props.secret || props.currentValue === undefined)
      ? "已配置，输入新值可修改"
      : "请输入" + props.label,
);
const extraOption = computed(() =>
  displayedValue.value &&
  props.options?.length &&
  !props.options.some((option) => option.value === displayedValue.value)
    ? displayedValue.value
    : "",
);
function input(event: Event) {
  const value = (event.target as HTMLInputElement).value;
  emit("update:modelValue", value);
  if (!value) {
    // Blanking a write-only field cancels its draft; clearing the saved secret
    // is an explicit action. Required/missing fields cannot inherit a value.
    if (
      props.keepAllowed &&
      (props.secret || props.currentValue === undefined)
    ) {
      emit("update:operation", "keep");
    } else if (props.clearAllowed && (props.configured || !props.keepAllowed)) {
      emit("update:operation", "clear");
    } else {
      emit(
        "update:operation",
        props.keepAllowed && !props.configured ? "keep" : "replace",
      );
    }
  } else {
    emit(
      "update:operation",
      props.keepAllowed && !props.secret && value === props.currentValue
        ? "keep"
        : "replace",
    );
  }
}
function clear() {
  emit("update:modelValue", "");
  emit("update:operation", "clear");
}
function reset() {
  emit("update:modelValue", props.secret ? "" : (props.currentValue ?? ""));
  emit("update:operation", props.keepAllowed ? "keep" : "replace");
}
</script>

<template>
  <div class="config-value-editor">
    <select
      v-if="options?.length"
      :value="displayedValue"
      :disabled="disabled"
      :aria-label="optionLabel || inputLabel || label"
      @change="input"
    >
      <option value="">请选择</option>
      <option v-if="extraOption" :value="extraOption">{{ extraOption }}</option>
      <option
        v-for="option in options"
        :key="option.value"
        :value="option.value"
      >
        {{ option.label }}
      </option>
    </select>
    <textarea
      v-else-if="multiline"
      :value="displayedValue"
      :disabled="disabled"
      :aria-label="inputLabel || label"
      :placeholder="placeholder"
      autocomplete="off"
      autocapitalize="off"
      spellcheck="false"
      rows="3"
      @input="input"
    />
    <input
      v-else
      :value="displayedValue"
      :disabled="disabled"
      :type="secret ? 'password' : 'text'"
      :aria-label="inputLabel || label"
      :placeholder="placeholder"
      :autocomplete="secret ? 'new-password' : 'off'"
      autocapitalize="off"
      spellcheck="false"
      @input="input"
    />
    <div
      v-if="
        (configured && operation === 'clear') ||
        (clearAllowed && configured) ||
        (operation === 'replace' && keepAllowed)
      "
      class="config-value-actions"
    >
      <template v-if="operation === 'clear'">
        <span role="status">保存后将清除</span>
        <button
          type="button"
          class="pa-btn pa-btn--text"
          :disabled="disabled"
          :aria-label="'撤销清除' + label"
          @click="reset"
        >
          撤销清除
        </button>
      </template>
      <template v-else>
        <button
          v-if="operation === 'replace' && keepAllowed"
          type="button"
          class="pa-btn pa-btn--text"
          :disabled="disabled"
          :aria-label="'取消修改' + label"
          @click="reset"
        >
          取消修改
        </button>
        <button
          v-if="clearAllowed && configured"
          type="button"
          class="pa-btn pa-btn--text"
          :disabled="disabled"
          :aria-label="'清除' + label"
          @click="clear"
        >
          清除
        </button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.config-value-editor {
  min-width: 0;
  width: 100%;
}
.config-value-editor > input,
.config-value-editor > textarea,
.config-value-editor > select {
  box-sizing: border-box;
  display: block;
  width: 100%;
  min-width: 0;
  max-width: 100%;
  margin: 0;
}
.config-value-editor > textarea {
  resize: vertical;
  overflow-wrap: anywhere;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
}
.config-value-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 6px;
  color: var(--ink-muted);
  font-size: 12px;
  line-height: 1.5;
}
.config-value-actions .pa-btn {
  font-size: 12px;
  min-height: 28px;
  padding: 2px 8px;
}
</style>
