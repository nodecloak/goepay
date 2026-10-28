<script setup lang="ts">
import type { Area } from "../menu";
import {
  settlementStates,
  type SettlementFilters,
} from "../settlement-filters";
defineProps<{ modelValue: SettlementFilters; area: Area; loading: boolean }>();
const emit = defineEmits<{
  "update:modelValue": [value: SettlementFilters];
  submit: [];
  reset: [];
}>();
function change(
  value: SettlementFilters,
  key: keyof SettlementFilters,
  event: Event,
) {
  emit("update:modelValue", {
    ...value,
    [key]: (event.target as HTMLInputElement).value,
  });
}
</script>
<template>
  <form
    aria-label="结算筛选"
    class="settlement-filters"
    @submit.prevent="emit('submit')"
  >
    <div class="settlement-filter-grid">
      <label
        >搜索<input
          aria-label="结算账号/姓名"
          autocomplete="off"
          placeholder="结算账号/姓名"
          :value="modelValue.value"
          @input="change(modelValue, 'value', $event)"
      /></label>
      <label v-if="area === 'admin'"
        >商户号<input
          aria-label="商户号"
          inputmode="numeric"
          autocomplete="off"
          placeholder="商户号"
          :value="modelValue.merchant_id"
          @input="change(modelValue, 'merchant_id', $event)"
      /></label>
      <label
        >结算方式<select
          aria-label="结算方式"
          :value="modelValue.type"
          @change="change(modelValue, 'type', $event)"
        >
          <option value="">所有结算方式</option>
          <option value="alipay">支付宝</option>
          <option value="wxpay">微信</option>
          <option value="qqpay">QQ钱包</option>
          <option value="bank">银行卡</option>
        </select></label
      >
      <label
        >状态<select
          aria-label="状态"
          :value="modelValue.dstatus"
          @change="change(modelValue, 'dstatus', $event)"
        >
          <option value="">全部状态</option>
          <option
            v-for="[value, label] in settlementStates"
            :key="value"
            :value="value"
          >
            {{ label }}
          </option>
        </select></label
      >
      <label
        >开始日期<input
          aria-label="开始日期"
          type="date"
          :value="modelValue.from"
          @input="change(modelValue, 'from', $event)"
      /></label>
      <label
        >结束日期<input
          aria-label="结束日期"
          type="date"
          :value="modelValue.to"
          @input="change(modelValue, 'to', $event)"
      /></label>
    </div>
    <div class="toolbar-actions">
      <button type="submit" class="primary" :disabled="loading">查询</button
      ><button type="button" :disabled="loading" @click="emit('reset')">
        重置
      </button>
    </div>
  </form>
</template>
<style scoped>
.settlement-filters {
  display: grid;
  gap: 12px;
  margin-bottom: 0;
  padding: var(--space-4) var(--space-5);
  border-bottom: 1px solid var(--line);
}
.settlement-filters .toolbar-actions {
  gap: var(--space-2);
}
.settlement-filter-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 12px 16px;
}
.settlement-filter-grid label {
  display: grid;
  gap: 6px;
  min-width: 0;
  margin: 0;
  font-size: 12px;
  color: var(--ink-muted);
}
.settlement-filter-grid input,
.settlement-filter-grid select {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
  margin-top: 0;
}
@media (max-width: 520px) {
  .settlement-filter-grid {
    grid-template-columns: 1fr;
  }
}
</style>
