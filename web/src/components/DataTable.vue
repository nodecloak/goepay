<script setup lang="ts">
import { computed } from "vue";
import {
  consoleValue,
  consoleColumnLabel,
  isCredentialField,
  canRefundListOrder,
} from "../pro-console-fields";
const props = defineProps<{
  items: Record<string, unknown>[];
  columns: { key: string; label: string }[];
  orders?: boolean;
}>();
const visibleColumns = computed(() =>
  props.columns.filter((c) => !isCredentialField(c.key)),
);
defineEmits<{
  refund: [row: Record<string, unknown>];
  detail: [row: Record<string, unknown>];
}>();
</script>
<template>
  <div class="table-scroll">
    <table>
      <thead>
        <tr>
          <th v-for="c in visibleColumns" :key="c.key" scope="col">
            {{ consoleColumnLabel(c.key, c.label) }}
          </th>
          <th v-if="orders || $slots.actions" scope="col">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, i) in items" :key="String(row.id ?? i)">
          <td v-for="c in visibleColumns" :key="c.key">
            <span
              :class="[
                'cell',
                c.key.includes('status') || c.key.endsWith('_state')
                  ? 'badge'
                  : '',
                c.key.includes('money') || c.key.includes('amount')
                  ? 'numeric'
                  : '',
              ]"
              :data-state="String(row[c.key] ?? '')"
              >{{ consoleValue(c.key, row[c.key], orders) }}</span
            >
            <small
              v-if="orders && c.key === 'trade_no' && row.out_trade_no"
              class="cell muted"
              >商户订单号：{{ String(row.out_trade_no) }}</small
            >
            <small
              v-if="orders && c.key === 'trade_no' && row.api_trade_no"
              class="cell muted"
              >接口订单号：{{ String(row.api_trade_no) }}</small
            >
            <template v-if="orders && c.key === 'amount'">
              <small v-if="row.merchant_amount !== undefined" class="cell muted"
                >商户入账：{{
                  consoleValue("merchant_amount", row.merchant_amount)
                }}</small
              >
              <small v-if="row.platform_fee !== undefined" class="cell muted"
                >手续费：{{
                  consoleValue("platform_fee", row.platform_fee)
                }}</small
              >
              <small
                v-if="
                  row.refunded_amount !== undefined &&
                  row.refunded_amount !== '0'
                "
                class="cell muted"
                >已退款：{{
                  consoleValue("refunded_amount", row.refunded_amount)
                }}</small
              >
            </template>
            <small
              v-if="orders && c.key === 'type' && row.channel_id"
              class="cell muted"
              >通道 ID：{{ String(row.channel_id) }}</small
            >
          </td>
          <td v-if="orders || $slots.actions">
            <slot name="actions" :row="row"
              ><button
                v-if="orders"
                class="link-button"
                @click="$emit('detail', row)"
              >
                详情</button
              ><button
                class="link-button"
                :disabled="!canRefundListOrder(row)"
                :title="
                  row.income_status === 'held'
                    ? '请先解冻订单收入'
                    : !canRefundListOrder(row)
                      ? '当前订单不可申请退款'
                      : ''
                "
                @click="canRefundListOrder(row) && $emit('refund', row)"
              >
                申请退款
              </button></slot
            >
          </td>
        </tr>
        <tr v-if="!items.length">
          <td
            :colspan="
              visibleColumns.length + (orders || $slots.actions ? 1 : 0)
            "
            class="empty"
          >
            暂无记录
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
<style scoped>
.badge[data-state="paid"],
.badge[data-state="succeeded"],
.badge[data-state="released"] {
  color: #167245;
  background: #e8f7ef;
}
.badge[data-state="failed"],
.badge[data-state="dead"] {
  color: #b42318;
  background: #fff0ed;
}
.badge[data-state="pending"],
.badge[data-state="unknown"],
.badge[data-state="held"] {
  color: #965c00;
  background: #fff6dd;
}
small.cell {
  display: block;
  font-size: 12px;
  margin-top: 4px;
}
</style>
