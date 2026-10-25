<script setup lang="ts">
import { computed } from "vue";
import OrderActionMenu from "./OrderActionMenu.vue";
import type { OrderActionID } from "../order-actions";
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
  adminOrders?: boolean;
  orderActionsDisabled?: boolean;
  selectable?: boolean;
  selectedTrades?: string[];
}>();
const visibleColumns = computed(() =>
  props.columns.filter((c) => !isCredentialField(c.key)),
);
const emit = defineEmits<{
  "selection-change": [trades: string[]];
  refund: [row: Record<string, unknown>];
  detail: [row: Record<string, unknown>];
  "order-action": [row: Record<string, unknown>, action: OrderActionID];
  blacklist: [row: Record<string, unknown>, kind: "ip" | "buyer"];
}>();
function select(row: Record<string, unknown>, checked: boolean) {
  const id = String(row.trade_no);
  emit(
    "selection-change",
    checked
      ? [...new Set([...(props.selectedTrades || []), id])]
      : (props.selectedTrades || []).filter((v) => v !== id),
  );
}
const allSelected = computed(
  () =>
    props.items.length > 0 &&
    props.items.every((r) =>
      props.selectedTrades?.includes(String(r.trade_no)),
    ),
);
</script>
<template>
  <div class="table-scroll">
    <table>
      <thead>
        <tr>
          <th v-if="selectable" scope="col">
            <input
              type="checkbox"
              aria-label="全选本页订单"
              :checked="allSelected"
              :disabled="orderActionsDisabled || !items.length"
              @change="
                emit(
                  'selection-change',
                  ($event.target as HTMLInputElement).checked
                    ? items.map((r) => String(r.trade_no))
                    : [],
                )
              "
            />
          </th>
          <th v-for="c in visibleColumns" :key="c.key" scope="col">
            {{ consoleColumnLabel(c.key, c.label) }}
          </th>
          <th v-if="orders || $slots.actions" scope="col">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, i) in items" :key="String(row.id ?? i)">
          <td v-if="selectable">
            <input
              type="checkbox"
              :aria-label="'选择订单 ' + row.trade_no"
              :checked="selectedTrades?.includes(String(row.trade_no))"
              :disabled="orderActionsDisabled"
              @change="select(row, ($event.target as HTMLInputElement).checked)"
            />
          </td>
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
              :data-state="
                String(
                  c.key === 'status' && adminOrders
                    ? row.admin_status || row[c.key] || ''
                    : (row[c.key] ?? ''),
                )
              "
              >{{
                consoleValue(
                  c.key,
                  c.key === "status" && adminOrders
                    ? row.admin_status || row[c.key]
                    : row[c.key],
                  orders,
                )
              }}</span
            >
            <small
              v-if="
                orders && adminOrders && c.key === 'status' && row.admin_status
              "
              class="cell muted"
              >支付/账务状态：{{
                consoleValue("status", row.status, true)
              }}</small
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
            <div
              v-if="orders && adminOrders && c.key === 'client_ip'"
              class="order-risk-actions"
            >
              <button
                v-if="row.client_ip"
                class="link-button"
                :aria-label="'拉黑IP ' + row.trade_no"
                :disabled="orderActionsDisabled"
                @click="emit('blacklist', row, 'ip')"
              >
                拉黑IP
              </button>
              <small v-if="row.buyer" class="cell muted">{{
                String(row.buyer)
              }}</small
              ><button
                v-if="row.buyer"
                class="link-button"
                :aria-label="'拉黑账号 ' + row.trade_no"
                :disabled="orderActionsDisabled"
                @click="emit('blacklist', row, 'buyer')"
              >
                拉黑账号
              </button>
            </div>
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
              ><OrderActionMenu
                v-if="orders && adminOrders"
                :row="row"
                :disabled="orderActionsDisabled"
                @action="$emit('order-action', row, $event)"
              /><button
                v-else
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
              visibleColumns.length +
              (orders || $slots.actions ? 1 : 0) +
              (selectable ? 1 : 0)
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
