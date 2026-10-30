<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ApiClient } from "../api";

interface Child204 {
  sub_trade_no: string;
  api_trade_no: string;
  money: string;
  refundmoney: string | null;
  status: number;
  settle: number;
}
interface Combined204 {
  trade_no: string;
  read_only: true;
  verified_payment_source: false;
  source: "epay_pro" | "epay_release";
  export_id: string;
  exported_at: string;
  parent_settle: number;
  charged_money: string;
  children_money: string;
  amounts_match: true;
  items: Child204[];
}
const props = defineProps<{
  client: ApiClient;
  tradeNo: string;
  merchantID: string;
  contextStamp: number;
}>();
const emit = defineEmits<{ close: [] }>();
const data = ref<Combined204 | null>(null),
  loading = ref(false),
  error = ref("");
const card = ref<HTMLElement | null>(null),
  closeButton = ref<HTMLButtonElement | null>(null);
let alive = true,
  epoch = 0,
  controller: AbortController | undefined;
const invalid = () => new Error("历史子订单资料无法核对，请重新读取");
function text(value: unknown, limit: number, empty = false): value is string {
  return (
    typeof value === "string" &&
    (empty || value.length > 0) &&
    value.length <= limit &&
    !/[\u0000-\u001f\u007f]/.test(value)
  );
}
function cents(value: unknown): bigint {
  if (
    typeof value !== "string" ||
    value.length > 21 ||
    !/^(0|[1-9]\d*)\.\d{2}$/.test(value)
  )
    throw invalid();
  const [whole, fraction] = value.split(".");
  const amount = BigInt(whole!) * 100n + BigInt(fraction!);
  if (amount > 9223372036854775807n) throw invalid();
  return amount;
}
function parse(value: Combined204, trade: string): Combined204 {
  if (
    !value ||
    value.trade_no !== trade ||
    value.read_only !== true ||
    value.verified_payment_source !== false ||
    value.amounts_match !== true ||
    !["epay_pro", "epay_release"].includes(value.source) ||
    !text(value.export_id, 256) ||
    !text(value.exported_at, 64) ||
    !Number.isFinite(Date.parse(value.exported_at)) ||
    !Number.isSafeInteger(value.parent_settle) ||
    value.parent_settle < 0 ||
    !Array.isArray(value.items) ||
    !value.items.length ||
    value.items.length > 50
  )
    throw invalid();
  let total = 0n;
  const seen = new Set<string>();
  for (const child of value.items) {
    if (
      !child ||
      !text(child.sub_trade_no, 128) ||
      seen.has(child.sub_trade_no) ||
      !text(child.api_trade_no, 512, true) ||
      !Number.isInteger(child.status) ||
      child.status < 0 ||
      child.status > 4 ||
      ![0, 1].includes(child.settle)
    )
      throw invalid();
    const money = cents(child.money);
    if (
      money <= 0n ||
      (child.refundmoney !== null && cents(child.refundmoney) > money)
    )
      throw invalid();
    seen.add(child.sub_trade_no);
    total += money;
  }
  if (
    total !== cents(value.charged_money) ||
    total !== cents(value.children_money)
  )
    throw invalid();
  return value;
}
const statuses204 = ["未支付", "已支付", "已退款", "已冻结", "预授权"];
function status(child: Child204) {
  if (
    child.status === 2 &&
    child.refundmoney !== null &&
    cents(child.refundmoney) > 0n &&
    cents(child.refundmoney) < cents(child.money)
  ) {
    return `已退款(${child.refundmoney})`;
  }
  return statuses204[child.status];
}
function clear() {
  epoch++;
  controller?.abort();
  data.value = null;
  loading.value = false;
  error.value = "";
}
async function load() {
  clear();
  const client = props.client,
    area = client.area,
    csrf = client.csrfToken,
    trade = props.tradeNo,
    merchant = props.merchantID,
    stamp = props.contextStamp;
  const run = epoch,
    ac = new AbortController();
  controller = ac;
  const current = () =>
    alive &&
    !ac.signal.aborted &&
    run === epoch &&
    props.client === client &&
    client.area === area &&
    client.csrfToken === csrf &&
    props.tradeNo === trade &&
    props.merchantID === merchant &&
    props.contextStamp === stamp;
  loading.value = true;
  try {
    const value = await client.request<Combined204>(
      `/orders/${encodeURIComponent(trade)}/suborders`,
      { signal: ac.signal },
    );
    if (current()) data.value = parse(value, trade);
  } catch {
    if (current()) error.value = "历史子订单记录暂无法核对，请重新读取。";
  } finally {
    if (current()) loading.value = false;
  }
}
function close() {
  clear();
  emit("close");
}
function keyboard(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    close();
    return;
  }
  if (event.key !== "Tab" || !card.value) return;
  const controls = [
    ...card.value.querySelectorAll<HTMLElement>(
      'button:not([disabled]),[tabindex="0"]',
    ),
  ];
  const first = controls[0],
    last = controls.at(-1);
  if (!first || !last) {
    event.preventDefault();
    card.value.focus();
    return;
  }
  if (
    event.shiftKey &&
    (document.activeElement === first ||
      !card.value.contains(document.activeElement))
  ) {
    event.preventDefault();
    last.focus();
  } else if (
    !event.shiftKey &&
    (document.activeElement === last ||
      !card.value.contains(document.activeElement))
  ) {
    event.preventDefault();
    first.focus();
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.tradeNo,
    props.merchantID,
    props.contextStamp,
  ],
  close,
  { flush: "sync" },
);
onMounted(() => {
  void load();
  void nextTick(() => closeButton.value?.focus());
});
onBeforeUnmount(() => {
  alive = false;
  clear();
});
</script>

<template>
  <Teleport to="body">
    <div class="combined-backdrop204" @click.self="close" @keydown="keyboard">
      <section
        ref="card"
        class="combined-card204"
        role="dialog"
        aria-modal="true"
        aria-label="合单支付 - 子订单列表"
        tabindex="-1"
      >
        <header>
          <h2>合单支付 - 子订单列表</h2>
          <button ref="closeButton" type="button" @click="close">
            关闭子订单列表
          </button>
        </header>
        <p class="combined-intro204">历史合单记录，仅供查询。</p>
        <p v-if="loading" role="status" class="combined-loading204">
          正在读取子订单…
        </p>
        <div v-if="error" role="alert" class="alert error">
          <p>{{ error }}</p>
          <button type="button" :disabled="loading" @click="load">
            重新读取子订单
          </button>
        </div>
        <template v-if="data">
          <div
            class="combined-table204"
            role="region"
            aria-label="子订单列表"
            tabindex="0"
          >
            <table>
              <thead>
                <tr>
                  <th scope="col">系统订单号</th>
                  <th scope="col">接口订单号</th>
                  <th scope="col">订单金额</th>
                  <th scope="col">订单状态</th>
                  <th v-if="data.parent_settle > 0" scope="col">结算状态</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="child in data.items" :key="child.sub_trade_no">
                  <td class="pa-order-no">{{ child.sub_trade_no }}</td>
                  <td class="pa-order-no">{{ child.api_trade_no || "—" }}</td>
                  <td class="combined-money204">¥{{ child.money }}</td>
                  <td>
                    <span
                      class="combined-status204"
                      :class="`combined-status204-${child.status}`"
                      >{{ status(child) }}</span
                    ><span
                      v-if="child.status === 2 && child.refundmoney === null"
                      class="combined-unknown204"
                      >退款金额未知</span
                    >
                  </td>
                  <td v-if="data.parent_settle > 0">
                    {{ child.settle === 1 ? "已结算" : "未结算" }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <footer>
            <p>
              历史实付 ¥{{ data.charged_money }} · 子订单合计 ¥{{
                data.children_money
              }}
            </p>
            <p class="combined-source204">
              {{ data.source === "epay_pro" ? "Pro" : "Release" }} 历史资料 ·
              导出时间 {{ data.exported_at }}
            </p>
          </footer>
        </template>
      </section>
    </div>
  </Teleport>
</template>
<style scoped>
.combined-backdrop204 {
  position: fixed;
  inset: 0;
  z-index: 1600;
  display: grid;
  place-items: center;
  padding: 12px;
  background: var(--scrim);
  box-sizing: border-box;
}
.combined-card204 {
  box-sizing: border-box;
  width: min(680px, 100%);
  max-height: calc(100dvh - 24px);
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
  overflow: hidden;
  padding: 20px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--bg-card);
  color: var(--ink);
  box-shadow: var(--shadow-modal);
}
.combined-card204 header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.combined-card204 h2 {
  margin: 0;
  font-size: 18px;
  line-height: 1.5;
}
.combined-card204 header button {
  flex-shrink: 0;
}
.combined-card204 p {
  margin: 0;
  font-size: 13px;
  line-height: 1.6;
  overflow-wrap: anywhere;
}
.combined-intro204,
.combined-source204,
.combined-unknown204 {
  color: var(--ink);
}
.combined-table204 {
  min-width: 0;
  min-height: 0;
  max-width: 100%;
  max-height: 55dvh;
  overflow: auto;
  overscroll-behavior: contain;
  border: 1px solid var(--line-control);
}
.combined-table204 table {
  border-collapse: collapse;
  min-width: 650px;
  width: 100%;
  font-size: 13px;
}
.combined-table204 th,
.combined-table204 td {
  padding: 10px 12px;
  text-align: left;
  vertical-align: top;
  border-right: 1px solid var(--line);
  border-bottom: 1px solid var(--line);
}
.combined-table204 th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--bg-raised);
  white-space: nowrap;
}
.combined-table204 td:first-child {
  min-width: 156px;
  max-width: 220px;
  overflow-wrap: anywhere;
}
.combined-table204 td:nth-child(2) {
  min-width: 196px;
  max-width: 280px;
  overflow-wrap: anywhere;
}
.combined-table204 tbody tr:nth-child(even) {
  background: var(--bg-raised);
}
.combined-money204 {
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.combined-status204 {
  display: inline-block;
  min-width: 3em;
}
.combined-status204-1 {
  color: var(--success-text);
}
.combined-status204-2 {
  color: var(--danger-text);
}
.combined-status204-4 {
  color: var(--warning-text);
}
.combined-unknown204 {
  display: block;
  font-size: 12px;
  white-space: nowrap;
}
.combined-card204 footer {
  display: grid;
  gap: 6px;
}
.combined-card204 button:focus-visible,
.combined-table204:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 2px;
}
@media (max-width: 480px) {
  .combined-card204 {
    padding: 14px;
  }
  .combined-card204 header {
    align-items: flex-start;
  }
  .combined-card204 h2 {
    font-size: 16px;
  }
  .combined-card204 header button {
    max-width: 105px;
    white-space: normal;
  }
}
</style>
