<script setup lang="ts">
import { computed } from "vue";
import type { Area } from "../menu";
import {
  transferSearchFields,
  transferStatusFields,
  transferFiltersError,
  type TransferReadFilters,
} from "../transfer-filters";
const props = defineProps<{
  modelValue: TransferReadFilters;
  area: Area;
  loading: boolean;
  blocked?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: TransferReadFilters];
  reset: [];
}>();
const error = computed(() => transferFiltersError(props.modelValue));
function change(key: keyof TransferReadFilters, event: Event) {
  emit("update:modelValue", {
    ...props.modelValue,
    [key]: (event.target as HTMLInputElement | HTMLSelectElement).value,
  });
}
</script>
<template>
  <div class="transfer-list-filters">
    <div class="transfer-filter-grid">
      <label
        >搜索字段<select
          aria-label="搜索字段"
          :value="modelValue.search_field"
          @change="change('search_field', $event)"
        >
          <option
            v-for="field in transferSearchFields"
            :key="field.value"
            :value="field.value"
          >
            {{ field.label }}
          </option>
        </select></label
      >
      <label
        >搜索内容<input
          :value="modelValue.q"
          aria-label="搜索付款记录"
          :placeholder="
            modelValue.search_field === 'money'
              ? '请输入元金额'
              : '搜索付款记录'
          "
          @input="change('q', $event)"
      /></label>
      <label v-if="area === 'admin'"
        >商户号<input
          :value="modelValue.merchant_id"
          inputmode="numeric"
          @input="change('merchant_id', $event)"
      /></label>
      <label v-if="area === 'admin'"
        >通道ID<input
          :value="modelValue.channel_id"
          inputmode="numeric"
          @input="change('channel_id', $event)"
      /></label>
      <label
        >开始日期<input
          type="date"
          :value="modelValue.from"
          @input="change('from', $event)" /></label
      ><label
        >结束日期<input
          type="date"
          :value="modelValue.to"
          @input="change('to', $event)"
      /></label>
      <label
        >状态<select
          aria-label="状态"
          :value="modelValue.status"
          @change="change('status', $event)"
        >
          <option
            v-for="field in transferStatusFields"
            :key="field.value"
            :value="field.value"
          >
            {{ field.label }}
          </option>
        </select></label
      >
    </div>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <div class="toolbar-actions">
      <button type="submit" :disabled="loading || !!error || blocked">
        查询</button
      ><button type="button" :disabled="loading" @click="emit('reset')">
        重置
      </button>
    </div>
  </div>
</template>
<style scoped>
.transfer-list-filters {
  flex-basis: 100%;
  min-width: 0;
}
.transfer-filter-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 180px), 1fr));
  gap: 12px;
  margin-bottom: 12px;
}
.transfer-filter-grid label {
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-width: 0;
}
.transfer-filter-grid input,
.transfer-filter-grid select {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
</style>
