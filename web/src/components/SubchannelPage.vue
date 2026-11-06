<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  watch,
  reactive,
  markRaw,
} from "vue";
import { RouterLink } from "vue-router";
import { ApiClient, ApiError } from "../api";
import { effectiveRiskDescription } from "../core-risk-status";
import { safePaymentUrl } from "../commerce";
import ActionDropdown from "./ActionDropdown.vue";
import ConfigValueEditor from "./ConfigValueEditor.vue";
import KitSelect from "./KitSelect.vue";
import KitSwitch from "./KitSwitch.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitPagination from "./KitPagination.vue";
import KitHelpPopover from "./KitHelpPopover.vue";
import ShellIcon from "./ShellIcon.vue";
import {
  freshSubchannelFilters,
  readSubchannelPage,
  readSubchannelPolicy,
  readSubchannelSchemas,
  readSubchannelRow,
  readSubchannelMutation,
  readSubchannelRoute,
  subchannelSessionID,
  subchannelID,
  subchannelListQuery,
  subchannelMoney,
  subchannelMoneyInput,
  subchannelReason,
  subchannelFeeSource,
  subchannelRollNames,
  subchannelOrdersHref,
  type SubchannelRow,
  type SubchannelSchema,
  type SubchannelPolicy,
} from "../subchannels";
const props = defineProps<{
  client: ApiClient;
  merchantId: string | number;
  ownSession: boolean;
  adminClass?: string;
  initialQuery?: Record<string, unknown>;
}>();
const emit = defineEmits<{ clearRoute: [] }>();
const area = computed(() => props.client.area),
  admin = computed(() => area.value === "admin");
const selfId = computed(() => {
  try {
    return subchannelSessionID(props.merchantId);
  } catch {
    return "";
  }
});
const allowed = computed(
  () =>
    props.ownSession &&
    !!selfId.value &&
    (!admin.value || props.adminClass === "super"),
);
const rows = ref<SubchannelRow[]>([]),
  requiresRefresh = ref(false),
  schemas = ref<SubchannelSchema[]>([]),
  policy = ref<SubchannelPolicy | null>(null),
  total = ref(0),
  loading = ref(false),
  error = ref(""),
  notice = ref(""),
  filters = ref(freshSubchannelFilters()),
  applied = ref(freshSubchannelFilters());
let epoch = 0,
  loadVersion = 0,
  alive = true;
const controllers = new Set<AbortController>();
let surfaceContext: ReturnType<typeof context> | null = null;
function controller() {
  const c = new AbortController();
  controllers.add(c);
  return c;
}
function context() {
  return {
    epoch,
    client: props.client,
    csrf: props.client.csrfToken,
    area: area.value,
    self: selfId.value,
  };
}
function valid(c: ReturnType<typeof context>) {
  return (
    alive &&
    allowed.value &&
    c.epoch === epoch &&
    c.client === props.client &&
    c.csrf === props.client.csrfToken &&
    c.area === area.value &&
    c.self === selfId.value
  );
}
// ApiClient is a class instance: a raw csrfToken write may not schedule Vue's
// watch. Foreground interactions must invalidate the old DOM before any I/O.
function interact(c: ReturnType<typeof context> | null = surfaceContext) {
  if (c && valid(c)) return true;
  if (alive && c?.epoch === epoch) {
    start();
    notice.value = "会话资料已变化，旧表单已关闭，请读取最新资料后重新操作";
  }
  return false;
}
function message(e: unknown) {
  if (e instanceof ApiError) {
    if (e.status === 409) return "资料已变化，请刷新后重新确认";
    if (e.status === 403 || e.status === 401)
      return "当前会话无权操作，请重新登录";
    if (e.status === 422) return "设置未通过校验，请核对字段和当前通道状态";
    return "暂时无法核对通道，请稍后刷新";
  }
  return e instanceof Error ? e.message : "资料读取失败，请重试";
}
type Mode =
  | "create"
  | "params"
  | "settings"
  | "weight"
  | "status"
  | "roll"
  | "test"
  | "link"
  | "delete";
type Dialog = {
  mode: Mode;
  row: SubchannelRow | null;
  owner: string;
  loading: boolean;
  ready: boolean;
  busy: boolean;
  error: string;
  notice: string;
  policy: SubchannelPolicy | null;
  name: string;
  parentId: string;
  enabled: boolean;
  weight: string;
  min: string;
  max: string;
  rollType: number;
  values: Record<string, string>;
  operations: Record<string, string>;
  intent: string;
  testAmount: string;
  executed: boolean;
  uncertain: boolean;
  url: string;
  tradeNo: string;
  state: string;
  opener: HTMLElement | null;
  controller: AbortController;
  context: ReturnType<typeof context>;
};
const modal = ref<Dialog | null>(null),
  dialog = ref<HTMLElement>();
const modalSchema = computed(() =>
  schemas.value.find((s) => s.parentId === modal.value?.parentId),
);
const showWeight = computed(
  () =>
    policy.value?.rollType === 2 || rows.value.some((r) => r.rollType === 2),
);
const rollOwner = computed(() =>
  admin.value ? applied.value.merchantId : selfId.value,
);
const dialogTitle: Record<Mode, string> = {
  create: "新增子通道",
  params: "参数设置",
  settings: "单笔限额设置",
  weight: "权重设置",
  status: "变更子通道状态",
  roll: "轮询设置",
  test: "支付测试",
  link: "收款链接",
  delete: "删除子通道",
};
// Kit option lists + string bridges (KitSelect speaks strings; the underlying
// model keeps its original type so every call and validation is unchanged).
const payTypeFilterOptions = [
  { value: "", label: "全部" },
  { value: "wxpay", label: "微信支付" },
  { value: "alipay", label: "支付宝" },
];
const rollTypeOptions = computed(() =>
  subchannelRollNames.map((label, n) => ({ value: String(n), label })),
);
const parentOptions = computed(() => {
  const d = modal.value;
  if (!d) return [];
  const list: { value: string; label: string }[] = [];
  if (!schemas.value.some((s) => s.parentId === d.parentId))
    list.push({ value: d.parentId, label: "当前父通道不可用 #" + d.parentId });
  for (const s of schemas.value.filter(
    (s) => !d.row || s.payType === d.row.payType,
  ))
    list.push({
      value: s.parentId,
      label:
        (s.payType === "wxpay" ? "微信支付" : "支付宝") + " #" + s.parentId,
    });
  return list;
});
const pagerPage = computed({
  get: () => applied.value.page,
  set: (v: number) => {
    applied.value = { ...applied.value, page: v };
    filters.value.page = v;
  },
});
const pagerSize = computed({
  get: () => applied.value.size,
  set: (v: number) => {
    applied.value = { ...applied.value, size: v, page: 1 };
    filters.value.size = v;
    filters.value.page = 1;
  },
});
const dialogEnabledModel = computed({
  get: () => (modal.value?.enabled ? "true" : "false"),
  set: (v: string) => {
    if (modal.value) modal.value.enabled = v === "true";
  },
});
const dialogRollModel = computed({
  get: () => String(modal.value?.rollType ?? 0),
  set: (v: string) => {
    if (modal.value) modal.value.rollType = Number(v);
  },
});
// Only an existing field bound to this exact parent can remain unchanged.
// A new parent must receive its own account values, including required fields.
function canKeepField(f: SubchannelSchema["fields"][number]) {
  const d = modal.value;
  return (
    d?.mode === "params" &&
    d.parentId === d.row?.parentId &&
    !!d.row?.fields.some((v) => v.name === f.name && v.present)
  );
}
// The table 状态 cell word (tri-state) and its kit tone.
function statusWord(row: SubchannelRow) {
  return !row.parentAvailable ? "父通道不可用" : row.enabled ? "启用" : "停用";
}
function statusTone(row: SubchannelRow): "success" | "danger" | "neutral" {
  if (!row.parentAvailable) return "danger";
  return row.enabled ? "success" : "neutral";
}
async function load() {
  if (surfaceContext && !interact(surfaceContext)) return false;
  const c = context(),
    version = ++loadVersion,
    abort = controller();
  loading.value = true;
  error.value = "";
  rows.value = [];
  total.value = 0;
  try {
    const query = subchannelListQuery(applied.value, area.value, selfId.value);
    const [data, ownPolicy, catalog] = await Promise.all([
      c.client.request<unknown>("/payment-subchannels?" + query, {
        signal: abort.signal,
      }),
      rollOwner.value
        ? c.client.request<unknown>(
            "/payment-subchannels/policy" +
              (admin.value
                ? "?merchant_id=" + encodeURIComponent(rollOwner.value)
                : ""),
            { signal: abort.signal },
          )
        : Promise.resolve(null),
      admin.value
        ? c.client.request<unknown>("/payment-subchannels/schemas", {
            signal: abort.signal,
          })
        : Promise.resolve(null),
    ]);
    if (!valid(c) || abort.signal.aborted || version !== loadVersion)
      return false;
    const page = readSubchannelPage(data, c.area, c.self);
    if (page.page !== applied.value.page || page.size !== applied.value.size)
      throw Error("分页资料不一致，请重新查询");
    rows.value = page.items;
    total.value = page.total;
    policy.value = ownPolicy
      ? readSubchannelPolicy(ownPolicy, rollOwner.value)
      : null;
    schemas.value = catalog
      ? readSubchannelSchemas(catalog).sort((a, b) =>
          BigInt(a.parentId) < BigInt(b.parentId) ? -1 : 1,
        )
      : [];
    if (requiresRefresh.value)
      notice.value = "已读取最新列表，请按商户和名称核对上次提交结果后再操作";
    requiresRefresh.value = false;
    return true;
  } catch (e) {
    if (valid(c) && !abort.signal.aborted && version === loadVersion)
      error.value = message(e);
    return false;
  } finally {
    controllers.delete(abort);
    if (valid(c) && version === loadVersion) loading.value = false;
  }
}
function start() {
  epoch++;
  loadVersion++;
  for (const c of controllers) c.abort();
  controllers.clear();
  if (modal.value) {
    modal.value.values = Object.create(null);
    modal.value.operations = Object.create(null);
  }
  modal.value = null;
  surfaceContext = context();
  requiresRefresh.value = false;
  rows.value = [];
  schemas.value = [];
  policy.value = null;
  notice.value = "";
  error.value = "";
  loading.value = false;
  if (!allowed.value) {
    error.value = "请使用本人商户会话或原始超级管理员会话";
    return;
  }
  try {
    filters.value = readSubchannelRoute(
      props.initialQuery || {},
      area.value,
      selfId.value,
    );
    applied.value = { ...filters.value };
    void load();
  } catch (e) {
    error.value = message(e);
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.client.area,
    props.merchantId,
    props.ownSession,
    props.adminClass,
  ],
  start,
  { immediate: true, flush: "sync" },
);
watch(() => props.initialQuery, start, { deep: true });
function search() {
  if (loading.value || modal.value?.busy) return;
  try {
    subchannelListQuery(filters.value, area.value, selfId.value);
    applied.value = { ...filters.value, page: 1 };
    void load();
  } catch (e) {
    rows.value = [];
    total.value = 0;
    error.value = message(e);
  }
}
function reset() {
  filters.value = freshSubchannelFilters();
  applied.value = { ...filters.value };
  emit("clearRoute");
  void load();
}
function setupValues(d: Dialog) {
  d.values = Object.create(null);
  d.operations = Object.create(null);
  for (const f of schemas.value.find((s) => s.parentId === d.parentId)
    ?.fields || []) {
    d.values[f.name] = "";
    d.operations[f.name] =
      d.mode === "params" &&
      d.row?.parentId === d.parentId &&
      d.row.fields.some((v) => v.name === f.name && v.present)
        ? "keep"
        : d.mode === "params" && !f.required
          ? "clear"
          : "replace";
  }
}
async function open(mode: Mode, row: SubchannelRow | null = null) {
  if (!interact()) return;
  if (!allowed.value || loading.value || modal.value?.busy) return;
  if (requiresRefresh.value && mode !== "link") {
    notice.value = "请先刷新列表核对上次设置结果，再重新操作";
    return;
  }
  const ctx = context(),
    abort = controller();
  const d = reactive<Dialog>({
    mode,
    row,
    owner: row?.merchantId || rollOwner.value,
    loading: true,
    ready: false,
    busy: false,
    error: "",
    notice: "",
    policy: null,
    name: row?.name || "",
    parentId: row?.parentId || schemas.value[0]?.parentId || "",
    enabled: row?.enabled ?? false,
    weight: String(row?.weight ?? 1),
    min: row ? subchannelMoney(row.minAmount) : "",
    max: row && row.maxAmount !== "0" ? subchannelMoney(row.maxAmount) : "",
    rollType: 0,
    values: Object.create(null),
    operations: Object.create(null),
    intent: crypto.randomUUID().replaceAll("-", ""),
    testAmount: "1.00",
    executed: false,
    uncertain: false,
    url: "",
    tradeNo: "",
    state: "",
    opener: document.activeElement as HTMLElement | null,
    controller: markRaw(abort),
    context: markRaw(ctx),
  });
  modal.value = d;
  await nextTick();
  dialog.value?.focus();
  try {
    if (mode === "roll") {
      const owner = subchannelID(d.owner);
      const raw = await ctx.client.request<unknown>(
        "/payment-subchannels/policy" +
          (admin.value ? "?merchant_id=" + encodeURIComponent(owner) : ""),
        { signal: abort.signal },
      );
      if (!valid(ctx) || modal.value?.controller !== abort) return;
      d.policy = readSubchannelPolicy(raw, owner);
      d.rollType = d.policy.rollType;
    } else if (row) {
      const raw = await ctx.client.request<unknown>(
        "/payment-subchannels/" + encodeURIComponent(row.id),
        { signal: abort.signal },
      );
      if (!valid(ctx) || modal.value?.controller !== abort) return;
      const fresh = readSubchannelRow(raw, ctx.area, ctx.self);
      if (fresh.id !== row.id || fresh.merchantId !== row.merchantId)
        throw Error("原子通道资料不一致");
      d.row = fresh;
      d.owner = fresh.merchantId;
      d.name = fresh.name;
      d.parentId = fresh.parentId;
      d.enabled = fresh.enabled;
      d.weight = String(fresh.weight);
      d.min = subchannelMoney(fresh.minAmount);
      d.max = fresh.maxAmount === "0" ? "" : subchannelMoney(fresh.maxAmount);
      if (mode === "weight" && fresh.rollType !== 2)
        throw Error("当前商户未使用权重轮询，请刷新列表");
      if (mode === "test" && !fresh.browserTestAvailable)
        throw Error(
          subchannelReason(fresh.unavailableReason) ||
            "当前产品不支持浏览器支付测试",
        );
    }
    if ((mode === "create" || mode === "params") && admin.value) {
      const raw = await ctx.client.request<unknown>(
        "/payment-subchannels/schemas",
        { signal: abort.signal },
      );
      if (!valid(ctx) || modal.value?.controller !== abort) return;
      schemas.value = readSubchannelSchemas(raw);
      if (!d.parentId) d.parentId = schemas.value[0]?.parentId || "";
      setupValues(d);
    }
    d.ready = true;
  } catch (e) {
    if (
      valid(ctx) &&
      modal.value?.controller === abort &&
      !abort.signal.aborted
    )
      d.error = message(e);
  } finally {
    if (valid(ctx) && modal.value?.controller === abort) {
      d.loading = false;
      await nextTick();
      dialog.value
        ?.querySelector<HTMLElement>(
          "input:not([disabled]),select:not([disabled]),button:not([disabled])",
        )
        ?.focus();
    }
  }
}
async function close() {
  const d = modal.value;
  if (!d || !interact(d.context) || d.busy) return;
  if (d.uncertain && requiresRefresh.value)
    notice.value = "上次设置结果尚未核对，请先刷新列表核对，再重新操作";
  d.controller.abort();
  controllers.delete(d.controller);
  d.values = Object.create(null);
  modal.value = null;
  await nextTick();
  if (valid(d.context)) {
    if (d.opener?.isConnected) d.opener.focus();
    else
      document.querySelector<HTMLElement>("[data-subchannel-refresh]")?.focus();
  }
}
function trap(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    e.stopPropagation();
    void close();
    return;
  }
  if (e.key !== "Tab" || !dialog.value) return;
  const nodes = [
    ...dialog.value.querySelectorAll<HTMLElement>(
      "a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled])",
    ),
  ].filter((n) => n.getClientRects().length > 0);
  if (!nodes.length) {
    e.preventDefault();
    dialog.value.focus();
    return;
  }
  const first = nodes[0]!,
    last = nodes[nodes.length - 1]!;
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first.focus();
  }
}
function confirmAllowed(d: Dialog) {
  return (
    interact(d.context) &&
    modal.value?.controller === d.controller &&
    !d.loading &&
    d.ready &&
    !d.busy &&
    !d.executed &&
    !d.uncertain &&
    !requiresRefresh.value
  );
}
function readTest(d: Dialog, raw: unknown) {
  const r = raw as Record<string, unknown>;
  if (
    !r ||
    typeof r.trade_no !== "string" ||
    !r.trade_no ||
    r.trade_no.length > 128 ||
    /[\x00\r\n/]/.test(r.trade_no) ||
    typeof r.state !== "string" ||
    !["pending", "unknown", "failed", "processing", "not_submitted"].includes(
      r.state,
    )
  )
    throw Error("原支付测试资料无法核对");
  const address = typeof r.url === "string" ? safePaymentUrl(r.url) : null;
  if (!address) throw Error("原支付链接无法核对");
  const u = new URL(address);
  if (
    u.pathname !== "/checkout/" + encodeURIComponent(r.trade_no) ||
    u.searchParams.getAll("expires").length !== 1 ||
    u.searchParams.getAll("token").length !== 1
  )
    throw Error("原支付链接与订单不一致");
  d.tradeNo = r.trade_no;
  d.state = r.state;
  d.url = address;
  d.notice =
    r.state === "pending"
      ? "已生成支付入口；付款结果以原订单的可信确认或查单为准"
      : "结果需要核对，请查看原订单；不会重新发起本次支付";
}
async function testResult() {
  const d = modal.value;
  if (!d?.row || !interact(d.context) || !d.ready || d.busy) return;
  d.busy = true;
  try {
    const raw = await d.context.client.request<unknown>(
      `/payment-subchannels/${encodeURIComponent(d.row.id)}/test?intent=${d.intent}`,
      { signal: d.controller.signal },
    );
    if (valid(d.context) && modal.value?.controller === d.controller)
      readTest(d, raw);
  } catch (e) {
    if (valid(d.context) && modal.value?.controller === d.controller)
      d.error = message(e);
  } finally {
    if (valid(d.context) && modal.value?.controller === d.controller)
      d.busy = false;
  }
}
async function submit() {
  const d = modal.value;
  if (!d || !confirmAllowed(d)) return;
  d.error = "";
  d.notice = "";
  let path = "/payment-subchannels",
    method: "POST" | "PATCH" | "DELETE" = "PATCH",
    body: Record<string, unknown> = {};
  let submitted = false;
  try {
    if (d.mode === "roll") {
      if (!d.policy) throw Error("请先读取原轮询设置");
      path +=
        "/policy" +
        (admin.value ? "?merchant_id=" + encodeURIComponent(d.owner) : "");
      body = {
        roll_type: d.rollType,
        expected_fingerprint: d.policy.fingerprint,
      };
    } else if (d.mode === "create" || d.mode === "params") {
      if (!admin.value || !modalSchema.value)
        throw Error("父通道参数目录不可用");
      if (!d.name.trim() || new TextEncoder().encode(d.name).length > 120)
        throw Error("名称不能为空且不能超过120字节");
      const values: Record<string, unknown> = Object.create(null);
      for (const f of modalSchema.value.fields) {
        const op = d.operations[f.name] || "replace",
          value = d.values[f.name] || "";
        if (d.mode === "create") {
          if (f.required && !value) throw Error("请填写" + f.label);
          values[f.name] = value;
        } else {
          if (op === "replace" && !value) throw Error("请填写新的" + f.label);
          if (op === "clear" && f.required) throw Error(f.label + "不能清除");
          values[f.name] = {
            operation: op,
            ...(op === "replace" ? { value } : {}),
          };
        }
      }
      if (d.mode === "create") {
        if (!/^[1-9]\d?$/.test(d.weight) || Number(d.weight) > 99)
          throw Error("权重请输入1至99的整数");
        const min = subchannelMoneyInput(d.min),
          max = subchannelMoneyInput(d.max);
        if (max !== "0" && BigInt(min) > BigInt(max))
          throw Error("下限不能大于上限");
        method = "POST";
        body = {
          merchant_id: subchannelID(d.owner),
          parent_id: subchannelID(d.parentId),
          name: d.name,
          enabled: d.enabled,
          weight: Number(d.weight),
          min_amount: min,
          max_amount: max,
          values,
        };
      } else {
        if (!d.row) throw Error("原账户未读取");
        path += "/" + encodeURIComponent(d.row.id);
        body = {
          expected_fingerprint: d.row.fingerprint,
          name: d.name,
          parent_id: subchannelID(d.parentId),
          values,
        };
      }
    } else {
      if (!d.row) throw Error("原子通道未读取");
      path += "/" + encodeURIComponent(d.row.id);
      body = { expected_fingerprint: d.row.fingerprint };
      if (d.mode === "settings") {
        const min = subchannelMoneyInput(d.min),
          max = subchannelMoneyInput(d.max);
        if (max !== "0" && BigInt(min) > BigInt(max))
          throw Error("下限不能大于上限");
        body.min_amount = min;
        body.max_amount = max;
      } else if (d.mode === "weight") {
        if (!/^[1-9]\d?$/.test(d.weight) || Number(d.weight) > 99)
          throw Error("权重请输入1至99的整数");
        body.weight = Number(d.weight);
      } else if (d.mode === "status") body.enabled = !d.row.enabled;
      else if (d.mode === "delete") {
        method = "DELETE";
      } else if (d.mode === "test") {
        if (!d.row.browserTestAvailable)
          throw Error("当前未允许浏览器支付测试");
        const amount = subchannelMoneyInput(d.testAmount, false);
        if (
          BigInt(amount) < BigInt(d.row.minAmount) ||
          (d.row.maxAmount !== "0" && BigInt(amount) > BigInt(d.row.maxAmount))
        )
          throw Error("金额须满足当前子通道单笔限额");
        path += "/test";
        method = "POST";
        body = { amount: d.testAmount, intent: d.intent };
      } else throw Error("操作无效");
    }
    d.busy = true;
    if (d.mode === "test") d.executed = true;
    submitted = true;
    const raw = await d.context.client.request<unknown>(path, {
      method,
      body,
      signal: d.controller.signal,
    });
    if (!valid(d.context) || modal.value?.controller !== d.controller) return;
    if (d.mode === "test") {
      readTest(d, raw);
      return;
    }
    if (d.mode === "roll") {
      const saved = readSubchannelPolicy(raw, d.owner);
      if (
        !d.policy ||
        saved.rollType !== d.rollType ||
        saved.revision !== String(BigInt(d.policy.revision) + 1n) ||
        saved.fingerprint === d.policy.fingerprint
      )
        throw Error("保存后的轮询目标或版本无法核对，请刷新列表");
    } else {
      const previous = d.row;
      const saved = readSubchannelMutation(
        raw,
        d.context.area,
        d.context.self,
        {
          before: previous,
          merchantId: d.owner,
          parentId:
            typeof body.parent_id === "string"
              ? body.parent_id
              : previous?.parentId || d.parentId,
          name:
            typeof body.name === "string"
              ? body.name
              : previous?.name || d.name,
          enabled:
            d.mode === "delete"
              ? false
              : typeof body.enabled === "boolean"
                ? body.enabled
                : (previous?.enabled ?? d.enabled),
          weight:
            typeof body.weight === "number"
              ? body.weight
              : (previous?.weight ?? Number(d.weight)),
          minAmount:
            typeof body.min_amount === "string"
              ? body.min_amount
              : previous?.minAmount || "0",
          maxAmount:
            typeof body.max_amount === "string"
              ? body.max_amount
              : previous?.maxAmount || "0",
          accountChanged: d.mode === "params",
          archived: d.mode === "delete",
          ...((d.mode === "create" || d.mode === "params") && modalSchema.value
            ? { schema: modalSchema.value }
            : {}),
        },
      );
      if (d.mode === "create" && rows.value.some((r) => r.id === saved.id))
        throw Error("新增结果返回已有编号，请刷新列表核对");
    }
    notice.value =
      d.mode === "delete" ? "子通道已删除，已有订单仍可核对" : "设置已保存";
    d.busy = false;
    await close();
    await load();
  } catch (e) {
    if (valid(d.context) && modal.value?.controller === d.controller) {
      d.error = message(e);
      if (d.mode === "test" && d.executed)
        d.notice = "请核对本次测试的原订单，不要重新生成付款请求";
      else if (submitted) {
        d.uncertain = true;
        d.ready = false;
        requiresRefresh.value = true;
        d.notice = "本次设置提交结果未确认，请先读取最新列表核对，不要重复提交";
      }
    }
  } finally {
    d.values = Object.create(null);
    if (valid(d.context) && modal.value?.controller === d.controller)
      d.busy = false;
  }
}
async function reconcileChange() {
  const d = modal.value;
  if (!d || !interact(d.context) || !d.uncertain || d.busy) return;
  d.busy = true;
  let checked = false;
  try {
    filters.value = {
      ...freshSubchannelFilters(),
      id: d.row?.id || "",
      merchantId: admin.value ? d.owner : "",
      name: d.row ? "" : d.name,
    };
    applied.value = { ...filters.value };
    checked = (await load()) === true;
    if (
      !checked &&
      valid(d.context) &&
      modal.value?.controller === d.controller
    )
      d.error = "最新列表尚未读取成功，请继续核对，不能重复提交";
  } finally {
    if (valid(d.context) && modal.value?.controller === d.controller)
      d.busy = false;
  }
  if (checked && valid(d.context) && modal.value?.controller === d.controller)
    await close();
}
async function link() {
  const d = modal.value;
  if (!d?.row || !interact(d.context) || !d.ready || d.busy) return;
  d.busy = true;
  d.error = "";
  try {
    const raw = await d.context.client.request<Record<string, unknown>>(
      `/payment-subchannels/${encodeURIComponent(d.row.id)}/collection-link`,
      { signal: d.controller.signal },
    );
    if (!valid(d.context) || modal.value?.controller !== d.controller) return;
    const address =
      typeof raw.url === "string" ? safePaymentUrl(raw.url) : null;
    if (!address) throw Error("收款链接无法核对");
    const u = new URL(address);
    if (
      u.searchParams.get("subchannel_id") !== d.row.id ||
      u.searchParams.get("type") !== d.row.payType ||
      !/^\/collect\/[1-9]\d*$/.test(u.pathname)
    )
      throw Error("收款链接与当前子通道不一致");
    d.url = address;
    d.notice =
      typeof raw.expires_at === "string"
        ? "链接有效期至 " + raw.expires_at
        : "链接按原收款码有效期使用";
  } catch (e) {
    if (valid(d.context) && modal.value?.controller === d.controller)
      d.error = message(e);
  } finally {
    if (valid(d.context) && modal.value?.controller === d.controller)
      d.busy = false;
  }
}
async function copy() {
  const d = modal.value;
  if (!d?.url || !interact(d.context)) return;
  try {
    await navigator.clipboard.writeText(d.url);
    if (valid(d.context) && modal.value?.controller === d.controller)
      d.notice = "收款链接已复制";
  } catch {
    if (valid(d.context) && modal.value?.controller === d.controller)
      d.error = "请手动复制完整收款链接";
  }
}
function actions(r: SubchannelRow) {
  return [
    {
      id: "orders",
      label: "支付订单",
      href: subchannelOrdersHref(area.value, r.id),
    },
    ...(r.browserTestAvailable ? [{ id: "test", label: "支付测试" }] : []),
    { id: "link", label: "收款链接" },
    { id: "settings", label: "单笔限额设置" },
    ...(r.rollType === 2 ? [{ id: "weight", label: "权重设置" }] : []),
    ...(admin.value
      ? [
          { id: "params", label: "参数设置" },
          { id: "roll", label: "商户轮询设置" },
          { id: "delete", label: "删除子通道", separatorBefore: true },
        ]
      : []),
  ];
}
function action(id: string, row: SubchannelRow) {
  if (id === "orders") return;
  void open(id as Mode, row);
}
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  for (const c of controllers) c.abort();
  controllers.clear();
  if (modal.value) modal.value.values = Object.create(null);
  modal.value = null;
});
</script>
<template>
  <div class="list-page subchannel-page">
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <form
      class="subchannel-toolbar list-filter-card panel"
      @submit.prevent="search"
    >
      <div class="subchannel-filter-fields">
        <label
          >{{ admin ? "ID" : "子通道编号"
          }}<input
            v-model="filters.id"
            inputmode="numeric"
            placeholder="精确编号" /></label
        ><label
          >{{ admin ? "子通道备注" : "名称"
          }}<input
            v-model="filters.name"
            :placeholder="admin ? '子通道备注' : '子通道名称'" /></label
        ><label
          >支付方式<KitSelect
            v-model="filters.payType"
            :options="payTypeFilterOptions"
            aria-label="支付方式筛选" /></label
        ><label v-if="admin"
          >用户ID<input
            v-model="filters.merchantId"
            inputmode="numeric" /></label
        ><label v-if="admin"
          >通道ID<input v-model="filters.parentId" inputmode="numeric"
        /></label>
      </div>
      <div class="subchannel-filter-actions">
        <button type="button" @click="reset" :disabled="loading">
          {{ admin ? "重置" : "清空筛选" }}</button
        ><button type="submit" class="primary" :disabled="!allowed || loading">
          查询
        </button>
      </div>
    </form>
    <section class="panel records subchannel-card" aria-label="子通道列表">
      <div class="table-tools">
        <button
          v-if="admin"
          class="pa-btn pa-btn--primary"
          type="button"
          :disabled="!allowed || loading || !schemas.length || requiresRefresh"
          @click="open('create')"
        >
          新增子通道</button
        ><button
          class="pa-btn"
          type="button"
          :disabled="!allowed || loading || !rollOwner || requiresRefresh"
          @click="open('roll')"
        >
          轮询设置</button
        ><span class="table-summary">{{
          policy
            ? "当前轮询方式：" + subchannelRollNames[policy.rollType]
            : admin
              ? "可按商户筛选或从操作菜单设置轮询方式"
              : ""
        }}</span>
        <div class="table-tools-end">
          <button
            data-subchannel-refresh
            type="button"
            class="pa-btn"
            :disabled="!allowed || loading"
            @click="load"
          >
            <ShellIcon name="refresh" />刷新
          </button>
          <KitHelpPopover label="说明">
            <p>
              收款账户由管理员配置。轮询方式适用于本人子通道；单笔订单金额须同时满足父通道与子通道限额。
            </p>
            <p>
              金额按北京时间统计已记录子通道绑定的可信支付确认（含后续退款）；成功率为可信确认数
              / 已发起次数，暂无发起记录显示“—”。历史未记录绑定的订单不推算。
            </p>
          </KitHelpPopover>
        </div>
      </div>
      <div class="subchannel-table-wrap">
        <table class="subchannel-table">
          <thead>
            <tr>
              <th>{{ admin ? "ID" : "编号" }}</th>
              <th>支付方式</th>
              <th>{{ admin ? "子通道备注" : "名称" }}</th>
              <th v-if="admin">用户ID</th>
              <th v-if="admin">支付通道</th>
              <th>费率</th>
              <th>{{ admin ? "今日收款" : "今日" }}</th>
              <th>{{ admin ? "昨日收款" : "昨日" }}</th>
              <th>{{ admin ? "今日成功率" : "成功率" }}</th>
              <th>状态</th>
              <th v-if="showWeight">权重</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rows" :key="row.id" :data-subchannel-id="row.id">
              <td class="subchannel-id">{{ row.id }}</td>
              <td>
                <span class="subchannel-paytype" :class="row.payType">{{
                  row.payType === "wxpay" ? "微信支付" : "支付宝"
                }}</span>
              </td>
              <td class="subchannel-name">
                <strong>{{ row.name }}</strong
                ><small
                  >代收<span v-if="!admin">
                    · 父通道 #{{ row.parentId }}</span
                  ></small
                ><small
                  v-if="
                    row.unavailableReason &&
                    row.unavailableReason !== 'disabled'
                  "
                  class="unavailable"
                  >{{ subchannelReason(row.unavailableReason) }}</small
                >
              </td>
              <td v-if="admin" class="subchannel-id">{{ row.merchantId }}</td>
              <td v-if="admin" class="subchannel-id">{{ row.parentId }}</td>
              <td>
                <span>{{
                  row.feeRateBps === null
                    ? "未核对"
                    : subchannelMoney(String(row.feeRateBps)) + "%"
                }}</span
                ><small>{{ subchannelFeeSource(row.feeSource) }}</small>
              </td>
              <td>
                {{
                  row.statistics
                    ? subchannelMoney(row.statistics.today)
                    : "未记录"
                }}
              </td>
              <td>
                {{
                  row.statistics
                    ? subchannelMoney(row.statistics.yesterday)
                    : "未记录"
                }}
              </td>
              <td>
                {{
                  row.statistics?.successBps === null || !row.statistics
                    ? "—"
                    : subchannelMoney(String(row.statistics.successBps)) + "%"
                }}
              </td>
              <td>
                <div class="subchannel-state-cell">
                  <KitStatusTag
                    :text="statusWord(row)"
                    :tone="statusTone(row)"
                  />
                  <KitSwitch
                    :model-value="row.enabled && row.parentAvailable"
                    :label="`变更子通道 ${row.id} 状态`"
                    :disabled="
                      !allowed ||
                      loading ||
                      !!modal ||
                      requiresRefresh ||
                      (!row.parentAvailable && !row.enabled)
                    "
                    @change="open('status', row)"
                  />
                </div>
                <small class="cell">{{
                  effectiveRiskDescription(row.coreRisk, row.enabled)
                }}</small>
                <small v-if="row.parentCoreRisk?.blocked" class="cell"
                  >父通道风控：{{ row.parentCoreRisk.reason }}</small
                >
              </td>
              <td v-if="showWeight">
                <button
                  v-if="row.rollType === 2"
                  type="button"
                  :disabled="!allowed || loading || !!modal || requiresRefresh"
                  :aria-label="`设置子通道 ${row.id} 权重`"
                  @click="open('weight', row)"
                >
                  {{ row.weight }}</button
                ><span v-else>—</span>
              </td>
              <td>
                <ActionDropdown
                  :label="`子通道 ${row.id} 操作`"
                  :disabled="!allowed || loading || !!modal || requiresRefresh"
                  :items="actions(row)"
                  @action="action($event, row)"
                />
              </td>
            </tr>
            <tr v-if="!rows.length">
              <td
                :colspan="(admin ? 11 : 9) + (showWeight ? 1 : 0)"
                class="subchannel-empty"
              >
                {{
                  loading
                    ? "正在读取子通道…"
                    : error
                      ? "请核对筛选或刷新"
                      : "没有符合条件的子通道"
                }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <KitPagination
        v-model:page="pagerPage"
        v-model:page-size="pagerSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        @change="load"
      />
    </section>
    <Teleport to="body"
      ><div
        v-if="modal"
        class="modal-backdrop drawer-backdrop"
        @keydown="trap"
        @click.self="close"
      >
        <section
          ref="dialog"
          class="modal modal-drawer panel subchannel-dialog"
          role="dialog"
          aria-modal="true"
          tabindex="-1"
          :aria-label="dialogTitle[modal.mode]"
        >
          <h2>{{ dialogTitle[modal.mode] }}</h2>
          <p v-if="modal.row" class="subchannel-id">
            子通道 #{{ modal.row.id }} · 商户 #{{ modal.row.merchantId }}
          </p>
          <p v-if="modal.loading" role="status">正在核对原资料…</p>
          <button v-if="modal.loading" type="button" @click="close">
            取消
          </button>
          <p v-if="modal.error" class="alert error" role="alert">
            {{ modal.error }}
          </p>
          <p v-if="modal.notice" class="alert success" role="status">
            {{ modal.notice }}
          </p>
          <form
            v-if="!modal.loading"
            class="subchannel-form"
            @submit.prevent="submit"
          >
            <template v-if="modal.mode === 'create' || modal.mode === 'params'"
              ><p v-if="modal.mode === 'params'" class="muted">
                保存参数会建立新账户版本；已有订单继续按原账户处理。
              </p>
              <label
                >所属商户编号<input
                  v-model="modal.owner"
                  inputmode="numeric"
                  :readonly="modal.mode === 'params'"
                  :disabled="modal.busy || modal.uncertain"
                  required /></label
              ><label
                >名称<input
                  v-model="modal.name"
                  :disabled="modal.busy || modal.uncertain"
                  maxlength="120"
                  required /></label
              ><label
                >父通道<KitSelect
                  v-model="modal.parentId"
                  :options="parentOptions"
                  :disabled="modal.busy || modal.uncertain"
                  aria-label="父通道"
                  @change="setupValues(modal)" /></label
              ><template v-if="modal.mode === 'create'"
                ><label
                  >初始状态<KitSelect
                    v-model="dialogEnabledModel"
                    :options="[
                      { value: 'false', label: '关闭' },
                      { value: 'true', label: '启用' },
                    ]"
                    :disabled="modal.busy || modal.uncertain"
                    aria-label="初始状态" /></label
                ><label
                  >权重<input
                    v-model="modal.weight"
                    inputmode="numeric"
                    :disabled="modal.busy || modal.uncertain" /></label
              ></template>
              <div
                v-for="f in modalSchema?.fields || []"
                :key="modal.parentId + '.' + f.name"
                class="subchannel-param"
              >
                <span class="subchannel-param-label"
                  >{{ f.label }}{{ f.required ? " *" : "" }}</span
                >
                <ConfigValueEditor
                  :model-value="modal.values[f.name] || ''"
                  @update:model-value="modal.values[f.name] = $event"
                  v-model:operation="modal.operations[f.name]"
                  :label="f.label"
                  :configured="canKeepField(f)"
                  :keep-allowed="canKeepField(f)"
                  :clear-allowed="!f.required && modal.mode === 'params'"
                  :secret="f.secret"
                  multiline
                  :disabled="modal.busy || modal.uncertain"
                /></div></template
            ><template
              v-if="modal.mode === 'create' || modal.mode === 'settings'"
              ><label
                >单笔订单金额下限（元）<input
                  v-model="modal.min"
                  inputmode="decimal"
                  :disabled="modal.busy || modal.uncertain"
                  placeholder="0 为不限" /></label
              ><label
                >单笔订单金额上限（元）<input
                  v-model="modal.max"
                  inputmode="decimal"
                  :disabled="modal.busy || modal.uncertain"
                  placeholder="0 或留空为不限"
              /></label>
              <p class="muted">
                金额还需同时满足父通道和当前用户组限制。
              </p></template
            ><label v-if="modal.mode === 'weight'"
              >权重（1–99）<input
                v-model="modal.weight"
                inputmode="numeric"
                :disabled="modal.busy || modal.uncertain"
                required /></label
            ><template v-if="modal.mode === 'roll'"
              ><p>商户 #{{ modal.owner }} 的子通道轮询方式</p>
              <label
                >轮询方式<KitSelect
                  v-model="dialogRollModel"
                  :options="rollTypeOptions"
                  :disabled="modal.busy || modal.uncertain"
                  aria-label="轮询方式"
              /></label>
              <p class="muted">
                顺序轮询按最近使用时间选择；权重轮询只在当前符合条件的子通道中生效。
              </p></template
            >
            <p v-if="modal.mode === 'status'">
              确认{{
                modal.row?.enabled ? "关闭" : "启用"
              }}此子通道？关闭后不再接收新订单，已有订单仍可核对。
            </p>
            <p v-if="modal.mode === 'delete'">
              确认删除此子通道？删除后不再接收新订单，已有订单仍可查询、退款。
            </p>
            <template v-if="modal.mode === 'test'"
              ><label
                >测试金额（元）<input
                  v-model="modal.testAmount"
                  inputmode="decimal"
                  :disabled="modal.busy || modal.executed"
              /></label>
              <p>
                确认后仅生成一次本次支付请求。生成入口不代表付款成功；结果未知时请核对原订单。
              </p>
              <template v-if="modal.url"
                ><a :href="modal.url" target="_blank" rel="noopener noreferrer"
                  >打开原支付页面</a
                ><RouterLink
                  :to="`/${area}/trade/order?trade_no=${encodeURIComponent(modal.tradeNo)}`"
                  >查看原订单
                  <span class="pa-order-no">{{
                    modal.tradeNo
                  }}</span></RouterLink
                ></template
              ><button
                v-if="modal.executed"
                type="button"
                :disabled="modal.busy || modal.uncertain"
                @click="testResult"
              >
                核对本次测试
              </button></template
            ><template v-if="modal.mode === 'link'"
              ><p>读取本人已有收款码的子通道链接，不会自动新建收款码。</p>
              <button
                type="button"
                :disabled="modal.busy || !modal.ready"
                @click="link"
              >
                读取收款链接</button
              ><template v-if="modal.url"
                ><label>完整收款链接<input :value="modal.url" readonly /></label
                ><button type="button" @click="copy">复制收款链接</button
                ><a :href="modal.url" target="_blank" rel="noopener noreferrer"
                  >打开收款页面</a
                ></template
              ></template
            >
            <div class="modal-actions">
              <button
                v-if="modal.uncertain"
                type="button"
                :disabled="modal.busy"
                @click="reconcileChange"
              >
                读取最新列表核对</button
              ><button type="button" :disabled="modal.busy" @click="close">
                {{
                  modal.mode === "test" && modal.executed
                    ? "关闭并保留原订单"
                    : "取消"
                }}</button
              ><button
                v-if="modal.mode !== 'link' && !modal.executed"
                class="primary"
                type="submit"
                :disabled="
                  modal.busy ||
                  !modal.ready ||
                  ((modal.mode === 'params' || modal.mode === 'create') &&
                    !modalSchema)
                "
              >
                {{
                  modal.busy
                    ? "正在提交…"
                    : modal.mode === "test"
                      ? "确认生成一次支付测试"
                      : modal.mode === "delete"
                        ? "确认删除"
                        : modal.mode === "status"
                          ? "确认变更"
                          : "确认保存"
                }}
              </button>
            </div>
          </form>
        </section>
      </div></Teleport
    >
  </div>
</template>
<style scoped>
.subchannel-page {
  min-width: 0;
}
/* Standard filter card: stacked labels above ~220px fields, actions at the
   right end. */
.subchannel-toolbar {
  display: flex;
  align-items: flex-end;
  flex-wrap: wrap;
  gap: var(--space-3) var(--space-5);
}
.subchannel-filter-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr));
  gap: var(--space-3) var(--space-5);
  flex: 1;
  min-width: 0;
}
.subchannel-toolbar label {
  display: grid;
  gap: var(--space-2);
  margin: 0;
  font-size: 13px;
}
.subchannel-toolbar input,
.subchannel-toolbar select {
  width: 100%;
  min-width: 0;
  margin-top: 0;
}
.subchannel-filter-actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  justify-content: flex-end;
}
.subchannel-table-wrap {
  overflow-x: auto;
  max-width: 100%;
  width: 100%;
}
.subchannel-table {
  min-width: 1050px;
  width: 100%;
  border-collapse: collapse;
}
.subchannel-table small {
  display: block;
  color: var(--ink-muted);
  font-size: 0.75rem;
  margin-top: 0.3rem;
}
.subchannel-id {
  overflow-wrap: anywhere;
  min-width: 130px;
  font-variant-numeric: tabular-nums;
}
.subchannel-name {
  min-width: 170px;
  max-width: 240px;
  overflow-wrap: anywhere;
}
.subchannel-name strong {
  font-weight: 500;
}
.subchannel-name .unavailable {
  color: var(--warning-text);
}
.subchannel-paytype {
  display: inline-flex;
  align-items: center;
  border-radius: 4px;
  padding: 0.2rem 0.4rem;
  white-space: nowrap;
  background: var(--primary-soft);
  color: var(--primary-text);
}
.subchannel-paytype.wxpay {
  background: var(--success-soft);
  color: var(--success-text);
}
.subchannel-state {
  padding: 0.25rem 0.5rem;
  background: var(--bg-raised);
}
.subchannel-state.enabled {
  background: var(--success-soft);
  color: var(--success-text);
}
.subchannel-state-cell {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}
/* KitStatusTag carries the visible tri-state word; the switch's own text label
   is its accessible name only, not shown beside the tag. */
.subchannel-state-cell :deep(.pa-switch > span) {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
  border: 0;
}
.subchannel-empty {
  text-align: center !important;
  height: 150px;
  color: var(--ink-muted);
}
.subchannel-dialog {
  --drawer-w: 640px;
  overflow-wrap: anywhere;
}
.subchannel-dialog h2 {
  font-size: 1.1rem;
}
.subchannel-dialog .subchannel-id {
  font-size: 0.8rem;
  color: var(--ink-muted);
}
.subchannel-form {
  display: grid;
  gap: var(--space-4);
}
.subchannel-form label {
  display: grid;
  gap: 6px;
  font-size: 0.85rem;
}
.subchannel-form input,
.subchannel-form select,
.subchannel-form textarea {
  min-width: 0;
  width: 100%;
}
.subchannel-form textarea {
  resize: vertical;
  font-family: ui-monospace, monospace;
}
.subchannel-param {
  display: grid;
  gap: 6px;
  padding: 0.7rem;
  background: var(--bg-raised);
  border: 1px solid var(--line);
  border-radius: 5px;
  min-width: 0;
  box-sizing: border-box;
}
.subchannel-param-label {
  font-size: 0.85rem;
  overflow-wrap: anywhere;
}
.subchannel-form textarea,
.subchannel-form input,
.subchannel-form select {
  box-sizing: border-box;
  max-width: 100%;
}
.subchannel-form a {
  overflow-wrap: anywhere;
}
.subchannel-dialog .modal-actions {
  display: flex;
  gap: 0.6rem;
  justify-content: flex-end;
  padding-top: 0.6rem;
}
.subchannel-form .muted {
  font-size: 0.8rem;
  line-height: 1.6;
}
@media (max-width: 600px) {
  .subchannel-filter-fields {
    grid-template-columns: 1fr;
  }
  .subchannel-filter-actions {
    width: 100%;
  }
  .subchannel-dialog .modal-actions {
    flex-wrap: wrap;
  }
}
</style>
