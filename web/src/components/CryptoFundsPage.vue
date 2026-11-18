<script setup lang="ts">
import {
  computed,
  inject,
  nextTick,
  onBeforeUnmount,
  ref,
  toRaw,
  watch,
} from "vue";
import { ApiClient, ApiError, type User } from "../api";
import type { CryptoSection } from "../menu";
import CryptoOperationsMonitor from "./CryptoOperationsMonitor.vue";
import {
  readCryptoChannelOptions,
  usdtUnits,
  type CryptoChannelOption,
} from "../crypto-channels";
import { fundsPrincipal212, fundsScope212 } from "../funds-intent212";
import {
  CryptoIntentStore,
  cryptoIntentRefused,
  verifyCryptoIntentRecord,
  type CryptoIntent,
} from "../crypto-intent";

const props = withDefaults(
  defineProps<{
    client: ApiClient;
    ownSession: boolean;
    adminClass?: User["admin_class"];
    section?: CryptoSection;
    initialChannelID?: string;
  }>(),
  { section: "overview" },
);
type Network = { network: string; enabled: boolean; fee_bps: number };
type Balance = { network: string; available: string; reserved: string };
type Invoice = {
  id: string;
  merchant_id: string;
  out_trade_no: string;
  network: string;
  amount: string;
  received: string;
  fee: string;
  state: string;
  address: string;
  expires_at: string;
  checkout_url?: string;
  channel_id?: string;
  channel_revision?: string;
  fee_bps?: number;
  fee_source?: string;
};
type Withdrawal = {
  id: string;
  merchant_id: string;
  request_id: string;
  network: string;
  address: string;
  amount: string;
  state: string;
  kind: string;
  tx_id: string;
  cancellation_requested?: boolean;
};
type Recipient = {
  id: string;
  merchant_id: string;
  network: string;
  address: string;
  state: string;
  usable_at: string;
};
const recipients = ref<Recipient[]>([]),
  recipient = ref({ network: "tron", address: "" }),
  approveRecipientID = ref("");
const cancelID = ref(""),
  revokeID = ref("");
const networks = ref<Network[]>([]),
  networkStatus = ref<Network[]>([]),
  balances = ref<Balance[]>([]),
  invoices = ref<Invoice[]>([]),
  withdrawals = ref<Withdrawal[]>([]);
const loading = ref(false),
  busy = ref(false),
  enabled = ref(false),
  error = ref(""),
  notice = ref(""),
  uncertain = ref(false);
const simulation = ref(false);
const serviceState = ref<"loading" | "disabled" | "ready" | "error">("loading");
const principal = inject(fundsPrincipal212, undefined);
let intentStore: CryptoIntentStore | null = null;
const pendingIntent = ref<CryptoIntent | null>(null),
  restoring = ref(!!principal),
  storageError = ref("");
const heldRefund = ref(false);
const merchantID = ref(""),
  checkoutURL = ref(""),
  password = ref(""),
  approveID = ref(""),
  reconciliation = ref<{
    imbalanced_journals: number;
    balance_mismatches: number;
    negative_balances: number;
  } | null>(null);
const invoice = ref({
  out_trade_no: "",
  channel_id: "",
  network: "tron",
  amount: "",
  notify_url: "",
  return_url: "",
});
const withdrawal = ref<{
  request_id: string;
  network: string;
  address: string;
  amount: string;
}>({
  request_id: crypto.randomUUID(),
  network: "tron",
  address: "",
  amount: "",
});
const refund = ref<{
  request_id: string;
  invoice_id: string;
  address: string;
  amount: string;
}>({
  request_id: crypto.randomUUID(),
  invoice_id: "",
  address: "",
  amount: "",
});
const address = ref({ network: "tron", address: "", role: "deposit" });
const sweepRetry = ref({ network: "tron", address: "" });
const admin = computed(() => props.client.area === "admin");
const showMonitoring = computed(
  () =>
    admin.value &&
    props.adminClass === "super" &&
    props.ownSession &&
    props.section === "operations",
);
const monitoringRefresh = ref(0);
const invoiceChannels = ref<CryptoChannelOption[]>([]),
  channelLoading = ref(false),
  channelError = ref(""),
  channelScope = ref("");
let channelEpoch = 0;
const selectedInvoiceChannel = computed(() =>
  invoiceChannels.value.find((row) => row.id === invoice.value.channel_id),
);
const canCreateInvoice = computed(
  () =>
    canWrite.value &&
    !channelLoading.value &&
    !channelError.value &&
    channelScope.value === merchantID.value &&
    !!selectedInvoiceChannel.value,
);
const visibleInvoices = computed(() =>
  props.initialChannelID
    ? invoices.value.filter((row) => row.channel_id === props.initialChannelID)
    : invoices.value,
);
async function loadInvoiceChannels() {
  const run = ++channelEpoch,
    c = context(),
    owner = merchantID.value;
  invoiceChannels.value = [];
  channelError.value = "";
  channelScope.value = "";
  if (admin.value && !/^[1-9]\d{0,18}$/.test(owner)) {
    channelError.value = "请先填写商户 ID，以读取该商户的可用通道";
    channelLoading.value = false;
    return;
  }
  channelLoading.value = true;
  try {
    const query = admin.value
      ? "?merchant_id=" + encodeURIComponent(owner)
      : "";
    const rows = readCryptoChannelOptions(
      await c.client.request("/crypto/channels" + query),
    );
    if (!currentView(c) || run !== channelEpoch || owner !== merchantID.value)
      return;
    invoiceChannels.value = rows;
    channelScope.value = owner;
    if (
      !invoice.value.channel_id &&
      !invoice.value.out_trade_no &&
      !invoice.value.amount
    )
      invoice.value.channel_id = rows[0]?.id || "";
  } catch {
    if (currentView(c) && run === channelEpoch)
      channelError.value = "收款通道暂时无法读取，请关闭表单后重试";
  } finally {
    if (currentView(c) && run === channelEpoch) channelLoading.value = false;
  }
}
const headings: Record<CryptoSection, { title: string; description: string }> =
  {
    overview: {
      title: "USDT 资金概览",
      description: "查看各网络的可用与预留余额，收款与结算按原链分别核算。",
    },
    invoices: {
      title: "USDT 收款",
      description: "查看收款记录，按需创建收款单并核对到账结果。",
    },
    settlements: {
      title: "USDT 结算",
      description: "查看原链结算记录，核对地址与金额后申请出账。",
    },
    refunds: {
      title: "USDT 退币",
      description: "查看已入账订单与未入账款项的退币记录。",
    },
    recipients: {
      title: "结算与退币地址",
      description: "查看地址审批与冷静期，按需申请绑定或撤销地址。",
    },
    operations: {
      title: "地址池与归集",
      description: "查看通道日额度、收款地址余量与归集记录。",
    },
    reconciliation: {
      title: "USDT 账本对账",
      description: "核对账簿平衡、余额流水与异常负余额。",
    },
  };
type FormKind =
  "invoice" | "withdrawal" | "refund" | "recipient" | "address" | "sweep-retry";
const activeForm = ref<FormKind | "">("");
const actionPanel = ref<HTMLElement | null>(null);
const filteredWithdrawals = computed(() =>
  withdrawals.value.filter((w) =>
    props.section === "settlements"
      ? w.kind === "settlement"
      : props.section === "refunds"
        ? w.kind === "refund" || w.kind === "exception_refund"
        : props.section === "operations" && admin.value
          ? w.kind === "sweep"
          : false,
  ),
);
const readReady = computed(() => serviceState.value === "ready");
const showForms = computed(
  () => readReady.value && enabled.value && props.ownSession,
);
const canWrite = computed(
  () =>
    showForms.value &&
    !busy.value &&
    !loading.value &&
    !restoring.value &&
    !storageError.value &&
    !pendingIntent.value &&
    !uncertain.value,
);
const label = (network: string) =>
  network === "tron"
    ? "TRON · TRC20"
    : network === "ethereum"
      ? "Ethereum · ERC20"
      : network;
const usableRecipients = (network?: string) =>
  recipients.value.filter(
    (r) =>
      r.state === "approved" &&
      new Date(r.usable_at).getTime() <= Date.now() &&
      (!network || r.network === network) &&
      (!admin.value || r.merchant_id === merchantID.value),
  );
const states: Record<string, string> = {
  pending: "待处理",
  confirming: "确认中",
  paid: "已到账",
  underpaid: "少付待核对",
  overpaid: "超付待核对",
  expired: "已过期",
  late: "逾期到账待核对",
  approved: "已批准",
  submitted: "已提交链上",
  confirmed: "已确认",
  unknown: "结果未知",
  failed: "失败",
  requested: "待审批",
  preparing: "准备中",
  prepared: "已准备",
  signing: "签名中",
  signed: "已签名",
  broadcasting: "链上广播中",
  cancelled: "已取消",
  revoked: "已撤销",
};
let epoch = 0,
  sectionEpoch = 0,
  loadEpoch = 0,
  alive = true,
  controller: AbortController | undefined;
function context() {
  return {
    client: toRaw(props.client),
    area: props.client.area,
    csrf: props.client.csrfToken,
    epoch,
    sectionEpoch,
  };
}
function currentView(c: ReturnType<typeof context>) {
  return current(c) && c.sectionEpoch === sectionEpoch;
}
function closeActions() {
  channelEpoch++;
  invoiceChannels.value = [];
  channelLoading.value = false;
  activeForm.value = "";
  password.value = "";
  checkoutURL.value = "";
  approveID.value = "";
  approveRecipientID.value = "";
  cancelID.value = "";
  revokeID.value = "";
}
function openForm(kind: FormKind) {
  closeActions();
  activeForm.value = kind;
  if (kind === "invoice") void loadInvoiceChannels();
}
async function selectAction(
  kind: "approve" | "recipient-approve" | "cancel" | "revoke",
  id: string,
  owner?: string,
) {
  if (!canWrite.value) return;
  const c = context();
  closeActions();
  if (kind === "approve") approveID.value = id;
  else if (kind === "recipient-approve") approveRecipientID.value = id;
  else if (kind === "cancel") cancelID.value = id;
  else revokeID.value = id;
  if (owner) merchantID.value = owner;
  await nextTick();
  if (!currentView(c)) return;
  actionPanel.value?.scrollIntoView?.({ block: "center", behavior: "auto" });
  actionPanel.value
    ?.querySelector<HTMLInputElement>("input[type=password]")
    ?.focus({ preventScroll: true });
}
function cancelForm() {
  if (busy.value) return;
  const kind = activeForm.value;
  closeActions();
  if (kind === "invoice")
    invoice.value = {
      out_trade_no: "",
      channel_id: "",
      network: networks.value[0]?.network || "tron",
      amount: "",
      notify_url: "",
      return_url: "",
    };
  if (
    kind === "withdrawal" &&
    pendingIntent.value?.target !== "/crypto/withdrawals"
  )
    withdrawal.value = { ...withdrawal.value, address: "", amount: "" };
  if (kind === "refund" && !pendingIntent.value?.target.includes("refund")) {
    refund.value = { ...refund.value, invoice_id: "", address: "", amount: "" };
    heldRefund.value = false;
  }
  if (kind === "recipient") recipient.value.address = "";
  if (kind === "address")
    address.value = { ...address.value, address: "", role: "deposit" };
  if (kind === "sweep-retry") sweepRetry.value.address = "";
}
function current(c: ReturnType<typeof context>) {
  return (
    alive &&
    c.epoch === epoch &&
    c.client === toRaw(props.client) &&
    c.area === props.client.area &&
    c.csrf === props.client.csrfToken
  );
}
function scope() {
  if (!admin.value) return "";
  if (merchantID.value && !/^[1-9]\d*$/.test(merchantID.value))
    throw new Error("商户 ID 格式不正确");
  return merchantID.value
    ? `?merchant_id=${encodeURIComponent(merchantID.value)}`
    : "";
}
function target() {
  if (!admin.value) return {};
  if (!/^[1-9]\d*$/.test(merchantID.value)) throw new Error("请先选择商户 ID");
  return { merchant_id: merchantID.value };
}
function amount(value: string) {
  if (
    !/^(?:0|[1-9]\d{0,12})(?:\.\d{1,6})?$/.test(value) ||
    !/[1-9]/.test(value)
  )
    throw new Error("USDT 金额须大于零，最多六位小数");
}
function message(e: unknown) {
  return e instanceof Error ? e.message : "请求失败，请刷新核对";
}
async function initializeIntent() {
  const c = context();
  intentStore = null;
  pendingIntent.value = null;
  storageError.value = "";
  restoring.value = !!principal;
  try {
    const p = principal?.value;
    if (!p) return;
    if (p.area !== c.client.area || p.csrf !== c.csrf)
      throw Error("本人会话上下文尚未核实");
    const store = new CryptoIntentStore(await fundsScope212(p)),
      original = await store.read();
    if (!current(c)) return;
    intentStore = store;
    pendingIntent.value = original;
    if (original) {
      const b = original.body;
      uncertain.value = true;
      if (admin.value) merchantID.value = b.merchant_id!;
      if (original.target === "/crypto/withdrawals")
        withdrawal.value = {
          request_id: b.request_id!,
          network: b.network!,
          address: b.address!,
          amount: b.amount!,
        };
      else {
        refund.value = {
          request_id: b.request_id!,
          invoice_id: b.invoice_id!,
          address: b.address!,
          amount: b.amount!,
        };
        heldRefund.value = original.target === "/crypto/held-refunds";
      }
    }
  } catch (e) {
    if (current(c)) {
      storageError.value = message(e);
      uncertain.value = true;
    }
  } finally {
    if (current(c)) restoring.value = false;
  }
}
async function recoverOriginal() {
  const original = pendingIntent.value,
    store = intentStore,
    c = context(),
    run = loadEpoch;
  if (!original || !store || !readReady.value) return;
  try {
    const record = await c.client.request(
      `/crypto/withdrawals/by-request?merchant_id=${encodeURIComponent(original.body.merchant_id!)}&request_id=${encodeURIComponent(original.body.request_id!)}`,
    );
    const verified = verifyCryptoIntentRecord(record, original);
    if (
      !currentView(c) ||
      run !== loadEpoch ||
      pendingIntent.value?.digest !== original.digest
    )
      return;
    await store.clear(original);
    if (!currentView(c) || run !== loadEpoch) return;
    pendingIntent.value = null;
    uncertain.value = false;
    notice.value = `已找回原请求 ${verified.request_id}，当前状态：${states[verified.state] || verified.state}；未发起新的申请。`;
  } catch (e) {
    if (currentView(c) && run === loadEpoch)
      error.value = `原请求 ${original.body.request_id} 仍待核对：${message(e)}。不会自动更换请求号或重新提交。`;
  }
}
function chooseDefaultNetwork(form: { network: string }, untouched: boolean) {
  if (
    untouched &&
    networks.value.length &&
    !networks.value.some((n) => n.network === form.network)
  )
    form.network = networks.value[0]!.network;
}
function clearReadData() {
  balances.value = [];
  invoices.value = [];
  withdrawals.value = [];
  recipients.value = [];
  checkoutURL.value = "";
  password.value = "";
  reconciliation.value = null;
  activeForm.value = "";
  approveID.value = "";
  approveRecipientID.value = "";
  cancelID.value = "";
  revokeID.value = "";
  notice.value = "";
}
function refresh() {
  monitoringRefresh.value++;
  return load();
}
async function load() {
  controller?.abort();
  controller = new AbortController();
  const c = context(),
    run = ++loadEpoch;
  loading.value = true;
  serviceState.value = "loading";
  enabled.value = false;
  networks.value = [];
  networkStatus.value = [];
  simulation.value = false;
  clearReadData();
  error.value = "";
  try {
    const options = { signal: controller.signal };
    const config = await c.client.request<{
      configured?: boolean;
      enabled: boolean;
      networks: Network[];
      simulation?: boolean;
    }>("/crypto/config", options);
    if (!currentView(c) || run !== loadEpoch) return;
    const configured =
      config.configured ?? (config.enabled || config.networks.length > 0);
    if (!configured) {
      serviceState.value = "disabled";
      return;
    }
    const query = scope();
    const [b, i, w, recipientsResult] = await Promise.all([
      c.client.request<Balance[]>("/crypto/balances" + query, options),
      c.client.request<Invoice[]>(
        "/crypto/invoices" +
          query +
          (props.initialChannelID
            ? (query ? "&" : "?") +
              "channel_id=" +
              encodeURIComponent(props.initialChannelID)
            : ""),
        options,
      ),
      c.client.request<Withdrawal[]>("/crypto/withdrawals" + query, options),
      c.client.request<Recipient[]>("/crypto/recipients" + query, options),
    ]);
    if (!currentView(c) || run !== loadEpoch) return;
    enabled.value = config.enabled;
    networkStatus.value = config.networks;
    networks.value = config.networks.filter((n) => n.enabled);
    simulation.value = config.simulation === true;
    balances.value = b || [];
    invoices.value = i || [];
    withdrawals.value = w || [];
    recipients.value = recipientsResult || [];
    serviceState.value = "ready";
    if (pendingIntent.value) await recoverOriginal();
    if (!currentView(c) || run !== loadEpoch) return;
    chooseDefaultNetwork(
      invoice.value,
      !invoice.value.out_trade_no &&
        !invoice.value.amount &&
        !invoice.value.notify_url &&
        !invoice.value.return_url,
    );
    chooseDefaultNetwork(
      withdrawal.value,
      !pendingIntent.value &&
        !withdrawal.value.address &&
        !withdrawal.value.amount,
    );
    chooseDefaultNetwork(recipient.value, !recipient.value.address);
    chooseDefaultNetwork(address.value, !address.value.address);
    chooseDefaultNetwork(sweepRetry.value, !sweepRetry.value.address);
  } catch (e) {
    if (currentView(c) && run === loadEpoch) {
      clearReadData();
      enabled.value = false;
      serviceState.value = "error";
      error.value = message(e);
    }
  } finally {
    if (currentView(c) && run === loadEpoch) loading.value = false;
  }
}
async function submit(
  kind:
    | "invoice"
    | "withdrawal"
    | "refund"
    | "approve"
    | "address"
    | "recipient"
    | "recipient-approve"
    | "cancel"
    | "revoke"
    | "sweep-retry",
) {
  if (!canWrite.value) return;
  const c = context();
  const financial = kind === "withdrawal" || kind === "refund";
  let saved: CryptoIntent | null = null;
  const store = intentStore;
  let path = "",
    body: Record<string, unknown>;
  error.value = "";
  notice.value = "";
  try {
    if (kind === "invoice") {
      if (!canCreateInvoice.value || !selectedInvoiceChannel.value)
        throw new Error("请先选择当前商户可用的收款通道");
      amount(invoice.value.amount);
      const channel = selectedInvoiceChannel.value;
      const units = usdtUnits(invoice.value.amount),
        min = usdtUnits(channel.min_amount),
        max = usdtUnits(channel.max_amount);
      if (units < min || (max > 0n && units > max))
        throw new Error("收款金额超出所选通道的单笔限额");
      body = { ...invoice.value, network: channel.network, ...target() };
      path = "/crypto/invoices";
    } else {
      if (!password.value) throw new Error("请输入当前登录密码以确认本次操作");
      if (kind === "withdrawal") {
        amount(withdrawal.value.amount);
        body = { ...withdrawal.value, ...target() };
        path = "/crypto/withdrawals";
      } else if (kind === "refund") {
        amount(refund.value.amount);
        body = { ...refund.value, ...target() };
        path = heldRefund.value ? "/crypto/held-refunds" : "/crypto/refunds";
      } else if (kind === "approve") {
        if (!admin.value || !approveID.value)
          throw new Error("请选择待审批出账");
        body = {};
        path = `/crypto/withdrawals/${encodeURIComponent(approveID.value)}/approve`;
      } else if (kind === "recipient") {
        body = { ...recipient.value, ...target() };
        path = "/crypto/recipients";
      } else if (kind === "recipient-approve") {
        if (!admin.value || !approveRecipientID.value)
          throw new Error("请选择待审批地址");
        body = {};
        path = `/crypto/recipients/${encodeURIComponent(approveRecipientID.value)}/approve`;
      } else if (kind === "cancel") {
        if (!cancelID.value) return;
        body = target();
        path = `/crypto/withdrawals/${encodeURIComponent(cancelID.value)}/cancel`;
      } else if (kind === "revoke") {
        if (!revokeID.value) return;
        body = target();
        path = `/crypto/recipients/${encodeURIComponent(revokeID.value)}/revoke`;
      } else if (kind === "sweep-retry") {
        if (!admin.value) return;
        body = { ...sweepRetry.value };
        path = "/crypto/sweeps/retry";
      } else {
        if (!admin.value) return;
        body = { ...address.value };
        path = "/crypto/addresses";
      }
      body.current_password = password.value;
    }
    busy.value = true;
    if (financial) {
      if (!store || !principal?.value)
        throw Error("本人会话及原资金请求存储尚未核实");
      const owner = admin.value ? merchantID.value : principal.value.id;
      body.merchant_id = owner;
      const safe: Record<string, string> = { merchant_id: owner };
      for (const key of [
        "request_id",
        "invoice_id",
        "network",
        "address",
        "amount",
      ])
        if (typeof body[key] === "string") safe[key] = body[key] as string;
      saved = await store.begin(path, safe);
      if (!current(c)) return;
      pendingIntent.value = saved;
    }
    password.value = "";
    const result = await c.client.request<{ checkout_url?: string }>(path, {
      method: "POST",
      body,
    });
    if (!currentView(c)) return;
    if (saved && store) {
      verifyCryptoIntentRecord(result, saved);
      await store.clear(saved);
      if (!currentView(c)) return;
      pendingIntent.value = null;
    }
    if (kind === "withdrawal")
      withdrawal.value.request_id = crypto.randomUUID();
    if (kind === "refund") refund.value.request_id = crypto.randomUUID();
    cancelID.value = "";
    revokeID.value = "";
    approveID.value = "";
    approveRecipientID.value = "";
    if (kind === "address") monitoringRefresh.value++;
    await load();
    if (!currentView(c) || !readReady.value) return;
    if (showForms.value) checkoutURL.value = result?.checkout_url || "";
    notice.value =
      kind === "invoice"
        ? "收款单已创建，请打开收银台并核对网络及金额"
        : "申请已记录，最终结果请查看出账记录";
  } catch (e) {
    if (current(c)) {
      if (currentView(c)) error.value = message(e);
      if (saved && store && cryptoIntentRefused(e, saved)) {
        try {
          await store.clear(saved);
          if (current(c)) pendingIntent.value = null;
        } catch (storageFailure) {
          if (current(c)) storageError.value = message(storageFailure);
        }
      }
      if (
        busy.value &&
        ((saved && !cryptoIntentRefused(e, saved)) ||
          (!saved &&
            !(e instanceof ApiError && e.status >= 400 && e.status < 500)))
      ) {
        uncertain.value = true;
        error.value =
          message(e) +
          "。结果尚无法确认，请查询原商户订单号或原请求号；本页已停止继续提交。";
      }
    }
  } finally {
    if (current(c)) {
      password.value = "";
      busy.value = false;
    }
  }
}
async function reconcile() {
  if (!admin.value || !readReady.value || busy.value || loading.value) return;
  const c = context(),
    run = loadEpoch;
  busy.value = true;
  error.value = "";
  try {
    const v = await c.client.request<{
      imbalanced_journals: number;
      balance_mismatches: number;
      negative_balances: number;
    }>("/crypto/reconciliation");
    if (currentView(c) && run === loadEpoch) reconciliation.value = v;
  } catch (e) {
    if (currentView(c) && run === loadEpoch) {
      clearReadData();
      serviceState.value = "error";
      enabled.value = false;
      error.value = message(e);
    }
  } finally {
    if (current(c)) busy.value = false;
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.ownSession,
    principal?.value?.id,
    principal?.value?.issuer,
    principal?.value?.area,
    principal?.value?.csrf,
  ],
  () => {
    epoch++;
    controller?.abort();
    balances.value = [];
    invoices.value = [];
    withdrawals.value = [];
    recipients.value = [];
    password.value = "";
    checkoutURL.value = "";
    notice.value = "";
    merchantID.value = "";
    withdrawal.value = {
      request_id: crypto.randomUUID(),
      network: "tron",
      address: "",
      amount: "",
    };
    refund.value = {
      request_id: crypto.randomUUID(),
      invoice_id: "",
      address: "",
      amount: "",
    };
    invoice.value = {
      out_trade_no: "",
      channel_id: "",
      network: "tron",
      amount: "",
      notify_url: "",
      return_url: "",
    };
    recipient.value = { network: "tron", address: "" };
    address.value = { network: "tron", address: "", role: "deposit" };
    sweepRetry.value = { network: "tron", address: "" };
    heldRefund.value = false;
    enabled.value = false;
    serviceState.value = "loading";
    networks.value = [];
    simulation.value = false;
    busy.value = false;
    uncertain.value = false;
    reconciliation.value = null;
    cancelID.value = "";
    revokeID.value = "";
    approveID.value = "";
    approveRecipientID.value = "";
    const version = epoch;
    void initializeIntent().then(() => {
      if (alive && epoch === version) void load();
    });
  },
  { immediate: true },
);
watch(
  () => [props.section, props.initialChannelID],
  () => {
    sectionEpoch++;
    controller?.abort();
    closeActions();
    notice.value = "";
    reconciliation.value = null;
    if (!restoring.value) void load();
  },
);
watch(merchantID, () => {
  channelEpoch++;
  invoiceChannels.value = [];
  invoice.value.channel_id = "";
  if (activeForm.value === "invoice") void loadInvoiceChannels();
});
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  password.value = "";
});
</script>

<template>
  <section class="crypto-funds" :data-section="section">
    <header class="crypto-header">
      <div>
        <h2>{{ headings[section].title }}</h2>
        <p class="muted">
          {{ headings[section].description }}
        </p>
      </div>
      <button type="button" :disabled="loading || busy" @click="refresh">
        刷新记录
      </button>
    </header>
    <p v-if="readReady" class="alert">
      仅向收银台显示的网络及地址付款。少付、超付及逾期到账均须核对；出账申请通过审批后才会执行。
    </p>
    <p v-if="readReady && simulation" class="alert">
      本地模拟环境：仅使用合成资金及本地链数据，请勿转入真实资产。
    </p>
    <p v-if="!ownSession" class="alert">当前为代登录会话，仅能查看记录。</p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="storageError" role="alert" class="alert error">
      {{ storageError }}
    </p>
    <section v-if="pendingIntent && readReady" class="card">
      <h3>原资金请求待核对</h3>
      <p>请求号：{{ pendingIntent.body.request_id }}</p>
      <p>
        {{ pendingIntent.body.network }} · {{ pendingIntent.body.amount }} USDT
        · {{ pendingIntent.body.address }}
      </p>
      <p class="muted">
        已恢复原规范输入。确认原请求记录前，不会更换请求号或发起新的资金申请。
      </p>
      <button
        type="button"
        :disabled="busy || loading"
        @click="recoverOriginal"
      >
        仅查询原请求
      </button>
    </section>
    <p v-if="notice" role="status" class="alert">{{ notice }}</p>
    <p v-if="checkoutURL && section === 'invoices' && readReady">
      <a data-checkout :href="checkoutURL" target="_blank" rel="noreferrer"
        >打开 USDT 收银台</a
      >
    </p>
    <p v-if="loading" role="status">正在加载 USDT 账户…</p>
    <section
      v-if="serviceState === 'disabled'"
      data-testid="crypto-service-disabled"
      role="status"
      class="card"
    >
      <h3>USDT 收款与结算尚未启用</h3>
      <p class="muted">
        当前无法创建收款单或发起结算、退币。服务开放后可刷新查看。
      </p>
      <p v-if="pendingIntent" class="muted">
        原资金请求已保留。服务恢复后将仅查询原请求，不会自动重新提交。
      </p>
    </section>
    <section
      v-else-if="serviceState === 'error'"
      data-testid="crypto-service-error"
      role="status"
      class="card"
    >
      <h3>USDT 记录暂时无法读取</h3>
      <p class="muted">当前已暂停资金操作，请刷新记录后再核对。</p>
    </section>
    <p
      v-if="readReady && !enabled"
      data-testid="crypto-service-readonly"
      role="status"
      class="alert"
    >
      USDT 收款与结算已暂停，历史记录仍可查看，当前无法发起新的资金操作。
    </p>
    <CryptoOperationsMonitor
      v-if="showMonitoring"
      :key="principal?.id"
      :client="client"
      :refresh="monitoringRefresh"
    />
    <template v-if="readReady">
      <form
        v-if="admin && section !== 'reconciliation'"
        class="crypto-filter"
        @submit.prevent="load"
      >
        <label
          >商户 ID<input
            v-model="merchantID"
            name="merchant_id"
            inputmode="numeric"
            placeholder="留空查看全部记录" /></label
        ><button type="submit" :disabled="loading || busy">查询</button>
      </form>
      <p v-if="admin && section === 'operations'" class="muted">
        商户筛选仅作用于归集记录。
      </p>
      <div v-if="showForms && !activeForm" class="crypto-actions">
        <button
          v-if="section === 'invoices'"
          type="button"
          data-open-form="invoice"
          @click="openForm('invoice')"
        >
          创建收款单
        </button>
        <button
          v-if="section === 'settlements'"
          type="button"
          data-open-form="withdrawal"
          @click="openForm('withdrawal')"
        >
          申请原链结算
        </button>
        <button
          v-if="section === 'refunds'"
          type="button"
          data-open-form="refund"
          @click="openForm('refund')"
        >
          申请原链退币
        </button>
        <button
          v-if="section === 'recipients'"
          type="button"
          data-open-form="recipient"
          @click="openForm('recipient')"
        >
          申请绑定地址
        </button>
        <button
          v-if="section === 'operations' && admin"
          type="button"
          data-open-form="address"
          @click="openForm('address')"
        >
          登记受管地址
        </button>
        <button
          v-if="section === 'operations' && admin"
          type="button"
          data-open-form="sweep-retry"
          @click="openForm('sweep-retry')"
        >
          恢复归集
        </button>
      </div>
      <div v-if="showForms && activeForm" class="crypto-forms">
        <form
          v-if="section === 'invoices' && activeForm === 'invoice'"
          data-form="invoice"
          class="card"
          @submit.prevent="submit('invoice')"
        >
          <h3>创建 USDT 收款单</h3>
          <label
            >支付通道<select
              v-model="invoice.channel_id"
              name="channel_id"
              required
              :disabled="channelLoading || !invoiceChannels.length"
            >
              <option
                v-for="channel in invoiceChannels"
                :key="channel.id"
                :value="channel.id"
              >
                {{ channel.name }} · {{ label(channel.network) }}
              </option>
            </select></label
          >
          <p v-if="channelLoading" role="status" class="muted">
            正在读取可用通道…
          </p>
          <p v-else-if="channelError" role="alert" class="muted">
            {{ channelError }}
          </p>
          <p v-else-if="!invoiceChannels.length" role="status" class="muted">
            当前没有可用的 USDT
            通道，请联系管理员核对用户组通道分配、支付方式及主网状态。
          </p>
          <p v-if="selectedInvoiceChannel" class="muted">
            手续费 {{ selectedInvoiceChannel.fee_bps / 100 }}% · 单笔
            {{ selectedInvoiceChannel.min_amount }} 至
            {{
              selectedInvoiceChannel.max_amount === "0"
                ? "不限"
                : selectedInvoiceChannel.max_amount
            }}
            USDT
          </p>
          <label
            >商户订单号<input
              v-model="invoice.out_trade_no"
              name="out_trade_no"
              required
              maxlength="128" /></label
          ><label
            >收款金额（USDT）<input
              v-model="invoice.amount"
              name="amount"
              inputmode="decimal"
              required
              placeholder="10.000000"
          /></label>
          <details>
            <summary>通知及返回地址（可选）</summary>
            <label
              >异步通知 URL<input
                v-model="invoice.notify_url"
                type="url" /></label
            ><label
              >支付后返回 URL<input v-model="invoice.return_url" type="url"
            /></label>
          </details>
          <button type="submit" :disabled="!canCreateInvoice">
            创建收款单
          </button>
          <button
            type="button"
            data-cancel-form
            :disabled="busy"
            @click="cancelForm"
          >
            取消申请
          </button>
        </form>
        <form
          v-if="section === 'settlements' && activeForm === 'withdrawal'"
          data-form="withdrawal"
          class="card"
          @submit.prevent="submit('withdrawal')"
        >
          <h3>申请原链结算</h3>
          <div
            v-if="usableRecipients().length"
            class="crypto-recipient-shortcuts"
          >
            <span class="muted">选择已批准且冷静期结束的地址</span>
            <button
              v-for="r in usableRecipients()"
              :key="r.id"
              type="button"
              :disabled="!canWrite"
              @click="
                withdrawal.network = r.network;
                withdrawal.address = r.address;
                refund.address = r.address;
              "
            >
              {{ label(r.network) }} · {{ r.address }}
            </button>
          </div>

          <label
            >网络<select v-model="withdrawal.network" name="network" required>
              <option v-for="n in networks" :key="n.network" :value="n.network">
                {{ label(n.network) }}
              </option>
            </select></label
          ><label
            >收款地址<input
              v-model="withdrawal.address"
              name="address"
              required
              autocomplete="off" /></label
          ><label
            >金额（USDT）<input
              v-model="withdrawal.amount"
              name="amount"
              required
              inputmode="decimal" /></label
          ><label
            >请求号<input
              v-model="withdrawal.request_id"
              name="request_id"
              required
              readonly /></label
          ><label
            >当前登录密码<input
              v-model="password"
              name="current_password"
              type="password"
              autocomplete="current-password" /></label
          ><button type="submit" :disabled="!canWrite">
            确认地址并申请结算
          </button>
          <button
            type="button"
            data-cancel-form
            :disabled="busy"
            @click="cancelForm"
          >
            取消申请
          </button>
        </form>
        <form
          v-if="section === 'refunds' && activeForm === 'refund'"
          data-form="refund"
          class="card"
          @submit.prevent="submit('refund')"
        >
          <h3>申请原链退币</h3>
          <div
            v-if="usableRecipients().length"
            class="crypto-recipient-shortcuts"
          >
            <span class="muted">选择已批准且冷静期结束的地址</span>
            <button
              v-for="r in usableRecipients()"
              :key="r.id"
              type="button"
              :disabled="!canWrite"
              @click="
                withdrawal.network = r.network;
                withdrawal.address = r.address;
                refund.address = r.address;
              "
            >
              {{ label(r.network) }} · {{ r.address }}
            </button>
          </div>

          <label
            >退币来源<select v-model="heldRefund" name="refund_source">
              <option :value="false">退回已入账的订单款项</option>
              <option :value="true">
                退回未入账款项（少付、超付或逾期到账）
              </option>
            </select></label
          >
          <p class="muted">退币使用原收款网络，资金预留及出账均保留记录。</p>
          <label
            >收款单 ID<input
              v-model="refund.invoice_id"
              name="invoice_id"
              required /></label
          ><label
            >退币地址<input
              v-model="refund.address"
              name="address"
              required
              autocomplete="off" /></label
          ><label
            >退币金额（USDT）<input
              v-model="refund.amount"
              name="amount"
              required
              inputmode="decimal" /></label
          ><label>请求号<input :value="refund.request_id" readonly /></label
          ><label
            >当前登录密码<input
              v-model="password"
              type="password"
              autocomplete="current-password" /></label
          ><button type="submit" :disabled="!canWrite">确认并申请退币</button>
          <button
            type="button"
            data-cancel-form
            :disabled="busy"
            @click="cancelForm"
          >
            取消申请
          </button>
        </form>
        <form
          v-if="
            showForms && section === 'recipients' && activeForm === 'recipient'
          "
          data-form="recipient"
          class="card"
          @submit.prevent="submit('recipient')"
        >
          <label
            >网络<select v-model="recipient.network" required>
              <option v-for="n in networks" :key="n.network" :value="n.network">
                {{ label(n.network) }}
              </option>
            </select></label
          ><label class="crypto-address-field"
            >申请绑定地址<input
              v-model="recipient.address"
              name="recipient_address"
              required
              autocomplete="off" /></label
          ><label
            >当前登录密码<input
              v-model="password"
              type="password"
              autocomplete="current-password" /></label
          ><button type="submit" :disabled="!canWrite">确认并申请绑定</button>
          <button
            type="button"
            data-cancel-form
            :disabled="busy"
            @click="cancelForm"
          >
            取消申请
          </button>
        </form>
      </div>
      <form
        v-if="
          showForms &&
          admin &&
          section === 'operations' &&
          activeForm === 'address'
        "
        data-form="address"
        class="card"
        @submit.prevent="submit('address')"
      >
        <h3>添加受管地址</h3>
        <p class="muted">仅登记地址。私钥由独立签名系统保管。</p>
        <label
          >网络<select v-model="address.network" required>
            <option v-for="n in networks" :key="n.network" :value="n.network">
              {{ label(n.network) }}
            </option>
          </select></label
        ><label
          >角色<select v-model="address.role">
            <option value="deposit">收款地址</option>
            <option value="treasury">归集地址</option>
            <option value="hot">出账地址</option>
          </select></label
        ><label class="crypto-address-field"
          >地址<input
            v-model="address.address"
            required
            autocomplete="off" /></label
        ><label
          >当前登录密码<input
            v-model="password"
            type="password"
            autocomplete="current-password" /></label
        ><button type="submit" :disabled="!canWrite">登记地址</button>
        <button
          type="button"
          data-cancel-form
          :disabled="busy"
          @click="cancelForm"
        >
          取消申请
        </button>
      </form>
      <form
        v-if="
          showForms &&
          admin &&
          section === 'operations' &&
          activeForm === 'sweep-retry'
        "
        data-form="sweep-retry"
        class="card"
        @submit.prevent="submit('sweep-retry')"
      >
        <h3>归集恢复</h3>
        <p class="muted">
          连续三次失败后暂停归集。仅最终失败或已安全取消的归集可恢复；存在已签名或未知交易时拒绝恢复。此操作仅重新排队。
        </p>
        <label
          >网络<select v-model="sweepRetry.network" required>
            <option v-for="n in networks" :key="n.network" :value="n.network">
              {{ label(n.network) }}
            </option>
          </select></label
        ><label class="crypto-address-field"
          >原收款地址<input
            v-model="sweepRetry.address"
            required
            autocomplete="off" /></label
        ><label
          >当前登录密码<input
            v-model="password"
            type="password"
            autocomplete="current-password" /></label
        ><button type="submit" :disabled="!canWrite">确认并申请恢复归集</button>
        <button
          type="button"
          data-cancel-form
          :disabled="busy"
          @click="cancelForm"
        >
          取消申请
        </button>
      </form>
      <div v-if="section === 'overview'" class="crypto-balances">
        <article v-for="b in balances" :key="b.network" class="card">
          <h3>{{ label(b.network) }}</h3>
          <p class="crypto-amount">
            {{ b.available }} <small>USDT 可用</small>
          </p>
          <p class="muted">已预留 {{ b.reserved }} USDT</p>
        </article>
      </div>
      <section v-if="section === 'overview'" class="card">
        <h3>网络状态</h3>
        <ul v-if="networkStatus.length" class="crypto-network-status">
          <li v-for="n in networkStatus" :key="n.network">
            <span>{{ label(n.network) }}</span
            ><strong>{{ n.enabled ? "已启用" : "已暂停" }}</strong>
          </li>
        </ul>
        <p v-else class="muted">当前暂无可用网络。</p>
        <p class="muted">
          收款、结算、退币与地址管理各有独立页面。请从 USDT
          资金菜单选择要处理的业务。
        </p>
      </section>
      <section v-if="section === 'recipients'" class="card">
        <h3>结算与退币地址</h3>
        <p class="muted">
          新增地址须平台审批，并等待 24
          小时冷静期。签名系统独立核对允许的地址；审批不能缩短冷静期。
        </p>

        <div class="crypto-table">
          <table>
            <thead>
              <tr>
                <th v-if="admin">商户</th>
                <th>网络</th>
                <th>地址</th>
                <th>审批状态</th>
                <th>最早可用时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in recipients" :key="r.id">
                <td v-if="admin">{{ r.merchant_id }}</td>
                <td>{{ label(r.network) }}</td>
                <td class="crypto-address">{{ r.address }}</td>
                <td>{{ states[r.state] || r.state }}</td>
                <td>{{ new Date(r.usable_at).toLocaleString() }}</td>
                <td>
                  <button
                    v-if="showForms && admin && r.state === 'requested'"
                    type="button"
                    :disabled="!canWrite"
                    @click="selectAction('recipient-approve', r.id)"
                  >
                    核对地址并审批</button
                  ><button
                    v-if="showForms && r.state !== 'revoked'"
                    type="button"
                    :disabled="!canWrite"
                    @click="selectAction('revoke', r.id, String(r.merchant_id))"
                  >
                    撤销绑定
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <form
          v-if="showForms && admin && approveRecipientID"
          ref="actionPanel"
          data-confirm-action="recipient-approve"
          class="crypto-filter"
          @submit.prevent="submit('recipient-approve')"
        >
          <p>确认绑定地址申请：{{ approveRecipientID }}</p>
          <label
            >当前登录密码<input
              v-model="password"
              type="password"
              autocomplete="current-password"
              required /></label
          ><button type="submit" :disabled="!canWrite">确认并批准地址</button
          ><button
            type="button"
            @click="
              approveRecipientID = '';
              password = '';
            "
          >
            取消
          </button>
        </form>
        <p v-if="!recipients.length" class="muted">
          暂无绑定地址；结算与退币前请先申请绑定。
        </p>
      </section>

      <section v-if="section === 'invoices'" class="card">
        <h3>收款记录</h3>
        <p v-if="initialChannelID" class="muted">
          当前查看通道 #{{ initialChannelID }} 的收款记录
        </p>
        <div class="crypto-table">
          <table>
            <thead>
              <tr>
                <th>收款单 / 商户订单</th>
                <th v-if="admin">商户</th>
                <th>网络</th>
                <th>支付通道</th>
                <th>应付 / 已收 USDT</th>
                <th>手续费 USDT</th>
                <th>状态</th>
                <th>有效期</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="i in visibleInvoices" :key="i.id">
                <td>
                  {{ i.id }}<br /><small>{{ i.out_trade_no }}</small>
                </td>
                <td v-if="admin">{{ i.merchant_id }}</td>
                <td>{{ label(i.network) }}</td>
                <td>
                  {{
                    i.channel_id && i.channel_id !== "0"
                      ? "#" + i.channel_id + " · 版本 " + i.channel_revision
                      : "历史未绑定通道"
                  }}
                </td>
                <td>{{ i.amount }} / {{ i.received }}</td>
                <td>{{ i.fee }}</td>
                <td>{{ states[i.state] || i.state }}</td>
                <td>{{ new Date(i.expires_at).toLocaleString() }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="!visibleInvoices.length && !loading" class="muted">
            暂无收款记录
          </p>
        </div>
      </section>
      <section
        v-if="
          section === 'settlements' ||
          section === 'refunds' ||
          (section === 'operations' && admin)
        "
        class="card"
      >
        <h3>
          {{
            section === "settlements"
              ? "结算记录"
              : section === "refunds"
                ? "退币记录"
                : "归集记录"
          }}
        </h3>
        <div class="crypto-table">
          <table>
            <thead>
              <tr>
                <th>请求号</th>
                <th v-if="admin">商户</th>
                <th>类型 / 网络</th>
                <th>金额 USDT</th>
                <th>地址</th>
                <th>状态 / 链上交易</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="w in filteredWithdrawals" :key="w.id">
                <td>{{ w.request_id }}</td>
                <td v-if="admin">{{ w.merchant_id }}</td>
                <td>
                  {{
                    w.kind === "refund" || w.kind === "exception_refund"
                      ? "退币"
                      : w.kind === "sweep"
                        ? "归集"
                        : "结算"
                  }}
                  /
                  {{ label(w.network) }}
                </td>
                <td>{{ w.amount }}</td>
                <td class="crypto-address">{{ w.address }}</td>
                <td>
                  {{ states[w.state] || w.state }}<br /><small>{{
                    w.tx_id
                  }}</small>
                  <p v-if="w.cancellation_requested">
                    已申请核对取消，确认前资金仍预占
                  </p>
                </td>
                <td>
                  <button
                    v-if="
                      showForms &&
                      admin &&
                      w.kind !== 'sweep' &&
                      (w.state === 'pending' || w.state === 'requested')
                    "
                    data-action="approve"
                    type="button"
                    :disabled="!canWrite"
                    @click="selectAction('approve', w.id)"
                  >
                    核对并审批</button
                  ><button
                    v-if="
                      showForms &&
                      w.kind !== 'sweep' &&
                      [
                        'requested',
                        'approved',
                        'preparing',
                        'prepared',
                        'signing',
                      ].includes(w.state) &&
                      !w.cancellation_requested
                    "
                    type="button"
                    :disabled="!canWrite"
                    @click="selectAction('cancel', w.id, String(w.merchant_id))"
                  >
                    申请核对取消
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
          <p v-if="!filteredWithdrawals.length && !loading" class="muted">
            暂无{{
              section === "settlements"
                ? "结算"
                : section === "refunds"
                  ? "退币"
                  : "归集"
            }}记录
          </p>
        </div>
      </section>
      <form
        v-if="
          showForms &&
          admin &&
          approveID &&
          ['settlements', 'refunds'].includes(section)
        "
        ref="actionPanel"
        data-confirm-action="approve"
        class="card"
        @submit.prevent="submit('approve')"
      >
        <h3>审批原链出账</h3>
        <p>出账单：{{ approveID }}。请核对上方网络、金额及地址。</p>
        <label
          >当前登录密码<input
            v-model="password"
            type="password"
            autocomplete="current-password"
            required /></label
        ><button type="submit" :disabled="!canWrite">确认并批准</button
        ><button
          type="button"
          @click="
            approveID = '';
            password = '';
          "
        >
          取消
        </button>
      </form>

      <section v-if="admin && section === 'reconciliation'" class="card">
        <h3>USDT 对账</h3>
        <p class="muted">此处检查内部账本是否一致，不代表链上资产余额。</p>
        <button type="button" :disabled="busy || loading" @click="reconcile">
          查询账本对账结果
        </button>
        <dl v-if="reconciliation">
          <dt>账簿不平衡</dt>
          <dd>{{ reconciliation.imbalanced_journals }}</dd>
          <dt>余额与流水不符</dt>
          <dd>{{ reconciliation.balance_mismatches }}</dd>
          <dt>异常负余额</dt>
          <dd>{{ reconciliation.negative_balances }}</dd>
        </dl>
        <p
          v-if="
            reconciliation &&
            !reconciliation.imbalanced_journals &&
            !reconciliation.balance_mismatches &&
            !reconciliation.negative_balances
          "
          role="status"
        >
          账本检查通过
        </p>
      </section>
      <form
        v-if="showForms && (cancelID || revokeID)"
        ref="actionPanel"
        data-confirm-action="cancel-or-revoke"
        class="card"
        @submit.prevent="submit(cancelID ? 'cancel' : 'revoke')"
      >
        <h3>{{ cancelID ? "取消待审批出账" : "撤销绑定地址" }}</h3>
        <p>{{ cancelID || revokeID }}</p>
        <p class="muted">
          {{
            cancelID
              ? "申请核对取消，确认前资金仍预占；已签名或已广播的交易无法取消。"
              : "撤销后阻止尚未签名的出账；已签名交易须继续核对原链结果。"
          }}
        </p>
        <label
          >当前登录密码<input
            v-model="password"
            type="password"
            autocomplete="current-password"
            required /></label
        ><button type="submit" :disabled="!canWrite">确认操作</button
        ><button
          type="button"
          @click="
            cancelID = '';
            revokeID = '';
            password = '';
          "
        >
          返回
        </button>
      </form>
    </template>
  </section>
</template>

<style scoped>
.crypto-funds {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
  box-sizing: border-box;
  width: 100%;
  padding: 20px;
  gap: 18px;
}
.crypto-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.crypto-header h2 {
  margin: 0;
}
.crypto-header > div {
  min-width: 0;
}
.crypto-balances,
.crypto-forms {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 290px), 1fr));
  gap: 18px;
}
.card {
  min-width: 0;
  padding: 22px;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--bg-card);
}
.card h3 {
  margin-top: 0;
}
.card p {
  overflow-wrap: anywhere;
}
.crypto-forms {
  width: 100%;
  max-width: 46rem;
  grid-template-columns: minmax(0, 1fr);
}
.crypto-funds > form.card {
  max-width: 46rem;
}
.crypto-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.crypto-actions:empty {
  display: none;
}
.crypto-network-status {
  display: grid;
  gap: 12px;
  max-width: 32rem;
  padding: 0;
  list-style: none;
}
.crypto-network-status li {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 12px;
}
.crypto-recipient-shortcuts {
  display: grid;
  min-width: 0;
  gap: 8px;
}
.crypto-recipient-shortcuts button {
  white-space: normal;
  overflow-wrap: anywhere;
}
.crypto-amount {
  font-size: 1.6rem;
  font-variant-numeric: tabular-nums;
}
.crypto-amount small {
  font-size: 0.8rem;
}
.crypto-forms > form.card {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.crypto-forms > form.card > label:has(select),
.crypto-forms > form.card > label:has(input[type="password"]),
.crypto-forms > form.card > label:has(input[name="amount"]) {
  width: 100%;
  max-width: 280px;
}
.crypto-filter,
.crypto-funds > form.card {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 12px;
}
.crypto-filter > label,
.crypto-funds > form.card > label {
  flex: 0 1 240px;
  max-width: 100%;
}
.crypto-filter > .crypto-address-field,
.crypto-funds > form.card > .crypto-address-field {
  flex: 1 1 320px;
  max-width: 32rem;
}
.crypto-filter > p,
.crypto-funds > form.card > :is(h3, p) {
  flex-basis: 100%;
  margin: 0;
  overflow-wrap: anywhere;
}
.crypto-filter > button,
.crypto-funds > form.card > button {
  max-width: 100%;
  white-space: normal;
  overflow-wrap: anywhere;
}
label {
  margin: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
input,
select {
  margin: 0;
  min-width: 0;
  width: 100%;
  box-sizing: border-box;
  min-height: 40px;
}
button {
  min-height: 40px;
  cursor: pointer;
}
button:disabled {
  cursor: default;
}
.crypto-table {
  overflow: auto;
}
table {
  width: 100%;
  border-collapse: collapse;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
th,
td {
  text-align: left;
  padding: 12px 10px;
  border-bottom: 1px solid var(--line);
}
.crypto-address {
  min-width: 12rem;
  max-width: 250px;
  overflow-wrap: anywhere;
  white-space: normal;
}
pre {
  overflow: auto;
  max-height: 420px;
}
.muted {
  color: var(--ink-muted);
}
.alert {
  padding: 14px;
  border-radius: 8px;
  background: var(--warning-soft);
  color: var(--warning-text);
}
.error {
  color: var(--danger-text);
}
@media (max-width: 600px) {
  .crypto-funds {
    padding: 14px;
  }
  .crypto-header {
    align-items: flex-start;
    flex-direction: column;
  }
  .card {
    padding: 16px;
  }
  .crypto-filter,
  .crypto-funds > form.card {
    flex-direction: column;
    align-items: stretch;
  }
  .crypto-filter > label,
  .crypto-funds > form.card > label,
  .crypto-filter > .crypto-address-field,
  .crypto-funds > form.card > .crypto-address-field {
    flex: none;
    width: 100%;
    max-width: none;
  }
  .crypto-forms > form.card > label:has(select),
  .crypto-forms > form.card > label:has(input[type="password"]),
  .crypto-forms > form.card > label:has(input[name="amount"]) {
    max-width: none;
  }
  .crypto-filter > p,
  .crypto-funds > form.card > :is(h3, p) {
    flex-basis: auto;
  }
}
</style>
