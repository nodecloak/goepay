<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch, toRaw } from "vue";
import { useRouter } from "vue-router";
import { ApiError, type ApiClient } from "../api";
import type { Area } from "../menu";
import { minorYuan } from "../pro-console-fields";
import KitFilterBar from "./KitFilterBar.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitSelect from "./KitSelect.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitPayMethod from "./KitPayMethod.vue";
import KitEmpty from "./KitEmpty.vue";
import KitModal from "./KitModal.vue";
import ShellIcon from "./ShellIcon.vue";
import { sharingMinorInput } from "../sharing-overview";
import {
  sharingRequestContext,
  type SharingRequestContext,
} from "../sharing-request-context";

// Pro 分账规则 (profitsharing/receiver) presented over GOEPAY's provider-bound
// channel-sharing API. One Pro "规则" row = one payment channel's current rule
// version with its bound receivers + ratios. The add/edit dialog's single 保存
// runs the underlying GOEPAY steps in order (create receiver draft -> bind ->
// publish a new rule version), and reports which step failed. GOEPAY's safety
// model (encrypted drafts, masked accounts, versioned immutable rules,
// funds authorization on publish) stays underneath untouched.
const props = defineProps<{
  client: ApiClient;
  area: Area;
  initialMerchantId?: string;
  initialChannelId?: string;
  initialRuleId?: string;
  autoCreate?: boolean;
}>();
const emit = defineEmits<{ busy: [value: boolean] }>();
let alive = true,
  loadEpoch = 0;
let loadController: AbortController | undefined;
const sessionScope = sharingRequestContext(
  () => ({
    client: toRaw(props.client),
    csrf: props.client.csrfToken,
    area: props.area,
  }),
  () => {
    ++loadEpoch;
    loadController?.abort();
    loadedContext = undefined;
    rows.value = [];
    resolved.value = "";
    loaded.value = false;
    loading.value = false;
    saving.value = false;
    dialogOpen.value = false;
    form.value = {
      channel_id: "",
      minimum: "0",
      methods: [],
      receivers: [{ id: "", account: "", name: "", rate: "" }],
    };
    error.value = "会话已变化，请重新查询分账规则";
    notice.value = "";
  },
);
let loadedContext: SharingRequestContext | undefined;
const capture = () => ({ ...sessionScope.capture(), owner: resolved.value });
const current = sessionScope.current;
const sameOwner = (c: ReturnType<typeof capture>) =>
  current(c) && c.owner === resolved.value;

const router = useRouter();

// Pro 记录 row link: open 分账记录 (profitsharing/order) filtered to this rule's
// id, mirroring epay_pro's 记录 → /profitsharing/order?rid=. The order list
// reads rule_id from the route query and prefills its 分账规则ID filter.
function openRecord(row: Row) {
  if (!loadedContext || !current(loadedContext)) return;
  if (!row.rule || !positive(row.rule.id)) return;
  if (props.area === "admin" && !positive(resolved.value)) return;
  void router.push({
    path: `/${props.area}/profitsharing/order`,
    query: {
      rule_id: row.rule.id,
      ...(props.area === "admin" ? { merchant_id: resolved.value } : {}),
    },
  });
}

type Channel = {
  channel_id: string;
  provider: string;
  product: string;
  methods: string[];
  receiver_types: string[];
};
type Receiver = {
  id: string;
  channel_id: string;
  provider: string;
  type: string;
  account_masked: string;
  name_masked: string;
  state: string;
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
// One list row: a channel plus its current rule and bound receivers.
type Row = {
  channel: Channel;
  rule?: Rule;
  receivers: Receiver[];
  enabled: boolean;
};

const merchant = ref("");
const resolved = ref("");
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const notice = ref("");
const reason = ref("");
const rows = ref<Row[]>([]);
const loaded = ref(false);
// Why 新增 is grey: the admin has not queried a merchant yet, or the merchant
// has no channel that supports channel profit-sharing (the server's reason).
const createHint = computed(() => {
  if (loading.value || saving.value) return "";
  if (!loaded.value)
    return props.area === "admin" ? "请先输入商户ID并查询" : "";
  if (!rows.value.length)
    return reason.value || "当前商户没有支持渠道分账的支付通道";
  return "";
});

// Pro 分账规则 list search: by 规则ID or 接收方账号 (client-side over loaded rows).
const searchField = ref("id");
const searchKeyword = ref("");
const ruleSearchFields = [
  { value: "id", label: "规则ID" },
  { value: "account", label: "接收方账号" },
];
// Pro 分账模式 实时/延迟. GOEPAY channel-sharing timing is provider-fixed
// (实时, unfreeze-on-finish), so this is shown for parity but not editable.
const shareModeOptions = [
  { value: "realtime", label: "实时分账" },
  { value: "delayed", label: "延迟分账" },
];
const shareMode = ref("realtime");
const filteredRows = computed(() => {
  const kw = searchKeyword.value.trim().toLowerCase();
  if (!kw) return rows.value;
  return rows.value.filter((row) => {
    if (searchField.value === "id")
      return !!row.rule && row.rule.id.toLowerCase().includes(kw);
    return row.receivers.some((r) =>
      r.account_masked.toLowerCase().includes(kw),
    );
  });
});

const paymentMethods: Record<string, string> = {
  native: "扫码支付",
  jsapi: "公众号支付",
  h5: "手机网站支付",
  app: "APP支付",
  scan: "付款码支付",
  page: "电脑网站支付",
  wap: "手机网站支付",
  applet: "小程序支付",
};
const receiverStates: Record<string, string> = {
  draft: "待绑定",
  binding: "绑定中",
  bound: "已绑定",
  unbinding: "解绑中",
  unbound: "已解绑",
  unknown: "结果未知",
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

const ownerQuery = () =>
  props.area === "admin"
    ? `merchant_id=${encodeURIComponent(resolved.value)}`
    : "";
const query = (path: string, extra = "") =>
  path +
  ([ownerQuery(), extra].filter(Boolean).length
    ? "?" + [ownerQuery(), extra].filter(Boolean).join("&")
    : "");

const newNo = (prefix: string) =>
  prefix +
  Array.from(crypto.getRandomValues(new Uint8Array(12)), (v) =>
    v.toString(16).padStart(2, "0"),
  ).join("");

function parseChannel(c: any): Channel {
  if (
    !positive(c?.channel_id) ||
    !["wxpay", "alipay"].includes(c.provider) ||
    !safe(c.product) ||
    !Array.isArray(c.receiver_types) ||
    !Array.isArray(c.methods)
  )
    throw Error("分账通道响应格式错误");
  return {
    channel_id: c.channel_id,
    provider: c.provider,
    product: c.product,
    methods: c.methods,
    receiver_types: c.receiver_types,
  };
}
function parseReceiver(v: any): Receiver {
  if (
    !positive(v?.id) ||
    !positive(v.channel_id) ||
    !["wxpay", "alipay"].includes(v.provider) ||
    !safe(v.type) ||
    !safe(v.account_masked) ||
    !safe(v.name_masked) ||
    !safe(v.state)
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
  };
}
function parseRule(r: any): Rule {
  if (
    !positive(r?.id) ||
    !positive(r.revision) ||
    typeof r.enabled !== "boolean" ||
    !unsigned(r.minimum_amount) ||
    !Array.isArray(r.methods) ||
    !Array.isArray(r.items)
  )
    throw Error("分账规则响应格式错误");
  return {
    id: r.id,
    revision: r.revision,
    enabled: r.enabled,
    minimum_amount: r.minimum_amount,
    methods: r.methods.slice(),
    items: r.items.map((i: any) => ({
      receiver_id: i.receiver_id,
      mode: i.mode,
      rate_bps: i.rate_bps,
      fixed_amount: i.fixed_amount,
    })),
  };
}

async function load(afterWrite = false) {
  if (!alive) return;
  if (saving.value && !afterWrite) return;
  if (props.area === "admin" && !positive(merchant.value)) {
    error.value = "请输入有效商户ID";
    return;
  }
  if (loading.value) return;
  const c = capture();
  loadController?.abort();
  const ac = new AbortController();
  loadController = ac;
  const version = ++loadEpoch,
    requested = props.area === "admin" ? merchant.value : "";
  const live = () => current(c) && version === loadEpoch && !ac.signal.aborted;
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    resolved.value = props.area === "admin" ? merchant.value : "";
    const caps: any = await props.client.request(
      query("/channel-sharing/channels"),
      { signal: ac.signal },
    );
    if (!live()) return;
    if (!positive(caps?.merchant_id) || !Array.isArray(caps.channels))
      throw Error("分账通道响应格式错误");
    if (props.area === "admin" && caps.merchant_id !== requested)
      throw Error("分账通道商户归属不匹配");
    resolved.value = caps.merchant_id;
    reason.value = safe(caps.reason) ? caps.reason : "";
    const channels: Channel[] = caps.channels.map(parseChannel);
    // Read all bound receivers once, grouped by channel.
    const receivers: Receiver[] = [];
    let cursor = "0";
    const seen = new Set<string>();
    do {
      if (seen.has(cursor) || seen.size >= 100)
        throw Error("接收方分页无效，请缩小查询范围");
      seen.add(cursor);
      const page: any = await props.client.request(
        query(
          "/channel-sharing/receivers",
          `limit=100${cursor !== "0" ? `&cursor=${cursor}` : ""}`,
        ),
        { signal: ac.signal },
      );
      if (!live()) return;
      if (!Array.isArray(page?.items) || !unsigned(page.next_cursor))
        throw Error("接收方列表响应格式错误");
      receivers.push(...page.items.map(parseReceiver));
      cursor = page.next_cursor;
    } while (cursor !== "0");
    // Each channel -> its current rule (if any) + its receivers.
    const out: Row[] = [];
    for (const channel of channels) {
      let rule: Rule | undefined;
      try {
        rule = parseRule(
          await props.client.request(
            query(`/channel-sharing/rules/${channel.channel_id}`),
            { signal: ac.signal },
          ),
        );
        if (!live()) return;
      } catch (e) {
        if (!(e instanceof ApiError && e.status === 404)) throw e;
      }
      out.push({
        channel,
        rule,
        receivers: receivers.filter((r) => r.channel_id === channel.channel_id),
        enabled: !!rule?.enabled,
      });
    }
    if (!live()) return;
    rows.value = out;
    loadedContext = c;
    loaded.value = true;
  } catch (e) {
    if (live()) {
      rows.value = [];
      loaded.value = false;
      error.value = e instanceof Error ? e.message : "读取分账规则失败";
    }
  } finally {
    if (live()) loading.value = false;
  }
}

// ---- add / edit dialog ----
const dialogOpen = ref(false);
watch(
  [loading, saving, dialogOpen],
  () => emit("busy", loading.value || saving.value || dialogOpen.value),
  { flush: "sync" },
);
const dialogMode = ref<"add" | "edit">("add");
type DraftReceiver = {
  id: string;
  account: string;
  name: string;
  rate: string;
  mode?: string;
  fixed_amount?: string;
  type?: string;
};
const form = ref<{
  channel_id: string;
  minimum: string;
  methods: string[];
  receivers: DraftReceiver[];
}>({
  channel_id: "",
  minimum: "0",
  methods: [],
  receivers: [{ id: "", account: "", name: "", rate: "" }],
});
const editingRow = ref<Row>();

const channelOptions = computed(() =>
  rows.value.map((r) => ({
    value: r.channel.channel_id,
    label: `${r.channel.channel_id} · ${r.channel.provider === "wxpay" ? "微信" : "支付宝"}`,
  })),
);
const selectedChannel = computed(
  () =>
    rows.value.find((r) => r.channel.channel_id === form.value.channel_id)
      ?.channel,
);
const receiverHint = computed(() => {
  const c = selectedChannel.value;
  if (!c) return "";
  return c.provider === "wxpay"
    ? "接收方账号填写微信商户号或个人OpenID"
    : "接收方账号填写支付宝UID（2088开头）或支付宝账号";
});
const calculationModes = [
  { value: "rate", label: "按比例" },
  { value: "fixed", label: "固定金额" },
];
const receiverTypeOptions = computed(() =>
  (selectedChannel.value?.receiver_types || []).map((value) => ({
    value,
    label:
      (
        {
          MERCHANT_ID: "微信商户号",
          PERSONAL_OPENID: "微信 OpenID",
          userId: "支付宝 UID",
          loginName: "支付宝账号",
          openId: "支付宝 OpenID",
        } as Record<string, string>
      )[value] || value,
  })),
);
watch(
  () => form.value.channel_id,
  (value, old) => {
    if (dialogOpen.value && dialogMode.value === "add" && value !== old) {
      form.value.methods = selectedChannelMethods(value);
      form.value.receivers = [{ id: "", account: "", name: "", rate: "" }];
    }
  },
);

function openAdd() {
  if (!loadedContext || !current(loadedContext)) return;
  dialogMode.value = "add";
  editingRow.value = undefined;
  form.value = {
    channel_id: rows.value[0]?.channel.channel_id || "",
    minimum: "0",
    methods: selectedChannelMethods(rows.value[0]?.channel.channel_id || ""),
    receivers: [{ id: "", account: "", name: "", rate: "" }],
  };
  error.value = "";
  dialogOpen.value = true;
}
function selectedChannelMethods(channelId: string): string[] {
  return (
    rows.value
      .find((r) => r.channel.channel_id === channelId)
      ?.channel.methods.slice() || []
  );
}
function openEdit(row: Row) {
  if (!loadedContext || !current(loadedContext)) return;
  dialogMode.value = "edit";
  editingRow.value = row;
  const ratePct = (item?: RuleItem, r?: Receiver) => {
    if (!item) return "";
    return item.mode === "rate" ? (Number(item.rate_bps) / 100).toString() : "";
  };
  const receivers: DraftReceiver[] = row.receivers
    .filter((r) => r.state === "bound")
    .map((r) => {
      const item = row.rule?.items.find((i) => i.receiver_id === r.id);
      return {
        id: r.id,
        account: r.account_masked,
        name: r.name_masked,
        rate: ratePct(item, r),
        type: r.type,
        mode: item?.mode || "rate",
        fixed_amount:
          item?.mode === "fixed" ? minorYuan(item.fixed_amount) : "",
      };
    });
  form.value = {
    channel_id: row.channel.channel_id,
    minimum: row.rule?.minimum_amount || "0",
    methods: row.rule?.methods.slice() || row.channel.methods.slice(),
    receivers: receivers.length
      ? receivers
      : [{ id: "", account: "", name: "", rate: "" }],
  };
  error.value = "";
  dialogOpen.value = true;
}
function addReceiverRow() {
  if (form.value.receivers.length >= 10) {
    error.value = "最多只能添加10个分账接收方";
    return;
  }
  form.value.receivers.push({ id: "", account: "", name: "", rate: "" });
}
function removeReceiverRow(i: number) {
  if (form.value.receivers.length <= 1) return;
  form.value.receivers.splice(i, 1);
}

function rateToBps(rate: string): string {
  if (!/^(0|[1-9][0-9]*)(\.[0-9]{1,2})?$/.test(rate))
    throw Error("分账比例填写不正确");
  const [a, b = ""] = rate.split(".");
  const bps = Number(a) * 100 + Number(b.padEnd(2, "0"));
  if (bps <= 0) throw Error("分账比例必须大于0");
  return String(bps);
}

async function save() {
  if (!loadedContext || !current(loadedContext)) return;
  if (saving.value) return;
  const channel = form.value.channel_id;
  const row = rows.value.find((r) => r.channel.channel_id === channel);
  if (!channel || !row) {
    error.value = "请选择支付通道";
    return;
  }
  if (
    form.value.receivers.some(
      (r) =>
        (r.account || r.name || r.rate || r.fixed_amount) &&
        (!r.account || !(r.mode === "fixed" ? r.fixed_amount : r.rate)),
    )
  ) {
    error.value = "请完整填写接收方账号和分账比例或金额";
    return;
  }
  const entries = form.value.receivers
    .filter((r) => r.account && (r.mode === "fixed" ? r.fixed_amount : r.rate))
    .map((r) => ({ ...r }));
  if (!entries.length) {
    error.value = "请至少添加一个有效的分账接收方";
    return;
  }
  // Validate ratios sum to <= 100%.
  let items: RuleItem[];
  try {
    items = entries.map((e) =>
      e.mode === "fixed"
        ? {
            receiver_id: e.id,
            mode: "fixed",
            rate_bps: "0",
            fixed_amount: sharingMinorInput(e.fixed_amount || "", "分账金额"),
          }
        : {
            receiver_id: e.id,
            mode: "rate",
            rate_bps: rateToBps(e.rate),
            fixed_amount: "0",
          },
    );
    if (
      items.reduce((sum, item) => sum + BigInt(item.fixed_amount), 0n) >
      999999999999n
    )
      throw Error("固定分账金额总和超过上限");
  } catch (e) {
    error.value = e instanceof Error ? e.message : "分账比例填写不正确";
    return;
  }
  const totalBps = items.reduce((n, i) => n + Number(i.rate_bps), 0);
  if (totalBps > 10000) {
    error.value = "分账比例总和不能超过100%";
    return;
  }
  const c = capture();
  const owner = props.area === "admin" ? { merchant_id: c.owner } : {};
  if (
    !unsigned(form.value.minimum || "0") ||
    BigInt(form.value.minimum || "0") > 999999999999n ||
    !form.value.methods.length
  ) {
    error.value = "请核对最小金额并选择至少一种支付方式";
    return;
  }
  const minimum = form.value.minimum || "0",
    methods = form.value.methods.slice(),
    provider = selectedChannel.value?.provider;
  saving.value = true;
  error.value = "";
  try {
    // Step 1: create + bind any new receiver drafts (those without an id).
    for (let i = 0; i < items.length; i++) {
      const entry = entries[i]!;
      if (!sameOwner(c)) return;
      if (entry.id) continue;
      let created: Receiver;
      try {
        const type = entry.type || row.channel.receiver_types[0];
        if (!type || !row.channel.receiver_types.includes(type))
          throw Error("当前通道不支持所选接收方类型");
        created = parseReceiver(
          await props.client.request("/channel-sharing/receivers", {
            method: "POST",
            body: {
              ...owner,
              channel_id: channel,
              external_no: newNo("receiver_"),
              type,
              account: entry.account,
              name: entry.name,
              ...(provider === "wxpay" ? { relation_type: "PARTNER" } : {}),
            },
          }),
        );
        if (!sameOwner(c)) return;
      } catch (e) {
        throw Error(
          `保存接收方「${entry.account}」失败：${(e as Error).message}`,
        );
      }
      try {
        const bound = parseReceiver(
          await props.client.request(
            `/channel-sharing/receivers/${created.id}/bind`,
            {
              method: "POST",
              body: { ...owner, external_no: newNo("bind_") },
            },
          ),
        );
        if (!sameOwner(c)) return;
        items[i]!.receiver_id = bound.id;
      } catch (e) {
        throw Error(
          `绑定接收方「${entry.account}」失败，草稿已保存，请在列表中单独绑定后重试：${(e as Error).message}`,
        );
      }
    }
    // Step 2: publish a new rule version with all receivers + ratios.
    if (!sameOwner(c)) return;
    const published: any = await props.client.request(
      `/channel-sharing/rules/${channel}`,
      {
        method: "POST",
        body: {
          ...owner,
          expected: row.rule?.id || "0",
          enabled: true,
          minimum_amount: minimum,
          methods,
          items,
        },
      },
    );
    if (!sameOwner(c)) return;
    notice.value = `分账规则已保存（版本 ${parseRule(published).revision}）`;
    dialogOpen.value = false;
    await load(true);
  } catch (e) {
    if (current(c)) error.value = e instanceof Error ? e.message : "保存失败";
  } finally {
    if (current(c)) saving.value = false;
  }
}

async function toggleEnabled(row: Row) {
  if (!loadedContext || !current(loadedContext)) return;
  if (saving.value || !row.rule) return;
  const c = capture(),
    owner = props.area === "admin" ? { merchant_id: c.owner } : {};
  saving.value = true;
  error.value = "";
  notice.value = "";
  try {
    if (row.enabled) {
      await props.client.request(
        `/channel-sharing/rules/${row.channel.channel_id}/disable`,
        {
          method: "POST",
          body: { ...owner, expected: row.rule.id },
        },
      );
      notice.value = "已关闭该通道分账规则";
    } else {
      await props.client.request(
        `/channel-sharing/rules/${row.channel.channel_id}`,
        {
          method: "POST",
          body: {
            ...owner,
            expected: row.rule.id,
            enabled: true,
            minimum_amount: row.rule.minimum_amount,
            methods: row.rule.methods.slice(),
            items: row.rule.items.slice(),
          },
        },
      );
      notice.value = "已开启该通道分账规则";
    }
    if (!sameOwner(c)) return;
    await load(true);
  } catch (e) {
    if (current(c)) error.value = e instanceof Error ? e.message : "操作失败";
  } finally {
    if (current(c)) saving.value = false;
  }
}

onMounted(async () => {
  if (props.area === "admin" && positive(props.initialMerchantId))
    merchant.value = props.initialMerchantId;
  if (props.area === "merchant" || positive(props.initialMerchantId)) {
    await load();
    if (!alive || !loaded.value) return;
    if (props.autoCreate && rows.value.length) openAdd();
    else if (positive(props.initialChannelId)) {
      const row = rows.value.find(
        (r) => r.channel.channel_id === props.initialChannelId,
      );
      if (!row) error.value = "该通道当前不可配置，可返回总览查看已保存规则";
      else if (props.initialRuleId && row.rule?.id !== props.initialRuleId)
        error.value = "规则版本已变化，请返回总览刷新后核对";
      else openEdit(row);
    }
  }
});
onBeforeUnmount(() => {
  alive = false;
  sessionScope.dispose();
  ++loadEpoch;
  loadController?.abort();
});

function onReset() {
  searchField.value = "id";
  searchKeyword.value = "";
  if (props.area === "admin" && !props.initialMerchantId) merchant.value = "";
  else void load();
}
</script>

<template>
  <div class="list-page">
    <KitFilterBar :loading="loading" @query="load" @reset="onReset">
      <KitField v-if="area === 'admin'" label="商户ID">
        <KitInput
          v-model="merchant"
          inputmode="numeric"
          placeholder="输入商户ID"
          aria-label="商户ID"
          :disabled="!!initialMerchantId || loading || saving"
        />
      </KitField>
      <KitField label="搜索字段">
        <KitSelect
          v-model="searchField"
          :options="ruleSearchFields"
          aria-label="搜索字段"
        />
      </KitField>
      <KitField label="搜索内容">
        <KitInput
          v-model="searchKeyword"
          placeholder="规则ID或接收方账号"
          aria-label="搜索内容"
        />
      </KitField>
    </KitFilterBar>

    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <p v-if="reason && rows.length" class="muted" role="status">
      {{ reason }}
    </p>

    <section class="panel records sharing-rules" aria-label="分账规则">
      <div class="table-tools">
        <button
          type="button"
          class="pa-btn pa-btn--primary"
          data-action="create"
          :disabled="loading || saving || !loaded || !rows.length"
          @click="openAdd"
        >
          <ShellIcon name="plus" />新增
        </button>
        <span v-if="createHint" class="create-hint table-summary" role="note">{{
          createHint
        }}</span>
      </div>

      <div class="table-scroll">
        <table class="pa-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>支付通道</th>
              <th>分账接收方+分账比例</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in filteredRows" :key="row.channel.channel_id">
              <td class="pa-c-num">{{ row.rule ? row.rule.id : "-" }}</td>
              <td>
                <KitPayMethod :type="row.channel.provider" />
                <small class="muted">#{{ row.channel.channel_id }}</small>
              </td>
              <td>
                <template v-if="row.rule && row.rule.items.length">
                  <div
                    v-for="item in row.rule.items"
                    :key="item.receiver_id"
                    class="sharing-recv"
                  >
                    <span>{{
                      row.receivers.find((r) => r.id === item.receiver_id)
                        ?.account_masked || "接收方" + item.receiver_id
                    }}</span>
                    <span class="rate">{{
                      item.mode === "rate"
                        ? (Number(item.rate_bps) / 100).toFixed(2) + "%"
                        : "¥" + money(item.fixed_amount)
                    }}</span>
                  </div>
                </template>
                <span v-else class="muted">-</span>
              </td>
              <td>
                <KitStatusTag
                  :text="
                    row.rule ? (row.enabled ? '已开启' : '已关闭') : '未配置'
                  "
                  :tone="
                    row.rule ? (row.enabled ? 'success' : 'neutral') : 'info'
                  "
                />
              </td>
              <td>
                <div class="pa-row-actions">
                  <button
                    type="button"
                    class="pa-btn"
                    data-action="edit"
                    :disabled="saving"
                    @click="openEdit(row)"
                  >
                    {{ row.rule ? "编辑" : "配置" }}
                  </button>
                  <button
                    v-if="row.rule"
                    type="button"
                    class="pa-btn"
                    :class="row.enabled ? 'pa-btn--danger' : 'pa-btn--success'"
                    data-action="toggle"
                    :disabled="saving"
                    @click="toggleEnabled(row)"
                  >
                    {{ row.enabled ? "关闭" : "开启" }}
                  </button>
                  <button
                    v-if="row.rule"
                    type="button"
                    class="pa-btn"
                    data-action="record"
                    :disabled="saving"
                    title="查看该规则的分账记录"
                    @click="openRecord(row)"
                  >
                    记录
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="loaded && !filteredRows.length">
              <td colspan="5"><KitEmpty text="暂无数据" /></td>
            </tr>
            <tr v-if="!loaded">
              <td colspan="5">
                <KitEmpty
                  :text="area === 'admin' ? '请输入商户ID后查询' : '暂无数据'"
                />
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <KitModal
        v-if="dialogOpen"
        :title="dialogMode === 'add' ? '新增分账规则' : '修改分账规则'"
        :loading="saving"
        drawer
        width="720px"
        @close="dialogOpen = false"
        @submit="save"
      >
        <KitField label="支付通道" required>
          <KitSelect
            v-model="form.channel_id"
            :options="channelOptions"
            placeholder="请选择支付通道"
            :disabled="saving || dialogMode === 'edit'"
            aria-label="支付通道"
          />
        </KitField>
        <KitField label="订单最小金额">
          <KitInput
            v-model="form.minimum"
            inputmode="numeric"
            placeholder="订单金额（分）超过该值才分账，0为不限制"
            aria-label="订单最小金额"
            :disabled="saving"
          />
        </KitField>
        <KitField label="分账模式">
          <KitSelect
            v-model="shareMode"
            :options="shareModeOptions"
            disabled
            aria-label="分账模式"
          />
        </KitField>
        <div class="sharing-recv-head">分账接收方列表</div>
        <table class="pa-table pa-table--compact">
          <thead>
            <tr>
              <th>接收方账号</th>
              <th>接收方姓名</th>
              <th>分账比例/金额</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(r, i) in form.receivers" :key="i">
              <td>
                <KitSelect
                  :model-value="r.type || receiverTypeOptions[0]?.value || ''"
                  :options="receiverTypeOptions"
                  :disabled="saving || !!r.id"
                  aria-label="接收方类型"
                  @update:model-value="r.type = $event"
                />
                <KitInput
                  v-model="r.account"
                  placeholder="请输入"
                  :disabled="saving || !!r.id"
                  aria-label="接收方账号"
                />
              </td>
              <td>
                <KitInput
                  v-model="r.name"
                  placeholder="可留空"
                  :disabled="saving || !!r.id"
                  aria-label="接收方姓名"
                />
              </td>
              <td>
                <KitSelect
                  :model-value="r.mode || 'rate'"
                  :options="calculationModes"
                  :disabled="saving"
                  aria-label="计算方式"
                  @update:model-value="r.mode = $event"
                />
                <KitInput
                  v-if="r.mode === 'fixed'"
                  :model-value="r.fixed_amount || ''"
                  @update:model-value="r.fixed_amount = $event"
                  inputmode="decimal"
                  placeholder="元"
                  aria-label="分账金额（元）"
                  :disabled="saving"
                />
                <KitInput
                  v-else
                  v-model="r.rate"
                  placeholder="1-100"
                  inputmode="decimal"
                  aria-label="分账比例"
                  :disabled="saving"
                />
              </td>
              <td>
                <button
                  type="button"
                  class="pa-btn pa-btn--danger"
                  :disabled="saving || form.receivers.length <= 1"
                  @click="removeReceiverRow(i)"
                >
                  删除
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <div class="sharing-add-recv">
          <button
            type="button"
            class="pa-btn pa-btn--success"
            :disabled="saving || form.receivers.length >= 10"
            @click="addReceiverRow"
          >
            增加接收方
          </button>
        </div>
        <p v-if="receiverHint" class="muted">{{ receiverHint }}</p>
        <p class="muted">
          分账比例总和不能超过100%；保存后按顺序保存接收方、绑定并发布新规则版本，任一步失败会提示。
        </p>
        <template #footer-extra>
          <span class="muted">分账比例一般最高30%</span>
        </template>
      </KitModal>
    </section>
  </div>
</template>

<style scoped>
.create-hint {
  color: var(--ink-muted);
  font-size: 12px;
}
/* No pager under this table: the card edge closes the last row. */
.sharing-rules tbody tr:last-child td {
  border-bottom: 0;
}
.sharing-recv {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  line-height: 1.6;
}
.sharing-recv .rate {
  color: var(--primary, #4f46e5);
  font-weight: 600;
}
.sharing-recv-head {
  font-weight: 600;
  text-align: center;
  margin: 8px 0 4px;
}
.sharing-add-recv {
  text-align: center;
  margin: 8px 0;
}
.pa-row-actions {
  display: flex;
  gap: 8px;
}
td small.muted {
  display: block;
}
</style>
