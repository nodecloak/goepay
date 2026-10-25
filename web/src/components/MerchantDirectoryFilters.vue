<script setup lang="ts">
import {
  type MerchantDirectoryDraft,
  merchantDirectoryStatuses,
  merchantDirectorySorts,
} from "../merchant-directory";
const props = defineProps<{
  modelValue: MerchantDirectoryDraft;
  disabled: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: MerchantDirectoryDraft];
  clear: [];
}>();
function change(key: keyof MerchantDirectoryDraft, event: Event) {
  emit("update:modelValue", {
    ...props.modelValue,
    [key]: (event.target as HTMLInputElement).value,
  });
}
</script>
<template>
  <div class="merchant-directory-filters">
    <label
      >状态<select
        :value="modelValue.dstatus"
        :disabled="disabled"
        aria-label="商户状态筛选"
        @change="change('dstatus', $event)"
      >
        <option
          v-for="o in merchantDirectoryStatuses"
          :key="o.value"
          :value="o.value"
        >
          {{ o.label }}
        </option>
      </select></label
    ><label
      >排序<select
        :value="modelValue.order"
        :disabled="disabled"
        aria-label="商户排序"
        @change="change('order', $event)"
      >
        <option
          v-for="o in merchantDirectorySorts"
          :key="o.value"
          :value="o.value"
        >
          {{ o.label }}
        </option>
      </select></label
    ><label
      >注册开始日期<input
        :value="modelValue.from"
        type="date"
        :disabled="disabled"
        @input="change('from', $event)" /></label
    ><label
      >注册结束日期<input
        :value="modelValue.to"
        type="date"
        :disabled="disabled"
        @input="change('to', $event)" /></label
    ><label
      >用户组ID<input
        :value="modelValue.gid"
        inputmode="numeric"
        maxlength="19"
        placeholder="全部用户组，0为默认"
        :disabled="disabled"
        @input="change('gid', $event)" /></label
    ><label
      >上级商户<input
        :value="modelValue.upid"
        inputmode="numeric"
        maxlength="19"
        placeholder="输入上级商户号"
        :disabled="disabled"
        @input="change('upid', $event)" /></label
    ><button type="button" :disabled="disabled" @click="emit('clear')">
      清空商户筛选
    </button>
  </div>
</template>
<style scoped>
.merchant-directory-filters {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  width: 100%;
  min-width: 0;
  align-items: end;
}
.merchant-directory-filters label {
  display: grid;
  gap: 6px;
  margin: 0;
  min-width: 0;
  font-size: 12px;
}
.merchant-directory-filters input,
.merchant-directory-filters select {
  width: 100%;
  min-width: 0;
}
.merchant-directory-filters button {
  justify-self: start;
}
@media (max-width: 760px) {
  .merchant-directory-filters {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 420px) {
  .merchant-directory-filters {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
