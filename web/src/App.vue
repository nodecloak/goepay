<script setup lang="ts">
import { orderRefundRemaining104 } from "./order-charge104";
import { isPersonalMerchantID } from "./merchant-personal-settings";
import {
  fetchPublicMerchantAccess,
  type PublicMerchantAccess,
} from "./platform-merchant-access";
import {
  computed,
  ref,
  watch,
  onBeforeUnmount,
  onMounted,
  nextTick,
  provide,
} from "vue";
import { useRoute, useRouter } from "vue-router";
import { applyTheme, saveTheme, storedTheme, type ConsoleTheme } from "./theme";
import {
  merchantLoginEntry205,
  merchantLoginReturn205,
} from "./login-document205";
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
  fundsPrincipal212,
  fundsScope212,
  FundsIntentStore212,
  fundsDefinitiveRefusal212,
  fundsAmountCents212,
  verifiedRefund212,
  type FundsIntent212,
} from "./funds-intent212";
import {
  resolvePage,
  viewKind,
  navigationFor,
  pageAuthorizedFor,
  type Area,
} from "./menu";
import { setAdminPermissionCatalog } from "./admin-permission-context";
import ConsoleShell from "./components/ConsoleShell.vue";
import ConsoleTopBarTools from "./components/ConsoleTopBarTools.vue";
import ToastStack from "./components/ToastStack.vue";
import SiteBrandIcon193 from "./components/SiteBrandIcon193.vue";
import SiteContacts191 from "./components/SiteContacts191.vue";
import LoginCaptcha205 from "./components/LoginCaptcha205.vue";
const loginCaptchaRef205 = ref<InstanceType<typeof LoginCaptcha205> | null>(
  null,
);
import { usePublicSite191 } from "./site191";
const { site: publicSite191, reload: reloadPublicSite191 } = usePublicSite191();
import DataTable from "./components/DataTable.vue";
import LedgerRecordsTable from "./components/LedgerRecordsTable.vue";
import { proListColumns } from "./pro-console-fields";
import ResourcePage from "./components/ResourcePage.vue";
import AnnouncementsPage from "./components/AnnouncementsPage.vue";
import RichContent from "./components/RichContent.vue";
import {
  publicAnnouncementLayout,
  type PublicAnnouncementLayout,
} from "./announcement-layout";
import SubchannelPage from "./components/SubchannelPage.vue";
import RefundSummaryDialog from "./components/RefundSummaryDialog.vue";
import OrderSummaryDialog from "./components/OrderSummaryDialog.vue";
import LedgerSummaryDialog from "./components/LedgerSummaryDialog.vue";
import { resourceFor, resourceEntryFor } from "./resources";
import { reportFor } from "./reports";
import ReportPage from "./components/ReportPage.vue";
import { workflowFor } from "./workflows";
import WorkflowPage from "./components/WorkflowPage.vue";
import AccountPage from "./components/AccountPage.vue";
import MerchantAccountSettings from "./components/MerchantAccountSettings.vue";
import OperationsPage from "./components/OperationsPage.vue";
import SystemUpdatePage from "./components/SystemUpdatePage.vue";
import TaskSchedulerPage from "./components/TaskSchedulerPage.vue";
import { operationPages } from "./operations";
import TotpSettings from "./components/TotpSettings.vue";
import CommercePage from "./components/CommercePage.vue";
import { commerceFor } from "./commerce";
import { unavailableReasons } from "./deferred";
import OrderDetails from "./components/OrderDetails.vue";
import MerchantOrderActionDialog from "./components/MerchantOrderActionDialog.vue";
import type { MerchantOrderActionID } from "./order-actions";
import AdminOrderActionDialog from "./components/AdminOrderActionDialog.vue";
import type { OrderActionID, AdminOrderMenuActionID } from "./order-actions";
import { merchantOrderActions } from "./order-actions";
import { refundNotice } from "./order-operations";
import { centsMoney } from "./settlement-terms";
import { readHeldRefundFunding } from "./refund-funding";
import TransferPage from "./components/TransferPage.vue";
import GroupSalesPage from "./components/GroupSalesPage.vue";
import SettingsPage from "./components/SettingsPage.vue";
import MerchantCertification233 from "./components/MerchantCertification233.vue";
import MigrationWizard234 from "./components/MigrationWizard234.vue";
import InvitationCodesPage from "./components/InvitationCodesPage.vue";
import CollectionStylesPage from "./components/CollectionStylesPage.vue";
import CollectionGeneratorPage from "./components/CollectionGeneratorPage.vue";
import MerchantScanPage192 from "./components/MerchantScanPage192.vue";
import OrderBatchControls from "./components/OrderBatchControls.vue";
import MerchantOrderBatchControls from "./components/MerchantOrderBatchControls.vue";
import AutoRefreshControl from "./components/AutoRefreshControl.vue";
import OrderBlacklistDialog from "./components/OrderBlacklistDialog.vue";
import DashboardOverview from "./components/DashboardOverview.vue";
import TransferBatchPage from "./components/TransferBatchPage.vue";
import RedpackPage from "./components/RedpackPage.vue";
import SettlementBatchesPage from "./components/SettlementBatchesPage.vue";
import ListFilters from "./components/ListFilters.vue";
import KitPagination from "./components/KitPagination.vue";
import KitField from "./components/KitField.vue";
import KitSelect from "./components/KitSelect.vue";
import KitInput from "./components/KitInput.vue";
import {
  freshListFilters,
  appendListFilters,
  orderSearchFields,
  refundSearchFields,
  readListRouteQuery,
} from "./list-filters";
import { usePayTypeOptions } from "./pay-type-options";
import PublicEnrollment from "./components/PublicEnrollment.vue";
import PublicTestPay203 from "./components/PublicTestPay203.vue";
import CsvExportButton from "./components/CsvExportButton.vue";
import ShellIcon from "./components/ShellIcon.vue";
import PaymentPasswordSettings from "./components/PaymentPasswordSettings.vue";
import FundsAuthorizationModal from "./components/FundsAuthorizationModal.vue";
import SettlementRecipientPage from "./components/SettlementRecipientPage.vue";
import IdentityLookupPage from "./components/IdentityLookupPage.vue";
import ChannelSharingPage from "./components/ChannelSharingPage.vue";
import ChannelSharingRuleList from "./components/ChannelSharingRuleList.vue";
import AdminChannelSharingRules from "./components/AdminChannelSharingRules.vue";
import ChannelSharingOrderList from "./components/ChannelSharingOrderList.vue";
import ReferralPage from "./components/ReferralPage.vue";
import ReferralPolicyPage from "./components/ReferralPolicyPage.vue";
import SettlementPlansPage from "./components/SettlementPlansPage.vue";
import PrintingSettingsPage from "./components/PrintingSettingsPage.vue";
import MerchantNoticeSettings from "./components/MerchantNoticeSettings.vue";
import LegacyRefundHistoryPanel from "./components/LegacyRefundHistoryPanel.vue";
import TemplatePage from "./components/TemplatePage.vue";
import AppStorePage from "./components/AppStorePage.vue";
import LocalPluginManagement from "./components/LocalPluginManagement.vue";
import SettlementSchedulePage from "./components/SettlementSchedulePage.vue";
import ComplaintsPage from "./components/ComplaintsPage.vue";
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
  if (busy.value || loginUncertain.value) return;
  loginEpoch++;
  loginAPIKey.value = "";
  password.value = "";
  loginCode.value = "";
  error.value = "";
  notice.value = "";
  enrollmentPurpose.value = purpose;
}
function enrollmentFromEntry216(allowPromotion = false): "register" | null {
  if (area.value !== "merchant" || session.value) return null;
  if (route.path === "/merchant/login" && route.query.enroll === "register")
    return "register";
  return allowPromotion && promotionPrefill.value ? "register" : null;
}
async function closeEnrollment216() {
  enrollmentPurpose.value = null;
  promotionPrefill.value = "";
  if (route.path !== "/merchant/login" || !Object.hasOwn(route.query, "enroll"))
    return;
  const query = { ...route.query };
  delete query.enroll;
  try {
    await router.replace({ path: "/merchant/login", query, hash: route.hash });
  } catch {
    error.value = "返回登录页面失败，请重新加载页面";
  }
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
const orderSummaryQuery = ref<string | null>(null);
let orderSummaryReturn: HTMLElement | undefined;
function openOrderSummary() {
  if (
    !authorized.value ||
    kind.value !== "orders" ||
    loading.value ||
    busy.value ||
    listRouteError.value
  )
    return;
  const q = listQuery();
  q.delete("page");
  q.delete("limit");
  orderSummaryReturn =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : undefined;
  orderSummaryQuery.value = q.toString();
}
function closeOrderSummary() {
  const target = orderSummaryReturn,
    context = client.value,
    csrf = context.csrfToken;
  orderSummaryQuery.value = null;
  orderSummaryReturn = undefined;
  void nextTick(() => {
    if (
      client.value === context &&
      context.csrfToken === csrf &&
      target?.isConnected
    )
      target.focus();
  });
}
const ledgerSummaryQuery = ref<string | null>(null);
let ledgerSummaryReturn: HTMLElement | undefined;
function openLedgerSummary() {
  if (
    !authorized.value ||
    kind.value !== "ledger" ||
    loading.value ||
    busy.value ||
    listRouteError.value
  )
    return;
  const q = listQuery();
  q.delete("page");
  q.delete("limit");
  ledgerSummaryReturn =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : undefined;
  ledgerSummaryQuery.value = q.toString();
}
function closeLedgerSummary() {
  const target = ledgerSummaryReturn,
    context = client.value,
    csrf = context.csrfToken;
  ledgerSummaryQuery.value = null;
  ledgerSummaryReturn = undefined;
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
const router = useRouter();
const merchantLoginDocument205 = computed(
  () => route.path === "/merchant/login",
);
const adminLoginDocument = computed(() => route.path === "/admin/login");
const publicTestPayEntry203 = computed(
  () => route.path === "/test" || route.path === "/test/",
);
const area = computed<Area>(() =>
  route.path.split("/")[1] === "merchant" ? "merchant" : "admin",
);
const path = computed(
  () => "/" + route.path.split("/").slice(2).join("/") || "/dashboard",
);
const page = computed(() => resolvePage(area.value, path.value));
const kind = computed(() => viewKind(area.value, path.value));
const sharingFlowOpen = ref(false);
const sharingFlowBusy = ref(false);
const sharingFlowContext = ref({ merchant_id: "", sharing_external_no: "" });
let sharingFlowTrigger: HTMLElement | null = null;
function openSharingFlow(context: {
  merchant_id: string;
  sharing_external_no: string;
}) {
  if (sharingFlowBusy.value) return;
  sharingFlowTrigger = document.activeElement as HTMLElement;
  sharingFlowContext.value = context;
  sharingFlowOpen.value = true;
}
function closeSharingFlow() {
  if (sharingFlowBusy.value) return;
  sharingFlowOpen.value = false;
  if (sharingFlowTrigger?.isConnected) sharingFlowTrigger.focus();
}
watch([area, path], () => {
  sharingFlowOpen.value = false;
  sharingFlowBusy.value = false;
  sharingFlowContext.value = { merchant_id: "", sharing_external_no: "" };
});
const searchFieldOptions = computed(() => [
  { value: "", label: "综合搜索" },
  ...(kind.value === "refunds" ? refundSearchFields : orderSearchFields).map(
    (f) => ({ value: f.value, label: f.label }),
  ),
]);
const searchFieldInputmode = computed<"decimal" | "text">(() =>
  ["money", "realmoney", "getmoney"].includes(listFilters.value.search_field)
    ? "decimal"
    : "text",
);
const searchFieldPlaceholder = computed(() =>
  ["orders", "refunds"].includes(kind.value) && listFilters.value.search_field
    ? searchFieldInputmode.value === "decimal"
      ? "输入金额（元）"
      : "搜索内容"
    : "搜索订单号或业务编号",
);
const client = ref(new ApiClient(area.value)),
  session = ref<Session | null>(null),
  initializing = ref(true),
  busy = ref(false),
  loading = ref(false);
const moneyPrincipal212 = computed(() =>
  session.value
    ? {
        area: area.value,
        id: String(session.value.user.id),
        issuer: String(session.value.delegated_by?.id || ""),
        csrf: session.value.csrf_token,
      }
    : null,
);
provide(fundsPrincipal212, moneyPrincipal212);
// Only the order and refund lists show the 支付方式 filter; other pages must
// not send this read (pages such as 计划任务 assert they read nothing extra).
const payTypeFilterOptions = usePayTypeOptions(
  client,
  computed(
    () => session.value !== null && ["orders", "refunds"].includes(kind.value),
  ),
);
const error = ref(""),
  notice = ref(""),
  username = ref(""),
  password = ref(""),
  loginCode = ref(""),
  loginMode = ref<"password" | "key">("password"),
  loginMerchantID = ref(""),
  loginAPIKey = ref(""),
  loginUncertain = ref(false),
  search = ref(""),
  pageNumber = ref(1),
  items = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  stats = ref<Record<string, unknown>>({});
const loginPlatformAccess = ref<PublicMerchantAccess | null>(null),
  loginPlatformLoading = ref(false),
  loginPlatformError = ref("");
let loginPlatformController: AbortController | undefined,
  loginPlatformEpoch = 0;
async function loadLoginPlatformAccess() {
  const run = ++loginPlatformEpoch,
    current = epoch,
    c = client.value;
  loginPlatformController?.abort();
  const ac = new AbortController();
  loginPlatformController = ac;
  loginPlatformAccess.value = null;
  loginPlatformError.value = "";
  loginMode.value = "password";
  loginAPIKey.value = "";
  if (area.value !== "merchant" || session.value) return;
  loginPlatformLoading.value = true;
  const valid = () =>
    run === loginPlatformEpoch &&
    current === epoch &&
    c === client.value &&
    !ac.signal.aborted &&
    area.value === "merchant" &&
    !session.value;
  try {
    const v = await fetchPublicMerchantAccess(ac.signal);
    if (valid()) loginPlatformAccess.value = v;
  } catch {
    if (valid())
      loginPlatformError.value = "平台登录功能状态无法确认，密码登录仍可使用。";
  } finally {
    if (valid()) loginPlatformLoading.value = false;
  }
}
// Quick-login (QQ/微信/支付宝/Telegram): which providers the admin enabled, and
// the pending-connect completion after an unknown-identity callback.
const connectProviders = ref<Record<string, boolean>>({});
const connectPending = ref("");
const connectForm = ref({ merchant_id: "", api_key: "", code: "" });
const connectBusy = ref(false);
const connectError = ref("");
const connectTitles: Record<string, string> = {
  qq: "QQ",
  wx: "微信",
  alipay: "支付宝",
  telegram: "Telegram",
};
async function loadConnectOptions() {
  connectProviders.value = {};
  if (area.value !== "merchant" || session.value) return;
  try {
    const r = await fetch("/console/v1/public/connect/options", {
      credentials: "same-origin",
      cache: "no-store",
    });
    const envelope = await r.json();
    if (r.ok && envelope?.code === 0 && envelope.data?.providers)
      connectProviders.value = envelope.data.providers;
  } catch {
    connectProviders.value = {};
  }
}
function startQuickLogin(provider: string) {
  if (!connectProviders.value[provider]) return;
  window.location.href = "/console/v1/public/connect/" + provider + "/start";
}
const connectProviderList = computed(() =>
  (["qq", "wx", "alipay", "telegram"] as const).filter(
    (p) => connectProviders.value[p],
  ),
);
async function submitConnectComplete() {
  if (connectBusy.value || !connectPending.value) return;
  connectBusy.value = true;
  connectError.value = "";
  try {
    const r = await fetch("/console/v1/public/connect/complete", {
      method: "POST",
      credentials: "same-origin",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        state: connectPending.value,
        merchant_id: connectForm.value.merchant_id,
        api_key: connectForm.value.api_key,
        code: connectForm.value.code,
      }),
    });
    const envelope = await r.json();
    if (r.ok && envelope?.code === 0) {
      connectPending.value = "";
      connectForm.value = { merchant_id: "", api_key: "", code: "" };
      await initialize();
      return;
    }
    connectError.value =
      typeof envelope?.message === "string" && envelope.message
        ? envelope.message
        : "绑定登录未成功，请核对商户ID与密钥后重试";
  } catch {
    connectError.value = "绑定登录结果无法确认，请稍后重试";
  } finally {
    connectBusy.value = false;
  }
}
const navigation = computed(() =>
  navigationFor(
    area.value,
    session.value?.user.role || "",
    session.value?.user.admin_class,
    session.value?.user.permissions,
    session.value?.merchant_features,
  ),
);
watch(session, (s) => setAdminPermissionCatalog(s?.admin_permission_catalog), {
  immediate: true,
});
watch(items, () => {
  selectedOrderTrades.value = [];
});
const authorized = computed(
  () =>
    !!page.value &&
    pageAuthorizedFor(
      area.value,
      page.value.path,
      session.value?.user.role || "",
      session.value?.user.admin_class,
      session.value?.user.permissions,
      session.value?.merchant_features,
    ),
);
const refundRow = ref<Record<string, unknown> | null>(null),
  orderDetails = ref<Record<string, unknown> | null>(null),
  refundMoney = ref(""),
  refundExternal = ref(""),
  refundFunding = ref<Record<string, unknown> | null>(null),
  refundFundingBinding = ref("");
const refundIntent212 = ref<FundsIntent212 | null>(null),
  refundRestoring212 = ref(false),
  refundStorageError212 = ref(""),
  refundRecoveryOnly212 = ref(false),
  refundListRecoveryError212 = ref("");
let refundStore212: FundsIntentStore212 | null = null,
  refundEpoch212 = 0;
watch(
  moneyPrincipal212,
  () => {
    refundEpoch212++;
    refundStore212 = null;
    refundIntent212.value = null;
    refundRestoring212.value = false;
    refundStorageError212.value = "";
    refundRecoveryOnly212.value = false;
    refundListRecoveryError212.value = "";
  },
  { flush: "sync" },
);
const refundRecoveryNotice212 = computed(() => {
  const intent = refundIntent212.value;
  if (!intent) return "";
  if (intent.record?.status === "succeeded")
    return "原退款已核实：退款成功，请核对资金明细。";
  if (intent.record?.status === "failed")
    return "原退款已核实：退款失败，请核对记录后结束本次操作。";
  return intent.phase === "accepted"
    ? "原退款申请已登记，处理结果尚待核对；金额和请求号保持锁定。"
    : "上次退款结果尚未核实；原金额和请求号已锁定，请查询原退款或重试原请求。";
});
let orderDetailsOpener: {
  element: HTMLElement;
  client: ApiClient;
  csrf: string;
  path: string;
} | null = null;
const merchantOrderAction = ref<{
    trade: string;
    action: "query" | "notify" | "refund" | "print";
  } | null>(null),
  merchantOrderBusy = ref(false);
// Batch refund is stepped one order at a time through the existing per-order
// refund flow (merchant refund permission + rules + funds authorisation); it is
// never auto-fired. The queue holds the remaining selected trades.
const refundStepQueue = ref<string[]>([]);
function startMerchantRefundStep(trades: string[]) {
  if (area.value !== "merchant") return;
  refundStepQueue.value = trades
    .filter((t) => /^[A-Za-z0-9_-]{1,64}$/.test(t))
    .slice();
  advanceMerchantRefundStep();
}
function advanceMerchantRefundStep() {
  if (area.value !== "merchant") {
    refundStepQueue.value = [];
    return;
  }
  // Re-check each row against the server's current eligibility before opening;
  // a row no longer refundable is skipped, never guessed.
  while (refundStepQueue.value.length) {
    const trade = refundStepQueue.value.shift()!;
    const row = items.value.find((r) => String(r.trade_no) === trade);
    if (row && merchantOrderActions(row).some((a) => a.id === "refund")) {
      openMerchantOrderAction(row, "refund");
      return;
    }
  }
}
const refundDialog = ref<HTMLElement>();
function trapRefundFocus(event: KeyboardEvent) {
  if (event.key !== "Tab" || !refundDialog.value) return;
  const all = Array.from(
    refundDialog.value.querySelectorAll<HTMLElement>(
      "input:not([disabled]),button:not([disabled]),a[href]",
    ),
  );
  const first = all[0],
    last = all.at(-1);
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}
type MerchantMenuReturn = {
  element: HTMLElement | null;
  trade: string;
  client: ApiClient;
  csrf: string;
  path: string;
};
let merchantMenuReturn: MerchantMenuReturn | null = null,
  refundMenuReturn: MerchantMenuReturn | null = null,
  pendingMenuFocus: MerchantMenuReturn | null = null;
async function returnMerchantFocus(context: MerchantMenuReturn | null) {
  if (!context) return;
  await nextTick();
  if (
    context.client !== client.value ||
    context.csrf !== client.value.csrfToken ||
    context.path !== route.fullPath ||
    merchantOrderAction.value ||
    orderDetails.value ||
    refundRow.value
  )
    return;
  if (loading.value) {
    pendingMenuFocus = context;
    return;
  }
  pendingMenuFocus = null;
  const target = context.element?.isConnected
    ? context.element
    : document.querySelector<HTMLElement>(
        `button[aria-label="订单操作 ${context.trade}"]`,
      );
  target?.focus();
}
watch(loading, (value) => {
  if (!value && pendingMenuFocus) void returnMerchantFocus(pendingMenuFocus);
});
function openAdminOrderAction(
  row: Record<string, unknown>,
  action: AdminOrderMenuActionID,
) {
  if (area.value !== "admin") return;
  if (action === "recover-refund") {
    if (row.local_refund_recovery212 !== true) return;
    refundMenuReturn = {
      element:
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null,
      trade: String(row.trade_no),
      client: client.value,
      csrf: client.value.csrfToken,
      path: route.fullPath,
    };
    void openRefund(row, true);
    return;
  }
  orderAction.value = { row, action };
}
function openMerchantOrderAction(
  row: Record<string, unknown>,
  action: MerchantOrderActionID,
) {
  if (area.value !== "merchant") return;
  if (action === "detail") {
    openOrderDetails(row);
    return;
  }
  if (action === "recover-refund") {
    if (row.local_refund_recovery212 !== true) return;
    refundMenuReturn = {
      element:
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null,
      trade: String(row.trade_no),
      client: client.value,
      csrf: client.value.csrfToken,
      path: route.fullPath,
    };
    void openRefund(row, true);
    return;
  }
  if (!["query", "notify", "refund", "print"].includes(action)) return;
  const trade = row.trade_no;
  if (typeof trade !== "string" || !/^[A-Za-z0-9_-]{1,64}$/.test(trade)) return;
  merchantMenuReturn = {
    element:
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null,
    trade,
    client: client.value,
    csrf: client.value.csrfToken,
    path: route.fullPath,
  };
  merchantOrderAction.value = {
    trade,
    action: action as "query" | "notify" | "refund" | "print",
  };
}
async function closeMerchantOrderAction() {
  const context = merchantMenuReturn;
  merchantMenuReturn = null;
  merchantOrderAction.value = null;
  merchantOrderBusy.value = false;
  await returnMerchantFocus(context);
}
function merchantRefund(row: Record<string, unknown>) {
  refundMenuReturn = merchantMenuReturn;
  merchantMenuReturn = null;
  merchantOrderAction.value = null;
  merchantOrderBusy.value = false;
  openRefund(row);
  void nextTick(() =>
    refundDialog.value?.querySelector<HTMLInputElement>("input")?.focus(),
  );
}
async function closeRefundModal() {
  if (busy.value) return;
  refundRow.value = null;
  // The table uses the shared error state to decide whether it can render.
  // A dismissed refund error must not hide the row needed to resume its intent.
  error.value = "";
  if (kind.value === "orders")
    items.value = await refundRecoveryRows212(items.value);
  const context = refundMenuReturn;
  refundMenuReturn = null;
  await returnMerchantFocus(context);
  if (refundStepQueue.value.length) void nextTick(advanceMerchantRefundStep);
}
watch(
  () => [client.value, client.value.csrfToken, route.fullPath],
  () => {
    merchantOrderAction.value = null;
    merchantOrderBusy.value = false;
    merchantMenuReturn = null;
    refundMenuReturn = null;
    pendingMenuFocus = null;
    refundStepQueue.value = [];
  },
);
function openOrderDetails(row: Record<string, unknown>) {
  orderDetailsOpener =
    document.activeElement instanceof HTMLElement
      ? {
          element: document.activeElement,
          client: client.value,
          csrf: client.value.csrfToken,
          path: route.fullPath,
        }
      : null;
  orderDetails.value = row;
}
async function closeOrderDetails() {
  const opener = orderDetailsOpener;
  orderDetailsOpener = null;
  orderDetails.value = null;
  await nextTick();
  if (
    opener &&
    !orderDetails.value &&
    !refundRow.value &&
    opener.element.isConnected &&
    opener.client === client.value &&
    opener.csrf === client.value.csrfToken &&
    opener.path === route.fullPath
  )
    opener.element.focus();
}
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
let loginEpoch = 0,
  loginController: AbortController | undefined;
watch(
  loginMode,
  () => {
    loginEpoch++;
    loginController?.abort();
    loginAPIKey.value = "";
    password.value = "";
    loginCode.value = "";
    error.value = "";
  },
  { flush: "sync" },
);
async function initialize() {
  const current = ++epoch;
  loginEpoch++;
  loginController?.abort();
  loginAPIKey.value = "";
  password.value = "";
  loginCode.value = "";
  loginMode.value = "password";
  loginUncertain.value = false;
  busy.value = false;
  initializing.value = true;
  session.value = null;
  loginPlatformEpoch++;
  loginPlatformController?.abort();
  loginPlatformAccess.value = null;
  loginPlatformLoading.value = false;
  loginPlatformError.value = "";
  error.value = "";
  client.value = new ApiClient(area.value);
  connectFundsAuthorization(client.value);
  client.value.onUnauthorized = () => {
    if (current === epoch) {
      if (session.value) notice.value = "登录状态已失效，请重新登录";
      session.value = null;
    }
  };
  if (publicTestPayEntry203.value) {
    controller?.abort();
    loading.value = false;
    initializing.value = false;
    return;
  }
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
        if (merchantLoginDocument205.value) return;
        applyListRouteQuery();
        await load();
      } else if (area.value === "merchant") {
        if (merchantLoginDocument205.value && route.query.reauth === "password")
          notice.value = "请使用当前有效密码重新登录";
        else if (
          merchantLoginDocument205.value &&
          route.query.reauth === "security"
        )
          notice.value = "请重新登录以继续操作";
        void loadLoginPlatformAccess();
        void loadConnectOptions();
        connectPending.value =
          typeof route.query.connect === "string" ? route.query.connect : "";
        if (connectPending.value)
          notice.value = "请输入商户ID和密钥完成绑定和登录";
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
    ["orders", "refunds"].includes(kind.value) && listFilters.value.search_field
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
  if (busy.value || loginUncertain.value) return;
  if (
    loginMode.value === "key" &&
    loginPlatformAccess.value?.close_keylogin !== 0
  ) {
    loginMode.value = "password";
    loginAPIKey.value = "";
    error.value = "请先核对平台密钥登录状态";
    return;
  }
  if (
    loginMode.value === "key" &&
    (!isPersonalMerchantID(loginMerchantID.value) ||
      !loginAPIKey.value ||
      loginAPIKey.value.length > 512)
  ) {
    error.value = "请填写完整商户ID和商户密钥";
    return;
  }
  const c = client.value,
    current = epoch,
    run = ++loginEpoch,
    mode = loginMode.value,
    owner = loginMerchantID.value,
    secret = mode === "key" ? loginAPIKey.value : password.value,
    code = loginCode.value,
    name = username.value;
  const ac = new AbortController();
  loginController?.abort();
  loginController = ac;
  const currentContext = () =>
    !ac.signal.aborted &&
    current === epoch &&
    run === loginEpoch &&
    c === client.value &&
    mode === loginMode.value;
  loginAPIKey.value = "";
  notice.value = "";
  busy.value = true;
  error.value = "";
  let credentialsSent205 = false;
  const captcha205 =
    mode === "password" && c.area === "merchant"
      ? loginCaptchaRef205.value
      : null;
  try {
    let proof205: Record<string, string> = {};
    if (mode === "password" && c.area === "merchant") {
      if (!captcha205) throw Error("请先核对登录验证码设置");
      proof205 = await captcha205.verify();
      if (!currentContext() || name !== username.value) return;
    }
    credentialsSent205 = true;
    const result =
      mode === "key"
        ? await c.merchantKeyLogin(owner, secret, code, ac.signal)
        : c.area === "merchant"
          ? await c.login(name, secret, code, {
              captcha: proof205,
              signal: ac.signal,
            })
          : await c.login(name, secret, code);
    if (!currentContext()) return;
    session.value = result;
    loginCode.value = "";
    password.value = "";
    if (merchantLoginDocument205.value) return;
    applyListRouteQuery();
    await load();
  } catch (e) {
    if (!currentContext()) return;
    if (credentialsSent205 && (mode === "key" || c.area === "merchant")) {
      // A transport error or unverified response may follow a committed cookie.
      // Only read /session before another credential submission.
      const knownRejection =
        e instanceof ApiError &&
        e.responseDecoded &&
        (mode === "key"
          ? [400, 401, 403, 422, 429]
          : [400, 401, 403, 409, 422, 429]
        ).includes(e.status);
      loginUncertain.value = !knownRejection;
    }
    error.value = loginUncertain.value
      ? "登录结果无法确认，请重新检查登录状态后再操作"
      : message(e);
  } finally {
    if (currentContext()) {
      captcha205?.invalidate();
      loginAPIKey.value = "";
      loginCode.value = "";
      busy.value = false;
    }
  }
}
const theme = ref<ConsoleTheme>(storedTheme());
function toggleTheme() {
  theme.value = theme.value === "dark" ? "light" : "dark";
  applyTheme(theme.value);
  saveTheme(theme.value);
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
    kind.value === "certification" ||
    kind.value === "migration" ||
    kind.value === "template" ||
    kind.value === "app-store" ||
    kind.value === "complain" ||
    kind.value === "transfers" ||
    kind.value === "transfer-batches" ||
    kind.value === "redpacks"
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
        const rows =
          kind.value === "orders"
            ? await refundRecoveryRows212(data.items || [])
            : data.items || [];
        if (c.signal.aborted) return;
        items.value = rows;
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
async function refundRecoveryRows212(rows: Record<string, unknown>[]) {
  const clean: Record<string, unknown>[] = rows.map((row) => ({
    ...row,
    local_refund_recovery212: false,
  }));
  const context = moneyPrincipal212.value;
  if (!context) return clean;
  try {
    const principal = await fundsScope212(context);
    if (context !== moneyPrincipal212.value) return clean;
    let issue = "";
    for (const row of clean) {
      if (
        typeof row.trade_no !== "string" ||
        !/^[A-Za-z0-9_-]{1,64}$/.test(row.trade_no)
      )
        continue;
      try {
        row.local_refund_recovery212 = !!new FundsIntentStore212(
          principal,
          "refund",
          row.trade_no,
        ).read();
      } catch (e) {
        issue =
          "原退款恢复资料无法核对，未开放新的恢复或退款请求。" + message(e);
      }
    }
    refundListRecoveryError212.value = issue;
    return clean;
  } catch (e) {
    refundListRecoveryError212.value =
      "无法检查本人原退款请求，请恢复浏览器存储后再核对。" + message(e);
    return clean;
  }
}
async function openRefund(row: Record<string, unknown>, recoveryOnly = false) {
  if (
    !recoveryOnly &&
    row.income_status === "held" &&
    row.can_refund_held_income !== true
  ) {
    orderDetails.value = row;
    notice.value = "请先读取订单详情，核对冻结款的专用退款来源";
    return;
  }
  const remaining104 = orderRefundRemaining104(row);
  if (
    !recoveryOnly &&
    (Object.hasOwn(row, "realmoney") ||
      Object.hasOwn(row, "charge_verified104")) &&
    remaining104 === undefined
  ) {
    error.value = "实际支付金额无法核对，请重新读取原订单";
    return;
  }
  refundRecoveryOnly212.value = recoveryOnly;
  refundRow.value = recoveryOnly ? null : row;
  refundMoney.value = "";
  if (area.value === "admin" && remaining104 !== undefined && remaining104 > 0n)
    refundMoney.value = centsMoney(remaining104.toString());
  refundExternal.value = recoveryOnly ? "" : crypto.randomUUID();
  refundFunding.value = null;
  refundFundingBinding.value = "";
  notice.value = "";
  error.value = "";
  refundIntent212.value = null;
  refundStore212 = null;
  refundStorageError212.value = "";
  const context = moneyPrincipal212.value,
    stamp = ++refundEpoch212;
  refundRestoring212.value = true;
  try {
    if (!context) throw new Error("本人会话尚未核实，不能恢复资金请求");
    const principal = await fundsScope212(context);
    if (stamp !== refundEpoch212 || context !== moneyPrincipal212.value) return;
    const store = new FundsIntentStore212(
      principal,
      "refund",
      String(row.trade_no ?? row.id),
    );
    const intent = store.read();
    if (recoveryOnly && !intent)
      throw new Error("当前主体未找到该订单的原退款请求，未开放新的退款");
    refundStore212 = store;
    refundIntent212.value = intent;
    if (intent) {
      refundRow.value = row;
      refundMoney.value = intent.body.amount;
      refundExternal.value = intent.body.external_no;
    }
  } catch (e) {
    if (stamp === refundEpoch212) {
      refundStorageError212.value = message(e);
      if (recoveryOnly) {
        refundListRecoveryError212.value = message(e);
        items.value = items.value.map((item) =>
          item.trade_no === row.trade_no
            ? { ...item, local_refund_recovery212: false }
            : item,
        );
      }
    }
  } finally {
    if (stamp === refundEpoch212) refundRestoring212.value = false;
  }
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
  if (
    busy.value ||
    refundRestoring212.value ||
    refundRecoveryOnly212.value ||
    refundIntent212.value ||
    refundStorageError212.value
  )
    return;
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
    if (!refundStore212) throw new Error("无法保存原退款请求，尚未发送");
    await sendRefundIntent212({
      target: `/orders/${String(row.trade_no ?? row.id)}/refund`,
      body: { amount: refundMoney.value, external_no: refundExternal.value },
    });
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
async function sendRefundIntent212(
  intent: Pick<FundsIntent212, "target" | "body">,
  retry = false,
) {
  const store = refundStore212,
    stamp = refundEpoch212,
    currentClient = client.value,
    originalOrder = refundRow.value;
  if (!store) throw new Error("原退款请求尚未恢复");
  let dispatched = false;
  notice.value = "";
  try {
    const result = await currentClient.requestResult<Record<string, unknown>>(
      intent.target,
      {
        method: "POST",
        body: intent.body,
        beforeDispatch: () => {
          if (stamp !== refundEpoch212 || currentClient !== client.value)
            throw new Error("本人会话或原操作已变化，尚未发送退款");
          if (!retry)
            refundIntent212.value = store.begin(intent.target, {
              ...intent.body,
            });
          dispatched = true;
        },
      },
    );
    if (stamp !== refundEpoch212 || currentClient !== client.value) return;
    const merchant =
      originalOrder?.merchant_id ??
      (area.value === "merchant" ? session.value?.user.id : undefined);
    const record = verifiedRefund212(
      result.data,
      intent,
      {
        merchant_id: merchant === undefined ? undefined : String(merchant),
        order_id:
          originalOrder?.id === undefined
            ? undefined
            : String(originalOrder.id),
      },
      result.status,
    );
    refundIntent212.value = store.accept(record);
    notice.value = refundNotice(
      { status: record.status, refund_no: intent.body.external_no },
      result.status,
    );
  } catch (e) {
    if (stamp !== refundEpoch212 || currentClient !== client.value) return;
    if (!dispatched) {
      error.value = retry
        ? "本次核对或授权未完成，尚未重发；之前的原退款结果仍待核实，已保留原请求。" +
          message(e)
        : "退款尚未发送，请修正或完成核对后提交。" + message(e);
    } else if (!retry && fundsDefinitiveRefusal212(e)) {
      store.clear();
      refundIntent212.value = null;
      error.value = "本次请求已明确拒绝，未按新请求重新发送。" + message(e);
    } else {
      notice.value = refundNotice(undefined, 202);
      error.value =
        "退款结果尚未核实。已保留原金额及请求号，请查询或重试原请求。" +
        message(e);
    }
  }
}
async function retryRefundIntent212() {
  if (
    busy.value ||
    !refundIntent212.value ||
    refundIntent212.value.phase !== "unknown"
  )
    return;
  busy.value = true;
  error.value = "";
  try {
    await sendRefundIntent212(refundIntent212.value, true);
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
async function queryRefundIntent212() {
  const intent = refundIntent212.value,
    store = refundStore212,
    stamp = refundEpoch212;
  if (busy.value || !intent || !store) return;
  busy.value = true;
  error.value = "";
  try {
    const q = new URLSearchParams({
      search_field: "out_refund_no",
      q: intent.body.external_no,
      limit: "2",
    });
    const merchant = String(
      refundRow.value?.merchant_id ||
        (area.value === "merchant" ? session.value?.user.id : ""),
    );
    if (merchant) q.set("merchant_id", merchant);
    const data = await client.value.request<{
      items: Record<string, unknown>[];
    }>("/refunds?" + q);
    if (stamp !== refundEpoch212) return;
    if (!Array.isArray(data.items) || data.items.length > 1)
      throw new Error("原退款查询结果不唯一，不能解除原请求");
    const row = data.items[0];
    if (!row) {
      error.value =
        "尚未查到原退款，不代表未受理；继续保留原请求号，不能重新生成请求。";
      return;
    }
    if (
      row.trade_no !== intent.object ||
      String(row.out_refund_no ?? row.refund_no) !== intent.body.external_no ||
      row.amount !== fundsAmountCents212(intent.body.amount) ||
      (merchant && row.merchant_id !== merchant) ||
      !/^[1-9]\d{0,18}$/.test(String(row.id)) ||
      ![
        "succeeded",
        "failed",
        "pending",
        "unknown",
        "processing",
        "reserved",
      ].includes(String(row.status))
    )
      throw new Error("原退款金额或订单归属不一致，请保留原请求核对");
    refundIntent212.value = store.accept({
      id: String(row.id),
      status: String(row.status),
    });
  } catch (e) {
    if (stamp === refundEpoch212) error.value = message(e);
  } finally {
    if (stamp === refundEpoch212) busy.value = false;
  }
}
async function finishRefundIntent212() {
  if (
    busy.value ||
    !refundStore212 ||
    !["succeeded", "failed"].includes(
      refundIntent212.value?.record?.status || "",
    )
  )
    return;
  try {
    refundStore212.clear();
    refundIntent212.value = null;
    await closeRefundModal();
    await load();
  } catch (e) {
    error.value = message(e);
  }
}
watch(
  [area, publicTestPayEntry203],
  () => {
    enrollmentPurpose.value = enrollmentFromEntry216(true);
    if (area.value !== "merchant") promotionPrefill.value = "";
    refundSummaryQuery.value = null;
    refundSummaryReturn = undefined;
    orderSummaryQuery.value = null;
    orderSummaryReturn = undefined;
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
  orderSummaryQuery.value = null;
  orderSummaryReturn = undefined;
  if (value) {
    promotionPrefill.value = "";
    enrollmentPurpose.value = null;
  }
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
    orderSummaryQuery.value = null;
    orderSummaryReturn = undefined;
    promotionPrefill.value = "";
    settleFundsAction(new Error("页面已切换，资金授权已清除，请核对原业务"));
    enrollmentPurpose.value = enrollmentFromEntry216();
    pageNumber.value = 1;
    search.value = "";
    listFilters.value = freshListFilters();
    notice.value = "";
    refundRow.value = null;
    orderAction.value = null;
    orderDetails.value = null;
    includeArchivedOrders.value = false;
    blacklistSelection.value = null;
    if (merchantLoginDocument205.value) return;
    applyListRouteQuery();
    void load();
  },
);
let loginDocumentTarget205 = "";
watch(
  [
    area,
    () => route.fullPath,
    initializing,
    session,
    enrollmentPurpose,
    publicTestPayEntry203,
  ],
  () => {
    if (
      area.value !== "merchant" ||
      initializing.value ||
      publicTestPayEntry203.value ||
      enrollmentPurpose.value
    ) {
      loginDocumentTarget205 = "";
      return;
    }
    const target =
      !session.value && !merchantLoginDocument205.value
        ? merchantLoginEntry205(
            route.fullPath,
            notice.value === "密码已修改，请重新登录"
              ? "password"
              : notice.value
                ? "security"
                : "",
          )
        : session.value && merchantLoginDocument205.value
          ? merchantLoginReturn205(route.query.return)
          : "";
    if (!target) {
      loginDocumentTarget205 = "";
      return;
    }
    if (target === loginDocumentTarget205) return;
    loginDocumentTarget205 = target;
    const originPath = route.fullPath,
      originEpoch = epoch;
    void router.replace(target).catch(() => {
      if (
        originEpoch === epoch &&
        area.value === "merchant" &&
        route.fullPath === originPath &&
        loginDocumentTarget205 === target
      )
        error.value = "登录页面切换失败，请重新加载页面";
    });
  },
  { immediate: true },
);
// Admin has no login-document return machinery of its own: a logged-in admin
// landing on /admin/login (fresh login, or a bookmarked login URL) would sit on
// an unresolved route and render 页面不存在. Mirror the merchant flow — send a
// logged-in admin to the requested ?return= path, else /admin/dashboard.
function adminLoginReturn(value: unknown): string {
  const fallback = "/admin/dashboard";
  if (
    typeof value !== "string" ||
    !value.startsWith("/admin/") ||
    /[\u0000-\u0020\u007f-\u009f\\]/.test(value)
  )
    return fallback;
  try {
    const base = "https://goepay-return.invalid",
      target = new URL(value, base);
    if (
      target.origin !== base ||
      !target.pathname.startsWith("/admin/") ||
      /^\/admin\/login\/?$/i.test(target.pathname)
    )
      return fallback;
    const page = target.pathname.slice("/admin".length);
    if (
      !resolvePage("admin", page) &&
      viewKind("admin", page) === "unavailable"
    )
      return fallback;
    return target.pathname + target.search;
  } catch {
    return fallback;
  }
}
let adminLoginRedirectTarget = "";
watch(
  [area, () => route.fullPath, initializing, session],
  () => {
    if (
      area.value !== "admin" ||
      initializing.value ||
      !session.value ||
      !adminLoginDocument.value
    ) {
      adminLoginRedirectTarget = "";
      return;
    }
    const target = adminLoginReturn(route.query.return);
    if (target === adminLoginRedirectTarget) return;
    adminLoginRedirectTarget = target;
    const originPath = route.fullPath,
      originEpoch = epoch;
    void router.replace(target).catch(() => {
      if (
        originEpoch === epoch &&
        area.value === "admin" &&
        route.fullPath === originPath &&
        adminLoginRedirectTarget === target
      )
        error.value = "登录页面切换失败，请重新加载页面";
    });
  },
  { immediate: true },
);
const publicLayout = ref<PublicAnnouncementLayout | null>(null),
  publicLayoutController = new AbortController();
watch(
  [
    publicSite191,
    () => area.value,
    () => page.value?.title,
    () => !!session.value,
    publicTestPayEntry203,
  ],
  () => {
    const site = publicSite191.value;
    if (!site) return;
    const label = publicTestPayEntry203.value
      ? "测试支付"
      : session.value
        ? page.value?.title
        : area.value === "admin"
          ? "管理员登录"
          : "商户登录";
    document.title = label ? `${label} · ${site.title}` : site.title;
    for (const key of ["keywords", "description"] as const) {
      let element = document.querySelector<HTMLMetaElement>(
        `meta[name="${key}"]`,
      );
      if (!element) {
        element = document.createElement("meta");
        element.name = key;
        document.head.append(element);
      }
      element.content = site[key];
    }
  },
  { immediate: true },
);
onMounted(async () => {
  try {
    const r = await fetch("/console/v1/public/layout", {
      credentials: "same-origin",
      headers: { Accept: "application/json" },
      signal: publicLayoutController.signal,
    });
    if (!r.ok) return;
    const p = await r.json();
    if (p.code === 0 && !publicLayoutController.signal.aborted)
      publicLayout.value = publicAnnouncementLayout(p.data);
  } catch {
    /* Keep the existing footer when optional layout cannot be read. */
  }
});
onBeforeUnmount(() => {
  loginPlatformEpoch++;
  loginPlatformController?.abort();
  publicLayoutController.abort();
  loginEpoch++;
  loginController?.abort();
  loginAPIKey.value = "";
  promotionPrefill.value = "";
  controller?.abort();
  settleFundsAction(new Error("页面已关闭，资金授权已清除"));
});
</script>

<template>
  <PublicTestPay203
    v-if="publicTestPayEntry203"
    :fallback-site-name="publicSite191?.sitename"
  />
  <div v-else-if="initializing" class="loading-screen" role="status">
    正在验证会话…
  </div>
  <main v-else-if="!session" class="login-layout" :data-area="area">
    <section class="login-intro">
      <div class="brand">
        <SiteBrandIcon193
          ><span class="brand-mark">{{
            Array.from(publicSite191?.sitename || "GOEPAY")[0]
          }}</span></SiteBrandIcon193
        >
        {{ publicSite191?.sitename || "GOEPAY" }}
      </div>
      <p class="eyebrow">PAYMENT OPERATIONS</p>
      <h1>每一笔交易，<br />清晰可循。</h1>
      <p>交易、退款与资金流转，在同一工作台中有序处理。</p>
      <div class="login-rule">
        <span>交易</span><span>账务</span><span>核对</span>
      </div>
      <SiteContacts191 v-if="publicSite191" :site="publicSite191" />
    </section>
    <section class="login-panel">
      <PublicEnrollment
        v-if="area === 'merchant' && enrollmentPurpose"
        :key="enrollmentPurpose"
        :purpose="enrollmentPurpose"
        :initial-promotion-token="promotionPrefill"
        :registration-announcement="publicLayout?.zhuce ?? ''"
        @prefill-consumed="promotionPrefill = ''"
        @close="closeEnrollment216"
      />
      <template v-else>
        <h2>{{ area === "admin" ? "管理员登录" : "商户登录" }}</h2>
        <p class="muted">
          {{ publicSite191?.login_subtitle || "使用您的账户继续" }}
        </p>
        <div
          v-if="area === 'merchant'"
          class="toolbar-actions"
          aria-label="商户登录方式"
        >
          <button
            type="button"
            data-login-mode="password"
            :aria-pressed="loginMode === 'password'"
            :disabled="busy || loginUncertain"
            @click="loginMode = 'password'"
          >
            密码登录
          </button>
          <button
            v-if="loginPlatformAccess?.close_keylogin === 0"
            type="button"
            data-login-mode="key"
            :aria-pressed="loginMode === 'key'"
            :disabled="busy || loginUncertain"
            @click="loginMode = 'key'"
          >
            密钥登录
          </button>
        </div>
        <template v-if="area === 'merchant'">
          <p v-if="loginPlatformLoading" role="status">正在读取平台登录功能…</p>
          <p
            v-else-if="loginPlatformAccess?.close_keylogin === 1"
            class="muted"
          >
            平台已关闭密钥登录，请使用密码登录。
          </p>
          <p v-if="loginPlatformError" role="alert">{{ loginPlatformError }}</p>
          <button
            v-if="!loginPlatformLoading && !loginPlatformAccess"
            type="button"
            :disabled="busy || loginUncertain"
            @click="loadLoginPlatformAccess"
          >
            重新读取平台登录功能
          </button>
        </template>
        <div v-if="error" class="alert error" role="alert">{{ error }}</div>
        <div v-if="notice" class="alert success" role="status">
          {{ notice }}
        </div>
        <form @submit.prevent="login">
          <template v-if="loginMode === 'key' && area === 'merchant'">
            <label
              >商户ID<input
                v-model="loginMerchantID"
                name="merchant_id"
                inputmode="numeric"
                autocomplete="off"
                maxlength="19"
                required
                :disabled="busy || loginUncertain"
            /></label>
            <label
              >商户密钥<input
                v-model="loginAPIKey"
                name="api_key"
                type="password"
                autocomplete="off"
                maxlength="512"
                required
                :disabled="busy || loginUncertain"
            /></label>
            <p class="muted">
              使用当前商户密钥；需本人已开启密钥登录。已绑定验证器时仍需动态验证码。
            </p>
          </template>
          <template v-else
            ><label
              >用户名<input
                v-model="username"
                name="username"
                autocomplete="username"
                :disabled="busy"
                required
                maxlength="128" /></label
            ><label
              >密码<input
                v-model="password"
                name="password"
                type="password"
                autocomplete="current-password"
                :disabled="busy"
                required
                maxlength="1024" /></label></template
          ><label
            >动态验证码（已绑定验证器时填写）<input
              v-model="loginCode"
              inputmode="numeric"
              pattern="[0-9]{6}"
              maxlength="6"
              autocomplete="one-time-code"
              :disabled="busy || loginUncertain" /></label
          ><LoginCaptcha205
            v-if="
              area === 'merchant' &&
              merchantLoginDocument205 &&
              loginMode === 'password'
            "
            ref="loginCaptchaRef205"
            active
            :username="username"
            :context-key="client"
            :disabled="busy || loginUncertain"
          /><button
            type="submit"
            class="primary full"
            :disabled="busy || loginUncertain"
          >
            {{ busy ? "正在登录…" : "登录工作台" }}
          </button>
        </form>
        <div
          v-if="area === 'merchant' && connectPending"
          class="connect-complete"
        >
          <p class="muted">请输入商户ID和密钥完成绑定和登录</p>
          <p v-if="connectError" class="alert error" role="alert">
            {{ connectError }}
          </p>
          <form @submit.prevent="submitConnectComplete">
            <label
              >商户ID<input
                v-model="connectForm.merchant_id"
                name="connect_merchant_id"
                inputmode="numeric"
                autocomplete="off"
                maxlength="19"
                required
                :disabled="connectBusy"
            /></label>
            <label
              >商户密钥<input
                v-model="connectForm.api_key"
                name="connect_api_key"
                type="password"
                autocomplete="off"
                maxlength="512"
                required
                :disabled="connectBusy"
            /></label>
            <label
              >动态验证码（已绑定验证器时填写）<input
                v-model="connectForm.code"
                inputmode="numeric"
                pattern="[0-9]{6}"
                maxlength="6"
                autocomplete="one-time-code"
                :disabled="connectBusy"
            /></label>
            <button type="submit" class="primary full" :disabled="connectBusy">
              {{ connectBusy ? "正在绑定登录…" : "绑定并登录" }}
            </button>
          </form>
        </div>
        <div
          v-if="area === 'merchant' && connectProviderList.length"
          class="quick-login"
          aria-label="快捷登录"
        >
          <p class="muted">快捷登录</p>
          <div class="toolbar-actions">
            <button
              v-for="p in connectProviderList"
              :key="p"
              type="button"
              :data-quick-login="p"
              :disabled="busy || loginUncertain"
              @click="startQuickLogin(p)"
            >
              {{ connectTitles[p] }}
            </button>
          </div>
        </div>
        <button
          v-if="loginUncertain"
          type="button"
          data-action="check-login"
          class="full"
          @click="initialize"
        >
          重新检查登录状态
        </button>
        <div v-if="area === 'merchant'" class="toolbar-actions">
          <button
            :disabled="busy || loginUncertain"
            @click="openEnrollment('register')"
          >
            注册商户
          </button>
          <button
            :disabled="busy || loginUncertain"
            @click="openEnrollment('recover')"
          >
            找回密码
          </button>
        </div>
      </template>
    </section>
    <footer v-if="publicLayout?.footer">
      <RichContent :content="publicLayout.footer" format="html" />
    </footer>
  </main>
  <MerchantScanPage192
    v-else-if="area === 'merchant' && path === '/scanpay'"
    :client="client"
    :own-session="!session.delegated_by"
    :merchant-i-d="String(session.user.id)"
  />
  <ConsoleShell
    v-else
    :area="area"
    :navigation="navigation"
    :active="path"
    :title="page?.title || '页面不存在'"
    :owner="session"
    :brand-name="publicSite191?.sitename"
    :inert="!!fundsAction"
  >
    <template #account>
      <ConsoleTopBarTools
        :area="area"
        :navigation="navigation"
        :display-name="session.user.name"
        :theme="theme"
        :logout-busy="busy"
        @navigate="(path) => $router.push('/' + area + path)"
        @toggle-theme="toggleTheme"
        @account="
          $router.push(
            '/' +
              area +
              (area === 'admin'
                ? '/system/change-password'
                : '/account/settings'),
          )
        "
        @logout="logout"
      />
    </template>
    <main class="content">
      <p
        v-if="kind === 'orders' && refundListRecoveryError212"
        role="alert"
        class="alert error"
      >
        {{ refundListRecoveryError212 }}
      </p>
      <p
        v-if="area === 'merchant' && session.delegated_by"
        role="status"
        class="alert"
      >
        管理员 {{ session.delegated_by.name }} 正在代登录本商户。此会话最长 15
        分钟，管理员退出或权限撤销后失效。
      </p>
      <div class="page-heading-spacer"></div>
      <div v-if="error && !refundRow" class="alert error" role="alert">
        {{ error }}
      </div>
      <div v-if="notice && !refundRow" class="alert success" role="status">
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
      <AppStorePage v-else-if="kind === 'app-store'" />
      <ComplaintsPage
        v-else-if="kind === 'complain'"
        :key="area + path"
        :client="client"
        :area="area"
        :path="path"
        :can-configure="
          area === 'admin' &&
          session?.user.admin_class === 'super' &&
          !session?.delegated_by
        "
      />
      <SettlementSchedulePage
        v-else-if="kind === 'settlement-schedule'"
        :client="client"
        :own-session="!!session && !session.delegated_by"
      />
      <AdminChannelSharingRules
        v-else-if="
          kind === 'channel-sharing' &&
          path === '/profitsharing/receiver' &&
          area === 'admin'
        "
        :key="area + path"
        :client="client"
      />
      <ChannelSharingRuleList
        v-else-if="
          kind === 'channel-sharing' && path === '/profitsharing/receiver'
        "
        :key="area + path"
        :client="client"
        :area="area"
      />
      <template v-else-if="kind === 'channel-sharing'">
        <ChannelSharingOrderList
          :key="area + path"
          :client="client"
          :area="area"
          @open-flow="openSharingFlow"
        />
        <div v-if="sharingFlowOpen" class="panel table-tools">
          <span>分账操作台</span>
          <button
            type="button"
            class="pa-btn"
            :disabled="sharingFlowBusy"
            @click="closeSharingFlow"
          >
            关闭操作台
          </button>
        </div>
        <ChannelSharingPage
          v-if="sharingFlowOpen"
          :key="
            area +
            path +
            '-flow-' +
            sharingFlowContext.merchant_id +
            '-' +
            sharingFlowContext.sharing_external_no
          "
          :client="client"
          :area="area"
          :page="'order'"
          :initial-merchant-id="
            area === 'admin' ? sharingFlowContext.merchant_id : undefined
          "
          :initial-external-no="sharingFlowContext.sharing_external_no"
          @busy="sharingFlowBusy = $event"
        />
      </template>
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
        :merchant-id="String(session.user.id)"
        :own-session="!session.delegated_by"
      />
      <SettingsPage
        v-else-if="kind === 'settings'"
        :key="area + path"
        :client="client"
        :admin-class="session.user.admin_class"
        :own-session="!session.delegated_by"
        @site-changed="reloadPublicSite191"
      />
      <MerchantCertification233
        v-else-if="kind === 'certification'"
        :key="area + path"
        :client="client"
        :merchant-i-d="String(session.user.id)"
        :own-session="!session.delegated_by"
      />
      <MigrationWizard234
        v-else-if="kind === 'migration'"
        :key="area + path"
        :client="client"
        :own-session="!session.delegated_by"
        :admin-class="session.user.admin_class"
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
        @created="
          (id) =>
            router.replace({ path: route.path, query: { ...route.query, id } })
        "
      />
      <CollectionGeneratorPage
        v-else-if="kind === 'collection-generator'"
        :key="area + path"
        :client="client"
        :own-session="!session.delegated_by"
      />
      <AnnouncementsPage
        v-else-if="
          kind === 'resource' && area === 'admin' && path === '/system/anounce'
        "
        :key="area + path"
        :client="client"
        :own-session="!session.delegated_by"
      />
      <SubchannelPage
        v-else-if="
          kind === 'resource' && resourceFor(area, path) === 'subchannels'
        "
        :key="area + path"
        :client="client"
        :merchant-id="session.user.id"
        :own-session="!session.delegated_by"
        :admin-class="session.user.admin_class"
        :initial-query="route.query"
        @clear-route="$router.replace({ query: {} })"
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
        :own-session="!session.delegated_by"
        :admin-class="session.user.admin_class"
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
        :key="area + path + JSON.stringify(route.query)"
        :client="client"
        :area="area"
        :workflow="workflowFor(area, path)"
        :application="area === 'merchant' && path === '/settle/apply'"
        :initial-query="
          workflowFor(area, path) === 'settlements' ? route.query : undefined
        "
        :self-i-d="String(session.user.id)"
      />
      <MerchantAccountSettings
        v-else-if="
          kind === 'account' &&
          area === 'merchant' &&
          path === '/account/settings'
        "
        :client="client"
        :merchant-id="String(session.user.id)"
        :own-session="!!session && !session.delegated_by"
        @reauthenticate="
          (message) => {
            session = null;
            notice = message;
            error = '';
          }
        "
      />
      <div
        v-else-if="kind === 'account'"
        :class="{ 'account-security-grid': area === 'admin' }"
      >
        <AccountPage
          :key="path"
          :client="client"
          :own-session="
            area === 'merchant' &&
            !!session &&
            session.user.role === 'merchant' &&
            !session.delegated_by
          "
          :merchant-id="
            session?.user.role === 'merchant' ? String(session.user.id) : ''
          "
          :section="
            area === 'admin'
              ? 'security'
              : path === '/account/api-info'
                ? 'api-info'
                : 'profile'
          "
          @reauthenticate="
            (message) => {
              session = null;
              notice = message || '密码已修改，请重新登录';
              error = '';
            }
          "
        />
        <PaymentPasswordSettings
          v-if="area === 'admin'"
          :key="area + 'funds-security'"
          :client="client"
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
      </div>
      <div v-else-if="kind === 'operation'">
        <TaskSchedulerPage
          v-if="path === '/system/cron'"
          :client="client"
          :allowed="
            area === 'admin' &&
            session.user.admin_class === 'super' &&
            !session.delegated_by
          "
        />
        <template v-else>
          <SystemUpdatePage
            v-if="path === '/system/update'"
            :client="client"
            :own-session="!session.delegated_by"
            :admin-class="session.user.admin_class"
          />
          <LocalPluginManagement
            v-if="path === '/app/plugin-list'"
            :client="client"
          />
          <OperationsPage
            v-if="path !== '/system/update' && path !== '/app/plugin-list'"
            :key="path"
            :client="client"
            :operation="operationPages[path]"
            :own-session="!session.delegated_by"
            :admin-class="session.user.admin_class"
          />
          <details v-if="path === '/system/update'" class="panel">
            <summary>运行信息</summary>
            <OperationsPage
              :client="client"
              operation="system-info"
              :own-session="!session.delegated_by"
              :admin-class="session.user.admin_class"
            />
          </details>
        </template>
      </div>
      <CommercePage
        v-else-if="kind === 'commerce'"
        :key="area + path"
        :client="client"
        :area="area"
        :kind="commerceFor(area, path)"
        :new-use-allowed="
          area !== 'merchant' ||
          (commerceFor(area, path) === 'deposits'
            ? session.merchant_features?.user_deposit !== false
            : commerceFor(area, path) === 'recharges'
              ? session.merchant_features?.recharge !== false
              : true)
        "
      />
      <TransferBatchPage
        v-else-if="kind === 'transfer-batches'"
        :key="area + path"
        :client="client"
        :new-use-allowed="
          area !== 'merchant' ||
          session.merchant_features?.user_transfer_batch !== false
        "
      />
      <RedpackPage
        v-else-if="kind === 'redpacks'"
        :key="area + path"
        :client="client"
      />
      <TransferPage
        v-else-if="kind === 'transfers'"
        :key="area + path + JSON.stringify(route.query)"
        :client="client"
        :area="area"
        :initial-query="route.query"
        :self-i-d="String(session.user.id)"
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
          :merchant-id="String(session?.user.id ?? '')"
          :own-session="!!session && !session.delegated_by"
          :client="client"
      /></template>
      <template v-else>
        <form
          class="toolbar list-filter-card panel"
          @submit.prevent="
            pageNumber = 1;
            load();
          "
        >
          <div
            class="search-group list-filters-grid"
            :class="{
              'order-search-group': ['orders', 'refunds'].includes(kind),
            }"
          >
            <KitField
              v-if="['orders', 'refunds'].includes(kind)"
              label="搜索字段"
            >
              <KitSelect
                v-model="listFilters.search_field"
                :options="searchFieldOptions"
                placeholder="综合搜索"
                :aria-label="
                  kind === 'orders' ? '订单搜索字段' : '退款搜索字段'
                "
              />
            </KitField>
            <KitField
              :label="
                ['orders', 'refunds', 'ledger'].includes(kind)
                  ? '搜索内容'
                  : '搜索记录'
              "
            >
              <KitInput
                v-model="search"
                :placeholder="searchFieldPlaceholder"
                :inputmode="searchFieldInputmode"
                aria-label="搜索内容"
              />
            </KitField>
            <KitField
              v-if="
                area === 'admin' &&
                ['orders', 'refunds', 'ledger'].includes(kind)
              "
              label="商户号"
            >
              <KitInput
                v-model="listFilters.merchant_id"
                inputmode="numeric"
                placeholder="商户号"
                aria-label="商户号筛选"
              />
            </KitField>
          </div>
          <ListFilters
            v-model="listFilters"
            :kind="kind"
            :area="area"
            :loading="loading"
            :pay-type-options="payTypeFilterOptions"
            @reset="resetListFilters"
          />
        </form>
        <section class="panel records">
          <div
            v-if="['orders', 'refunds', 'ledger'].includes(kind)"
            class="table-tools"
          >
            <button
              v-if="kind === 'orders'"
              type="button"
              class="pa-btn pa-btn--primary"
              :disabled="loading || busy || !!listRouteError"
              @click="openOrderSummary"
            >
              统计
            </button>
            <button
              v-if="kind === 'refunds'"
              type="button"
              class="pa-btn pa-btn--primary"
              :disabled="loading || busy || !!listRouteError"
              @click="openRefundSummary"
            >
              退款统计
            </button>
            <button
              v-if="kind === 'ledger'"
              type="button"
              class="pa-btn pa-btn--primary"
              :disabled="loading || busy || !!listRouteError"
              @click="openLedgerSummary"
            >
              统计
            </button>
            <CsvExportButton
              v-if="['orders', 'refunds', 'ledger'].includes(kind)"
              :client="client"
              :path="listCSVPath"
              :filename="`${kind}-filtered.csv`"
              :disabled="loading || !!listRouteError"
            />
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
            <MerchantOrderBatchControls
              v-if="kind === 'orders' && area === 'merchant' && !error"
              :key="area + path"
              :client="client"
              :items="items"
              :selected="selectedOrderTrades"
              :own-session="!!session && !session.delegated_by"
              @busy="orderBatchBusy = $event"
              @active="orderBatchOpen = $event"
              @updated="load"
              @refund-step="startMerchantRefundStep"
            />
            <div class="table-tools-end">
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
              <AutoRefreshControl
                v-if="kind === 'orders'"
                :key="area + path"
                :refresh="load"
                :blocked="
                  loading ||
                  busy ||
                  !!orderAction ||
                  !!merchantOrderAction ||
                  !!orderDetails ||
                  !!refundRow ||
                  orderBatchOpen ||
                  !!blacklistSelection ||
                  !!listRouteError
                "
              />
              <button
                type="button"
                class="icon-button table-refresh"
                :aria-label="loading ? '正在更新…' : '刷新数据'"
                :title="loading ? '正在更新…' : '刷新数据'"
                :disabled="loading"
                @click="load"
              >
                <ShellIcon name="refresh" />
              </button>
            </div>
          </div>
          <div v-if="loading" class="loading-row" role="status">
            正在加载记录…
          </div>
          <LedgerRecordsTable
            v-if="!loading && !error && kind === 'ledger'"
            :key="area + path"
            :area="area"
            :items="items"
          />
          <DataTable
            v-else-if="!loading && !error"
            :items="items"
            :columns="columns"
            :orders="kind === 'orders'"
            :refunds="kind === 'refunds'"
            :admin-orders="kind === 'orders' && area === 'admin'"
            :merchant-orders="kind === 'orders' && area === 'merchant'"
            :selectable="kind === 'orders'"
            :selected-trades="selectedOrderTrades"
            @selection-change="selectedOrderTrades = $event"
            :order-actions-disabled="
              orderBatchBusy ||
              merchantOrderBusy ||
              !!merchantOrderAction ||
              (area === 'admin' &&
                (!!session?.delegated_by ||
                  !['super', 'operator'].includes(
                    session?.user.admin_class || '',
                  )))
            "
            @refund="openRefund"
            @detail="openOrderDetails"
            @order-action="openAdminOrderAction"
            @merchant-order-action="openMerchantOrderAction"
            @blacklist="openBlacklist"
          />
          <KitPagination
            v-model:page="pageNumber"
            v-model:page-size="listLimit"
            :total="total"
            @change="load"
          />
        </section>
      </template>
      <LegacyRefundHistoryPanel
        v-if="authorized && kind === 'refunds'"
        :key="area + path + 'legacy-history'"
        :client="client"
        :own-session="!!session && !session.delegated_by"
      />
    </main>
    <footer v-if="publicLayout === null || publicLayout.footer">
      <RichContent
        v-if="publicLayout?.footer"
        :content="publicLayout.footer"
        format="html"
      /><template v-else>GOEPAY · 支付运营工作台</template>
    </footer>
    <template #overlays>
      <RefundSummaryDialog
        v-if="refundSummaryQuery !== null && authorized && kind === 'refunds'"
        :client="client"
        :query="refundSummaryQuery"
        @close="closeRefundSummary"
      />
      <OrderSummaryDialog
        v-if="orderSummaryQuery !== null && authorized && kind === 'orders'"
        :client="client"
        :query="orderSummaryQuery"
        @close="closeOrderSummary"
      />
      <LedgerSummaryDialog
        v-if="ledgerSummaryQuery !== null && authorized && kind === 'ledger'"
        :client="client"
        :query="ledgerSummaryQuery"
        @close="closeLedgerSummary"
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
      <MerchantOrderActionDialog
        v-if="merchantOrderAction && session && area === 'merchant'"
        :key="merchantOrderAction.trade + '/' + merchantOrderAction.action"
        :client="client"
        :trade-no="merchantOrderAction.trade"
        :action="merchantOrderAction.action"
        :own-session="!session.delegated_by"
        @close="closeMerchantOrderAction"
        @updated="load"
        @refund="merchantRefund"
        @busy="merchantOrderBusy = $event"
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
        @close="closeOrderDetails"
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
        @keydown.esc="closeRefundModal"
      >
        <section
          ref="refundDialog"
          class="modal panel"
          role="dialog"
          aria-modal="true"
          :aria-label="
            refundRecoveryOnly212
              ? '核对原退款'
              : area === 'admin'
                ? 'API退款确认'
                : '申请退款'
          "
          @keydown="trapRefundFocus"
        >
          <div class="toolbar">
            <h2>
              {{
                refundRecoveryOnly212
                  ? "核对原退款"
                  : area === "admin"
                    ? "API退款确认"
                    : "申请退款"
              }}
            </h2>
            <button
              type="button"
              class="pa-icon-btn pa-icon-btn--ghost pa-modal-x"
              aria-label="关闭窗口"
              :disabled="busy"
              @click="closeRefundModal"
            >
              ×
            </button>
          </div>
          <div v-if="error" class="alert error" role="alert">{{ error }}</div>
          <form v-if="refundRow" @submit.prevent="submitRefund">
            <p v-if="refundRestoring212" role="status">
              正在恢复本人原退款请求…
            </p>
            <p v-if="refundStorageError212" role="alert" class="alert error">
              {{ refundStorageError212 }}
            </p>
            <div v-if="refundIntent212" role="status" class="alert">
              {{ refundRecoveryNotice212 }}
              <p v-if="refundIntent212.record">
                原退款记录：{{ refundIntent212.record.id }}
              </p>
              <div class="toolbar-actions">
                <button
                  type="button"
                  :disabled="busy"
                  @click="queryRefundIntent212"
                >
                  查询原退款
                </button>
                <button
                  v-if="refundIntent212.phase === 'unknown'"
                  type="button"
                  :disabled="busy"
                  @click="retryRefundIntent212"
                >
                  重试原退款请求
                </button>
                <button
                  v-if="
                    ['succeeded', 'failed'].includes(
                      refundIntent212.record?.status || '',
                    )
                  "
                  type="button"
                  :disabled="busy"
                  @click="finishRefundIntent212"
                >
                  已核对，结束本次退款
                </button>
              </div>
            </div>
            <p class="muted">
              订单
              <span class="pa-order-no">{{
                refundRow.trade_no ?? refundRow.id
              }}</span>
            </p>
            <div class="form-rows">
              <label
                ><span class="field-label field-label--req">退款金额（元）</span
                ><input
                  v-model="refundMoney"
                  :readonly="
                    !!refundIntent212 ||
                    busy ||
                    refundRestoring212 ||
                    refundRecoveryOnly212
                  "
                  inputmode="decimal"
                  required
                  autofocus /></label
              ><label
                ><span class="field-label field-label--req">退款请求号</span
                ><input
                  v-model="refundExternal"
                  :readonly="
                    !!refundIntent212 ||
                    busy ||
                    refundRestoring212 ||
                    refundRecoveryOnly212
                  "
                  required
                  maxlength="128"
              /></label>
            </div>
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
              <button type="button" :disabled="busy" @click="closeRefundModal">
                取消</button
              ><button
                v-if="!refundIntent212 && !refundRecoveryOnly212"
                class="primary"
                :disabled="
                  busy || refundRestoring212 || !!refundStorageError212
                "
              >
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
    :before-dispatch="fundsAction.beforeDispatch"
    @close="settleFundsAction(new Error('资金授权已取消，尚未自动提交原业务'))"
    @completed="(result) => settleFundsAction(undefined, result)"
    @failed="(error) => settleFundsAction(error)"
  />
  <ToastStack />
</template>
