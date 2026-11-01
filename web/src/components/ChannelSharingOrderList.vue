<script setup lang="ts">
import {
  computed,
  inject,
  onMounted,
  onBeforeUnmount,
  ref,
  watch,
  toRaw,
  nextTick,
  shallowRef,
  triggerRef,
} from "vue";
import { useRoute, useRouter } from "vue-router";
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
import { sharingPageTotal } from "../sharing-overview";
import {
  sharingRequestContext,
  type SharingRequestContext,
} from "../sharing-request-context";
import {
  fundsPrincipal212,
  fundsScope212,
  fundsDefinitiveRefusal212,
} from "../funds-intent212";
import {
  SharingReturnIntentStore,
  type SharingReturnIntent,
} from "../sharing-return-intent";

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
  merchant_id: string;
  subchannel_id: string;
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
const router = useRouter();
let loadVersion = 0;
let routeSignature = "";
let alive = true;
let listController: AbortController | undefined;
const principal = inject(
  fundsPrincipal212,
  computed(() => null),
);
const merchant = ref("");
const resolved = ref("");
const loading = ref(false);
const busy = ref(false);
const error = ref("");
const notice = ref("");
const loaded = ref(false);
const routeValid = ref(true);
const rows = ref<Order[]>([]);
const total = ref(0);

// Pro filter state.
const field = ref("external_no");
const keyword = ref("");
const status = ref("");
const ruleId = ref("");
const fromDate = ref("");
const toDate = ref("");
const channelId = ref("");
const subchannelId = ref("");
const freshFilters = () => ({
  merchant: "",
  field: "external_no",
  keyword: "",
  status: "",
  rule: "",
  from: "",
  to: "",
  channel: "",
  subchannel: "",
});
const applied = ref(freshFilters());
const listEndpoint = computed(() =>
  props.area === "admin"
    ? "/channel-sharing/order-overview"
    : "/channel-sharing/orders",
);
const sessionScope = sharingRequestContext(
  () => ({
    client: toRaw(props.client),
    csrf: props.client.csrfToken,
    area: props.area,
  }),
  () => {
    ++loadVersion;
    ++returnReadVersion;
    listController?.abort();
    rows.value = [];
    selected.value = new Set();
    total.value = 0;
    loaded.value = false;
    loading.value = false;
    busy.value = false;
    loadedContext = undefined;
    returnContext = undefined;
    returnStore = undefined;
    returnIntent.value = null;
    returnOpen.value = false;
    returnLoading.value = false;
    returnOrder.value = undefined;
    batchReview.value = undefined;
    error.value = "会话已变化，请重新查询分账数据";
    notice.value = "";
  },
);
const context = sessionScope.capture;
const current = sessionScope.current;
let loadedContext: SharingRequestContext | undefined;

const showStats = ref(false);
const stats = ref<Stats>();

const page = ref(1);
const pageSize = ref(20);
const selected = ref<Set<string>>(new Set());
type BatchReview = {
  rows: Order[];
  context: SharingRequestContext;
  version: number;
  filters: string;
  route: string;
  done: number;
};
const batchReview = shallowRef<BatchReview>();
const batchTrigger = ref<HTMLButtonElement>();
const refreshTrigger = ref<HTMLButtonElement>();

function restoreBatchFocus() {
  void nextTick(() => {
    if (!alive) return;
    const target = batchTrigger.value?.disabled
      ? refreshTrigger.value
      : batchTrigger.value;
    if (target && !target.disabled) target.focus();
  });
}
function closeBatch() {
  if (busy.value) return;
  batchReview.value = undefined;
  restoreBatchFocus();
}
function openBatch() {
  if (!loadedContext || !current(loadedContext) || loading.value || busy.value)
    return;
  const chosen = rows.value.filter((o) => selected.value.has(o.id));
  if (!chosen.length || chosen.length > 20) return;
  batchReview.value = {
    rows: chosen.map((o) => Object.freeze({ ...o })),
    context: context(),
    version: loadVersion,
    filters: filterParams(),
    route: route.fullPath,
    done: 0,
  };
}
function batchScope(b: BatchReview) {
  return (
    batchReview.value === b &&
    current(b.context) &&
    b.version === loadVersion &&
    b.filters === filterParams() &&
    b.route === route.fullPath
  );
}
async function queryOriginalRecord(o: Order, c: SharingRequestContext) {
  const result: any = await c.client.request(
    `/channel-sharing/orders/${encodeURIComponent(o.external_no)}/query`,
    {
      method: "POST",
      body: c.area === "admin" ? { merchant_id: o.merchant_id } : {},
    },
  );
  if (!current(c)) return false;
  if (
    result?.id !== o.id ||
    result.external_no !== o.external_no ||
    (o.merchant_id && result.merchant_id !== o.merchant_id) ||
    !Object.hasOwn(stateWords, result.state)
  )
    throw Error("查询结果与原分账记录或商户不匹配");
  return true;
}
async function queryBatch() {
  const b = batchReview.value;
  if (!b || busy.value || !batchScope(b)) return;
  busy.value = true;
  error.value = "";
  notice.value = "";
  let problem = "";
  try {
    for (const row of b.rows) {
      if (!batchScope(b)) return;
      if (!(await queryOriginalRecord(row, b.context)) || !batchScope(b))
        return;
      b.done++;
      triggerRef(batchReview);
    }
  } catch (e) {
    if (batchScope(b))
      problem = `已停止后续查询，请核对商户 ${b.rows[b.done]?.merchant_id || "当前商户"} 的原分账号 ${b.rows[b.done]?.external_no}：${e instanceof Error ? e.message : "查询结果尚未核实"}`;
  } finally {
    if (current(b.context) && batchReview.value === b) {
      const sameScope = batchScope(b),
        remaining = new Set(b.rows.slice(b.done).map((o) => o.id));
      batchReview.value = undefined;
      busy.value = false;
      if (sameScope) {
        await load();
        if (
          current(b.context) &&
          b.route === route.fullPath &&
          b.filters === filterParams()
        ) {
          selected.value = new Set(
            rows.value.filter((o) => remaining.has(o.id)).map((o) => o.id),
          );
          notice.value = `已核对 ${b.done} / ${b.rows.length} 条分账记录`;
          error.value = [problem, error.value].filter(Boolean).join("；");
        }
      } else if (b.route !== route.fullPath) applyRecordRoute();
      restoreBatchFocus();
    }
  }
}

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
  { value: "partial", label: "部分成功" },
  { value: "unknown", label: "结果未知" },
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
  typeof s === "string" &&
  /^[1-9][0-9]{0,18}$/.test(s) &&
  BigInt(s) <= 9223372036854775807n;
const unsigned = (s: unknown): s is string =>
  typeof s === "string" && /^(0|[1-9][0-9]{0,18})$/.test(s);
const safe = (s: unknown): s is string =>
  typeof s === "string" && s.length <= 256 && !/[\u0000-\u001f\u007f]/.test(s);
const money = (s: string) => (unsigned(s) ? minorYuan(s) : "-");

// Build the shared filter query string (used by list, statistics and CSV).
function filterParams(): string {
  const f = applied.value,
    q = new URLSearchParams();
  if (props.area === "admin" && f.merchant) q.set("merchant_id", f.merchant);
  if (f.keyword) {
    q.set("field", f.field);
    q.set("keyword", f.keyword);
  }
  for (const [key, value] of [
    ["status", f.status],
    ["rule_id", f.rule],
    ["from", f.from],
    ["to", f.to],
    ["channel_id", f.channel],
    ["subchannel_id", f.subchannel],
  ])
    if (value) q.set(key!, value!);
  return q.toString();
}

function applyDraft() {
  for (const [label, value] of [
    ["商户ID", props.area === "admin" ? merchant.value : ""],
    ["分账规则ID", ruleId.value],
    ["通道ID", channelId.value],
    ["子通道ID", subchannelId.value],
  ]) {
    if (value!.trim() && !positive(value!.trim())) {
      error.value = label + "无效";
      return false;
    }
  }
  if (fromDate.value && toDate.value && fromDate.value > toDate.value) {
    error.value = "开始日期不能晚于结束日期";
    return false;
  }
  applied.value = {
    merchant: props.area === "admin" ? merchant.value.trim() : "",
    field: field.value,
    keyword: keyword.value.trim(),
    status: status.value,
    rule: ruleId.value.trim(),
    from: fromDate.value,
    to: toDate.value,
    channel: channelId.value.trim(),
    subchannel: subchannelId.value.trim(),
  };
  resolved.value = applied.value.merchant;
  return true;
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
  if (
    props.area === "admin" &&
    (!positive(v.merchant_id) ||
      !unsigned(v.subchannel_id) ||
      (applied.value.merchant && v.merchant_id !== applied.value.merchant))
  )
    throw Error("分账记录商户归属或子通道无效");
  return {
    id: v.id,
    merchant_id: positive(v.merchant_id) ? v.merchant_id : "",
    subchannel_id: unsigned(v.subchannel_id) ? v.subchannel_id : "0",
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
  if (!busy.value) batchReview.value = undefined;
  if (!alive || !routeValid.value) return;
  const c = context();
  listController?.abort();
  const ac = new AbortController();
  listController = ac;
  const version = ++loadVersion;
  loading.value = true;
  error.value = "";
  notice.value = "";
  selected.value = new Set();
  stats.value = undefined;
  try {
    const qs = [
      filterParams(),
      `limit=${pageSize.value}`,
      `offset=${(page.value - 1) * pageSize.value}`,
    ]
      .filter(Boolean)
      .join("&");
    const result: any = await props.client.request(
      `${listEndpoint.value}?${qs}`,
      { signal: ac.signal },
    );
    if (version !== loadVersion || !current(c) || ac.signal.aborted) return;
    // The server sends total as a JSON number (ChannelSharingOrderListPage),
    // like every other list; amounts and ids stay exact digit strings.
    const totalOK =
      unsigned(result?.total) ||
      (Number.isSafeInteger(result?.total) && result.total >= 0);
    if (
      !Array.isArray(result?.items) ||
      !totalOK ||
      result.limit !== pageSize.value ||
      result.items.length > pageSize.value
    )
      throw Error("分账记录列表响应格式错误");
    rows.value = result.items.map(parseOrder);
    total.value = sharingPageTotal(result.total);
    const last = Math.max(1, Math.ceil(total.value / pageSize.value));
    if (page.value > last) {
      page.value = last;
      void load();
      return;
    }
    loaded.value = true;
    loadedContext = c;
    selected.value = new Set();
    if (showStats.value) await loadStats();
  } catch (e) {
    if (version === loadVersion && current(c) && !ac.signal.aborted) {
      rows.value = [];
      total.value = 0;
      loaded.value = false;
      error.value = e instanceof Error ? e.message : "读取分账记录失败";
    }
  } finally {
    if (version === loadVersion && current(c)) loading.value = false;
  }
}

// 查询 from the filter bar resets to page 1, then loads.
function onQuery() {
  if (!routeValid.value) {
    error.value = "链接筛选无效，请先重置后查询";
    return;
  }
  if (busy.value || !applyDraft()) return;
  page.value = 1;
  void load();
}
function onReset() {
  if (busy.value) return;
  routeValid.value = true;
  ++loadVersion;
  loading.value = false;
  resolved.value = "";
  selected.value = new Set();
  showStats.value = false;
  stats.value = undefined;
  returnOpen.value = false;
  returnOrder.value = undefined;
  returnConfirm.value = false;
  field.value = "external_no";
  keyword.value = "";
  status.value = "";
  ruleId.value = "";
  fromDate.value = "";
  toDate.value = "";
  channelId.value = "";
  subchannelId.value = "";
  applied.value = freshFilters();
  page.value = 1;
  merchant.value = "";
  void load();
  const clean = { ...route.query };
  delete clean.merchant_id;
  delete clean.rule_id;
  delete clean.channel_id;
  delete clean.subchannel_id;
  routeSignature = JSON.stringify([props.area, null, null, null, null]);
  if (
    route.query.merchant_id !== undefined ||
    route.query.rule_id !== undefined ||
    route.query.channel_id !== undefined ||
    route.query.subchannel_id !== undefined
  )
    void router.replace({ path: route.path, query: clean });
}

async function loadStats() {
  const version = loadVersion;
  const c = context();
  try {
    const qs = filterParams();
    const result: any = await props.client.request(
      `${listEndpoint.value}/statistics${qs ? "?" + qs : ""}`,
    );
    if (version !== loadVersion || !current(c)) return;
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
    if (version === loadVersion && current(c))
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
      `${listEndpoint.value}?${qs}`,
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
  if (!loadedContext || !current(loadedContext)) return;
  if (
    busy.value ||
    loading.value ||
    (props.area === "admin" && !positive(o.merchant_id))
  )
    return;
  const c = context();
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    if (!(await queryOriginalRecord(o, c))) return;
    await load();
    if (current(c)) notice.value = "已向上游刷新该分账记录";
  } catch (e) {
    if (current(c)) error.value = e instanceof Error ? e.message : "查询失败";
  } finally {
    if (current(c)) busy.value = false;
  }
}

const emit = defineEmits<{
  "open-flow": [context: { merchant_id: string; sharing_external_no: string }];
}>();

// Inline 分账回退 (Pro order row `return`): epay_pro prompts an amount on the row
// and calls return directly; GOEPAY's return is PER RECEIVER and needs the
// succeeded item breakdown, so the row action loads the order detail
// (GET /channel-sharing/orders/{external_no}) and opens a per-receiver modal that
// posts to the existing POST /channel-sharing/returns with explicit confirmation
// and exact minor-unit parsing. No new API, no record mutation — the overlay
// 操作台 stays available for the fuller flow.
type ReturnItem = { receiver_id: string; amount: string; state: string };
type ReturnOrder = {
  id: string;
  merchant_id: string;
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
const returnTarget = ref<Order>();
const returnIntent = ref<SharingReturnIntent | null>(null);
let returnStore: SharingReturnIntentStore | undefined;
let returnReadVersion = 0;
let returnContext: SharingRequestContext | undefined;
const returnStateWords: Record<string, string> = {
  unknown: "结果未知",
  unsent: "待发送",
  claimed: "已提交",
  pending: "处理中",
  succeeded: "成功",
  failed: "明确失败",
};
watch([returnReceiver, returnAmountInput], () => {
  returnConfirm.value = false;
});
function closeReturn() {
  if (busy.value) return;
  ++returnReadVersion;
  returnLoading.value = false;
  returnOpen.value = false;
}

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
    !positive(v?.id) ||
    !positive(v?.merchant_id) ||
    !safe(v?.external_no) ||
    typeof v.completed !== "boolean" ||
    typeof v.funds_released !== "boolean" ||
    !Array.isArray(v.items)
  )
    throw Error("分账记录详情格式错误");
  return {
    id: v.id,
    merchant_id: v.merchant_id,
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
  if (!loadedContext || !current(loadedContext)) return;
  if (busy.value || loading.value) return;
  const c = context(),
    token = ++returnReadVersion;
  const live = () =>
    current(c) && token === returnReadVersion && returnOpen.value;
  returnContext = c;
  returnTarget.value = o;
  returnStore = undefined;
  returnIntent.value = null;
  returnError.value = "";
  returnConfirm.value = false;
  returnReceiver.value = "";
  returnAmountInput.value = "";
  returnOrder.value = undefined;
  returnOpen.value = true;
  returnLoading.value = true;
  try {
    const own =
      props.area === "admin"
        ? `merchant_id=${encodeURIComponent(o.merchant_id)}`
        : "";
    const detail: any = await props.client.request(
      `/channel-sharing/orders/${encodeURIComponent(o.external_no)}${own ? "?" + own : ""}`,
    );
    if (!live()) return;
    const parsed = parseOrderDetail(detail);
    if (
      parsed.id !== o.id ||
      parsed.external_no !== o.external_no ||
      (o.merchant_id && parsed.merchant_id !== o.merchant_id)
    )
      throw Error("分账详情与原记录或商户不匹配");
    returnOrder.value = parsed;
    const actor = principal.value;
    if (!actor || actor.area !== props.area || actor.csrf !== c.csrf)
      throw Error("本人会话尚未核实，不能发起回退");
    const scope = await fundsScope212(actor);
    if (!live()) return;
    returnStore = new SharingReturnIntentStore(
      scope,
      parsed.merchant_id,
      parsed.id,
      parsed.external_no,
    );
    returnIntent.value = returnStore.read();
    if (returnIntent.value) {
      if (
        !parsed.items.some(
          (i) => i.receiver_id === returnIntent.value!.receiver_id,
        )
      )
        throw Error("原回退接收方与当前分账记录不匹配");
      returnReceiver.value = returnIntent.value.receiver_id;
      returnAmountInput.value = minorYuan(returnIntent.value.amount);
      return;
    }
    const opts = returnReceiverOptions.value;
    if (!opts.length)
      returnError.value =
        "该记录没有可回退的接收方（需执行结束、资金已释放且该接收方已成功分账）";
    else returnReceiver.value = opts[0]!.value;
  } catch (e) {
    if (live())
      returnError.value =
        e instanceof Error ? e.message : "读取分账记录详情失败";
  } finally {
    if (live()) returnLoading.value = false;
  }
}

async function submitReturn() {
  if (!returnContext || !current(returnContext)) return;
  const o = returnOrder.value;
  if (
    !o ||
    busy.value ||
    returnLoading.value ||
    !returnReceiver.value ||
    returnIntent.value ||
    !returnStore
  )
    return;
  const c = context(),
    store = returnStore;
  const token = returnReadVersion;
  const live = () =>
    current(c) && token === returnReadVersion && returnOpen.value;
  let amount: string;
  try {
    amount = returnMinor(returnAmountInput.value);
    const item = o.items.find(
      (i) => i.receiver_id === returnReceiver.value && i.state === "succeeded",
    );
    if (!item || BigInt(amount) > BigInt(item.amount))
      throw Error("回退金额不能超过该接收方已分账金额");
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
  let dispatched = false;
  let sentIntent: SharingReturnIntent | undefined;
  try {
    await store.exclusive(async () => {
      if (!live()) return;
      const existing = store.read();
      if (existing) {
        returnIntent.value = existing;
        returnReceiver.value = existing.receiver_id;
        returnAmountInput.value = minorYuan(existing.amount);
        return;
      }
      const intent = store.save({
        version: 1,
        merchant_id: o.merchant_id,
        sharing_id: o.id,
        sharing_external_no: o.external_no,
        receiver_id: returnReceiver.value,
        external_no: newNo("return_"),
        amount,
        state: "unknown",
      });
      sentIntent = intent;
      returnIntent.value = intent;
      const result = await props.client.request("/channel-sharing/returns", {
        method: "POST",
        beforeDispatch: () => {
          dispatched = true;
        },
        body: {
          ...(props.area === "admin"
            ? { merchant_id: intent.merchant_id }
            : {}),
          sharing_external_no: o.external_no,
          receiver_id: intent.receiver_id,
          external_no: intent.external_no,
          amount,
        },
      });
      if (!live()) return;
      returnIntent.value = store.save(verifiedReturn(result, intent));
      returnConfirm.value = false;
      notice.value = "分账回退请求已记录，请核对原请求结果";
    });
  } catch (e) {
    if (!live()) return;
    returnConfirm.value = false;
    if (!sentIntent) {
      try {
        returnIntent.value = store.read();
        if (returnIntent.value) {
          returnReceiver.value = returnIntent.value.receiver_id;
          returnAmountInput.value = minorYuan(returnIntent.value.amount);
        }
      } catch {
        /* Keep the storage error; no business request was sent. */
      }
      returnError.value = e instanceof Error ? e.message : "原回退请求尚未核实";
    } else if (!dispatched || fundsDefinitiveRefusal212(e)) {
      try {
        await store.exclusive(() => store.clear(sentIntent));
        returnIntent.value = null;
      } catch (storageError) {
        returnError.value = (storageError as Error).message;
        return;
      }
      returnError.value = e instanceof Error ? e.message : "分账回退未提交";
    } else
      returnError.value = "回退结果尚未核实，请只查询原请求，不会重新发起回退";
  } finally {
    if (current(c)) busy.value = false;
  }
}

function verifiedReturn(
  value: any,
  intent: SharingReturnIntent,
): SharingReturnIntent {
  if (
    !positive(value?.id) ||
    value.sharing_id !== intent.sharing_id ||
    value.merchant_id !== intent.merchant_id ||
    value.receiver_id !== intent.receiver_id ||
    value.external_no !== intent.external_no ||
    value.amount !== intent.amount ||
    !Object.hasOwn(returnStateWords, value.state)
  )
    throw Error("回退响应与原请求、金额或商户不匹配");
  return { ...intent, state: value.state, record_id: value.id };
}
async function queryOriginalReturn() {
  if (!returnContext || !current(returnContext)) return;
  const intent = returnIntent.value,
    store = returnStore;
  if (!intent || !store || busy.value) return;
  const c = context(),
    token = returnReadVersion;
  busy.value = true;
  returnError.value = "";
  try {
    await store.exclusive(async () => {
      const result = await c.client.request(
        `/channel-sharing/returns/${encodeURIComponent(intent.external_no)}/query`,
        {
          method: "POST",
          body:
            props.area === "admin" ? { merchant_id: intent.merchant_id } : {},
        },
      );
      if (!current(c) || token !== returnReadVersion) return;
      returnIntent.value = store.save(verifiedReturn(result, intent));
    });
  } catch (e) {
    if (current(c) && token === returnReadVersion)
      returnError.value =
        (e instanceof Error ? e.message : "查询失败") +
        "；原回退请求保留，不会重新发送";
  } finally {
    if (current(c)) busy.value = false;
  }
}
async function newReturnAfterTerminal() {
  if (!returnContext || !current(returnContext)) return;
  if (
    busy.value ||
    !returnStore ||
    !returnIntent.value ||
    !["succeeded", "failed"].includes(returnIntent.value.state)
  )
    return;
  const store = returnStore,
    intent = returnIntent.value;
  busy.value = true;
  try {
    await store.exclusive(() => store.clear(intent));
    returnIntent.value = null;
    returnConfirm.value = false;
    returnAmountInput.value = "";
    returnError.value = "";
  } catch (e) {
    returnError.value = (e as Error).message;
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

function applyRecordRoute() {
  if (!alive) return;
  if (busy.value) return;
  const signature = JSON.stringify([
    props.area,
    props.area === "admin" ? (route.query.merchant_id ?? null) : null,
    route.query.rule_id ?? null,
    props.area === "admin" ? (route.query.channel_id ?? null) : null,
    props.area === "admin" ? (route.query.subchannel_id ?? null) : null,
  ]);
  if (signature === routeSignature) return;
  routeSignature = signature;
  routeValid.value = true;
  ++loadVersion;
  loading.value = false;
  loaded.value = false;
  rows.value = [];
  total.value = 0;
  selected.value = new Set();
  showStats.value = false;
  stats.value = undefined;
  returnOpen.value = false;
  returnOrder.value = undefined;
  returnConfirm.value = false;
  returnLoading.value = false;
  resolved.value = "";
  merchant.value = "";
  field.value = "external_no";
  keyword.value = "";
  status.value = "";
  fromDate.value = "";
  toDate.value = "";
  channelId.value = "";
  subchannelId.value = "";
  applied.value = freshFilters();
  ruleId.value = "";
  page.value = 1;
  error.value = "";
  notice.value = "";
  const rid = route.query.rule_id;
  if (rid !== undefined && !positive(rid)) {
    routeValid.value = false;
    error.value = "分账规则ID无效";
    return;
  }
  if (positive(rid)) ruleId.value = rid;
  if (props.area === "admin") {
    const mid = route.query.merchant_id;
    if (mid !== undefined && !positive(mid)) {
      routeValid.value = false;
      error.value = "商户ID无效";
      return;
    }
    if (positive(mid)) {
      merchant.value = mid;
    }
    for (const [key, target] of [
      ["channel_id", channelId],
      ["subchannel_id", subchannelId],
    ] as const) {
      const value = route.query[key];
      if (value !== undefined && !positive(value)) {
        routeValid.value = false;
        error.value = "通道或子通道ID无效";
        return;
      }
      if (positive(value)) target.value = value;
    }
  }
  if (applyDraft()) void load();
}
onMounted(applyRecordRoute);
watch(
  () => [
    props.area,
    route.query.rule_id,
    route.query.merchant_id,
    route.query.channel_id,
    route.query.subchannel_id,
  ],
  applyRecordRoute,
);
watch(busy, (value) => {
  if (!value) applyRecordRoute();
});
onBeforeUnmount(() => {
  alive = false;
  sessionScope.dispose();
  listController?.abort();
  ++returnReadVersion;
  ++loadVersion;
});
</script>

<template>
  <div class="list-page">
    <KitFilterBar :loading="loading" @query="onQuery" @reset="onReset">
      <KitField v-if="area === 'admin'" label="商户ID">
        <KitInput
          v-model="merchant"
          inputmode="numeric"
          placeholder="全部商户，可输入商户ID筛选"
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
        <KitField v-if="area === 'admin'" label="通道ID">
          <KitInput
            v-model="channelId"
            inputmode="numeric"
            aria-label="通道ID"
          />
        </KitField>
        <KitField v-if="area === 'admin'" label="子通道ID">
          <KitInput
            v-model="subchannelId"
            inputmode="numeric"
            aria-label="子通道ID"
          />
        </KitField>
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
    <p
      v-if="
        applied.merchant ||
        applied.rule ||
        applied.channel ||
        applied.subchannel
      "
      class="table-note"
      aria-label="当前分账范围"
    >
      <span v-if="applied.merchant">商户 {{ applied.merchant }} </span
      ><span v-if="applied.rule">规则 {{ applied.rule }} </span
      ><span v-if="applied.channel">通道 {{ applied.channel }} </span
      ><span v-if="applied.subchannel">子通道 {{ applied.subchannel }}</span>
    </p>

    <section class="panel records sharing-orders" aria-label="分账记录">
      <div class="table-tools">
        <button
          type="button"
          class="pa-btn"
          data-action="stats"
          :disabled="!loaded || loading || busy"
          @click="toggleStats"
        >
          统计
        </button>
        <button
          type="button"
          class="pa-btn"
          data-action="batch"
          ref="batchTrigger"
          :disabled="
            !loaded || loading || busy || !selected.size || selected.size > 20
          "
          title="逐条核对所选原分账结果，单次最多 20 条"
          @click="openBatch"
        >
          批量操作
        </button>
        <button
          type="button"
          class="pa-btn pa-btn--success"
          data-action="export"
          :disabled="!loaded || busy || loading"
          @click="exportCsv"
        >
          导出
        </button>
        <div class="table-tools-end">
          <button
            type="button"
            class="pa-btn"
            data-action="refresh"
            ref="refreshTrigger"
            :disabled="loading || busy"
            title="刷新"
            @click="load"
          >
            <ShellIcon name="refresh" />刷新
          </button>
        </div>
      </div>
      <p v-if="selected.size" class="muted selection-summary" role="status">
        已选择 {{ selected.size }} 条；单次最多核对 20 条
      </p>
      <p v-if="loading" class="table-note" role="status">正在读取分账记录…</p>

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
                  :disabled="loading || busy"
                  @change="toggleAll"
                />
              </th>
              <th v-if="area === 'admin'">用户ID</th>
              <th>系统订单号</th>
              <th>分账规则</th>
              <th>支付方式(通道ID)</th>
              <th v-if="area === 'admin'">子通道ID</th>
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
                  :disabled="loading || busy"
                  @change="toggleOne(o.id)"
                />
              </td>
              <td v-if="area === 'admin'" class="pa-c-num">
                {{ o.merchant_id }}
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
                <small
                  v-if="positive(o.channel_id)"
                  class="muted"
                  :title="'支付通道ID：' + o.channel_id"
                  >({{ o.channel_id }})</small
                >
              </td>
              <td
                v-if="area === 'admin'"
                class="pa-c-num"
                :title="
                  positive(o.subchannel_id)
                    ? '原订单选择的子通道ID：' + o.subchannel_id
                    : '原订单未记录子通道'
                "
              >
                {{ positive(o.subchannel_id) ? o.subchannel_id : "—" }}
              </td>
              <td class="pa-c-num">¥{{ money(o.order_amount) }}</td>
              <td class="pa-c-num">¥{{ money(o.share_amount) }}</td>
              <td class="sharing-date">{{ beijingTimestamp(o.created_at) }}</td>
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
                    :disabled="busy || loading"
                    @click="queryOrder(o)"
                  >
                    查询结果
                  </button>
                  <button
                    type="button"
                    class="pa-btn"
                    data-action="return"
                    :disabled="busy || loading"
                    title="对该记录已成功分出的接收方发起分账回退"
                    @click="startReturn(o)"
                  >
                    分账回退
                  </button>
                  <button
                    type="button"
                    class="pa-btn"
                    data-action="open-flow"
                    :disabled="busy || loading"
                    title="打开分账操作台（按原支付声明提交 / 解冻剩余 / 批量回退）"
                    @click="
                      emit('open-flow', {
                        merchant_id: o.merchant_id,
                        sharing_external_no: o.external_no,
                      })
                    "
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
              <td :colspan="area === 'admin' ? 11 : 9">
                <KitEmpty text="暂无数据" />
              </td>
            </tr>
            <tr v-if="!loaded">
              <td :colspan="area === 'admin' ? 11 : 9">
                <KitEmpty
                  :text="
                    loading
                      ? '正在读取…'
                      : error
                        ? '读取失败，请刷新重试'
                        : '暂无数据'
                  "
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
        @change="!busy && load()"
      />

      <KitModal
        v-if="batchReview"
        title="批量查询分账结果"
        submit-text="确认查询"
        :loading="busy"
        width="520px"
        @close="closeBatch"
        @submit="queryBatch"
      >
        <p>将逐条核对所选原分账记录；遇到错误会停止，剩余记录需要手动核对。</p>
        <p role="status">
          已核对 {{ batchReview.done }} / {{ batchReview.rows.length }} 条
        </p>
        <ul class="batch-review-rows">
          <li v-for="o in batchReview.rows" :key="o.id">
            商户 {{ o.merchant_id || "当前商户" }} · {{ o.external_no }}
          </li>
        </ul>
      </KitModal>
      <KitModal
        v-if="returnOpen"
        title="分账回退"
        :loading="busy"
        :submit-disabled="
          returnLoading ||
          !!returnIntent ||
          !returnStore ||
          !returnReceiverOptions.length
        "
        :submit-text="returnConfirm ? '确认回退' : '下一步'"
        width="520px"
        @close="closeReturn"
        @submit="submitReturn"
      >
        <p v-if="returnError" class="error" role="alert">{{ returnError }}</p>
        <p v-if="returnLoading" role="status">正在读取原分账记录…</p>
        <div v-if="returnIntent" class="return-intent" role="status">
          <p>
            原回退请求：<span class="pa-order-no">{{
              returnIntent.external_no
            }}</span>
          </p>
          <p>
            商户 {{ returnIntent.merchant_id }} · 接收方
            {{ returnIntent.receiver_id }} · ¥{{ money(returnIntent.amount) }} ·
            {{ returnStateWords[returnIntent.state] }}
          </p>
          <button
            type="button"
            class="pa-btn"
            :disabled="busy"
            @click="queryOriginalReturn"
          >
            查询回退结果
          </button>
          <button
            v-if="['succeeded', 'failed'].includes(returnIntent.state)"
            type="button"
            class="pa-btn"
            :disabled="busy"
            @click="newReturnAfterTerminal"
          >
            核对完毕，填写新的回退
          </button>
        </div>
        <p class="muted">
          将已成功分出的资金从分账接收方的账户回退给分账方。此操作不代表买家退款，回退金额不能超过该接收方已分得的金额。
        </p>
        <KitField label="分账接收方" required>
          <KitSelect
            v-model="returnReceiver"
            :options="returnReceiverOptions"
            :disabled="
              busy ||
              returnLoading ||
              !!returnIntent ||
              !returnReceiverOptions.length
            "
            placeholder="选择接收方"
            aria-label="分账接收方"
          />
        </KitField>
        <KitField label="回退金额（元）" required>
          <KitInput
            v-model="returnAmountInput"
            inputmode="decimal"
            placeholder="请输入回退金额"
            :disabled="
              busy ||
              returnLoading ||
              !!returnIntent ||
              !returnReceiverOptions.length
            "
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
.selection-summary {
  margin: 0 var(--space-5) var(--space-3);
}
.batch-review-rows {
  max-height: 260px;
  overflow: auto;
  overflow-wrap: anywhere;
  padding-left: 20px;
}
.sharing-orders .pa-c-num,
.sharing-date {
  white-space: nowrap;
}
</style>
