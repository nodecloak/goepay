<script setup lang="ts">
import { computed } from "vue";
import type { Area } from "../menu";
import ShellIcon from "./ShellIcon.vue";
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
  <div class="search-group">
    <label class="sr-only" for="transfer-search-field">搜索字段</label>
    <select
      id="transfer-search-field"
      aria-label="搜索字段"
      class="order-search-field"
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
    </select>
    <label class="sr-only" for="transfer-search">搜索内容</label>
    <input
      id="transfer-search"
      :value="modelValue.q"
      aria-label="搜索付款记录"
      :placeholder="
        modelValue.search_field === 'money' ? '请输入元金额' : '搜索付款记录'
      "
      @input="change('q', $event)"
    />
  </div>
  <label v-if="area === 'admin'" class="filter-inline"
    >商户号<input
      :value="modelValue.merchant_id"
      aria-label="商户号"
      inputmode="numeric"
      @input="change('merchant_id', $event)"
  /></label>
  <div class="filter-actions">
    <button
      type="submit"
      class="primary"
      :disabled="loading || !!error || blocked"
    >
      查询</button
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
      <label v-if="area === 'admin'"
        >通道ID<input
          :value="modelValue.channel_id"
          inputmode="numeric"
          @input="change('channel_id', $event)"
      /></label>
      <label
        >开始日期<input
          type="date"
          aria-label="开始日期"
          :value="modelValue.from"
          @input="change('from', $event)"
      /></label>
      <label
        >结束日期<input
          type="date"
          aria-label="结束日期"
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
    <div class="list-filters-foot">
      <button type="button" @click="emit('reset')">清空筛选</button>
    </div>
  </details>
  <p v-if="error" class="alert error transfer-filter-error" role="alert">
    {{ error }}
  </p>
</template>
<style scoped>
.transfer-filter-error {
  flex-basis: 100%;
  margin: 0;
}
</style>
