<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount, nextTick } from "vue";
import { useRoute } from "vue-router";
import {
  ApiClient,
  ApiError,
  validAmount,
  displayValue,
  type Session,
  type FundsActionRequest,
  type FundsActionResult,
} from "./api";
import {
  menus,
  visibleMenu,
  resolvePage,
  viewKind,
  navigationMenus,
  menuRole,
  type Area,
} from "./menu";
import ConsoleShell from "./components/ConsoleShell.vue";
import DataTable from "./components/DataTable.vue";
import { proListColumns } from "./pro-console-fields";
import ResourcePage from "./components/ResourcePage.vue";
import RefundSummaryDialog from "./components/RefundSummaryDialog.vue";
import { resourceFor, resourceEntryFor } from "./resources";
import { reportFor } from "./reports";
import ReportPage from "./components/ReportPage.vue";
import { workflowFor } from "./workflows";
import WorkflowPage from "./components/WorkflowPage.vue";
import AccountPage from "./components/AccountPage.vue";
import MerchantAccountSettings from "./components/MerchantAccountSettings.vue";
import OperationsPage from "./components/OperationsPage.vue";
import { operationPages } from "./operations";
import TotpSettings from "./components/TotpSettings.vue";
import CommercePage from "./components/CommercePage.vue";
import { commerceFor } from "./commerce";
import { unavailableReasons } from "./deferred";
import OrderDetails from "./components/OrderDetails.vue";
import AdminOrderActionDialog from "./components/AdminOrderActionDialog.vue";
import type { OrderActionID } from "./order-actions";
import { refundNotice } from "./order-operations";
import { centsMoney } from "./settlement-terms";
import { readHeldRefundFunding } from "./refund-funding";
import TransferPage from "./components/TransferPage.vue";
import GroupSalesPage from "./components/GroupSalesPage.vue";
import SettingsPage from "./components/SettingsPage.vue";
import InvitationCodesPage from "./components/InvitationCodesPage.vue";
import CollectionStylesPage from "./components/CollectionStylesPage.vue";
import CollectionGeneratorPage from "./components/CollectionGeneratorPage.vue";
import OrderBatchControls from "./components/OrderBatchControls.vue";
import AutoRefreshControl from "./components/AutoRefreshControl.vue";
import OrderBlacklistDialog from "./components/OrderBlacklistDialog.vue";
import DashboardOverview from "./components/DashboardOverview.vue";
import TransferBatchPage from "./components/TransferBatchPage.vue";
import SettlementBatchesPage from "./components/SettlementBatchesPage.vue";
import ListFilters from "./components/ListFilters.vue";
import {
  freshListFilters,
  appendListFilters,
  orderSearchFields,
  readListRouteQuery,
} from "./list-filters";
import PublicEnrollment from "./components/PublicEnrollment.vue";
import CsvExportButton from "./components/CsvExportButton.vue";
import PaymentPasswordSettings from "./components/PaymentPasswordSettings.vue";
import FundsAuthorizationModal from "./components/FundsAuthorizationModal.vue";
import SettlementRecipientPage from "./components/SettlementRecipientPage.vue";
import IdentityLookupPage from "./components/IdentityLookupPage.vue";
import ChannelSharingPage from "./components/ChannelSharingPage.vue";
import ReferralPage from "./components/ReferralPage.vue";
import ReferralPolicyPage from "./components/ReferralPolicyPage.vue";
import SettlementPlansPage from "./components/SettlementPlansPage.vue";
import PrintingSettingsPage from "./components/PrintingSettingsPage.vue";
import MerchantNoticeSettings from "./components/MerchantNoticeSettings.vue";
import LegacyRefundHistoryPanel from "./components/LegacyRefundHistoryPanel.vue";
import TemplatePage from "./components/TemplatePage.vue";
import SettlementSchedulePage from "./components/SettlementSchedulePage.vue";
import { consumePromotionPrefill } from "./referral";
const promotionPrefill = ref(consumePromotionPrefill());
import {
  readPaymentPasswordStatus,
  needsFundsAuthorization,
  snapshotFundsBody,
} from "./funds-authorization";
const fundsAction = ref<
  | (FundsActionRequest & {
      resolve: (result: FundsActionResult) => void;
      reject: (error: Error) => void;
      cleanup: () => void;
    })
  | null
>(null);
function settleFundsAction(error?: Error, result?: FundsActionResult) {
  const action = fundsAction.value;
  if (!action) return;
  fundsAction.value = null;
  action.cleanup();
  if (error) action.reject(error);
  else action.resolve(result!);
}
function connectFundsAuthorization(c: ApiClient) {
  if (c.area !== "admin") return;
  c.beforeFundsAction = async (action, send) => {
    const version = epoch,
      originPath = route.path;
    if (
      !session.value ||
      session.value.user.role !== "admin" ||
      session.value.delegated_by
    )
      throw new Error("仅本人管理员登录会话可授权资金操作");
    const status = readPaymentPasswordStatus(
      await c.request("/security/payment-password", { signal: action.signal }),
    );
    if (
      version !== epoch ||
      originPath !== route.path ||
      client.value !== c ||
      !session.value ||
      action.signal?.aborted
    )
      throw new Error("原操作页面或会话已变化，请重新核对");
    if (!needsFundsAuthorization(action.target, status.enforced)) return send();
    if (!status.configured)
      throw new Error(
        "尚未设置本人管理员资金密码，请先在账户安全中设置后重新核对原业务",
      );
    if (fundsAction.value)
      throw new Error("已有资金操作等待确认，请先处理当前授权");
    return new Promise<FundsActionResult>((resolve, reject) => {
      const abort = () =>
        settleFundsAction(new Error("原业务请求已取消，未自动重试"));
      fundsAction.value = {
        ...action,
        body: snapshotFundsBody(action.body),
        resolve,
        reject,
        cleanup: () => action.signal?.removeEventListener("abort", abort),
      };
      action.signal?.addEventListener("abort", abort, { once: true });
    });
  };
}
const enrollmentPurpose = ref<"register" | "recover" | null>(null);
function openEnrollment(purpose: "register" | "recover") {
  password.value = "";
  loginCode.value = "";
  error.value = "";
  notice.value = "";
  enrollmentPurpose.value = purpose;
}
const listFilters = ref(freshListFilters());
const refundSummaryQuery = ref<string | null>(null);
let refundSummaryReturn: HTMLElement | undefined;
function openRefundSummary() {
  if (
    !authorized.value ||
    kind.value !== "refunds" ||
    loading.value ||
    busy.value ||
    listRouteError.value
  )
    return;
  const q = listQuery();
  q.delete("page");
  q.delete("limit");
  refundSummaryReturn =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : undefined;
  refundSummaryQuery.value = q.toString();
}
function closeRefundSummary() {
  const target = refundSummaryReturn,
    context = client.value,
    csrf = context.csrfToken;
  refundSummaryQuery.value = null;
  refundSummaryReturn = undefined;
  void nextTick(() => {
    if (
      client.value === context &&
      context.csrfToken === csrf &&
      target?.isConnected
    )
      target.focus();
  });
}
const listLimit = ref(20),
  listRouteError = ref("");
const includeArchivedOrders = ref(false);
const selectedOrderTrades = ref<string[]>([]),
  orderBatchBusy = ref(false);
const orderBatchOpen = ref(false);
const blacklistSelection = ref<{
  tradeNo: string;
  kind: "ip" | "buyer";
  expected: string;
} | null>(null);
function openBlacklist(row: Record<string, unknown>, kind: "ip" | "buyer") {
  if (
    area.value !== "admin" ||
    !session.value ||
    session.value.delegated_by ||
    !["super", "operator"].includes(session.value.user.admin_class || "")
  )
    return;
  orderDetails.value = null;
  blacklistSelection.value = {
    tradeNo: String(row.trade_no),
    kind,
    expected: String(row[kind === "ip" ? "client_ip" : "buyer"] || ""),
  };
}
const orderAction = ref<{
  row: Record<string, unknown>;
  action: OrderActionID;
} | null>(null);
const route = useRoute();
const area = computed<Area>(() =>
  route.path.split("/")[1] === "merchant" ? "merchant" : "admin",
);
const path = computed(
  () => "/" + route.path.split("/").slice(2).join("/") || "/dashboard",
);
const page = computed(() => resolvePage(area.value, path.value));
const kind = computed(() => viewKind(area.value, path.value));
const client = ref(new ApiClient(area.value)),
  session = ref<Session | null>(null),
  initializing = ref(true),
  busy = ref(false),
  loading = ref(false);
const error = ref(""),
  notice = ref(""),
  username = ref(""),
  password = ref(""),
  loginCode = ref(""),
  search = ref(""),
  pageNumber = ref(1),
  items = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  stats = ref<Record<string, unknown>>({});
const navigation = computed(() =>
  visibleMenu(
    navigationMenus(area.value),
    menuRole(session.value?.user.role || "", session.value?.user.admin_class),
  ),
);
watch(items, () => {
  selectedOrderTrades.value = [];
});
const authorized = computed(
  () =>
    !!page.value &&
    (!page.value.roles.length ||
      page.value.roles.includes(
        menuRole(
          session.value?.user.role || "",
          session.value?.user.admin_class,
        ),
      )),
);
const refundRow = ref<Record<string, unknown> | null>(null),
  orderDetails = ref<Record<string, unknown> | null>(null),
  refundMoney = ref(""),
  refundExternal = ref(""),
  refundFunding = ref<Record<string, unknown> | null>(null),
  refundFundingBinding = ref("");
watch([refundMoney, refundExternal], () => {
  refundFunding.value = null;
  refundFundingBinding.value = "";
});
let controller: AbortController | undefined,
  epoch = 0;
const columnSets: Record<string, { key: string; label: string }[]> = {
  orders: [
    { key: "trade_no", label: "平台订单号" },
    { key: "out_trade_no", label: "商户订单号" },
    { key: "merchant_id", label: "商户 ID" },
    { key: "name", label: "商品名称" },
    { key: "money", label: "交易金额" },
    { key: "status", label: "状态" },
    { key: "created_at", label: "创建时间" },
  ],
  refunds: [
    { key: "refund_no", label: "退款单号" },
    { key: "trade_no", label: "平台订单号" },
    { key: "amount", label: "退款金额（分）" },
    { key: "status", label: "状态" },
    { key: "created_at", label: "申请时间" },
  ],
  transfers: [
    { key: "transfer_no", label: "付款单号" },
    { key: "recipient", label: "收款账号" },
    { key: "amount", label: "付款金额（分）" },
    { key: "status", label: "状态" },
    { key: "created_at", label: "创建时间" },
  ],
  merchants: [
    { key: "id", label: "商户 ID" },
    { key: "name", label: "商户名称" },
    { key: "status", label: "状态" },
    { key: "available_money", label: "可用余额" },
    { key: "created_at", label: "创建时间" },
  ],
  ledger: [
    { key: "id", label: "流水 ID" },
    { key: "merchant_id", label: "商户 ID" },
    { key: "kind", label: "业务类型" },
    { key: "amount", label: "变动金额（分）" },
    { key: "created_at", label: "发生时间" },
  ],
};
const columns = computed(() => {
  return proListColumns(
    kind.value,
    area.value,
    items.value,
    columnSets[kind.value] || [],
  );
});
const labels: Record<string, string> = {
  id: "编号",
  merchant_id: "商户 ID",
  amount_minor: "金额（分）",
  money: "金额",
  amount: "金额（分）",
  out_trade_no: "商户订单号",
  external_no: "业务请求号",
  available_minor: "可用余额（分）",
  frozen_minor: "冻结余额（分）",
  channel_id: "通道 ID",
  updated_at: "更新时间",
  currency: "币种",
  entry_id: "分录 ID",
  account: "账户",
  direction: "方向",
  balance_after: "变动后余额",
  subject: "商品名称",
  paid_at: "支付时间",
  type: "类型",
  merchant_amount: "商户入账额（分）",
  platform_fee: "平台手续费（分）",
  fee_rate_bps: "费率（万分比）",
  fee_policy: "订单费率策略",
  refund_fee_payer: "退款手续费承担方",
  income_status: "订单收入状态",
};
async function initialize() {
  const current = ++epoch;
  initializing.value = true;
  session.value = null;
  error.value = "";
  client.value = new ApiClient(area.value);
  connectFundsAuthorization(client.value);
  client.value.onUnauthorized = () => {
    if (current === epoch) {
      if (session.value) notice.value = "登录状态已失效，请重新登录";
      session.value = null;
    }
  };
  try {
    const s = await client.value.session();
    if (current === epoch) session.value = s;
  } catch (e) {
    if (current === epoch && (!(e instanceof ApiError) || e.status !== 401))
      error.value = message(e);
  } finally {
    if (current === epoch) {
      initializing.value = false;
      if (session.value) {
        applyListRouteQuery();
        await load();
      }
    }
  }
}
function applyListRouteQuery() {
  listRouteError.value = "";
  listLimit.value = 20;
  if (!["orders", "refunds", "ledger"].includes(kind.value)) return;
  const parsed = readListRouteQuery(
    route.query,
    kind.value,
    area.value,
    String(session.value?.user.id ?? ""),
  );
  listFilters.value = parsed.filters;
  search.value = parsed.search;
  pageNumber.value = parsed.page;
  listLimit.value = parsed.limit;
  includeArchivedOrders.value = parsed.includeArchived;
  listRouteError.value = parsed.error;
}
function message(e: unknown) {
  return e instanceof Error ? e.message : "请求失败，请重试";
}
function listQuery() {
  const q = new URLSearchParams({
    page: String(pageNumber.value),
    limit: String(listLimit.value),
  });
  const queryText =
    kind.value === "orders" && listFilters.value.search_field
      ? search.value
      : search.value.trim();
  if (queryText !== "") q.set("q", queryText);
  appendListFilters(q, listFilters.value, kind.value, area.value);
  if (
    kind.value === "orders" &&
    area.value === "admin" &&
    includeArchivedOrders.value
  )
    q.set("include_archived", "true");
  return q;
}
const listCSVPath = computed(() => {
  const q = listQuery();
  q.set("format", "csv");
  return `/${kind.value}?${q}`;
});
async function login() {
  notice.value = "";
  busy.value = true;
  error.value = "";
  try {
    session.value = await client.value.login(
      username.value,
      password.value,
      loginCode.value,
    );
    loginCode.value = "";
    password.value = "";
    applyListRouteQuery();
    await load();
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
async function logout() {
  busy.value = true;
  error.value = "";
  try {
    await client.value.logout();
    notice.value = "";
    session.value = null;
    items.value = [];
    stats.value = {};
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
async function load() {
  controller?.abort();
  controller = new AbortController();
  const c = controller;
  items.value = [];
  stats.value = {};
  error.value = "";
  if (
    !session.value ||
    !authorized.value ||
    kind.value === "unavailable" ||
    kind.value === "resource" ||
    kind.value === "report" ||
    kind.value === "workflow" ||
    kind.value === "account" ||
    kind.value === "operation" ||
    kind.value === "commerce" ||
    kind.value === "group-sales" ||
    kind.value === "settings" ||
    kind.value === "invitations" ||
    kind.value === "collection-styles" ||
    kind.value === "collection-generator" ||
    kind.value === "identity-lookup" ||
    kind.value === "channel-sharing" ||
    kind.value === "referral" ||
    kind.value === "referral-policy" ||
    kind.value === "settlement-plans" ||
    kind.value === "settlement-batches" ||
    kind.value === "settlement-schedule" ||
    kind.value === "printing" ||
    kind.value === "merchant-notice" ||
    kind.value === "template" ||
    kind.value === "transfers" ||
    kind.value === "transfer-batches"
  )
    return;
  if (listRouteError.value) {
    loading.value = false;
    total.value = 0;
    error.value = listRouteError.value;
    return;
  }
  loading.value = true;
  try {
    if (kind.value === "dashboard") {
      const data = await client.value.request<Record<string, unknown>>(
        "/dashboard",
        {
          signal: c.signal,
        },
      );
      if (!c.signal.aborted) stats.value = data;
    } else {
      const q = listQuery();
      const data = await client.value.request<{
        items: Record<string, unknown>[];
        total: number;
      }>(`/${kind.value}?${q}`, { signal: c.signal });
      if (!c.signal.aborted) {
        items.value = data.items || [];
        total.value = data.total || 0;
      }
    }
  } catch (e) {
    if (!c.signal.aborted) {
      error.value = message(e);
      if (e instanceof ApiError && e.status === 401) session.value = null;
    }
  } finally {
    if (!c.signal.aborted) loading.value = false;
  }
}
function openRefund(row: Record<string, unknown>) {
  if (row.income_status === "held" && row.can_refund_held_income !== true) {
    orderDetails.value = row;
    notice.value = "请先读取订单详情，核对冻结款的专用退款来源";
    return;
  }
  refundRow.value = row;
  refundMoney.value = "";
  if (
    area.value === "admin" &&
    /^\d+$/.test(String(row.amount)) &&
    /^\d+$/.test(String(row.refunded_amount ?? "0"))
  ) {
    const remaining =
      BigInt(String(row.amount)) - BigInt(String(row.refunded_amount ?? "0"));
    if (remaining > 0n) refundMoney.value = centsMoney(remaining.toString());
  }
  refundExternal.value = crypto.randomUUID();
  refundFunding.value = null;
  refundFundingBinding.value = "";
  notice.value = "";
  error.value = "";
}
function resetListFilters() {
  listRouteError.value = "";
  includeArchivedOrders.value = false;
  listFilters.value = freshListFilters();
  search.value = "";
  pageNumber.value = 1;
  void load();
}
async function submitRefund() {
  if (!validAmount(refundMoney.value)) {
    error.value = "请输入大于 0、最多两位小数的退款金额";
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    const row = refundRow.value!;
    const binding = JSON.stringify([
      String(row.trade_no ?? row.id),
      refundMoney.value,
      refundExternal.value,
    ]);
    if (
      row.income_status === "held" &&
      refundFundingBinding.value !== binding
    ) {
      const preview = await client.value.request<Record<string, unknown>>(
        `/orders/${encodeURIComponent(String(row.trade_no ?? row.id))}/preview-refund`,
        {
          method: "POST",
          body: {
            amount: refundMoney.value,
            external_no: refundExternal.value,
          },
        },
      );
      if (
        refundRow.value === row &&
        binding ===
          JSON.stringify([
            String(row.trade_no ?? row.id),
            refundMoney.value,
            refundExternal.value,
          ])
      ) {
        refundFunding.value = readHeldRefundFunding(preview, refundMoney.value);
        refundFundingBinding.value = binding;
      }
      return;
    }
    const result = await client.value.requestResult(
      `/orders/${encodeURIComponent(String(row.trade_no ?? row.id))}/refund`,
      {
        method: "POST",
        body: { amount: refundMoney.value, external_no: refundExternal.value },
      },
    );
    refundRow.value = null;
    orderAction.value = null;
    includeArchivedOrders.value = false;
    blacklistSelection.value = null;
    notice.value = refundNotice(result.data, result.status);
    await load();
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
watch(
  area,
  () => {
    enrollmentPurpose.value =
      area.value === "merchant" && promotionPrefill.value ? "register" : null;
    if (area.value !== "merchant") promotionPrefill.value = "";
    refundSummaryQuery.value = null;
    refundSummaryReturn = undefined;
    pageNumber.value = 1;
    search.value = "";
    listFilters.value = freshListFilters();
    notice.value = "";
    refundRow.value = null;
    void initialize();
  },
  { immediate: true },
);
watch(session, (value) => {
  refundSummaryQuery.value = null;
  refundSummaryReturn = undefined;
  if (value) promotionPrefill.value = "";
  if (!value) {
    settleFundsAction(new Error("管理员会话已失效，资金授权已清除"));
    refundRow.value = null;
    orderDetails.value = null;
    orderAction.value = null;
    includeArchivedOrders.value = false;
    items.value = [];
    stats.value = {};
  }
});
watch(
  () => [route.path, route.query],
  () => {
    refundSummaryQuery.value = null;
    refundSummaryReturn = undefined;
    promotionPrefill.value = "";
    settleFundsAction(new Error("页面已切换，资金授权已清除，请核对原业务"));
    enrollmentPurpose.value = null;
    pageNumber.value = 1;
    search.value = "";
    listFilters.value = freshListFilters();
    notice.value = "";
    refundRow.value = null;
    orderAction.value = null;
    orderDetails.value = null;
    includeArchivedOrders.value = false;
    blacklistSelection.value = null;
    applyListRouteQuery();
    void load();
  },
);
onBeforeUnmount(() => {
  promotionPrefill.value = "";
  controller?.abort();
  settleFundsAction(new Error("页面已关闭，资金授权已清除"));
});
</script>

<template>
  <div v-if="initializing" class="loading-screen" role="status">
    正在验证会话…
  </div>
  <main v-else-if="!session" class="login-layout" :data-area="area">
    <section class="login-intro">
      <div class="brand"><span class="brand-mark">G</span> GOEPAY</div>
      <p class="eyebrow">PAYMENT OPERATIONS</p>
      <h1>每一笔交易，<br />清晰可循。</h1>
      <p>交易、退款与资金流转，在同一工作台中有序处理。</p>
      <div class="login-rule">
        <span>交易</span><span>账务</span><span>核对</span>
      </div>
    </section>
    <section class="login-panel">
      <nav class="area-switch" aria-label="登录身份">
        <RouterLink
          to="/admin/dashboard"
          :class="{ selected: area === 'admin' }"
          >管理后台</RouterLink
        ><RouterLink
          to="/merchant/dashboard"
          :class="{ selected: area === 'merchant' }"
          >商户后台</RouterLink
        >
      </nav>
      <PublicEnrollment
        v-if="area === 'merchant' && enrollmentPurpose"
        :key="enrollmentPurpose"
        :purpose="enrollmentPurpose"
        :initial-promotion-token="promotionPrefill"
        @prefill-consumed="promotionPrefill = ''"
        @close="
          enrollmentPurpose = null;
          promotionPrefill = '';
        "
      />
      <template v-else>
        <h2>{{ area === "admin" ? "管理员登录" : "商户登录" }}</h2>
        <p class="muted">使用您的账户继续</p>
        <div v-if="error" class="alert error" role="alert">{{ error }}</div>
        <div v-if="notice" class="alert success" role="status">
          {{ notice }}
        </div>
        <form @submit.prevent="login">
          <label
            >用户名<input
              v-model="username"
              name="username"
              autocomplete="username"
              required
              maxlength="128" /></label
          ><label
            >密码<input
              v-model="password"
              name="password"
              type="password"
              autocomplete="current-password"
              required
              maxlength="1024" /></label
          ><label
            >动态验证码（已绑定验证器时填写）<input
              v-model="loginCode"
              inputmode="numeric"
              pattern="[0-9]{6}"
              maxlength="6"
              autocomplete="one-time-code" /></label
          ><button class="primary full" :disabled="busy">
            {{ busy ? "正在登录…" : "登录工作台" }}
          </button>
        </form>
        <div v-if="area === 'merchant'" class="toolbar-actions">
          <button @click="openEnrollment('register')">注册商户</button>
          <button @click="openEnrollment('recover')">找回密码</button>
        </div>
      </template>
    </section>
  </main>
  <ConsoleShell
    v-else
    :area="area"
    :navigation="navigation"
    :active="path"
    :title="page?.title || '页面不存在'"
    :owner="session"
    :inert="!!fundsAction"
  >
    <template #account>
      <div class="account">
        <span>{{ session.user.name }}</span
        ><RouterLink
          :to="
            '/' +
            area +
            (area === 'admin' ? '/system/change-password' : '/account/settings')
          "
          >账户安全</RouterLink
        ><button :disabled="busy" @click="logout">退出登录</button>
      </div>
    </template>
    <main class="content">
      <p
        v-if="area === 'merchant' && session.delegated_by"
        role="status"
        class="alert"
      >
        管理员 {{ session.delegated_by.name }} 正在代登录本商户。此会话最长 15
        分钟，管理员退出或权限撤销后失效。
      </p>
      <div class="page-heading">
        <div>
          <p class="eyebrow">
            {{ area === "admin" ? "PLATFORM CONSOLE" : "MERCHANT CONSOLE" }}
          </p>
          <h1>{{ page?.title || "页面不存在" }}</h1>
        </div>
        <button
          v-if="
            authorized &&
            ![
              'unavailable',
              'resource',
              'report',
              'workflow',
              'account',
              'operation',
              'commerce',
              'group-sales',
              'settings',
              'invitations',
              'collection-styles',
              'collection-generator',
              'identity-lookup',
              'channel-sharing',
              'referral',
              'referral-policy',
              'settlement-plans',
              'settlement-schedule',
              'printing',
              'merchant-notice',
              'template',
              'transfers',
            ].includes(kind)
          "
          :disabled="loading"
          @click="load"
        >
          {{ loading ? "正在更新…" : "刷新数据" }}
        </button>
      </div>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <div v-if="notice" class="alert success" role="status">
        {{ notice }}
      </div>
      <section v-if="!page || !authorized" class="panel unavailable">
        <h2>{{ !page ? "未找到该页面" : "您没有访问此页面的权限" }}</h2>
        <RouterLink :to="'/' + area + '/dashboard'">返回仪表盘</RouterLink>
      </section>
      <SettlementPlansPage
        v-else-if="kind === 'settlement-plans'"
        :client="client"
        :admin-class="session?.user.admin_class"
        :own-session="!!session && !session.delegated_by"
      />
      <PrintingSettingsPage
        v-else-if="kind === 'printing'"
        :key="area + path"
        :client="client"
        :own-session="!!session && !session.delegated_by"
        :admin-class="session?.user.admin_class"
      />
      <MerchantNoticeSettings
        v-else-if="kind === 'merchant-notice'"
        :key="area + path"
        :client="client"
        :own-session="!!session && !session.delegated_by"
        :admin-class="session?.user.admin_class"
      />
      <TemplatePage
        v-else-if="kind === 'template'"
        :client="client"
        :own-session="!!session && !session.delegated_by"
        :admin-class="session?.user.admin_class"
      />
      <SettlementSchedulePage
        v-else-if="kind === 'settlement-schedule'"
        :client="client"
        :own-session="!!session && !session.delegated_by"
      />
      <ChannelSharingPage
        v-else-if="kind === 'channel-sharing'"
        :key="area + path"
        :client="client"
        :area="area"
        :page="path === '/profitsharing/receiver' ? 'receiver' : 'order'"
      />
      <ReferralPage
        v-else-if="kind === 'referral'"
        :key="area + path"
        :client="client"
      />
      <ReferralPolicyPage
        v-else-if="kind === 'referral-policy'"
        :key="area + path"
        :client="client"
        :admin-class="session.user.admin_class"
      />
      <IdentityLookupPage
        v-else-if="kind === 'identity-lookup'"
        :key="area + path"
        :client="client"
        :area="area"
        :own-session="!!session && !session.delegated_by"
        :admin-class="session.user.admin_class"
      />
      <SettlementRecipientPage
        v-else-if="resourceFor(area, path) === 'settlement-settings'"
        :key="area + path"
        :client="client"
        :area="area"
      />
      <SettingsPage
        v-else-if="kind === 'settings'"
        :key="area + path"
        :client="client"
        :admin-class="session.user.admin_class"
        :own-session="!session.delegated_by"
      />
      <GroupSalesPage
        v-else-if="kind === 'group-sales'"
        :key="area + path"
        :client="client"
        :admin-class="session.user.admin_class"
        :own-session="!session.delegated_by"
      />
      <InvitationCodesPage
        v-else-if="kind === 'invitations'"
        :key="area + path"
        :client="client"
        :own-session="!session.delegated_by"
      />
      <CollectionStylesPage
        v-else-if="kind === 'collection-styles'"
        :key="area + path"
        :client="client"
        :own-session="!session.delegated_by"
        :editing="path === '/other/onecode/edit'"
        :style-id="route.query.id"
      />
      <CollectionGeneratorPage
        v-else-if="kind === 'collection-generator'"
        :key="area + path"
        :client="client"
        :own-session="!session.delegated_by"
      />
      <ResourcePage
        v-else-if="kind === 'resource'"
        :key="area + path"
        :client="client"
        :admin-class="session.user.admin_class"
        :resource="resourceFor(area, path)"
        :entry="resourceEntryFor(area, path, route.query.id)"
        :merchant-query="
          resourceFor(area, path) === 'merchants' ? route.query : undefined
        "
        :own-session="!!session && !session.delegated_by"
      />
      <ReportPage
        v-else-if="kind === 'report'"
        :key="area + path"
        :client="client"
        :report="reportFor(area, path)"
      />
      <SettlementBatchesPage
        v-else-if="kind === 'settlement-batches'"
        :key="area + path"
        :client="client"
        :admin-class="session.user.admin_class"
        :own-session="!session.delegated_by"
        :initial-request-ids="
          typeof route.query.request_ids === 'string'
            ? route.query.request_ids
            : undefined
        "
      />
      <WorkflowPage
        v-else-if="kind === 'workflow'"
        :key="area + path"
        :client="client"
        :area="area"
        :workflow="workflowFor(area, path)"
        :application="area === 'merchant' && path === '/settle/apply'"
      />
      <MerchantAccountSettings
        v-else-if="
          kind === 'account' &&
          area === 'merchant' &&
          path === '/account/settings'
        "
        :client="client"
        :own-session="!!session && !session.delegated_by"
        @reauthenticate="
          (message) => {
            session = null;
            notice = message;
            error = '';
          }
        "
      />
      <div v-else-if="kind === 'account'">
        <AccountPage
          :key="path"
          :client="client"
          :section="
            area === 'admin'
              ? 'security'
              : path === '/account/api-info'
                ? 'api-info'
                : 'profile'
          "
          @reauthenticate="
            session = null;
            notice = '密码已修改，请重新登录';
            error = '';
          "
        />
        <TotpSettings
          v-if="path !== '/account/api-info'"
          :key="area + 'totp'"
          :client="client"
          @reauthenticate="
            session = null;
            notice = '身份验证设置已更新，请重新登录';
            error = '';
          "
        />
        <PaymentPasswordSettings
          v-if="area === 'admin'"
          :key="area + 'funds-security'"
          :client="client"
        />
      </div>
      <div v-else-if="kind === 'operation'">
        <template v-if="['/system/cron', '/app/plugin-list'].includes(path)"
          ><h2>
            {{ path === "/system/cron" ? "任务计划配置" : "本地扩展资料" }}
          </h2>
          <ResourcePage
            :key="path + 'config'"
            :client="client"
            :resource="
              path === '/system/cron' ? 'cron-tasks' : 'plugin-configs'
            "
          />
          <h2 class="operation-heading">
            {{ path === "/system/cron" ? "实际任务队列" : "当前运行能力" }}
          </h2></template
        >
        <OperationsPage
          :key="path"
          :client="client"
          :operation="operationPages[path]"
        />
      </div>
      <CommercePage
        v-else-if="kind === 'commerce'"
        :key="area + path"
        :client="client"
        :area="area"
        :kind="commerceFor(area, path)"
      />
      <TransferBatchPage
        v-else-if="kind === 'transfer-batches'"
        :key="area + path"
        :client="client"
      />
      <TransferPage
        v-else-if="kind === 'transfers'"
        :key="area + path"
        :client="client"
        :area="area"
      />
      <section v-else-if="kind === 'unavailable'" class="panel unavailable">
        <span class="status-dot"></span>
        <h2>此功能尚未接入</h2>
        <p>
          {{
            unavailableReasons[area][path] ||
            page.title + "尚未接入，功能仍待实施。"
          }}
        </p>
        <RouterLink :to="'/' + area + '/dashboard'">返回仪表盘</RouterLink>
      </section>
      <template v-else-if="kind === 'dashboard'"
        ><div v-if="loading" class="panel" role="status">正在加载账户数据…</div>
        <DashboardOverview
          v-else-if="!error"
          :key="area + path"
          :data="stats"
          :area="area"
          :admin-class="session?.user.admin_class"
      /></template>
      <section v-else class="panel records">
        <form
          class="toolbar"
          @submit.prevent="
            pageNumber = 1;
            load();
          "
        >
          <div
            class="search-group"
            :class="{ 'order-search-group': kind === 'orders' }"
          >
            <label
              v-if="kind === 'orders'"
              class="sr-only"
              for="order-search-field"
              >订单搜索字段</label
            >
            <select
              v-if="kind === 'orders'"
              id="order-search-field"
              v-model="listFilters.search_field"
              aria-label="订单搜索字段"
              class="order-search-field"
            >
              <option value="">综合搜索</option>
              <option
                v-for="field in orderSearchFields"
                :key="field.value"
                :value="field.value"
              >
                {{ field.label }}
              </option>
            </select>
            <label class="sr-only" for="search">搜索记录</label
            ><input
              id="search"
              v-model="search"
              :placeholder="
                kind === 'orders' && listFilters.search_field
                  ? ['money', 'realmoney', 'getmoney'].includes(
                      listFilters.search_field,
                    )
                    ? '输入金额（元）'
                    : '搜索内容'
                  : '搜索订单号或业务编号'
              "
              :inputmode="
                ['money', 'realmoney', 'getmoney'].includes(
                  listFilters.search_field,
                )
                  ? 'decimal'
                  : 'text'
              "
            /><button :disabled="loading">查询</button>
          </div>
          <ListFilters
            v-model="listFilters"
            :kind="kind"
            :area="area"
            @reset="resetListFilters"
          />
          <AutoRefreshControl
            v-if="kind === 'orders'"
            :key="area + path"
            :refresh="load"
            :blocked="
              loading ||
              busy ||
              !!orderAction ||
              !!orderDetails ||
              !!refundRow ||
              orderBatchOpen ||
              !!blacklistSelection ||
              !!listRouteError
            "
          />
          <CsvExportButton
            v-if="['orders', 'refunds', 'ledger'].includes(kind)"
            :client="client"
            :path="listCSVPath"
            :filename="`${kind}-filtered.csv`"
            :disabled="loading || !!listRouteError"
          />
          <button
            v-if="kind === 'refunds'"
            type="button"
            :disabled="loading || busy || !!listRouteError"
            @click="openRefundSummary"
          >
            退款统计
          </button>
          <label
            v-if="kind === 'orders' && area === 'admin'"
            class="archived-order-toggle"
            ><input
              v-model="includeArchivedOrders"
              type="checkbox"
              @change="
                pageNumber = 1;
                load();
              "
            />显示已删除订单</label
          >
        </form>
        <div v-if="loading" class="loading-row" role="status">
          正在加载记录…
        </div>
        <OrderBatchControls
          v-if="kind === 'orders' && area === 'admin' && !error"
          :key="area + path"
          :client="client"
          :items="items"
          :selected="selectedOrderTrades"
          :own-session="!!session && !session.delegated_by"
          :admin-class="session?.user.admin_class"
          @busy="orderBatchBusy = $event"
          @active="orderBatchOpen = $event"
          @updated="load"
        />
        <DataTable
          v-if="!loading && !error"
          :items="items"
          :columns="columns"
          :orders="kind === 'orders'"
          :admin-orders="kind === 'orders' && area === 'admin'"
          :selectable="kind === 'orders' && area === 'admin'"
          :selected-trades="selectedOrderTrades"
          @selection-change="selectedOrderTrades = $event"
          :order-actions-disabled="
            orderBatchBusy ||
            !!session?.delegated_by ||
            !['super', 'operator'].includes(session?.user.admin_class || '')
          "
          @refund="openRefund"
          @detail="(row) => (orderDetails = row)"
          @order-action="(row, action) => (orderAction = { row, action })"
          @blacklist="openBlacklist"
        />
        <div class="pagination">
          <span>共 {{ total }} 条记录</span>
          <div>
            <button
              :disabled="pageNumber <= 1 || loading"
              @click="
                pageNumber--;
                load();
              "
            >
              上一页</button
            ><span>第 {{ pageNumber }} 页</span
            ><button
              :disabled="pageNumber * listLimit >= total || loading"
              @click="
                pageNumber++;
                load();
              "
            >
              下一页
            </button>
          </div>
        </div>
      </section>
      <LegacyRefundHistoryPanel
        v-if="authorized && kind === 'refunds'"
        :key="area + path + 'legacy-history'"
        :client="client"
        :own-session="!!session && !session.delegated_by"
      />
    </main>
    <footer>GOEPAY · 支付运营工作台</footer>
    <template #overlays>
      <RefundSummaryDialog
        v-if="refundSummaryQuery !== null && authorized && kind === 'refunds'"
        :client="client"
        :query="refundSummaryQuery"
        @close="closeRefundSummary"
      />
      <OrderBlacklistDialog
        v-if="blacklistSelection && session && authorized && area === 'admin'"
        :key="blacklistSelection.tradeNo + '/' + blacklistSelection.kind"
        :client="client"
        :trade-no="blacklistSelection.tradeNo"
        :kind="blacklistSelection.kind"
        :expected="blacklistSelection.expected"
        :own-session="!session.delegated_by"
        :admin-class="session.user.admin_class"
        @close="blacklistSelection = null"
        @updated="load"
      />
      <AdminOrderActionDialog
        v-if="orderAction && session && authorized && area === 'admin'"
        :key="
          String(orderAction.row.trade_no ?? orderAction.row.id) +
          '/' +
          orderAction.action
        "
        :client="client"
        :trade-no="String(orderAction.row.trade_no ?? orderAction.row.id)"
        :action="orderAction.action"
        @close="orderAction = null"
        @updated="load"
        @refund="
          (row) => {
            orderAction = null;
            openRefund(row);
          }
        "
      />
      <OrderDetails
        v-if="orderDetails"
        :key="String(orderDetails.trade_no ?? orderDetails.id)"
        :trade-no="String(orderDetails.trade_no ?? orderDetails.id)"
        :client="client"
        :own-session="!!session && !session.delegated_by"
        :can-blacklist="
          area === 'admin' &&
          !!session &&
          !session.delegated_by &&
          ['super', 'operator'].includes(session.user.admin_class || '')
        "
        @blacklist="openBlacklist"
        @close="orderDetails = null"
        @refund="
          (row) => {
            orderDetails = null;
            openRefund(row);
          }
        "
        @updated="load"
      />
      <div
        v-if="refundRow"
        class="modal-backdrop"
        @keydown.esc="!busy && (refundRow = null)"
      >
        <section
          class="modal panel"
          role="dialog"
          aria-modal="true"
          :aria-label="area === 'admin' ? 'API退款确认' : '申请退款'"
        >
          <h2>{{ area === "admin" ? "API退款确认" : "申请退款" }}</h2>
          <div v-if="error" class="alert error" role="alert">{{ error }}</div>
          <form v-if="refundRow" @submit.prevent="submitRefund">
            <p class="muted">订单 {{ refundRow.trade_no ?? refundRow.id }}</p>
            <label
              >退款金额（元）<input
                v-model="refundMoney"
                inputmode="decimal"
                required
                autofocus /></label
            ><label
              >退款请求号<input
                v-model="refundExternal"
                required
                maxlength="128"
            /></label>
            <p class="muted">
              提交后请查看退款记录确认处理结果。重复提交请保留相同请求号。
            </p>
            <div v-if="refundFunding" class="alert" role="status">
              <h3>原订单退款资金来源</h3>
              <dl class="record-detail">
                <dt>原冻结批次</dt>
                <dd>{{ displayValue(refundFunding.hold_id) }}</dd>
                <dt>本次使用原订单冻结款（元）</dt>
                <dd>{{ centsMoney(refundFunding.held_amount) }}</dd>
                <dt>本次使用商户可用余额（元）</dt>
                <dd>{{ centsMoney(refundFunding.available_amount) }}</dd>
                <dt>本次平台手续费部分（元）</dt>
                <dd>{{ centsMoney(refundFunding.fee_amount) }}</dd>
              </dl>
              <p>
                确认时服务端会重新核对原资金来源。结果未知时继续预留，不能解冻或重复发送。
              </p>
            </div>
            <div class="modal-actions">
              <button type="button" :disabled="busy" @click="refundRow = null">
                取消</button
              ><button class="primary" :disabled="busy">
                {{
                  busy
                    ? "正在处理…"
                    : refundRow.income_status === "held" && !refundFunding
                      ? "核对退款资金来源"
                      : "确认申请退款"
                }}
              </button>
            </div>
          </form>
        </section>
      </div>
    </template>
  </ConsoleShell>
  <FundsAuthorizationModal
    v-if="fundsAction"
    :client="client"
    :target="fundsAction.target"
    :body="fundsAction.body"
    @close="settleFundsAction(new Error('资金授权已取消，尚未自动提交原业务'))"
    @completed="(result) => settleFundsAction(undefined, result)"
    @failed="(error) => settleFundsAction(error)"
  />
</template>
