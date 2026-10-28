<script setup lang="ts">
import {
  type MerchantDirectoryDraft,
  merchantDirectoryStatuses,
  merchantDirectorySorts,
} from "../merchant-directory";
import ShellIcon from "./ShellIcon.vue";
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
  <!-- Extra filters stay collapsed behind 更多筛选, like the original
       console's 展开 and the orders page. -->
  <details class="list-filters merchant-directory-filters">
    <summary>
      <ShellIcon name="filter" />更多筛选<ShellIcon
        name="chevronDown"
        class="list-filters-chevron"
      />
    </summary>
    <div class="list-filter-fields">
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
          @input="change('upid', $event)"
      /></label>
    </div>
    <div class="list-filters-foot">
      <button type="button" :disabled="disabled" @click="emit('clear')">
        清空商户筛选
      </button>
    </div>
  </details>
</template>
<style scoped>
.merchant-directory-filters .list-filters-foot {
  justify-content: flex-end;
}
</style>
