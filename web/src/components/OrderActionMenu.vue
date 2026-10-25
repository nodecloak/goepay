<script setup lang="ts">
import { computed } from "vue";
import ActionDropdown from "./ActionDropdown.vue";
import { adminOrderActions, type OrderActionID } from "../order-actions";
const props = defineProps<{
  row: Record<string, unknown>;
  disabled?: boolean;
}>();
const emit = defineEmits<{ action: [id: OrderActionID] }>();
const actions = computed(() => adminOrderActions(props.row));
const name = computed(
  () => `订单操作 ${String(props.row.trade_no ?? props.row.id)}`,
);
</script>
<template>
  <ActionDropdown
    :label="name"
    :items="actions"
    :disabled="disabled"
    @action="emit('action', $event as OrderActionID)"
  />
</template>
