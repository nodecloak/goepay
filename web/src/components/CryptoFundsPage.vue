<script setup lang="ts">
import { computed, inject, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import { fundsPrincipal212, fundsScope212 } from "../funds-intent212";
import {
  CryptoIntentStore,
  cryptoIntentRefused,
  verifyCryptoIntentRecord,
  type CryptoIntent,
} from "../crypto-intent";

const props = defineProps<{ client: ApiClient; ownSession: boolean }>();
type Network = { network: string; enabled: boolean; fee_bps: number };
type Balance = { network: string; available: string; reserved: string };
type Invoice = {
  id: string;
  merchant_id: number;
  out_trade_no: string;
  network: string;
  amount: string;
  received: string;
  fee: string;
  state: string;
  address: string;
  expires_at: string;
  checkout_url?: string;
};
type Withdrawal = {
  id: string;
  merchant_id: number;
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
const canWrite = computed(
  () =>
    enabled.value &&
    props.ownSession &&
    !busy.value &&
    !loading.value &&
    !restoring.value &&
    !storageError.value &&
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
  loadEpoch = 0,
  alive = true,
  controller: AbortController | undefined;
function context() {
  return { client: toRaw(props.client), csrf: props.client.csrfToken, epoch };
}
function current(c: ReturnType<typeof context>) {
  return (
    alive &&
    c.epoch === epoch &&
    c.client === toRaw(props.client) &&
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
    c = context();
  if (!original || !store) return;
  try {
    const record = await c.client.request(
      `/crypto/withdrawals/by-request?merchant_id=${encodeURIComponent(original.body.merchant_id!)}&request_id=${encodeURIComponent(original.body.request_id!)}`,
    );
    const verified = verifyCryptoIntentRecord(record, original);
    if (!current(c) || pendingIntent.value?.digest !== original.digest) return;
    await store.clear(original);
    if (!current(c)) return;
    pendingIntent.value = null;
    uncertain.value = false;
    notice.value = `已找回原请求 ${verified.request_id}，当前状态：${states[verified.state] || verified.state}；未发起新的申请。`;
  } catch (e) {
    if (current(c))
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
async function load() {
  controller?.abort();
  controller = new AbortController();
  const c = context(),
    run = ++loadEpoch;
  loading.value = true;
  error.value = "";
  try {
    const query = scope(),
      options = { signal: controller.signal };
    const [config, b, i, w, recipientsResult] = await Promise.all([
      c.client.request<{
        enabled: boolean;
        networks: Network[];
        simulation?: boolean;
      }>("/crypto/config", options),
      c.client.request<Balance[]>("/crypto/balances" + query, options),
      c.client.request<Invoice[]>("/crypto/invoices" + query, options),
      c.client.request<Withdrawal[]>("/crypto/withdrawals" + query, options),
      c.client.request<Recipient[]>("/crypto/recipients" + query, options),
    ]);
    if (!current(c) || run !== loadEpoch) return;
    enabled.value = config.enabled;
    networks.value = config.networks.filter((n) => n.enabled);
    simulation.value = config.simulation === true;
    balances.value = b || [];
    invoices.value = i || [];
    withdrawals.value = w || [];
    recipients.value = recipientsResult || [];
    if (pendingIntent.value) await recoverOriginal();
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
    if (current(c) && run === loadEpoch) error.value = message(e);
  } finally {
    if (current(c) && run === loadEpoch) loading.value = false;
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
      amount(invoice.value.amount);
      body = { ...invoice.value, ...target() };
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
    if (!current(c)) return;
    if (saved && store) {
      verifyCryptoIntentRecord(result, saved);
      await store.clear(saved);
      if (!current(c)) return;
      pendingIntent.value = null;
    }
    checkoutURL.value = result?.checkout_url || "";
    notice.value =
      kind === "invoice"
        ? "收款单已创建，请打开收银台并核对网络及金额"
        : "申请已记录，最终结果请查看出账记录";
    if (kind === "withdrawal")
      withdrawal.value.request_id = crypto.randomUUID();
    if (kind === "refund") refund.value.request_id = crypto.randomUUID();
    cancelID.value = "";
    revokeID.value = "";
    approveID.value = "";
    approveRecipientID.value = "";
    await load();
  } catch (e) {
    if (current(c)) {
      error.value = message(e);
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
        error.value +=
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
  if (!admin.value || busy.value) return;
  const c = context();
  busy.value = true;
  try {
    const v = await c.client.request<{
      imbalanced_journals: number;
      balance_mismatches: number;
      negative_balances: number;
    }>("/crypto/reconciliation");
    if (current(c)) reconciliation.value = v;
  } catch (e) {
    if (current(c)) error.value = message(e);
  } finally {
    if (current(c)) busy.value = false;
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.ownSession,
    principal?.value?.id,
    principal?.value?.issuer,
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
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  password.value = "";
});
</script>

<template>
  <section class="crypto-funds">
    <header class="crypto-header">
      <div>
        <h2>USDT 收款与结算</h2>
        <p class="muted">
          统一代收，按原链 USDT 结算。TRON 与 Ethereum 的余额分别核算。
        </p>
      </div>
      <button type="button" :disabled="loading || busy" @click="load">
        刷新记录
      </button>
    </header>
    <p class="alert">
      仅向收银台显示的网络及地址付款。少付、超付及逾期到账均须核对；出账申请通过审批后才会执行。
    </p>
    <p v-if="simulation" class="alert">
      本地模拟环境：仅使用合成资金及本地链数据，请勿转入真实资产。
    </p>
    <p v-if="!ownSession" class="alert">当前为代登录会话，仅能查看记录。</p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="storageError" role="alert" class="alert error">
      {{ storageError }}
    </p>
    <section v-if="pendingIntent" class="card">
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
    <p v-if="loading" role="status">正在加载 USDT 账户…</p>
    <form v-if="admin" class="crypto-filter" @submit.prevent="load">
      <label
        >商户 ID<input
          v-model="merchantID"
          name="merchant_id"
          inputmode="numeric"
          placeholder="留空查看全部记录" /></label
      ><button type="submit" :disabled="loading || busy">查询</button>
    </form>
    <div class="crypto-balances">
      <article v-for="b in balances" :key="b.network" class="card">
        <h3>{{ label(b.network) }}</h3>
        <p class="crypto-amount">{{ b.available }} <small>USDT 可用</small></p>
        <p class="muted">已预留 {{ b.reserved }} USDT</p>
      </article>
    </div>
    <p v-if="!loading && !enabled" class="muted">
      USDT 服务尚未启用，请联系平台配置受信任的网络与地址池。
    </p>
    <section class="card">
      <h3>结算与退币地址</h3>
      <p class="muted">
        新增地址须平台审批，并等待 24
        小时冷静期。签名系统独立核对允许的地址；审批不能缩短冷静期。
      </p>
      <form
        data-form="recipient"
        class="crypto-filter"
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
      </form>
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
                  v-if="admin && r.state === 'requested'"
                  type="button"
                  :disabled="!canWrite"
                  @click="approveRecipientID = r.id"
                >
                  核对地址并审批</button
                ><button
                  v-if="r.state !== 'revoked'"
                  type="button"
                  :disabled="!canWrite"
                  @click="
                    revokeID = r.id;
                    merchantID = String(r.merchant_id);
                  "
                >
                  撤销绑定
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <form
        v-if="admin && approveRecipientID"
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
      <div v-if="usableRecipients().length" class="crypto-filter">
        <span>已批准且冷静期已结束的地址</span
        ><button
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
          使用 {{ label(r.network) }} · {{ r.address }}
        </button>
      </div>
    </section>
    <div class="crypto-forms">
      <form
        data-form="invoice"
        class="card"
        @submit.prevent="submit('invoice')"
      >
        <h3>创建 USDT 收款单</h3>
        <label
          >网络<select v-model="invoice.network" name="network" required>
            <option v-for="n in networks" :key="n.network" :value="n.network">
              {{ label(n.network) }} · 费率 {{ n.fee_bps / 100 }}%
            </option>
          </select></label
        ><label
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
        <button type="submit" :disabled="!canWrite">创建收款单</button
        ><a
          v-if="checkoutURL"
          data-checkout
          :href="checkoutURL"
          target="_blank"
          rel="noreferrer"
          >打开 USDT 收银台</a
        >
      </form>
      <form
        data-form="withdrawal"
        class="card"
        @submit.prevent="submit('withdrawal')"
      >
        <h3>申请原链结算</h3>
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
        ><button type="submit" :disabled="!canWrite">确认地址并申请结算</button>
      </form>
      <form data-form="refund" class="card" @submit.prevent="submit('refund')">
        <h3>申请原链退币</h3>
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
      </form>
    </div>
    <section class="card">
      <h3>收款记录</h3>
      <div class="crypto-table">
        <table>
          <thead>
            <tr>
              <th>收款单 / 商户订单</th>
              <th v-if="admin">商户</th>
              <th>网络</th>
              <th>应付 / 已收 USDT</th>
              <th>手续费 USDT</th>
              <th>状态</th>
              <th>有效期</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="i in invoices" :key="i.id">
              <td>
                {{ i.id }}<br /><small>{{ i.out_trade_no }}</small>
              </td>
              <td v-if="admin">{{ i.merchant_id }}</td>
              <td>{{ label(i.network) }}</td>
              <td>{{ i.amount }} / {{ i.received }}</td>
              <td>{{ i.fee }}</td>
              <td>{{ states[i.state] || i.state }}</td>
              <td>{{ new Date(i.expires_at).toLocaleString() }}</td>
            </tr>
          </tbody>
        </table>
        <p v-if="!invoices.length && !loading" class="muted">暂无收款记录</p>
      </div>
    </section>
    <section class="card">
      <h3>结算与退币记录</h3>
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
            <tr v-for="w in withdrawals" :key="w.id">
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
                    admin && (w.state === 'pending' || w.state === 'requested')
                  "
                  data-action="approve"
                  type="button"
                  :disabled="!canWrite"
                  @click="approveID = w.id"
                >
                  核对并审批</button
                ><button
                  v-if="
                    [
                      'requested',
                      'approved',
                      'preparing',
                      'prepared',
                      'signing',
                    ].includes(w.state) && !w.cancellation_requested
                  "
                  type="button"
                  :disabled="!canWrite"
                  @click="
                    cancelID = w.id;
                    merchantID = String(w.merchant_id);
                  "
                >
                  申请核对取消
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <p v-if="!withdrawals.length && !loading" class="muted">暂无出账记录</p>
      </div>
    </section>
    <form
      v-if="admin && approveID"
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
    <form
      v-if="admin"
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
    </form>
    <section v-if="admin" class="card">
      <h3>USDT 对账</h3>
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
    </section>
    <form
      v-if="cancelID || revokeID"
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
    <form
      v-if="admin"
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
    </form>
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
  .crypto-filter > p,
  .crypto-funds > form.card > :is(h3, p) {
    flex-basis: auto;
  }
}
</style>
