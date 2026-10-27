<script setup lang="ts">
import { computed } from "vue";
import ActionDropdown from "./ActionDropdown.vue";
import {
  merchantOrderActions,
  type MerchantOrderActionID,
} from "../order-actions";
const props = defineProps<{
  row: Record<string, unknown>;
  disabled?: boolean;
}>();
const emit = defineEmits<{ action: [id: MerchantOrderActionID] }>();
const actions = computed(() => merchantOrderActions(props.row));
</script>
<template>
  <span :data-merchant-order-trade="String(row.trade_no)"
    ><ActionDropdown
      :label="'订单操作 ' + String(row.trade_no)"
      :items="actions"
      :disabled="disabled"
      @action="emit('action', $event as MerchantOrderActionID)"
  /></span>
</template>
