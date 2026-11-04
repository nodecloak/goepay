<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{
  modelValue: string;
  configured: boolean;
  label: string;
  disabled?: boolean;
  clearDisabled?: boolean;
  clearHint?: string;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: "keep" | "replace" | "clear"];
}>();
const action = computed(() =>
  props.modelValue === "replace" || props.modelValue === "clear"
    ? props.modelValue
    : "keep",
);
const primaryText = computed(() =>
  action.value === "clear"
    ? "撤销清除"
    : action.value === "replace"
      ? "取消修改"
      : props.configured
        ? "修改"
        : "设置",
);
function primary() {
  if (!props.disabled)
    emit("update:modelValue", action.value === "keep" ? "replace" : "keep");
}
function clear() {
  if (!props.disabled && !props.clearDisabled && props.configured)
    emit("update:modelValue", "clear");
}
</script>

<template>
  <div class="credential-control" role="group" :aria-label="`${label}配置`">
    <span class="credential-control-label">{{ label }}</span>
    <span class="credential-control-status">{{
      configured ? "已配置" : "未配置"
    }}</span>
    <span
      v-if="action === 'clear'"
      class="credential-control-pending"
      role="status"
    >
      保存后清除
    </span>
    <span v-else-if="action === 'replace'" class="credential-control-status"
      >编辑中</span
    >
    <div class="credential-control-buttons">
      <button
        type="button"
        class="pa-btn"
        :aria-label="`${primaryText}${label}`"
        :disabled="disabled"
        @click="primary"
      >
        {{ primaryText }}
      </button>
      <button
        v-if="configured && action !== 'clear'"
        type="button"
        class="pa-btn credential-control-clear"
        :aria-label="`清除${label}`"
        :disabled="disabled || clearDisabled"
        :title="clearDisabled ? clearHint : undefined"
        @click="clear"
      >
        清除
      </button>
    </div>
    <small
      v-if="configured && clearDisabled && clearHint"
      class="credential-control-hint"
    >
      {{ clearHint }}
    </small>
  </div>
</template>

<style scoped>
.credential-control {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 12px;
  min-width: 0;
  margin-block: 12px;
}
.credential-control-label {
  font-weight: 500;
  overflow-wrap: anywhere;
}
.credential-control-status,
.credential-control-hint {
  color: var(--ink-muted);
}
.credential-control-status,
.credential-control-pending {
  font-size: 12px;
}
.credential-control-pending,
.credential-control-clear {
  color: var(--danger-text);
}
.credential-control-buttons {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.credential-control-hint {
  flex-basis: 100%;
  font-size: 12px;
  line-height: 1.5;
}
@media (max-width: 480px) {
  .credential-control-buttons {
    margin-left: auto;
  }
}
</style>
