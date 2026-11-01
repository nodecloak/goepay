<script setup lang="ts">
import { ref, watch } from "vue";
import type { Area } from "../menu";
import { beijingTimestamp } from "../console-datetime";
import { ledgerTerm, minorYuan } from "../pro-console-fields";
import KitModal from "./KitModal.vue";
import KitEmpty from "./KitEmpty.vue";

const props = defineProps<{ area: Area; items: Record<string, unknown>[] }>();
const selected = ref<Record<string, unknown> | null>(null);
const detailFields = [
  ["id", "流水 ID"],
  ["merchant_id", "商户号"],
  ["ledger_type", "操作类型"],
  ["description", "业务说明"],
  ["amount", "变更金额（元）"],
  ["balance_before", "账户变更前（元）"],
  ["balance_after", "账户变更后（元）"],
  ["kind", "账务账户"],
  ["account_id", "账户 ID"],
  ["created_at", "时间（北京时间）"],
  ["trade_no", "关联订单号"],
  ["event_key", "业务请求号"],
] as const;

function scalar(value: unknown): string {
  return typeof value === "string" && value !== ""
    ? value
    : typeof value === "number" && Number.isSafeInteger(value)
      ? String(value)
      : "—";
}
function amount(value: unknown): string {
  const text = minorYuan(value);
  return text === "—" ? text : "¥" + text;
}
function businessNumber(row: Record<string, unknown>): string {
  return typeof row.trade_no === "string" && row.trade_no.trim() !== ""
    ? row.trade_no
    : scalar(row.event_key);
}
function operation(value: unknown): string {
  return typeof value === "string" ? ledgerTerm("ledger_type", value) : "—";
}
function detailValue(key: string, value: unknown): string {
  if (["amount", "balance_before", "balance_after"].includes(key))
    return minorYuan(value);
  if (key === "created_at") return beijingTimestamp(scalar(value));
  if (
    ["ledger_type", "kind", "description"].includes(key) &&
    typeof value === "string"
  )
    return ledgerTerm(key, value);
  return scalar(value);
}
function openDetails(row: Record<string, unknown>) {
  // This local view never expands arbitrary response keys or fetches new data.
  selected.value = Object.fromEntries(
    detailFields.map(([key]) => [key, row[key]]),
  );
}
watch([() => props.items, () => props.area], () => {
  selected.value = null;
});
</script>

<template>
  <div class="ledger-records">
    <p class="ledger-note muted">
      金额单位：元。变更前后为对应账务账户的余额，不代表商户总资产；无关联订单号时保留完整业务请求号。
    </p>
    <div class="ledger-scroll">
      <table aria-label="资金明细列表">
        <thead>
          <tr>
            <th v-if="area === 'admin'" scope="col">商户号</th>
            <th scope="col">操作类型</th>
            <th scope="col">变更金额</th>
            <th scope="col" title="对应账务账户的变更前余额">账户变更前</th>
            <th scope="col" title="对应账务账户的变更后余额">账户变更后</th>
            <th scope="col" title="北京时间">时间</th>
            <th scope="col">关联订单/业务号</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(row, index) in items"
            :key="scalar(row.id) + ':' + scalar(row.account_id) + ':' + index"
          >
            <td v-if="area === 'admin'">{{ scalar(row.merchant_id) }}</td>
            <td :title="scalar(row.ledger_type)">
              {{ operation(row.ledger_type) }}
            </td>
            <td class="ledger-money">{{ amount(row.amount) }}</td>
            <td class="ledger-money">{{ minorYuan(row.balance_before) }}</td>
            <td class="ledger-money">{{ minorYuan(row.balance_after) }}</td>
            <td>{{ beijingTimestamp(scalar(row.created_at)) }}</td>
            <td class="ledger-business-number">
              <code>{{ businessNumber(row) }}</code>
            </td>
            <td>
              <button
                type="button"
                class="link-button"
                :aria-label="
                  '查看资金明细 ' +
                  (scalar(row.id) === '—'
                    ? '第' + (index + 1) + '条'
                    : scalar(row.id))
                "
                @click="openDetails(row)"
              >
                详情
              </button>
            </td>
          </tr>
          <tr v-if="!items.length">
            <td :colspan="area === 'admin' ? 8 : 7">
              <KitEmpty text="暂无资金记录" />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <KitModal
      v-if="selected"
      title="资金明细详情"
      drawer
      width="680px"
      submit-text="关闭详情"
      hide-cancel
      @close="selected = null"
      @submit="selected = null"
    >
      <p class="muted">
        只读查看当前已加载记录。变更前后属于此账务账户，不代表商户总资产。
      </p>
      <dl class="ledger-details">
        <template v-for="[key, label] in detailFields" :key="key">
          <template v-if="area === 'admin' || key !== 'merchant_id'">
            <dt>{{ label }}</dt>
            <dd>{{ detailValue(key, selected[key]) }}</dd>
          </template>
        </template>
      </dl>
    </KitModal>
  </div>
</template>

<style scoped>
.ledger-note {
  margin: 0;
  padding: 12px 16px;
  font-size: 12px;
}
.ledger-scroll {
  max-width: 100%;
  overflow: auto;
}
table {
  width: 100%;
  min-width: 800px;
}
.ledger-money {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.ledger-business-number {
  min-width: 160px;
  max-width: 320px;
  overflow-wrap: anywhere;
}
.ledger-details {
  display: grid;
  grid-template-columns: minmax(110px, 1fr) minmax(0, 2fr);
  gap: 12px 16px;
}
.ledger-details dt {
  color: var(--ink-muted);
}
.ledger-details dd {
  margin: 0;
  overflow-wrap: anywhere;
  font-variant-numeric: tabular-nums;
}
@media (max-width: 480px) {
  .ledger-details {
    grid-template-columns: 1fr;
    gap: 6px;
  }
  .ledger-details dd {
    margin-bottom: 12px;
  }
}
</style>
