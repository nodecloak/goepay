<script setup lang="ts">
import { computed } from "vue";
import OrderActionMenu from "./OrderActionMenu.vue";
import MerchantOrderActionMenu from "./MerchantOrderActionMenu.vue";
import KitPayMethod from "./KitPayMethod.vue";
import KitEmpty from "./KitEmpty.vue";
import KitStatusTag from "./KitStatusTag.vue";
import type { MerchantOrderActionID } from "../order-actions";
import type { AdminOrderMenuActionID } from "../order-actions";
import { beijingTimestamp } from "../console-datetime";
import { isOrderNumberKey } from "../order-number";
import {
  consoleValue,
  consoleColumnLabel,
  isCredentialField,
  canRefundListOrder,
  ledgerTerm,
  money,
  groupYuan,
} from "../pro-console-fields";
const props = defineProps<{
  items: Record<string, unknown>[];
  columns: { key: string; label: string }[];
  orders?: boolean;
  refunds?: boolean;
  // Super administrators may close refunds no automatic path can finish.
  refundAttestation?: boolean;
  adminOrders?: boolean;
  merchantOrders?: boolean;
  orderActionsDisabled?: boolean;
  selectable?: boolean;
  selectedTrades?: string[];
  ledger?: boolean;
}>();
const visibleColumns = computed(() =>
  props.columns.filter((c) => !isCredentialField(c.key)),
);
const emit = defineEmits<{
  "selection-change": [trades: string[]];
  refund: [row: Record<string, unknown>];
  detail: [row: Record<string, unknown>];
  "order-action": [
    row: Record<string, unknown>,
    action: AdminOrderMenuActionID,
  ];
  "merchant-order-action": [
    row: Record<string, unknown>,
    action: MerchantOrderActionID,
  ];
  blacklist: [row: Record<string, unknown>, kind: "ip" | "buyer"];
  "attest-refund": [row: Record<string, unknown>];
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
function refundStatus(value: unknown): string {
  return (
    (
      {
        pending: "退款中",
        unknown: "待核实",
        succeeded: "退款成功",
        failed: "退款失败",
      } as Record<string, string>
    )[String(value)] ?? consoleValue("status", value)
  );
}
function refundTone(
  value: unknown,
): "success" | "warning" | "danger" | "info" | "neutral" {
  return (
    (
      {
        pending: "warning",
        unknown: "neutral",
        succeeded: "success",
        failed: "danger",
      } as Record<string, "success" | "warning" | "danger" | "info" | "neutral">
    )[String(value)] ?? "neutral"
  );
}
function associatedOrder(value: unknown): boolean {
  return typeof value === "string" && /^[A-Za-z0-9_-]{1,64}$/.test(value);
}
// Money columns show the kit's grouped figures (¥1,232.00). consoleValue still
// decides what the value is (integer minor units for the order/ledger fields,
// exact yuan strings for report *_money); only the separators are added here.
function isMoneyKey(key: string): boolean {
  return key.includes("money") || key.includes("amount");
}
function cellValue(key: string, value: unknown): string {
  const text = consoleValue(key, value, props.orders);
  return isMoneyKey(key) ? groupYuan(text) : text;
}
// Order rows follow epay_pro's list: two short lines per cell. The order
// numbers cell shows the merchant order number under the system one; both
// numbers, and the 接口订单号, are in its hover text and in 详情.
function orderNumbersTitle(row: Record<string, unknown>): string {
  return [
    row.out_trade_no ? "商户订单号：" + String(row.out_trade_no) : "",
    row.api_trade_no ? "接口订单号：" + String(row.api_trade_no) : "",
  ]
    .filter(Boolean)
    .join("\n");
}
type FlagTone = "success" | "warning" | "danger" | "neutral";
function notifyTone(value: unknown): FlagTone {
  return (
    (
      {
        succeeded: "success",
        pending: "warning",
        running: "warning",
        dead: "danger",
      } as Record<string, FlagTone>
    )[String(value)] ?? "neutral"
  );
}
// epay_pro shows a notice state only once an order is paid; an unpaid order
// has nothing to notify.
function showNotify(row: Record<string, unknown>): boolean {
  return (
    row.status === "paid" || String(row.notification_state ?? "none") !== "none"
  );
}
function refunded(row: Record<string, unknown>): boolean {
  return /^[1-9]\d*$/.test(String(row.refunded_amount ?? ""));
}
</script>
<template>
  <div
    class="table-scroll"
    :class="{ 'trade-table': orders || refunds, 'order-table': orders }"
  >
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
          <th
            v-for="c in visibleColumns"
            :key="c.key"
            scope="col"
            :class="[
              'col-' + c.key,
              {
                'money-cell':
                  c.key.includes('money') || c.key.includes('amount'),
              },
            ]"
          >
            {{
              refunds && c.key === "trade_no"
                ? "关联订单号"
                : consoleColumnLabel(c.key, c.label)
            }}
            <small v-if="refunds && c.key === 'refund_no'" class="cell"
              >商户退款单号</small
            >
            <small v-if="refunds && c.key === 'amount'" class="cell"
              >扣减金额（元）</small
            >
            <small v-if="refunds && c.key === 'created_at'" class="cell"
              >完成时间（北京时间）</small
            >
            <small v-if="orders && c.key === 'trade_no'" class="cell"
              >商户订单号</small
            >
            <small v-if="orders && c.key === 'merchant_id'" class="cell"
              >网站域名</small
            >
            <small v-if="orders && c.key === 'name'" class="cell"
              >订单金额</small
            >
            <small v-if="orders && c.key === 'realmoney'" class="cell"
              >商户分成</small
            >
            <small v-if="orders && c.key === 'type'" class="cell"
              >插件名称</small
            >
            <small v-if="orders && c.key === 'created_at'" class="cell"
              >完成时间</small
            >
            <small v-if="orders && c.key === 'status'" class="cell"
              >通知状态</small
            >
          </th>
          <th
            v-if="orders || $slots.actions"
            scope="col"
            class="operation-cell"
          >
            操作
          </th>
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
          <td
            v-for="c in visibleColumns"
            :key="c.key"
            :class="[
              'col-' + c.key,
              {
                'identifier-cell':
                  (orders || refunds) &&
                  ['trade_no', 'refund_no'].includes(c.key),
                'money-cell':
                  c.key.includes('money') || c.key.includes('amount'),
              },
            ]"
          >
            <template v-if="refunds && c.key === 'refund_no'">
              <span class="cell pa-order-no">{{
                String(row.platform_refund_no ?? row.refund_no ?? "—")
              }}</span>
              <small
                class="cell refund-merchant-no"
                :class="{ 'pa-order-no': row.out_refund_no }"
                >{{ String(row.out_refund_no ?? "未记录") }}</small
              >
            </template>
            <template v-else-if="refunds && c.key === 'amount'">
              <span class="cell numeric">¥{{ money(row.amount) }}</span>
              <small class="cell numeric muted">{{
                row.merchant_amount === undefined ||
                row.merchant_amount === null
                  ? "未记录"
                  : "¥" + money(row.merchant_amount)
              }}</small>
            </template>
            <template v-else-if="refunds && c.key === 'created_at'">
              <span class="cell">{{ beijingTimestamp(row.created_at) }}</span>
              <small class="cell muted">{{
                row.completed_at ? beijingTimestamp(row.completed_at) : "未记录"
              }}</small>
            </template>
            <template v-else-if="refunds && c.key === 'status'">
              <KitStatusTag
                :text="refundStatus(row.status)"
                :tone="refundTone(row.status)"
              />
              <button
                v-if="
                  refundAttestation &&
                  (row.status === 'pending' || row.status === 'unknown')
                "
                type="button"
                class="link-button refund-attest"
                :aria-label="'核实退款结果 ' + row.refund_no"
                @click="emit('attest-refund', row)"
              >
                核实结果
              </button>
            </template>
            <button
              v-else-if="
                refunds && c.key === 'trade_no' && associatedOrder(row.trade_no)
              "
              class="link-button pa-order-no"
              :aria-label="'查看关联订单 ' + row.trade_no"
              @click="emit('detail', row)"
            >
              {{ row.trade_no }}
            </button>
            <button
              v-else-if="orders && c.key === 'trade_no' && row.trade_no"
              type="button"
              class="link-button trade-link pa-order-no"
              :aria-label="'查看订单详情 ' + row.trade_no"
              @click="emit('detail', row)"
            >
              {{ consoleValue(c.key, row[c.key], orders) }}
            </button>
            <KitPayMethod
              v-else-if="c.key === 'type' && row[c.key]"
              :type="String(row[c.key])"
            />
            <span
              v-else-if="orders && c.key === 'name'"
              class="cell order-ellipsis"
              :title="String(row.name ?? '')"
              >{{ cellValue(c.key, row[c.key]) }}</span
            >
            <span
              v-else-if="isMoneyKey(c.key)"
              class="cell"
              :class="orders ? 'order-money' : 'numeric'"
              >¥{{ groupYuan(consoleValue(c.key, row[c.key], orders)) }}</span
            >
            <KitStatusTag
              v-else-if="
                c.key === 'status' &&
                adminOrders &&
                (row.admin_status || row[c.key])
              "
              :status="String(row.admin_status || row[c.key] || '')"
              :orders="orders"
            />
            <KitStatusTag
              v-else-if="
                (c.key.includes('status') || c.key.endsWith('_state')) &&
                !(ledger && c.key === 'ledger_type')
              "
              :status="String(row[c.key] ?? '')"
              :orders="orders"
            />
            <span
              v-else
              class="cell"
              :class="{ 'pa-order-no': isOrderNumberKey(c.key) }"
              :title="
                ledger &&
                (c.key === 'description' ||
                  c.key === 'kind' ||
                  c.key === 'ledger_type')
                  ? String(row[c.key] ?? '')
                  : undefined
              "
              >{{
                ledger &&
                (c.key === "description" ||
                  c.key === "kind" ||
                  c.key === "ledger_type")
                  ? ledgerTerm(c.key, row[c.key])
                  : cellValue(c.key, row[c.key])
              }}</span
            >
            <small
              v-if="
                orders &&
                adminOrders &&
                c.key === 'status' &&
                row.admin_status &&
                row.admin_status !== row.status
              "
              class="cell muted"
              >实际状态：{{ consoleValue("status", row.status, true) }}</small
            >
            <small
              v-if="orders && c.key === 'trade_no' && row.out_trade_no"
              class="cell muted order-ellipsis pa-order-no"
              :title="orderNumbersTitle(row)"
              >{{ String(row.out_trade_no) }}</small
            >
            <div
              v-if="orders && c.key === 'trade_no'"
              class="order-mobile-summary"
              aria-label="订单金额与支付状态"
            >
              <strong>订单金额 ¥{{ money(row.amount) }}</strong>
              <span
                >支付状态：{{ consoleValue("status", row.status, true) }}</span
              >
              <span
                v-if="
                  adminOrders &&
                  row.admin_status &&
                  row.admin_status !== row.status
                "
                class="muted"
                >后台标记：{{
                  consoleValue("status", row.admin_status, true)
                }}</span
              >
              <span
                v-if="row.income_status === 'held'"
                class="order-flag--warning"
                >收入冻结中</span
              >
              <span v-if="refunded(row)" class="order-flag--danger"
                >已退款 ¥{{ money(row.refunded_amount) }}</span
              >
            </div>
            <small
              v-if="orders && c.key === 'merchant_id'"
              class="cell muted order-ellipsis"
              :title="row.domain ? String(row.domain) : undefined"
              >{{ row.domain ? String(row.domain) : "-" }}</small
            >
            <small v-if="orders && c.key === 'name'" class="cell order-amount"
              >¥{{ money(row.amount) }}</small
            >
            <small
              v-if="orders && c.key === 'realmoney'"
              class="cell order-money"
              >{{
                row.merchant_amount === undefined ||
                row.merchant_amount === null
                  ? "-"
                  : "¥" + money(row.merchant_amount)
              }}</small
            >
            <small
              v-if="orders && c.key === 'type'"
              class="cell muted order-ellipsis"
              :title="row.plugin ? String(row.plugin) : undefined"
              >{{ row.plugin ? String(row.plugin) : "-" }}</small
            >
            <small v-if="orders && c.key === 'created_at'" class="cell muted">{{
              row.paid_at ? beijingTimestamp(row.paid_at) : "-"
            }}</small>
            <small
              v-if="orders && c.key === 'status' && showNotify(row)"
              class="cell order-flag"
              :class="'order-flag--' + notifyTone(row.notification_state)"
              >{{
                consoleValue(
                  "notification_state",
                  row.notification_state ?? "none",
                )
              }}</small
            >
            <small
              v-if="orders && c.key === 'status' && refunded(row)"
              class="cell order-flag order-flag--danger"
              >已退款 ¥{{ money(row.refunded_amount) }}</small
            >
            <small
              v-if="
                orders && c.key === 'status' && row.income_status === 'held'
              "
              class="cell order-flag order-flag--warning"
              >收入冻结中</small
            >
          </td>
          <td v-if="orders || $slots.actions" class="operation-cell">
            <slot name="actions" :row="row"
              ><MerchantOrderActionMenu
                v-if="orders && merchantOrders"
                :row="row"
                :disabled="orderActionsDisabled"
                @action="emit('merchant-order-action', row, $event)"
              /><template v-else
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
                </button></template
              ></slot
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
            <KitEmpty text="暂无数据" />
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
<style scoped>
.order-mobile-summary {
  display: none;
}
@media (max-width: 640px) {
  .order-table .col-trade_no {
    min-width: 170px;
    width: 170px;
  }
  .order-mobile-summary {
    display: grid;
    gap: 4px;
    max-width: 180px;
    margin-top: 7px;
    font-size: 12px;
    line-height: 17px;
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .order-mobile-summary strong {
    font-size: 13px;
    color: var(--ink);
    font-variant-numeric: tabular-nums;
  }
}
.trade-table:not(.order-table) .identifier-cell {
  min-width: 200px;
  width: 240px;
}
.trade-table td {
  white-space: nowrap;
}
.trade-table:not(.order-table) td.identifier-cell {
  white-space: normal;
}
.trade-table:not(.order-table) .identifier-cell .cell:not(.badge) {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
/* Orders, as epay_pro's list: every cell is two short lines. A long value
   ends in an ellipsis; the full text is in its hover text and in 详情. */
.order-ellipsis {
  display: block;
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.order-money,
.order-amount {
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.order-amount {
  color: var(--warning-text);
}
small.cell.order-money,
small.cell.order-amount {
  font-size: 13px;
}
.order-table th.col-realmoney {
  min-width: 88px;
}
.order-flag--success {
  color: var(--success-text);
}
.order-flag--warning {
  color: var(--warning-text);
}
.order-flag--danger {
  color: var(--danger-text);
}
.order-flag--neutral {
  color: var(--ink-muted);
}
.trade-link {
  padding: 0;
  min-height: 0;
  font-weight: 600;
  font-size: 13px;
  line-height: 20px;
  font-variant-numeric: tabular-nums;
  text-align: left;
}
.trade-link:hover {
  background: transparent;
  text-decoration: underline;
}
.trade-table .operation-cell {
  position: sticky;
  right: 0;
  min-width: 96px;
  width: 96px;
  white-space: nowrap;
  text-align: center;
  background: var(--bg-card);
  z-index: 1;
  box-shadow: -4px 0 5px -5px var(--line-control);
}
.trade-table td.operation-cell > .link-button {
  min-height: 24px;
  padding: 0 4px;
  vertical-align: middle;
}
.trade-table th.operation-cell {
  background: var(--bg-raised);
  z-index: 2;
}
.trade-table tbody tr:hover .operation-cell {
  background: var(--bg-hover);
}
small.cell {
  display: block;
  font-size: 12px;
  line-height: 18px;
  margin-top: 2px;
}
.refund-merchant-no {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
