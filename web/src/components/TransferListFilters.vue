<script setup lang="ts">
import { computed } from "vue";
import type { Area } from "../menu";
import KitFilterBar from "./KitFilterBar.vue";
import KitField from "./KitField.vue";
import KitSelect from "./KitSelect.vue";
import KitInput from "./KitInput.vue";
import {
  transferSearchFields,
  transferStatusFields,
  transferTypeFields,
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
  query: [];
  reset: [];
}>();
const error = computed(() => transferFiltersError(props.modelValue));
function set(key: keyof TransferReadFilters, value: string) {
  emit("update:modelValue", { ...props.modelValue, [key]: value });
}
const searchFieldOptions = transferSearchFields.map((f) => ({
  value: f.value,
  label: f.label,
}));
const statusOptions = transferStatusFields.map((f) => ({
  value: f.value,
  label: f.label,
}));
const typeOptions = transferTypeFields.map((f) => ({
  value: f.value,
  label: f.label,
}));
</script>
<template>
  <KitFilterBar
    :loading="loading"
    :blocked="!!error || blocked"
    @query="!error && !blocked && emit('query')"
    @reset="emit('reset')"
  >
    <KitField label="搜索字段">
      <KitSelect
        :model-value="modelValue.search_field"
        :options="searchFieldOptions"
        aria-label="搜索字段"
        @update:model-value="set('search_field', $event)"
      />
    </KitField>
    <KitField label="搜索内容">
      <KitInput
        :model-value="modelValue.q"
        aria-label="搜索内容"
        :placeholder="
          modelValue.search_field === 'money' ? '请输入元金额' : '输入关键字'
        "
        @update:model-value="set('q', $event)"
      />
    </KitField>
    <KitField label="付款方式">
      <KitSelect
        :model-value="modelValue.type"
        :options="typeOptions"
        aria-label="付款方式"
        @update:model-value="set('type', $event)"
      />
    </KitField>
    <KitField v-if="area === 'admin'" label="商户号">
      <KitInput
        :model-value="modelValue.merchant_id"
        aria-label="商户号"
        inputmode="numeric"
        @update:model-value="set('merchant_id', $event)"
      />
    </KitField>
    <template #extra>
      <KitField label="状态">
        <KitSelect
          :model-value="modelValue.status"
          :options="statusOptions"
          aria-label="状态"
          @update:model-value="set('status', $event)"
        />
      </KitField>
      <KitField v-if="area === 'admin'" label="通道">
        <KitInput
          :model-value="modelValue.channel_id"
          aria-label="通道ID"
          inputmode="numeric"
          @update:model-value="set('channel_id', $event)"
        />
      </KitField>
      <KitField label="开始日期">
        <KitInput
          :model-value="modelValue.from"
          type="date"
          aria-label="开始日期"
          @update:model-value="set('from', $event)"
        />
      </KitField>
      <KitField label="结束日期">
        <KitInput
          :model-value="modelValue.to"
          type="date"
          aria-label="结束日期"
          @update:model-value="set('to', $event)"
        />
      </KitField>
    </template>
  </KitFilterBar>
  <p v-if="error" class="alert error transfer-filter-error" role="alert">
    {{ error }}
  </p>
</template>
<style scoped>
.transfer-filter-error {
  margin: 0 0 var(--space-3, 12px);
}
</style>
