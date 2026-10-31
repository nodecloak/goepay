<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ApiError, type ApiClient } from "../api";
import type { Area } from "../menu";
import ChannelSharingFinishPanel from "./ChannelSharingFinishPanel.vue";
const props = defineProps<{
  client: ApiClient;
  area: Area;
  page: "receiver" | "order";
}>();
type Channel = {
  channel_id: string;
  provider: string;
  product: string;
  methods: string[];
  receiver_types: string[];
  return_supported: boolean;
  finish_supported: boolean;
};
type Receiver = {
  id: string;
  channel_id: string;
  provider: string;
  type: string;
  account_masked: string;
  name_masked: string;
  state: string;
  operation?: { state: string; source: string; out_no: string };
};
type RuleItem = {
  receiver_id: string;
  mode: string;
  rate_bps: string;
  fixed_amount: string;
};
type Rule = {
  id: string;
  revision: string;
  enabled: boolean;
  minimum_amount: string;
  methods: string[];
  items: RuleItem[];
};
type Share = {
  id: string;
  external_no: string;
  out_no: string;
  state: string;
  completed: boolean;
  funds_released: boolean;
  released_amount: string;
  items: {
    receiver_id: string;
    amount: string;
    state: string;
    failure_code?: string;
  }[];
};
type ShareReturn = {
  id: string;
  sharing_id: string;
  receiver_id: string;
  external_no: string;
  out_no: string;
  amount: string;
  state: string;
};
const merchant = ref(""),
  resolved = ref(""),
  channels = ref<Channel[]>([]),
  selected = ref(""),
  ready = ref(false),
  reason = ref(""),
  error = ref(""),
  notice = ref(""),
  busy = ref(false);
const receivers = ref<Receiver[]>([]),
  orders = ref<Share[]>([]),
  returns = ref<ShareReturn[]>([]),
  returnCursor = ref("0"),
  receiverCursor = ref("0"),
  orderCursor = ref("0");
const account = ref(""),
  name = ref(""),
  receiverType = ref("MERCHANT_ID"),
  relation = ref("PARTNER"),
  customRelation = ref("");
const rule = ref<Rule>(),
  methods = ref<string[]>([]),
  minimum = ref("0"),
  ruleItems = ref<RuleItem[]>([]);
const trade = ref(""),
  external = ref(""),
  submitted = ref(false),
  originalIntent = ref(""),
  declaration = ref<any>(),
  confirmation = ref<() => Promise<void>>(),
  confirmText = ref("");
const returnDraft = ref<{
  sharing_external_no: string;
  receiver_id: string;
  external_no: string;
  amount: string;
  locked: boolean;
}>();
const selectedChannel = computed(() =>
  channels.value.find((c) => c.channel_id === selected.value),
);
const isV2Single = computed(
  () => selectedChannel.value?.product === "wxpay_v2_single_direct",
);
const paymentMethods: Record<string, string> = {
  native: "扫码支付",
  jsapi: "公众号支付",
  h5: "手机网站支付",
  app: "APP 支付",
  scan: "付款码支付",
  page: "电脑网站支付",
  wap: "手机网站支付",
  applet: "小程序支付",
};
const bound = computed(() =>
  receivers.value.filter(
    (r) => r.state === "bound" && r.channel_id === selected.value,
  ),
);
const positive = (s: unknown): s is string =>
  typeof s === "string" &&
  /^[1-9][0-9]{0,18}$/.test(s) &&
  BigInt(s) <= 9223372036854775807n;
const unsigned = (s: unknown): s is string =>
  typeof s === "string" &&
  /^(0|[1-9][0-9]{0,18})$/.test(s) &&
  BigInt(s) <= 9223372036854775807n;
const safe = (s: unknown): s is string =>
  typeof s === "string" && s.length <= 256 && !/[\u0000-\u001f\u007f]/.test(s);
const states: Record<string, string> = {
  draft: "待绑定",
  binding: "绑定处理中",
  bound: "已绑定",
  unbinding: "解绑处理中",
  unbound: "已解绑",
  unknown: "结果未知",
  unsent: "未发送",
  claimed: "已受理",
  pending: "处理中",
  succeeded: "成功",
  failed: "失败",
  partial: "部分成功",
};
const label = (s: string) => states[s] || s;
const money = (s: string) =>
  unsigned(s)
    ? `${BigInt(s) / 100n}.${(BigInt(s) % 100n).toString().padStart(2, "0")}`
    : "—";
let epoch = 0,
  controller: AbortController | undefined;
const operationNos = new Map<string, string>();
const newNo = (prefix: string) =>
  prefix +
  Array.from(crypto.getRandomValues(new Uint8Array(12)), (v) =>
    v.toString(16).padStart(2, "0"),
  ).join("");
function clearPrivate() {
  account.value = "";
  name.value = "";
  customRelation.value = "";
}
function reset() {
  epoch++;
  controller?.abort();
  busy.value = false;
  resolved.value = "";
  channels.value = [];
  selected.value = "";
  ready.value = false;
  receivers.value = [];
  orders.value = [];
  returns.value = [];
  returnDraft.value = undefined;
  rule.value = undefined;
  ruleItems.value = [];
  declaration.value = undefined;
  submitted.value = false;
  originalIntent.value = "";
  trade.value = "";
  external.value = "";
  confirmation.value = undefined;
  error.value = "";
  notice.value = "";
  reason.value = "";
  clearPrivate();
  operationNos.clear();
}
watch(() => [props.client, props.area, merchant.value], reset);
watch(selected, () => {
  clearPrivate();
  rule.value = undefined;
  ruleItems.value = [];
  methods.value = selectedChannel.value?.methods.slice() || [];
  minimum.value = "0";
  receiverType.value =
    selectedChannel.value?.provider === "alipay" ? "userId" : "MERCHANT_ID";
  relation.value = isV2Single.value ? "SERVICE_PROVIDER" : "PARTNER";
  confirmation.value = undefined;
});
watch(trade, () => {
  declaration.value = undefined;
  confirmation.value = undefined;
  submitted.value = false;
  originalIntent.value = "";
  external.value = "";
});
watch(
  () => [
    external.value,
    minimum.value,
    methods.value,
    ruleItems.value,
    returnDraft.value,
  ],
  () => {
    confirmation.value = undefined;
  },
  { deep: true, flush: "sync" },
);
onBeforeUnmount(reset);
const ownerBody = () =>
  props.area === "admin" ? { merchant_id: resolved.value } : {};
const ownerQuery = () =>
  props.area === "admin"
    ? `merchant_id=${encodeURIComponent(resolved.value)}`
    : "";
const query = (path: string, extra = "") =>
  path +
  ([ownerQuery(), extra].filter(Boolean).length
    ? "?" + [ownerQuery(), extra].filter(Boolean).join("&")
    : "");
async function run(
  action: (version: number, signal: AbortSignal) => Promise<void>,
) {
  if (busy.value) return;
  const version = epoch;
  controller = new AbortController();
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    await action(version, controller.signal);
  } catch (e) {
    if (
      version === epoch &&
      !(e instanceof DOMException && e.name === "AbortError")
    )
      error.value = e instanceof Error ? e.message : "渠道分账操作失败";
  } finally {
    if (version === epoch) busy.value = false;
  }
}
function parseReceiver(v: any): Receiver {
  if (
    !v ||
    !positive(v.id) ||
    !positive(v.channel_id) ||
    !["wxpay", "alipay"].includes(v.provider) ||
    !safe(v.type) ||
    !safe(v.account_masked) ||
    !safe(v.name_masked) ||
    !Object.hasOwn(states, v.state)
  )
    throw Error("接收方响应格式错误");
  return {
    id: v.id,
    channel_id: v.channel_id,
    provider: v.provider,
    type: v.type,
    account_masked: v.account_masked,
    name_masked: v.name_masked,
    state: v.state,
    ...(v.operation &&
    safe(v.operation.state) &&
    safe(v.operation.source) &&
    safe(v.operation.out_no)
      ? {
          operation: {
            state: v.operation.state,
            source: v.operation.source,
            out_no: v.operation.out_no,
          },
        }
      : {}),
  };
}
function parseOrder(v: any): Share {
  if (
    !v ||
    !positive(v.id) ||
    !safe(v.external_no) ||
    !safe(v.out_no) ||
    !Object.hasOwn(states, v.state) ||
    typeof v.completed !== "boolean" ||
    typeof v.funds_released !== "boolean" ||
    !unsigned(v.released_amount) ||
    !Array.isArray(v.items) ||
    v.items.length > 50
  )
    throw Error("分账响应格式错误");
  const items = v.items.map((i: any) => {
    if (
      !positive(i.receiver_id) ||
      !unsigned(i.amount) ||
      !Object.hasOwn(states, i.state)
    )
      throw Error("分账明细响应格式错误");
    return {
      receiver_id: i.receiver_id,
      amount: i.amount,
      state: i.state,
      ...(safe(i.failure_code) ? { failure_code: i.failure_code } : {}),
    };
  });
  return {
    id: v.id,
    external_no: v.external_no,
    out_no: v.out_no,
    state: v.state,
    completed: v.completed,
    funds_released: v.funds_released,
    released_amount: v.released_amount,
    items,
  };
}
function parseReturn(v: any): ShareReturn {
  if (
    !v ||
    !positive(v.id) ||
    !positive(v.sharing_id) ||
    !positive(v.receiver_id) ||
    !safe(v.external_no) ||
    !safe(v.out_no) ||
    !positive(v.amount) ||
    !Object.hasOwn(states, v.state)
  )
    throw Error("分账回退响应格式错误");
  return {
    id: v.id,
    sharing_id: v.sharing_id,
    receiver_id: v.receiver_id,
    external_no: v.external_no,
    out_no: v.out_no,
    amount: v.amount,
    state: v.state,
  };
}
async function readReturns(
  version: number,
  signal: AbortSignal,
  append = false,
) {
  const result: any = await props.client.request(
    query(
      "/channel-sharing/returns",
      `limit=20${append ? `&cursor=${returnCursor.value}` : ""}`,
    ),
    { signal },
  );
  if (version !== epoch) return;
  if (!Array.isArray(result?.items) || !unsigned(result.next_cursor))
    throw Error("分账回退列表格式错误");
  const items = result.items.map(parseReturn);
  returns.value = append ? [...returns.value, ...items] : items;
  returnCursor.value = result.next_cursor;
}
async function load() {
  if (props.area === "admin" && !positive(merchant.value)) {
    error.value = "请输入有效商户 ID";
    return;
  }
  await run(async (version, signal) => {
    resolved.value = props.area === "admin" ? merchant.value : "";
    const caps: any = await props.client.request(
      query("/channel-sharing/channels"),
      { signal },
    );
    if (version !== epoch) return;
    if (
      !positive(caps?.merchant_id) ||
      typeof caps.available !== "boolean" ||
      !Array.isArray(caps.channels)
    )
      throw Error("分账通道响应格式错误");
    if (props.area === "admin" && caps.merchant_id !== resolved.value)
      throw Error("分账商户响应不一致");
    resolved.value = caps.merchant_id;
    channels.value = caps.channels.map((c: any) => {
      if (
        !positive(c.channel_id) ||
        !["wxpay", "alipay"].includes(c.provider) ||
        !safe(c.product) ||
        !Array.isArray(c.receiver_types) ||
        c.receiver_types.some((type: any) => !safe(type)) ||
        typeof c.return_supported !== "boolean" ||
        typeof c.finish_supported !== "boolean" ||
        !Array.isArray(c.methods) ||
        c.methods.some((m: any) => !safe(m))
      )
        throw Error("分账通道响应格式错误");
      return {
        channel_id: c.channel_id,
        provider: c.provider,
        product: c.product,
        methods: c.methods,
        receiver_types: c.receiver_types,
        return_supported: c.return_supported,
        finish_supported: c.finish_supported,
      };
    });
    ready.value = caps.available && channels.value.length > 0;
    reason.value = safe(caps.reason) ? caps.reason : "";
    await readPage(version, signal, false);
    if (props.page === "order") await readReturns(version, signal);
  });
}
async function readPage(version: number, signal: AbortSignal, append: boolean) {
  const kind = props.page === "receiver" ? "receivers" : "orders",
    cursor =
      props.page === "receiver" ? receiverCursor.value : orderCursor.value;
  const result: any = await props.client.request(
    query(
      `/channel-sharing/${kind}`,
      `limit=20${append ? `&cursor=${cursor}` : ""}`,
    ),
    { signal },
  );
  if (version !== epoch) return;
  if (!Array.isArray(result?.items) || !unsigned(result.next_cursor))
    throw Error("分账列表响应格式错误");
  if (props.page === "receiver") {
    const items = result.items.map(parseReceiver);
    receivers.value = append ? [...receivers.value, ...items] : items;
    receiverCursor.value = result.next_cursor;
  } else {
    const items = result.items.map(parseOrder);
    orders.value = append ? [...orders.value, ...items] : items;
    orderCursor.value = result.next_cursor;
  }
}
const next = () => run((v, s) => readPage(v, s, true));
function confirm(text: string, action: () => Promise<void>) {
  confirmText.value = text;
  confirmation.value = action;
}
async function executeConfirmed() {
  const action = confirmation.value;
  confirmation.value = undefined;
  await action?.();
}
async function createReceiver() {
  if (!ready.value || !selected.value || !account.value) return;
  if (isV2Single.value) {
    const bytes = (value: string) => new TextEncoder().encode(value).length;
    if (bytes(account.value) > 64) {
      error.value = "接收账号过长，请核对原账号";
      return;
    }
    if (bytes(name.value) > 1024) {
      error.value = "接收方姓名过长，请核对原姓名";
      return;
    }
    if (
      relation.value === "CUSTOM" &&
      (Array.from(customRelation.value).length > 10 ||
        bytes(customRelation.value) > 40)
    ) {
      error.value = "自定义关系最多 10 个字";
      return;
    }
  }
  const payload = {
    ...ownerBody(),
    channel_id: selected.value,
    external_no: newNo("receiver_"),
    type: receiverType.value,
    account: account.value,
    name: name.value,
    ...(selectedChannel.value?.provider === "wxpay"
      ? {
          relation_type: relation.value,
          ...(relation.value === "CUSTOM"
            ? { custom_relation: customRelation.value }
            : {}),
        }
      : {}),
  };
  clearPrivate();
  await run(async (v, signal) => {
    const result = parseReceiver(
      await props.client.request("/channel-sharing/receivers", {
        method: "POST",
        body: payload,
        signal,
      }),
    );
    if (v !== epoch) return;
    receivers.value = [
      result,
      ...receivers.value.filter((i) => i.id !== result.id),
    ];
    notice.value = "接收方草稿已加密保存，请核对后单独绑定";
  });
}
async function receiverAction(r: Receiver, action: "bind" | "unbind") {
  confirm(
    `${action === "bind" ? "绑定" : "解绑"}接收方 ${r.account_masked}？已有规则或订单引用时，服务端会拒绝解绑。`,
    () =>
      run(async (v, signal) => {
        const key = r.id + action;
        let no = operationNos.get(key);
        if (!no) {
          no = newNo(action + "_");
          operationNos.set(key, no);
        }
        const result = parseReceiver(
          await props.client.request(
            `/channel-sharing/receivers/${r.id}/${action}`,
            {
              method: "POST",
              body: { ...ownerBody(), external_no: no },
              signal,
            },
          ),
        );
        if (v === epoch)
          receivers.value = receivers.value.map((i) =>
            i.id === result.id ? result : i,
          );
      }),
  );
}
async function loadRule() {
  if (!selected.value) return;
  await run(async (v, signal) => {
    let result: any;
    try {
      result = await props.client.request(
        query(`/channel-sharing/rules/${selected.value}`),
        { signal },
      );
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) {
        if (v === epoch) {
          rule.value = undefined;
          notice.value = "该通道尚未配置规则，新版本从 0 开始";
        }
        return;
      }
      throw e;
    }
    if (v !== epoch) return;
    if (
      !positive(result?.id) ||
      !positive(result.revision) ||
      typeof result.enabled !== "boolean" ||
      !unsigned(result.minimum_amount) ||
      !Array.isArray(result.methods) ||
      !Array.isArray(result.items)
    )
      throw Error("分账规则响应格式错误");
    rule.value = result;
    minimum.value = result.minimum_amount;
    methods.value = result.methods.slice();
    ruleItems.value = result.items.map((i: RuleItem) => ({ ...i }));
  });
}
function addItem() {
  const candidate = bound.value.find(
    (r) => !ruleItems.value.some((i) => i.receiver_id === r.id),
  );
  if (candidate)
    ruleItems.value.push({
      receiver_id: candidate.id,
      mode: "rate",
      rate_bps: "100",
      fixed_amount: "0",
    });
}
async function saveRule(disable = false) {
  const channel = selected.value,
    expected = rule.value?.id || "0";
  if (!channel) return;
  const payload = disable
    ? { ...ownerBody(), expected }
    : {
        ...ownerBody(),
        expected,
        enabled: true,
        minimum_amount: minimum.value,
        methods: methods.value.slice(),
        items: ruleItems.value.map((i) => ({
          ...i,
          rate_bps: i.mode === "rate" ? i.rate_bps : "0",
          fixed_amount: i.mode === "fixed" ? i.fixed_amount : "0",
        })),
      };
  confirm(
    disable
      ? "禁用当前规则？已有支付订单仍按原规则快照处理。"
      : "发布新的渠道分账规则版本？只影响之后创建且声明分账的支付订单。",
    () =>
      run(async (v, signal) => {
        const result: any = await props.client.request(
          `/channel-sharing/rules/${channel}${disable ? "/disable" : ""}`,
          { method: "POST", body: payload, signal },
        );
        if (v !== epoch) return;
        rule.value = result;
        notice.value = `规则版本 ${result.revision} 已${disable ? "禁用" : "发布"}`;
      }),
  );
}
async function preview() {
  if (!trade.value) return;
  await run(async (v, signal) => {
    const d: any = await props.client.request(
      query(`/channel-sharing/payment/${encodeURIComponent(trade.value)}`),
      { signal },
    );
    if (v !== epoch) return;
    if (
      !d ||
      !positive(d.merchant_id) ||
      d.merchant_id !== resolved.value ||
      typeof d.declared !== "boolean" ||
      !Array.isArray(d.items) ||
      !unsigned(d.gross_amount) ||
      !unsigned(d.merchant_net)
    )
      throw Error("原支付声明响应格式错误");
    declaration.value = {
      declared: d.declared,
      gross_amount: d.gross_amount,
      merchant_net: d.merchant_net,
      items: d.items.map((i: any) => {
        if (!positive(i.receiver_id) || !unsigned(i.amount))
          throw Error("原分账明细格式错误");
        return { receiver_id: i.receiver_id, amount: i.amount };
      }),
    };
    if (!external.value) external.value = newNo("sharing_");
  });
}
async function submit() {
  if (!declaration.value?.declared || !external.value || !ready.value) return;
  const payload = {
    ...ownerBody(),
    trade_no: trade.value,
    external_no: external.value,
  };
  confirm(
    "确认按原支付声明提交渠道分账？结果未知时只能查询原请求，不能换号重发。",
    () =>
      run(async (v, signal) => {
        originalIntent.value = payload.external_no;
        submitted.value = true;
        let result: Share;
        try {
          result = parseOrder(
            await props.client.request("/channel-sharing/orders", {
              method: "POST",
              body: payload,
              signal,
            }),
          );
        } catch (e) {
          if (v === epoch && !signal.aborted) {
            try {
              const original = parseOrder(
                await props.client.request(
                  query(
                    `/channel-sharing/orders/${encodeURIComponent(payload.external_no)}`,
                  ),
                  { signal },
                ),
              );
              if (v === epoch)
                orders.value = [
                  original,
                  ...orders.value.filter((i) => i.id !== original.id),
                ];
            } catch (readError) {
              if (
                v === epoch &&
                readError instanceof ApiError &&
                readError.status === 404
              )
                submitted.value = false;
            }
          }
          throw e;
        }
        if (v === epoch) {
          orders.value = [
            result,
            ...orders.value.filter((i) => i.id !== result.id),
          ];
          notice.value = "分账请求已记录，请根据逐接收方结果查询处理进度";
        }
      }),
  );
}
async function queryOrder(row: Share) {
  await run(async (v, signal) => {
    const result = parseOrder(
      await props.client.request(
        `/channel-sharing/orders/${encodeURIComponent(row.external_no)}/query`,
        { method: "POST", body: ownerBody(), signal },
      ),
    );
    if (v === epoch)
      orders.value = orders.value.map((i) => (i.id === row.id ? result : i));
  });
}
async function recoverOriginal() {
  if (!originalIntent.value) return;
  await run(async (v, signal) => {
    const result = parseOrder(
      await props.client.request(
        query(
          `/channel-sharing/orders/${encodeURIComponent(originalIntent.value)}`,
        ),
        { signal },
      ),
    );
    if (v === epoch)
      orders.value = [
        result,
        ...orders.value.filter((i) => i.id !== result.id),
      ];
  });
}
function canStartReturn(row: Share, receiver: string) {
  return (
    row.completed &&
    row.funds_released &&
    ready.value &&
    channels.value.some((c) => c.return_supported) &&
    !returns.value.some(
      (r) =>
        r.sharing_id === row.id &&
        r.receiver_id === receiver &&
        !["succeeded", "failed"].includes(r.state),
    )
  );
}
async function prepareReturn(row: Share, receiver: string) {
  await run(async (v, signal) => {
    const original = parseReceiver(
      await props.client.request(
        query(`/channel-sharing/receivers/${receiver}`),
        { signal },
      ),
    );
    if (v !== epoch) return;
    const channel = channels.value.find(
      (c) => c.channel_id === original.channel_id,
    );
    if (
      !channel?.return_supported ||
      !channel.receiver_types.includes(original.type) ||
      !(
        (original.provider === "wxpay" && original.type === "MERCHANT_ID") ||
        (original.provider === "alipay" && original.type === "userId")
      )
    )
      throw Error("原接收方类型或当前通道不支持已验证的分账回退");
    returnDraft.value = {
      sharing_external_no: row.external_no,
      receiver_id: receiver,
      external_no: newNo("return_"),
      amount: "",
      locked: false,
    };
  });
}
function returnAmount(raw: string): string {
  if (!/^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$/.test(raw))
    throw Error("请输入正确的回退金额，最多两位小数");
  const [a, b = ""] = raw.split(".");
  const amount = BigInt(a!) * 100n + BigInt(b.padEnd(2, "0"));
  if (amount <= 0n) throw Error("回退金额必须大于 0");
  return amount.toString();
}
async function submitReturn() {
  const draft = returnDraft.value;
  if (!draft || draft.locked) return;
  let amount: string;
  try {
    amount = returnAmount(draft.amount);
  } catch (e) {
    error.value = (e as Error).message;
    return;
  }
  const payload = {
    ...ownerBody(),
    sharing_external_no: draft.sharing_external_no,
    receiver_id: draft.receiver_id,
    external_no: draft.external_no,
    amount,
  };
  confirm(
    `确认从原接收方 ${draft.receiver_id} 回退 ${draft.amount} 元至原商户？此操作不代表买家退款。`,
    () =>
      run(async (v, signal) => {
        draft.locked = true;
        try {
          const result = parseReturn(
            await props.client.request("/channel-sharing/returns", {
              method: "POST",
              body: payload,
              signal,
            }),
          );
          if (v === epoch) {
            returns.value = [
              result,
              ...returns.value.filter((i) => i.id !== result.id),
            ];
            returnDraft.value = undefined;
          }
        } catch (e) {
          if (v === epoch && !signal.aborted) {
            try {
              const original = parseReturn(
                await props.client.request(
                  query(
                    `/channel-sharing/returns/${encodeURIComponent(draft.external_no)}`,
                  ),
                  { signal },
                ),
              );
              if (v === epoch)
                returns.value = [
                  original,
                  ...returns.value.filter((i) => i.id !== original.id),
                ];
            } catch (readError) {
              if (
                v === epoch &&
                readError instanceof ApiError &&
                readError.status === 404
              )
                draft.locked = false;
            }
          }
          throw e;
        }
      }),
  );
}
async function queryReturn(row: ShareReturn) {
  await run(async (v, signal) => {
    const result = parseReturn(
      await props.client.request(
        `/channel-sharing/returns/${encodeURIComponent(row.external_no)}/query`,
        { method: "POST", body: ownerBody(), signal },
      ),
    );
    if (v === epoch)
      returns.value = returns.value.map((i) => (i.id === row.id ? result : i));
  });
}
onMounted(() => {
  if (props.area === "merchant") void load();
});
</script>

<template>
  <section
    class="panel sharing-page"
    :aria-label="page === 'receiver' ? '渠道分账规则' : '渠道分账记录'"
  >
    <header>
      <h2>{{ page === "receiver" ? "渠道分账规则" : "渠道分账记录" }}</h2>
      <p class="muted">
        规则与接收方绑定到原收款通道。支付后保留原声明，所有金额单位为人民币元。
      </p>
    </header>
    <form class="sharing-toolbar" @submit.prevent="load">
      <label v-if="area === 'admin'"
        >商户 ID<input
          v-model="merchant"
          aria-label="商户 ID"
          inputmode="numeric"
          autocomplete="off"
      /></label>
      <button data-action="load" :disabled="busy">
        {{ busy ? "处理中…" : "读取分账数据" }}
      </button>
    </form>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <p v-if="notice" role="status" class="success">{{ notice }}</p>
    <p v-if="reason" role="status">{{ reason }}</p>
    <template v-if="page === 'receiver'">
      <form class="sharing-form" @submit.prevent="createReceiver">
        <h3>新增接收方草稿</h3>
        <label
          >分账通道<select
            v-model="selected"
            aria-label="分账通道"
            :disabled="busy || !ready"
          >
            <option value="">请选择通道</option>
            <option
              v-for="c in channels"
              :key="c.channel_id"
              :value="c.channel_id"
            >
              {{ c.channel_id }} ·
              {{ c.provider === "wxpay" ? "微信" : "支付宝" }}
              {{ c.product === "wxpay_v2_single_direct" ? "· 单次分账" : "" }}
            </option>
          </select></label
        >
        <label
          >接收方类型<select
            v-model="receiverType"
            :disabled="busy || !selected"
          >
            <option
              v-if="selectedChannel?.provider === 'alipay'"
              value="userId"
            >
              支付宝用户 ID（2088）
            </option>
            <template v-else
              ><option value="MERCHANT_ID">微信商户号</option>
              <option value="PERSONAL_OPENID">微信 OpenID</option></template
            >
          </select></label
        >
        <label
          >接收账号<input
            v-model="account"
            aria-label="接收账号"
            :maxlength="isV2Single ? 64 : 128"
            autocomplete="off"
            :disabled="busy || !selected"
        /></label>
        <label
          >接收方姓名<input
            v-model="name"
            aria-label="接收方姓名"
            :maxlength="isV2Single ? 1024 : 256"
            autocomplete="off"
            :disabled="busy || !selected"
        /></label>
        <label v-if="selectedChannel?.provider === 'wxpay'"
          >关系类型<select v-model="relation">
            <option value="SERVICE_PROVIDER">服务商</option>
            <option value="PARTNER">合作伙伴</option>
            <option value="STORE">门店</option>
            <option value="STAFF">员工</option>
            <option value="SUPPLIER">供应商</option>
            <option value="CUSTOM">自定义</option>
          </select></label
        >
        <label
          v-if="relation === 'CUSTOM' && selectedChannel?.provider === 'wxpay'"
          >自定义关系<input
            v-model="customRelation"
            :maxlength="isV2Single ? 10 : 30"
        /></label>
        <p v-if="isV2Single" class="muted full">
          单次分账会同时解冻原订单剩余金额，请在支付前核对接收方与规则。
        </p>
        <p class="muted full">
          完整账号仅用于本次保存，提交后立即清空；列表只显示脱敏账号。
        </p>
        <button
          data-action="create-receiver"
          class="primary"
          :disabled="busy || !ready || !selected || !account"
        >
          加密保存草稿
        </button>
      </form>
      <div class="sharing-table">
        <table>
          <caption>
            接收方与绑定状态
          </caption>
          <thead>
            <tr>
              <th>编号 / 通道</th>
              <th>接收方</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in receivers" :key="r.id">
              <td>{{ r.id }} / {{ r.channel_id }}</td>
              <td>{{ r.account_masked }}<br />{{ r.name_masked }}</td>
              <td>
                {{ label(r.state)
                }}<small v-if="r.operation?.source === 'reference'"
                  >已有绑定引用</small
                ><small v-if="r.state === 'unknown'"
                  >请核实原操作，禁止换号重发</small
                >
              </td>
              <td>
                <button
                  v-if="['draft', 'unbound'].includes(r.state)"
                  :disabled="busy || !ready"
                  @click="receiverAction(r, 'bind')"
                >
                  绑定</button
                ><button
                  v-if="r.state === 'bound'"
                  :disabled="busy || !ready"
                  @click="receiverAction(r, 'unbind')"
                >
                  解绑
                </button>
              </td>
            </tr>
            <tr v-if="!receivers.length">
              <td colspan="4">暂无接收方记录</td>
            </tr>
          </tbody>
        </table>
      </div>
      <button v-if="receiverCursor !== '0'" :disabled="busy" @click="next">
        加载更多接收方
      </button>
      <section class="sharing-form" aria-label="分账规则版本">
        <h3>通道规则版本</h3>
        <p class="muted full">
          使用上方选择的通道。比例为万分比：100 表示
          1%；固定金额以分填写。接收方必须已绑定。
        </p>
        <button :disabled="busy || !selected" @click="loadRule">
          读取当前规则
        </button>
        <p v-if="rule" class="full">
          当前版本 {{ rule.revision }} · {{ rule.enabled ? "启用" : "禁用" }}
        </p>
        <label
          >最低订单金额（分）<input
            v-model="minimum"
            inputmode="numeric"
            :disabled="busy || !selected"
        /></label>
        <fieldset class="full">
          <legend>支付方式</legend>
          <label
            v-for="method in selectedChannel?.methods || []"
            :key="method"
            class="inline"
            ><input v-model="methods" type="checkbox" :value="method" />{{
              paymentMethods[method] || method
            }}</label
          >
        </fieldset>
        <div
          v-for="(item, index) in ruleItems"
          :key="index"
          class="sharing-rule-row full"
        >
          <label
            >接收方<select v-model="item.receiver_id">
              <option v-for="r in bound" :key="r.id" :value="r.id">
                {{ r.id }} · {{ r.account_masked }}
              </option>
            </select></label
          ><label
            >计算方式<select v-model="item.mode">
              <option value="rate">万分比</option>
              <option value="fixed">固定金额（分）</option>
            </select></label
          ><label
            >{{ item.mode === "rate" ? "比例（万分比）" : "金额（分）"
            }}<input
              v-if="item.mode === 'rate'"
              v-model="item.rate_bps"
              inputmode="numeric" /><input
              v-else
              v-model="item.fixed_amount"
              inputmode="numeric" /></label
          ><button @click="ruleItems.splice(index, 1)">移除</button>
        </div>
        <button
          :disabled="busy || !bound.length || ruleItems.length >= 50"
          @click="addItem"
        >
          添加已绑定接收方</button
        ><button
          class="primary"
          :disabled="
            busy || !ready || !selected || !ruleItems.length || !methods.length
          "
          @click="saveRule()"
        >
          发布新版本</button
        ><button :disabled="busy || !rule?.enabled" @click="saveRule(true)">
          禁用当前规则
        </button>
      </section>
    </template>
    <template v-else>
      <form class="sharing-form" @submit.prevent="preview">
        <h3>按原支付声明提交</h3>
        <label
          >平台订单号<input
            v-model="trade"
            aria-label="平台订单号"
            maxlength="64"
            autocomplete="off" /></label
        ><button :disabled="busy || !resolved || !trade">核对原支付声明</button
        ><template v-if="declaration"
          ><p class="full">
            {{
              declaration.declared
                ? "已声明渠道分账"
                : "该订单未声明渠道分账，不能补做声明"
            }}
            · 原金额 {{ money(declaration.gross_amount) }} / 商户净额
            {{ money(declaration.merchant_net) }}
          </p>
          <ul class="full">
            <li v-for="item in declaration.items" :key="item.receiver_id">
              接收方 {{ item.receiver_id }}：{{ money(item.amount) }}
            </li>
          </ul>
          <label
            >本次业务请求号<input
              v-model="external"
              aria-label="业务请求号"
              :readonly="!!originalIntent"
              maxlength="128"
              autocomplete="off" /></label
          ><button
            type="button"
            class="primary"
            :disabled="
              busy || submitted || !ready || !declaration.declared || !external
            "
            @click="submit"
          >
            核对并提交分账</button
          ><button
            v-if="originalIntent"
            type="button"
            :disabled="busy"
            @click="recoverOriginal"
          >
            读取原请求记录
          </button></template
        >
      </form>
      <p class="muted">
        “执行结束”与“全部成功”分别展示；只有可信的解冻证据才能释放剩余资金。回退从原接收方收回资金，不代表买家退款。
      </p>
      <article v-for="row in orders" :key="row.id" class="sharing-order">
        <header>
          <strong>{{ row.external_no }}</strong
          ><span
            >{{ label(row.state) }} ·
            {{ row.completed ? "执行结束" : "尚未结束" }} ·
            {{ row.funds_released ? "剩余资金已释放" : "原资金保留" }}</span
          >
        </header>
        <p>
          上游请求号 {{ row.out_no }} · 已释放 {{ money(row.released_amount) }}
        </p>
        <ul>
          <li v-for="item in row.items" :key="item.receiver_id">
            接收方 {{ item.receiver_id }} · {{ money(item.amount) }} ·
            {{ label(item.state)
            }}<span v-if="item.failure_code">（{{ item.failure_code }}）</span>
            <button
              v-if="
                row.completed &&
                row.funds_released &&
                item.state === 'succeeded'
              "
              data-action="prepare-return"
              :disabled="busy || !canStartReturn(row, item.receiver_id)"
              @click="prepareReturn(row, item.receiver_id)"
            >
              申请原接收方回退
            </button>
          </li>
        </ul>
        <button
          data-action="query-order"
          :disabled="busy || row.funds_released"
          @click="queryOrder(row)"
        >
          查询原请求
        </button>
      </article>
      <p v-if="!orders.length">暂无渠道分账记录</p>
      <button v-if="orderCursor !== '0'" :disabled="busy" @click="next">
        加载更多分账记录
      </button>
      <form
        v-if="returnDraft"
        class="sharing-form"
        aria-label="申请分账回退"
        @submit.prevent="submitReturn"
      >
        <h3>原接收方回退</h3>
        <p class="full">
          原分账 {{ returnDraft.sharing_external_no }} · 接收方
          {{
            returnDraft.receiver_id
          }}。只能回退原单已成功分出的额度，累计金额由服务端核对。
        </p>
        <label
          >回退金额（元）<input
            v-model="returnDraft.amount"
            aria-label="回退金额（元）"
            inputmode="decimal"
            :disabled="busy || returnDraft.locked"
        /></label>
        <label
          >回退业务请求号<input
            v-model="returnDraft.external_no"
            aria-label="回退业务请求号"
            :readonly="returnDraft.locked"
            maxlength="128"
        /></label>
        <button
          class="primary"
          :disabled="busy || returnDraft.locked || !returnDraft.amount"
        >
          核对并提交回退</button
        ><button
          type="button"
          :disabled="busy"
          @click="returnDraft = undefined"
        >
          关闭回退表单
        </button>
      </form>
      <section aria-label="分账回退记录">
        <h3>分账回退记录</h3>
        <p v-if="!returns.length">暂无回退记录</p>
        <article v-for="row in returns" :key="row.id" class="sharing-order">
          <strong>{{ row.external_no }}</strong>
          <p>
            原分账编号 {{ row.sharing_id }} · 接收方 {{ row.receiver_id }} ·
            {{ money(row.amount) }} · {{ label(row.state)
            }}{{ row.state === "succeeded" ? "（已回到原商户）" : "" }}
          </p>
          <p class="muted">上游请求号 {{ row.out_no }}</p>
          <button
            data-action="query-return"
            :disabled="busy || ['succeeded', 'failed'].includes(row.state)"
            @click="queryReturn(row)"
          >
            查询原回退请求
          </button>
        </article>
        <button
          v-if="returnCursor !== '0'"
          :disabled="busy"
          @click="run((v, s) => readReturns(v, s, true))"
        >
          加载更多回退记录
        </button>
      </section>
      <ChannelSharingFinishPanel
        v-if="resolved"
        :key="resolved"
        :client="client"
        :area="area"
        :merchant-id="resolved"
        :enabled="
          ready &&
          channels.some((c) => c.provider === 'wxpay' && c.finish_supported)
        "
      />
    </template>
    <div
      v-if="confirmation"
      class="pa-scrim pa-modal-scrim"
      @pointerdown.self="!busy && (confirmation = undefined)"
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label="确认渠道分账操作"
        class="pa-modal sharing-confirm"
        @keydown.esc.prevent="!busy && (confirmation = undefined)"
      >
        <header class="pa-modal-head">
          <h2 class="pa-modal-title">确认渠道分账操作</h2>
          <button
            type="button"
            class="pa-icon-btn pa-icon-btn--ghost pa-modal-x"
            aria-label="关闭"
            :disabled="busy"
            @click="confirmation = undefined"
          >
            ×
          </button>
        </header>
        <div class="pa-modal-content">
          <p>{{ confirmText }}</p>
        </div>
        <footer class="pa-modal-foot">
          <button
            type="button"
            class="pa-btn"
            :disabled="busy"
            @click="confirmation = undefined"
          >
            取消</button
          ><button
            type="button"
            class="pa-btn pa-btn--primary primary"
            :disabled="busy"
            @click="executeConfirmed"
          >
            确认操作
          </button>
        </footer>
      </section>
    </div>
  </section>
</template>

<style scoped>
.sharing-page {
  display: grid;
  gap: 18px;
  padding: 18px;
}
.sharing-page h2,
.sharing-page h3 {
  margin: 0;
}
.sharing-toolbar {
  display: flex;
  align-items: end;
  gap: 12px;
}
.sharing-toolbar label {
  max-width: 240px;
}
.sharing-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 14px;
  padding: 18px;
  border: 1px solid var(--line);
  border-radius: 6px;
}
.sharing-form h3,
.full {
  grid-column: 1/-1;
}
.sharing-form label,
.sharing-toolbar label {
  display: grid;
  gap: 6px;
  margin: 0;
}
.sharing-form label > input,
.sharing-form label > select,
.sharing-toolbar label > input,
.sharing-toolbar label > select {
  margin-top: 0;
}
.sharing-form button {
  align-self: end;
}
.sharing-page select {
  width: 100%;
}
.sharing-form fieldset {
  border: 1px solid var(--line);
}
.sharing-form label.inline {
  display: inline-flex;
  align-items: center;
  margin-right: 14px;
}
.inline input {
  width: auto;
}
.sharing-rule-row {
  display: grid;
  grid-template-columns: 2fr 1fr 1fr auto;
  gap: 10px;
  align-items: end;
}
.sharing-table {
  overflow-x: auto;
}
.sharing-table table {
  width: 100%;
  border-collapse: collapse;
  min-width: 540px;
}
.sharing-table th,
.sharing-table td {
  text-align: left;
  padding: 12px;
  border-bottom: 1px solid var(--line);
}
caption {
  text-align: left;
  font-weight: 600;
  padding: 10px 0;
}
small {
  display: block;
  color: var(--muted);
  margin-top: 5px;
}
.sharing-order {
  padding: 16px;
  border: 1px solid var(--line);
  border-radius: 6px;
  overflow-wrap: anywhere;
}
.sharing-order header {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
}
.sharing-confirm {
  width: min(100%, 480px);
}
.sharing-page :focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 2px;
}
@media (max-width: 640px) {
  .sharing-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .sharing-rule-row {
    grid-template-columns: 1fr;
  }
  .sharing-form {
    padding: 13px;
  }
  .sharing-order header {
    flex-direction: column;
  }
}
</style>
