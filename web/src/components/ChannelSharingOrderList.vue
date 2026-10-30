<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import type { ApiClient } from "../api";
import type { Area } from "../menu";
import { minorYuan, datetime as beijingTimestamp } from "../pro-console-fields";
import KitFilterBar from "./KitFilterBar.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitSelect from "./KitSelect.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitPayMethod from "./KitPayMethod.vue";
import KitEmpty from "./KitEmpty.vue";
import KitPagination from "./KitPagination.vue";
import KitModal from "./KitModal.vue";
import ShellIcon from "./ShellIcon.vue";

// Pro 分账记录 (profitsharing/order) over GOEPAY's SERVER-side channel-sharing
// orders endpoints (a4 N5): filters + offset paging with a total, the 统计
// aggregate, and the server CSV export all run on the backend, so the page
// scales and shows 订单金额 / 支付方式 / 分账规则 from the joined payment
// declaration. editmoney (a4 N6) stays disabled — share amounts are immutable.
const props = defineProps<{
  client: ApiClient;
  area: Area;
}>();

type Order = {
  id: string;
  external_no: string;
  out_no: string;
  state: string;
  share_amount: string;
  order_amount: string;
  provider: string;
  method: string;
  rule_id: string;
  channel_id: string;
  created_at: string;
};
type Stats = {
  count: string;
  share_amount: string;
  released_amount: string;
  success_count: string;
  fail_count: string;
};

const route = useRoute();
const merchant = ref("");
const resolved = ref("");
const loading = ref(false);
const busy = ref(false);
const error = ref("");
const notice = ref("");
const loaded = ref(false);
const rows = ref<Order[]>([]);
const total = ref(0);

// Pro filter state.
const field = ref("external_no");
const keyword = ref("");
const status = ref("");
const ruleId = ref("");
const fromDate = ref("");
const toDate = ref("");

const showStats = ref(false);
const stats = ref<Stats>();

const page = ref(1);
const pageSize = ref(20);
const selected = ref<Set<string>>(new Set());

const searchFields = [
  { value: "external_no", label: "系统订单号" },
  { value: "out_no", label: "接口订单号" },
  { value: "share_amount", label: "分账金额" },
];
// Pro 分账状态 → server status tokens.
const statusOptions = [
  { value: "", label: "全部状态" },
  { value: "unsent", label: "待分账" },
  { value: "submitted", label: "已提交" },
  { value: "succeeded", label: "分账成功" },
  { value: "failed", label: "分账失败" },
  { value: "cancelled", label: "已取消" },
];
const stateWords: Record<string, string> = {
  unsent: "待分账",
  claimed: "已提交",
  pending: "处理中",
  succeeded: "分账成功",
  failed: "分账失败",
  partial: "部分成功",
  cancelled: "已取消",
  unknown: "结果未知",
};
const stateTone = (
  s: string,
): "success" | "warning" | "danger" | "info" | "neutral" => {
  if (s === "succeeded") return "success";
  if (s === "failed" || s === "unknown") return "danger";
  if (s === "cancelled") return "neutral";
  if (s === "claimed" || s === "pending" || s === "partial") return "warning";
  return "info";
};

const positive = (s: unknown): s is string =>
  typeof s === "string" && /^[1-9][0-9]{0,18}$/.test(s);
const unsigned = (s: unknown): s is string =>
  typeof s === "string" && /^(0|[1-9][0-9]{0,18})$/.test(s);
const safe = (s: unknown): s is string =>
  typeof s === "string" && s.length <= 256 && !/[\u0000-\u001f\u007f]/.test(s);
const money = (s: string) => (unsigned(s) ? minorYuan(s) : "-");

const ownerQuery = () =>
  props.area === "admin"
    ? `merchant_id=${encodeURIComponent(resolved.value || merchant.value)}`
    : "";

// Build the shared filter query string (used by list, statistics and CSV).
function filterParams(): string {
  const parts: string[] = [];
  const own = ownerQuery();
  if (own) parts.push(own);
  if (keyword.value.trim()) {
    parts.push(`field=${encodeURIComponent(field.value)}`);
    parts.push(`keyword=${encodeURIComponent(keyword.value.trim())}`);
  }
  if (status.value) parts.push(`status=${encodeURIComponent(status.value)}`);
  if (ruleId.value.trim())
    parts.push(`rule_id=${encodeURIComponent(ruleId.value.trim())}`);
  if (fromDate.value) parts.push(`from=${encodeURIComponent(fromDate.value)}`);
  if (toDate.value) parts.push(`to=${encodeURIComponent(toDate.value)}`);
  return parts.join("&");
}

function parseOrder(v: any): Order {
  if (
    !positive(v?.id) ||
    !safe(v.external_no) ||
    !safe(v.out_no) ||
    !safe(v.state) ||
    !unsigned(v.share_amount) ||
    !unsigned(v.order_amount) ||
    !safe(v.provider) ||
    !safe(v.method) ||
    !unsigned(v.rule_id) ||
    !unsigned(v.channel_id) ||
    !safe(v.created_at)
  )
    throw Error("分账记录响应格式错误");
  return {
    id: v.id,
    external_no: v.external_no,
    out_no: v.out_no,
    state: v.state,
    share_amount: v.share_amount,
    order_amount: v.order_amount,
    provider: v.provider,
    method: v.method,
    rule_id: v.rule_id,
    channel_id: v.channel_id,
    created_at: v.created_at,
  };
}

async function load() {
  if (props.area === "admin" && !positive(merchant.value)) {
    error.value = "请输入有效商户ID";
    return;
  }
  if (loading.value) return;
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    resolved.value = props.area === "admin" ? merchant.value : "";
    const extra = [
      filterParams().replace(ownerQuery(), "").replace(/^&|&$/g, ""),
      `limit=${pageSize.value}`,
      `offset=${(page.value - 1) * pageSize.value}`,
    ]
      .filter(Boolean)
      .join("&");
    const own = ownerQuery();
    const qs = [own, extra].filter(Boolean).join("&");
    const result: any = await props.client.request(
      `/channel-sharing/orders${qs ? "?" + qs : ""}`,
    );
    // The server sends total as a JSON number (ChannelSharingOrderListPage),
    // like every other list; amounts and ids stay exact digit strings.
    const totalOK =
      unsigned(result?.total) ||
      (Number.isSafeInteger(result?.total) && result.total >= 0);
    if (
      !Array.isArray(result?.items) ||
      !totalOK ||
      typeof result.limit !== "number"
    )
      throw Error("分账记录列表响应格式错误");
    rows.value = result.items.map(parseOrder);
    total.value = Number(result.total);
    loaded.value = true;
    selected.value = new Set();
    if (showStats.value) await loadStats();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "读取分账记录失败";
  } finally {
    loading.value = false;
  }
}

// 查询 from the filter bar resets to page 1, then loads.
function onQuery() {
  page.value = 1;
  void load();
}
function onReset() {
  field.value = "external_no";
  keyword.value = "";
  status.value = "";
  ruleId.value = "";
  fromDate.value = "";
  toDate.value = "";
  page.value = 1;
  if (props.area === "admin") {
    merchant.value = "";
    loaded.value = false;
    rows.value = [];
    total.value = 0;
  } else {
    void load();
  }
}

async function loadStats() {
  try {
    const qs = filterParams();
    const result: any = await props.client.request(
      `/channel-sharing/orders/statistics${qs ? "?" + qs : ""}`,
    );
    if (
      !unsigned(result?.count) ||
      !unsigned(result.share_amount) ||
      !unsigned(result.released_amount) ||
      !unsigned(result.success_count) ||
      !unsigned(result.fail_count)
    )
      throw Error("统计响应格式错误");
    stats.value = result;
  } catch (e) {
    error.value = e instanceof Error ? e.message : "读取统计失败";
  }
}
async function toggleStats() {
  showStats.value = !showStats.value;
  if (showStats.value && loaded.value) await loadStats();
}
const successRate = computed(() => {
  if (!stats.value || Number(stats.value.count) === 0) return "-";
  return (
    (
      (Number(stats.value.success_count) / Number(stats.value.count)) *
      100
    ).toFixed(1) + "%"
  );
});

// 导出: server CSV (bounded 50000 rows / 8 MiB, Pro headers).
async function exportCsv() {
  if (busy.value || !loaded.value) return;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const qs = [filterParams(), "format=csv"].filter(Boolean).join("&");
    await props.client.downloadCSV(
      `/channel-sharing/orders?${qs}`,
      `channel-sharing-orders-${Date.now()}.csv`,
    );
    notice.value = "筛选结果已交给浏览器下载";
  } catch (e) {
    error.value = e instanceof Error ? e.message : "导出失败";
  } finally {
    busy.value = false;
  }
}

async function queryOrder(o: Order) {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    await props.client.request(
      `/channel-sharing/orders/${encodeURIComponent(o.external_no)}/query`,
      {
        method: "POST",
        body: props.area === "admin" ? { merchant_id: resolved.value } : {},
      },
    );
    notice.value = "已向上游刷新该分账记录";
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "查询失败";
  } finally {
    busy.value = false;
  }
}

const emit = defineEmits<{ "open-flow": [] }>();

// Inline 分账回退 (Pro order row `return`): epay_pro prompts an amount on the row
// and calls return directly; GOEPAY's return is PER RECEIVER and needs the
// succeeded item breakdown, so the row action loads the order detail
// (GET /channel-sharing/orders/{external_no}) and opens a per-receiver modal that
// posts to the existing POST /channel-sharing/returns with explicit confirmation
// and exact minor-unit parsing. No new API, no record mutation — the overlay
// 操作台 stays available for the fuller flow.
type ReturnItem = { receiver_id: string; amount: string; state: string };
type ReturnOrder = {
  external_no: string;
  completed: boolean;
  funds_released: boolean;
  items: ReturnItem[];
};
const returnOpen = ref(false);
const returnLoading = ref(false);
const returnError = ref("");
const returnConfirm = ref(false);
const returnOrder = ref<ReturnOrder>();
const returnReceiver = ref("");
const returnAmountInput = ref("");

const newNo = (prefix: string) =>
  prefix +
  Array.from(crypto.getRandomValues(new Uint8Array(12)), (v) =>
    v.toString(16).padStart(2, "0"),
  ).join("");

// Receivers eligible for return: succeeded items of a completed, funds-released
// share (same precondition the server enforces in channelSharingReturnConsole).
const returnReceiverOptions = computed(() => {
  const o = returnOrder.value;
  if (!o || !o.completed || !o.funds_released) return [];
  return o.items
    .filter((i) => i.state === "succeeded")
    .map((i) => ({
      value: i.receiver_id,
      label: `接收方 ${i.receiver_id} · 已分 ¥${money(i.amount)}`,
    }));
});

function parseOrderDetail(v: any): ReturnOrder {
  if (
    !safe(v?.external_no) ||
    typeof v.completed !== "boolean" ||
    typeof v.funds_released !== "boolean" ||
    !Array.isArray(v.items)
  )
    throw Error("分账记录详情格式错误");
  return {
    external_no: v.external_no,
    completed: v.completed,
    funds_released: v.funds_released,
    items: v.items.map((i: any) => {
      if (!positive(i?.receiver_id) || !unsigned(i.amount) || !safe(i.state))
        throw Error("分账明细格式错误");
      return { receiver_id: i.receiver_id, amount: i.amount, state: i.state };
    }),
  };
}

// Parse a yuan string ("12.34") to integer minor units; throws on bad input.
function returnMinor(raw: string): string {
  if (!/^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$/.test(raw))
    throw Error("请输入正确的回退金额，最多两位小数");
  const [a, b = ""] = raw.split(".");
  const amount = BigInt(a!) * 100n + BigInt(b.padEnd(2, "0"));
  if (amount <= 0n) throw Error("回退金额必须大于 0");
  return amount.toString();
}

async function startReturn(o: Order) {
  if (busy.value) return;
  returnError.value = "";
  returnConfirm.value = false;
  returnReceiver.value = "";
  returnAmountInput.value = "";
  returnOrder.value = undefined;
  returnOpen.value = true;
  returnLoading.value = true;
  try {
    const own = ownerQuery();
    const detail: any = await props.client.request(
      `/channel-sharing/orders/${encodeURIComponent(o.external_no)}${own ? "?" + own : ""}`,
    );
    returnOrder.value = parseOrderDetail(detail);
    const opts = returnReceiverOptions.value;
    if (!opts.length)
      returnError.value =
        "该记录没有可回退的接收方（需执行结束、资金已释放且该接收方已成功分账）";
    else returnReceiver.value = opts[0]!.value;
  } catch (e) {
    returnError.value = e instanceof Error ? e.message : "读取分账记录详情失败";
  } finally {
    returnLoading.value = false;
  }
}

async function submitReturn() {
  const o = returnOrder.value;
  if (!o || busy.value || !returnReceiver.value) return;
  let amount: string;
  try {
    amount = returnMinor(returnAmountInput.value);
  } catch (e) {
    returnError.value = (e as Error).message;
    return;
  }
  if (!returnConfirm.value) {
    returnConfirm.value = true;
    returnError.value = "";
    return;
  }
  busy.value = true;
  returnError.value = "";
  error.value = "";
  notice.value = "";
  try {
    await props.client.request("/channel-sharing/returns", {
      method: "POST",
      body: {
        ...(props.area === "admin" ? { merchant_id: resolved.value } : {}),
        sharing_external_no: o.external_no,
        receiver_id: returnReceiver.value,
        external_no: newNo("return_"),
        amount,
      },
    });
    returnOpen.value = false;
    notice.value = "分账回退请求已提交，请在操作台查询回退结果";
    await load();
  } catch (e) {
    returnConfirm.value = false;
    returnError.value = e instanceof Error ? e.message : "分账回退失败";
  } finally {
    busy.value = false;
  }
}

function toggleAll(e: Event) {
  const on = (e.target as HTMLInputElement).checked;
  selected.value = on ? new Set(rows.value.map((o) => o.id)) : new Set();
}
function toggleOne(id: string) {
  const next = new Set(selected.value);
  next.has(id) ? next.delete(id) : next.add(id);
  selected.value = next;
}
const allOnPageSelected = computed(
  () =>
    rows.value.length > 0 && rows.value.every((o) => selected.value.has(o.id)),
);

watch([page, pageSize], () => {
  if (loaded.value) void load();
});

onMounted(() => {
  // Pro 记录 deep-link: /profitsharing/order?rule_id= prefills the 分账规则ID
  // filter (set from the rule list's 记录 action).
  const rid = route.query.rule_id;
  if (typeof rid === "string" && /^[1-9][0-9]{0,18}$/.test(rid))
    ruleId.value = rid;
  if (props.area === "merchant") void load();
});
</script>

<template>
  <div class="list-page">
    <KitFilterBar :loading="loading" @query="onQuery" @reset="onReset">
      <KitField v-if="area === 'admin'" label="商户ID">
        <KitInput
          v-model="merchant"
          inputmode="numeric"
          placeholder="输入商户ID"
          aria-label="商户ID"
        />
      </KitField>
      <KitField label="搜索字段">
        <KitSelect
          v-model="field"
          :options="searchFields"
          aria-label="搜索字段"
        />
      </KitField>
      <KitField label="搜索内容">
        <KitInput
          v-model="keyword"
          placeholder="输入关键字"
          aria-label="搜索内容"
        />
      </KitField>
      <KitField label="分账状态">
        <KitSelect
          v-model="status"
          :options="statusOptions"
          aria-label="分账状态"
        />
      </KitField>
      <template #extra>
        <KitField label="分账规则ID">
          <KitInput
            v-model="ruleId"
            inputmode="numeric"
            placeholder="输入规则ID"
            aria-label="分账规则ID"
          />
        </KitField>
        <KitField label="开始日期">
          <KitInput v-model="fromDate" type="date" aria-label="开始日期" />
        </KitField>
        <KitField label="结束日期">
          <KitInput v-model="toDate" type="date" aria-label="结束日期" />
        </KitField>
      </template>
    </KitFilterBar>

    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>

    <section class="panel records sharing-orders" aria-label="分账记录">
      <div class="table-tools">
        <button
          type="button"
          class="pa-btn"
          data-action="stats"
          :disabled="!loaded"
          @click="toggleStats"
        >
          统计
        </button>
        <button
          type="button"
          class="pa-btn"
          data-action="batch"
          disabled
          title="分账请求以不可变业务号逐接收方执行，不支持列表批量操作"
        >
          批量操作
        </button>
        <button
          type="button"
          class="pa-btn pa-btn--success"
          data-action="export"
          :disabled="!loaded || busy"
          @click="exportCsv"
        >
          导出
        </button>
        <div class="table-tools-end">
          <button
            type="button"
            class="pa-btn"
            data-action="refresh"
            :disabled="loading"
            title="刷新"
            @click="load"
          >
            <ShellIcon name="refresh" />刷新
          </button>
        </div>
      </div>

      <div v-if="showStats && stats" class="sharing-stats" role="status">
        <div>
          <span class="muted">记录数</span><strong>{{ stats.count }}</strong>
        </div>
        <div>
          <span class="muted">分账金额</span
          ><strong>¥{{ money(stats.share_amount) }}</strong>
        </div>
        <div>
          <span class="muted">已释放</span
          ><strong>¥{{ money(stats.released_amount) }}</strong>
        </div>
        <div>
          <span class="muted">成功</span
          ><strong>{{ stats.success_count }}</strong>
        </div>
        <div>
          <span class="muted">失败</span><strong>{{ stats.fail_count }}</strong>
        </div>
        <div>
          <span class="muted">成功率</span><strong>{{ successRate }}</strong>
        </div>
      </div>

      <div class="table-scroll">
        <table class="pa-table">
          <thead>
            <tr>
              <th class="pa-c-check">
                <input
                  type="checkbox"
                  aria-label="全选"
                  :checked="allOnPageSelected"
                  @change="toggleAll"
                />
              </th>
              <th>系统订单号</th>
              <th>分账规则</th>
              <th>支付方式</th>
              <th>订单金额</th>
              <th>分账金额</th>
              <th>时间</th>
              <th>分账状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="o in rows" :key="o.id">
              <td class="pa-c-check">
                <input
                  type="checkbox"
                  :aria-label="'选择 ' + o.external_no"
                  :checked="selected.has(o.id)"
                  @change="toggleOne(o.id)"
                />
              </td>
              <td class="pa-c-num">
                <span class="mono pa-order-no">{{ o.external_no }}</span>
                <small v-if="o.out_no" class="muted"
                  >接口号 {{ o.out_no }}</small
                >
              </td>
              <td class="pa-c-num">
                {{ o.rule_id !== "0" ? o.rule_id : "-" }}
              </td>
              <td>
                <KitPayMethod v-if="o.provider" :type="o.provider" />
                <span v-else class="muted">-</span>
              </td>
              <td class="pa-c-num">¥{{ money(o.order_amount) }}</td>
              <td class="pa-c-num">¥{{ money(o.share_amount) }}</td>
              <td>{{ beijingTimestamp(o.created_at) }}</td>
              <td>
                <KitStatusTag
                  :text="stateWords[o.state] || o.state"
                  :tone="stateTone(o.state)"
                />
              </td>
              <td>
                <div class="pa-row-actions">
                  <button
                    type="button"
                    class="pa-btn"
                    data-action="query"
                    :disabled="busy"
                    @click="queryOrder(o)"
                  >
                    查询结果
                  </button>
                  <button
                    type="button"
                    class="pa-btn"
                    data-action="return"
                    :disabled="busy"
                    title="对该记录已成功分出的接收方发起分账回退"
                    @click="startReturn(o)"
                  >
                    分账回退
                  </button>
                  <button
                    type="button"
                    class="pa-btn"
                    data-action="open-flow"
                    :disabled="busy"
                    title="打开分账操作台（按原支付声明提交 / 解冻剩余 / 批量回退）"
                    @click="emit('open-flow')"
                  >
                    操作台
                  </button>
                  <button
                    type="button"
                    class="pa-btn"
                    data-action="editmoney"
                    disabled
                    title="分账金额按规则版本快照固定，不可修改（immutable）"
                  >
                    修改金额
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="loaded && !rows.length">
              <td colspan="9"><KitEmpty text="暂无数据" /></td>
            </tr>
            <tr v-if="!loaded">
              <td colspan="9">
                <KitEmpty
                  :text="area === 'admin' ? '请输入商户ID后查询' : '暂无数据'"
                />
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <KitPagination
        v-if="loaded && total"
        v-model:page="page"
        v-model:pageSize="pageSize"
        :total="total"
      />

      <KitModal
        v-if="returnOpen"
        title="分账回退"
        :loading="busy || returnLoading"
        width="520px"
        @close="returnOpen = false"
        @submit="submitReturn"
      >
        <p v-if="returnError" class="error" role="alert">{{ returnError }}</p>
        <p class="muted">
          将已成功分出的资金从分账接收方的账户回退给分账方。此操作不代表买家退款，回退金额不能超过该接收方已分得的金额。
        </p>
        <KitField label="分账接收方" required>
          <KitSelect
            v-model="returnReceiver"
            :options="returnReceiverOptions"
            :disabled="busy || returnLoading || !returnReceiverOptions.length"
            placeholder="选择接收方"
            aria-label="分账接收方"
          />
        </KitField>
        <KitField label="回退金额（元）" required>
          <KitInput
            v-model="returnAmountInput"
            inputmode="decimal"
            placeholder="请输入回退金额"
            :disabled="busy || returnLoading || !returnReceiverOptions.length"
            aria-label="回退金额（元）"
          />
        </KitField>
        <p v-if="returnConfirm" class="success" role="status">
          确认从该接收方回退 {{ returnAmountInput }}
          元至原分账方？再次点击提交以确认。
        </p>
      </KitModal>
    </section>
  </div>
</template>

<style scoped>
.sharing-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  margin: 0 var(--space-5) var(--space-4);
  padding: 14px 18px;
  border: 1px solid var(--line);
  border-radius: var(--radius-md, 8px);
  background: var(--bg-card);
}
.sharing-stats div {
  display: grid;
  gap: 4px;
}
.sharing-stats strong {
  font-size: 18px;
}
.mono {
  font-family: var(--mono, ui-monospace, monospace);
}
td small.muted {
  display: block;
}
.pa-row-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.pa-c-check {
  width: 36px;
  text-align: center;
}
</style>
