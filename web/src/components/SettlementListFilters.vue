<script setup lang="ts">
import type { Area } from "../menu";
import KitFilterBar from "./KitFilterBar.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitSelect from "./KitSelect.vue";
import {
  settlementStates,
  type SettlementFilters,
} from "../settlement-filters";
const props = defineProps<{
  modelValue: SettlementFilters;
  area: Area;
  loading: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: SettlementFilters];
  submit: [];
  reset: [];
}>();
// 结算方式与状态下拉的 kit 选项（Pro settle/list 的字段与标签）。
const methodOptions = [
  { value: "", label: "所有结算方式" },
  { value: "alipay", label: "支付宝" },
  { value: "wxpay", label: "微信" },
  { value: "qqpay", label: "QQ钱包" },
  { value: "bank", label: "银行卡" },
];
const statusOptions = [
  { value: "", label: "全部状态" },
  ...settlementStates.map(([value, label]) => ({ value, label })),
];
function set<K extends keyof SettlementFilters>(
  key: K,
  value: SettlementFilters[K],
) {
  emit("update:modelValue", { ...props.modelValue, [key]: value });
}
</script>
<template>
  <KitFilterBar
    :loading="loading"
    label="结算筛选"
    @query="emit('submit')"
    @reset="emit('reset')"
  >
    <KitField label="搜索内容">
      <KitInput
        :model-value="modelValue.value"
        placeholder="结算账号/姓名"
        aria-label="结算账号/姓名"
        @update:model-value="(v) => set('value', v)"
      />
    </KitField>
    <KitField v-if="area === 'admin'" label="商户号">
      <KitInput
        :model-value="modelValue.merchant_id"
        placeholder="商户号"
        inputmode="numeric"
        aria-label="商户号"
        @update:model-value="(v) => set('merchant_id', v)"
      />
    </KitField>
    <KitField label="结算方式">
      <KitSelect
        :model-value="modelValue.type"
        :options="methodOptions"
        aria-label="结算方式"
        @update:model-value="(v) => set('type', v)"
      />
    </KitField>
    <template #extra>
      <KitField label="状态">
        <KitSelect
          :model-value="modelValue.dstatus"
          :options="statusOptions"
          aria-label="状态"
          @update:model-value="(v) => set('dstatus', v)"
        />
      </KitField>
      <KitField label="开始日期">
        <KitInput
          :model-value="modelValue.from"
          type="date"
          aria-label="开始日期"
          @update:model-value="(v) => set('from', v)"
        />
      </KitField>
      <KitField label="结束日期">
        <KitInput
          :model-value="modelValue.to"
          type="date"
          aria-label="结束日期"
          @update:model-value="(v) => set('to', v)"
        />
      </KitField>
    </template>
  </KitFilterBar>
</template>
