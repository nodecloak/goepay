<script setup lang="ts">
import type { Area } from "../menu";
import ShellIcon from "./ShellIcon.vue";
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
    class="toolbar list-filter-card panel"
    @submit.prevent="emit('submit')"
  >
    <div class="search-group">
      <label class="sr-only" for="settlement-search">搜索</label>
      <input
        id="settlement-search"
        aria-label="结算账号/姓名"
        autocomplete="off"
        placeholder="结算账号/姓名"
        :value="modelValue.value"
        @input="change(modelValue, 'value', $event)"
      />
    </div>
    <label v-if="area === 'admin'" class="filter-inline"
      >商户号<input
        aria-label="商户号"
        inputmode="numeric"
        autocomplete="off"
        placeholder="商户号"
        :value="modelValue.merchant_id"
        @input="change(modelValue, 'merchant_id', $event)"
    /></label>
    <label class="filter-inline"
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
    <div class="filter-actions">
      <button type="submit" class="primary" :disabled="loading">查询</button
      ><button type="button" :disabled="loading" @click="emit('reset')">
        重置
      </button>
    </div>
    <details class="list-filters">
      <summary>
        <ShellIcon name="filter" />更多筛选<ShellIcon
          name="chevronDown"
          class="list-filters-chevron"
        />
      </summary>
      <div class="list-filter-fields">
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
      <div class="list-filters-foot">
        <button type="button" @click="emit('reset')">清空筛选</button>
      </div>
    </details>
  </form>
</template>
