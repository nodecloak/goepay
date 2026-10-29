<script setup lang="ts">
import { ref } from "vue";
import {
  type MerchantDirectoryDraft,
  merchantDirectoryStatuses,
  merchantDirectorySorts,
} from "../merchant-directory";
import ShellIcon from "./ShellIcon.vue";
import KitField from "./KitField.vue";
import KitSelect from "./KitSelect.vue";
import KitInput from "./KitInput.vue";
const props = defineProps<{
  modelValue: MerchantDirectoryDraft;
  disabled: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: MerchantDirectoryDraft];
  clear: [];
}>();
const expanded = ref(false);
function set(key: keyof MerchantDirectoryDraft, value: string) {
  emit("update:modelValue", { ...props.modelValue, [key]: value });
}
</script>
<template>
  <!-- Pro's one-row filter: 状态 stays promoted as the primary status field and
       joins the search-group row; rarely-used filters (排序/注册日期/用户组/上级)
       appear only after 展开. 重置(清空) / 查询 / 展开 sit at the row's right. -->
  <div class="merchant-directory-filters">
    <div class="pa-filter-fields merchant-directory-fields">
      <KitField label="状态">
        <KitSelect
          :model-value="modelValue.dstatus"
          :options="merchantDirectoryStatuses"
          aria-label="商户状态筛选"
          :disabled="disabled"
          @update:model-value="(v: string) => set('dstatus', v)"
        />
      </KitField>
      <template v-if="expanded">
        <KitField label="排序">
          <KitSelect
            :model-value="modelValue.order"
            :options="merchantDirectorySorts"
            aria-label="商户排序"
            :disabled="disabled"
            @update:model-value="(v: string) => set('order', v)"
          />
        </KitField>
        <KitField label="注册开始日期">
          <KitInput
            :model-value="modelValue.from"
            type="date"
            :disabled="disabled"
            @update:model-value="(v: string) => set('from', v)"
          />
        </KitField>
        <KitField label="注册结束日期">
          <KitInput
            :model-value="modelValue.to"
            type="date"
            :disabled="disabled"
            @update:model-value="(v: string) => set('to', v)"
          />
        </KitField>
        <KitField label="用户组ID">
          <KitInput
            :model-value="modelValue.gid"
            inputmode="numeric"
            :maxlength="19"
            placeholder="全部用户组，0为默认"
            :disabled="disabled"
            @update:model-value="(v: string) => set('gid', v)"
          />
        </KitField>
        <KitField label="上级商户">
          <KitInput
            :model-value="modelValue.upid"
            inputmode="numeric"
            :maxlength="19"
            placeholder="输入上级商户号"
            :disabled="disabled"
            @update:model-value="(v: string) => set('upid', v)"
          />
        </KitField>
      </template>
    </div>
    <div class="pa-filter-actions merchant-directory-foot">
      <button
        type="button"
        class="pa-btn"
        :disabled="disabled"
        @click="emit('clear')"
      >
        重置
      </button>
      <button
        type="submit"
        class="pa-btn pa-btn--primary"
        :class="{ 'pa-btn--loading': disabled }"
        :disabled="disabled"
      >
        查询
      </button>
      <button
        type="button"
        class="pa-btn pa-btn--text"
        :aria-expanded="expanded"
        @click="expanded = !expanded"
      >
        {{ expanded ? "收起" : "展开"
        }}<ShellIcon name="chevronDown" class="list-filters-chevron" />
      </button>
    </div>
  </div>
</template>
<style scoped>
.merchant-directory-filters {
  display: contents;
}
.merchant-directory-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 180px), 1fr));
  gap: var(--space-3) var(--space-5);
  flex: 1 1 auto;
  min-width: 0;
}
.merchant-directory-foot {
  align-self: flex-end;
  margin-left: auto;
}
.list-filters-chevron {
  margin-left: 2px;
}
</style>
