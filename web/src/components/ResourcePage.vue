<script setup lang="ts">
import { listPageWithinTotal } from "../list-page";
import { uncertainCreationResult } from "../mutation-result";
import {
  ref,
  computed,
  watch,
  onMounted,
  onBeforeUnmount,
  nextTick,
} from "vue";
import { ApiClient, ApiError, displayValue } from "../api";
import { isOrderNumberKey } from "../order-number";
import { beijingTimestamp } from "../console-datetime";
import { effectiveRiskDescription } from "../core-risk-status";
import { RouterLink } from "vue-router";
import { groupDirectoryRates, groupUsersHref } from "../group-directory";
import {
  groupPaymentRatesError,
  readGroupPaymentRates,
} from "../group-payment-rates";
import {
  resourceListFields,
  hasOriginalMerchantListFields,
  isCredentialField,
  consoleValue,
  consoleColumnLabel,
  merchantGroupLabel,
  merchantFieldValue,
  approvedMerchantDomains,
} from "../pro-console-fields";
import {
  validateFields,
  buildPayload,
  formValues,
  type ResourceSchema,
  type ResourceEntry,
  type ResourceField,
} from "../resources";
import {
  createRecipientErrors,
  omitUnusedCreateRecipient,
} from "../settlement-recipient";
import ResourceFields from "./ResourceFields.vue";
import KitPagination from "./KitPagination.vue";
import KitEmpty from "./KitEmpty.vue";
import KitSelect from "./KitSelect.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitHelpPopover from "./KitHelpPopover.vue";
import KitModal from "./KitModal.vue";
import MerchantEditorFields from "./MerchantEditorFields.vue";
import MerchantCreateFields from "./MerchantCreateFields.vue";
import GroupEditorFields from "./GroupEditorFields.vue";
import InternalSharingRuleFields from "./InternalSharingRuleFields.vue";
import CoreRiskGroupPanel from "./CoreRiskGroupPanel.vue";
import PaymentDefend206GroupPanel from "./PaymentDefend206GroupPanel.vue";
import GroupStatisticsDialog from "./GroupStatisticsDialog.vue";
import PaymentStatisticsDialog from "./PaymentStatisticsDialog.vue";
import AccountLoginLogDialog from "./AccountLoginLogDialog.vue";
import MerchantManualWithdrawal from "./MerchantManualWithdrawal.vue";
import MerchantBalanceAdjustDialog from "./MerchantBalanceAdjustDialog.vue";
import SettlementRecipientPanel from "./SettlementRecipientPanel.vue";
import CasePanel from "./CasePanel.vue";
import RuleExecutionPanel from "./RuleExecutionPanel.vue";
import CollectionCodePanel from "./CollectionCodePanel.vue";
import MerchantDirectoryFilters from "./MerchantDirectoryFilters.vue";
import ActionDropdown from "./ActionDropdown.vue";
import CsvExportButton from "./CsvExportButton.vue";
import ShellIcon from "./ShellIcon.vue";
import AddonConfigGroups from "./AddonConfigGroups.vue";
import {
  newMerchantDirectory,
  merchantDirectoryColumns,
  merchantDirectoryError,
  readMerchantDirectoryQuery,
  appendMerchantDirectory,
} from "../merchant-directory";
import ChannelStopPanel from "./ChannelStopPanel.vue";
import RuntimeChannelPolicyPanel from "./RuntimeChannelPolicyPanel.vue";
import ParentChannelPage from "./ParentChannelPage.vue";
import PayTypePage from "./PayTypePage.vue";
import PayRollCreateForm from "./PayRollCreateForm.vue";
import PayRollChannelEditor from "./PayRollChannelEditor.vue";
import { readPayRollRecord } from "../pay-roll";
import WeixinAppsPage from "./WeixinAppsPage.vue";
import MerchantDelegationPanel from "./MerchantDelegationPanel.vue";
import BusinessNoticeEvents from "./BusinessNoticeEvents.vue";
import BlacklistAddDialog from "./BlacklistAddDialog.vue";
import { businessNoticeSettingsFields } from "../business-notice";
const props = defineProps<{
  client: ApiClient;
  resource: string;
  adminClass?: string;
  ownSession?: boolean;
  entry?: ResourceEntry;
  merchantQuery?: Record<string, unknown>;
}>();
const delegation = ref<Record<string, unknown> | null>(null);
function configureDelegatedMerchantLogin() {
  const row = delegation.value;
  if (
    !row ||
    props.resource !== "merchants" ||
    !schema.value?.permissions.update
  )
    return;
  delegation.value = null;
  void open("edit", row);
}
const editMerchantRecipient = ref(false);
const merchantLogID = ref<string>();
const merchantStatisticsID = ref<string>();
const manualWithdrawalID = ref<string>();
const balanceAdjustID = ref<string>();
let balanceAdjustReturn: HTMLElement | undefined,
  balanceAdjustChanged = false;
// 后台加款/扣款 is an admin money action: same super/operator gate as 手动提现,
// and requires schema update permission.
const canAdjustBalance = computed(
  () =>
    canReadMerchantStatistics.value &&
    schema.value?.permissions.update === true,
);
async function closeBalanceAdjust() {
  const target = balanceAdjustReturn,
    id = balanceAdjustID.value,
    epoch = contextEpoch,
    refresh = balanceAdjustChanged;
  balanceAdjustID.value = undefined;
  balanceAdjustReturn = undefined;
  balanceAdjustChanged = false;
  if (refresh) await load();
  await nextTick();
  if (epoch !== contextEpoch) return;
  const current = target?.isConnected
    ? target
    : id
      ? document.querySelector<HTMLButtonElement>(
          'button[aria-label="商户关联记录 ' + id + '"]',
        )
      : undefined;
  current?.focus();
}
let manualWithdrawalReturn: HTMLElement | undefined,
  manualWithdrawalChanged = false;
const canManualWithdraw = computed(
  () =>
    canReadMerchantStatistics.value &&
    schema.value?.permissions.update === true,
);
async function closeManualWithdrawal() {
  const target = manualWithdrawalReturn,
    id = manualWithdrawalID.value,
    epoch = contextEpoch,
    refresh = manualWithdrawalChanged;
  manualWithdrawalID.value = undefined;
  manualWithdrawalReturn = undefined;
  manualWithdrawalChanged = false;
  if (refresh) await load();
  await nextTick();
  if (epoch !== contextEpoch) return;
  const current = target?.isConnected
    ? target
    : id
      ? document.querySelector<HTMLButtonElement>(
          'button[aria-label="商户关联记录 ' + id + '"]',
        )
      : undefined;
  current?.focus();
}
let merchantStatisticsReturn: HTMLElement | undefined;
const canReadMerchantStatistics = computed(
  () =>
    props.resource === "merchants" &&
    props.client.area === "admin" &&
    props.ownSession === true &&
    ["super", "operator"].includes(props.adminClass || ""),
);
function closeMerchantStatistics() {
  const target = merchantStatisticsReturn,
    epoch = contextEpoch;
  merchantStatisticsID.value = undefined;
  merchantStatisticsReturn = undefined;
  void nextTick(() => {
    if (epoch === contextEpoch && target?.isConnected) target.focus();
  });
}
const groupStatisticsID = ref<string>();
let groupStatisticsReturn: HTMLElement | undefined;
const canReadGroupStatistics = computed(
  () =>
    props.resource === "groups" &&
    props.client.area === "admin" &&
    props.ownSession === true &&
    ["super", "operator"].includes(props.adminClass || ""),
);
function openGroupStatistics(id: unknown) {
  if (
    !canReadGroupStatistics.value ||
    busy.value ||
    loading.value ||
    mode.value !== null ||
    !groupUsersHref(id)
  )
    return;
  groupStatisticsReturn =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : undefined;
  groupStatisticsID.value = String(id);
}
function closeGroupStatistics() {
  const target = groupStatisticsReturn,
    epoch = contextEpoch;
  groupStatisticsID.value = undefined;
  groupStatisticsReturn = undefined;
  void nextTick(() => {
    if (epoch === contextEpoch && target?.isConnected) target.focus();
  });
}
let merchantLogReturn: HTMLElement | undefined;
const canReadMerchantLogs = computed(
  () =>
    props.resource === "merchants" &&
    props.client.area === "admin" &&
    props.ownSession === true &&
    ["super", "operator"].includes(props.adminClass || ""),
);
const canDelegate = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.resource === "merchants" &&
    schema.value?.actions?.some(
      (a) => (typeof a === "string" ? a : a.name) === "sso",
    ),
);
// The original console heads these short configuration lists with
// "系统共有 N 个…".
const summaryNoun = computed(
  () =>
    (
      ({
        groups: "用户组",
        "pay-rolls": "轮询组",
        "pay-types": "支付方式",
      }) as Record<string, string>
    )[props.resource] ?? "",
);
const showBusinessNotices = computed(
  () =>
    props.resource === "settings" &&
    props.client.area === "admin" &&
    props.ownSession === true &&
    ["super", "operator"].includes(props.adminClass ?? ""),
);
// Pro omits the filter card on list pages that have nothing to filter
// (e.g. 支付通道轮询). A resource opts out here; one with declared schema.filters
// always keeps the card.
const noListFilterResources = new Set(["pay-rolls"]);
const showListFilter = computed(
  () =>
    !!schema.value?.filters?.length ||
    !noListFilterResources.has(props.resource),
);
// Poll-group channel configuration is separate from its advanced editor.
const rowPrimaryActions: Record<string, string> = {
  "pay-rolls": "配置通道",
};
const rowPrimaryLabel = computed(() => rowPrimaryActions[props.resource] ?? "");
// Resources whose editor already shows everything the generic 详情 would — hide
// the 详情 button for them to match Pro's row.
const hideDetailResources = new Set(["pay-rolls"]);
// Tag colour for an on/off style value (正常 green, 已禁用 red, …).
function stateTag(key: string, value: unknown): string {
  if (key === "enabled")
    return value === true ? "on" : value === false ? "bad" : "none";
  if (key === "pay")
    return (
      ({ "0": "bad", "1": "on", "2": "warn" } as Record<string, string>)[
        String(value)
      ] ?? "none"
    );
  if (key === "settle")
    return (
      ({ "0": "off", "1": "on" } as Record<string, string>)[String(value)] ??
      "none"
    );
  return "none";
}
function stateTone(
  key: string,
  value: unknown,
): "success" | "warning" | "danger" | "info" | "neutral" {
  const s = stateTag(key, value);
  return s === "on"
    ? "success"
    : s === "bad"
      ? "danger"
      : s === "warn"
        ? "warning"
        : "neutral";
}
// Pro's 状态 chips are compact 2-4 char words (not sentences). pay: 支付/停付/待审;
// settle: 结算/停结. The full risk explanation moves to the cell's tooltip.
function merchantStateChip(key: "pay" | "settle", value: unknown): string {
  const v = String(value);
  if (key === "pay") return v === "1" ? "支付" : v === "2" ? "待审" : "停付";
  return v === "1" ? "结算" : "停结";
}
const entityLabel = computed(
  () =>
    (
      ({
        merchants: "商户",
        "order-subject-policies": "订单名称限制策略",
        "admin-users": "管理员",
        groups: "用户组",
        domains: "授权域名",
        "invitation-codes": "邀请码",
        announcements: "公告",
        settings: "系统设置项",
        channels: "支付通道",
        blacklist: "黑名单项",
        "collection-styles": "收款码样式",
        "pay-types": "支付方式",
        "pay-rolls": "轮询配置",
        "weixin-apps": "公众号小程序",
        "onboarding-channels": "进件渠道",
        "onboarding-applications": "本地进件资料",
        subchannels: "子通道资料",
        complaints: "本地投诉工单",
        "complaint-tasks": "同步任务配置",
        "cron-tasks": "计划任务",
        "settlement-settings": "结算收款资料",
        "collection-codes": "收款码资料",
        "plugin-configs": "本地扩展资料",
        "template-configs": "模板资料",
        "addon-configs": "扩展参数",
        "profitsharing-rules": "站内分账规则",
      }) as Record<string, string>
    )[props.resource] ||
    schema.value?.title ||
    "记录",
);
const schema = ref<ResourceSchema | null>(null),
  items = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  page = ref(1),
  limit = ref(20),
  query = ref(""),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
const merchantFilters = ref(newMerchantDirectory()),
  merchantRouteError = ref("");
// Generic per-field toolbar filters (schema.filters). Keyed by filter param.
const fieldFilters = ref<Record<string, string>>({});
// Row selection for 批量删除 (schema.batch_delete).
const selectedIds = ref<string[]>([]);
const confirmBatch = ref(false);
const batchBusy = ref(false);
// Blacklist 批量添加 (Pro's 添加 dialog): the management page opens a dedicated
// bulk/validity dialog instead of the generic single-record create, while the
// generic editor is kept for 编辑. Only the admin blacklist resource uses it.
const isBlacklist = computed(() => props.resource === "blacklist");
const isAdminDomains = computed(
  () => props.client.area === "admin" && props.resource === "domains",
);
const isAdminWeixin = computed(
  () => props.client.area === "admin" && props.resource === "weixin-apps",
);
const isAdminDirectory = computed(
  () => props.client.area === "admin" && props.resource === "admin-users",
);
const isOperationalResource = computed(
  () =>
    props.client.area === "admin" &&
    [
      "admin-users",
      "risk-records",
      "merchant-risk",
      "onboarding-channels",
      "onboarding-applications",
    ].includes(props.resource),
);
const hasCompactFilters = computed(
  () =>
    isBlacklist.value ||
    isAdminDomains.value ||
    isAdminWeixin.value ||
    isOperationalResource.value,
);
const listFilters = computed(() =>
  (schema.value?.filters || []).filter(
    (f) => !isAdminDomains.value || f.param !== "enabled",
  ),
);
const searchLabel = computed(() =>
  isAdminDomains.value
    ? "域名"
    : isOperationalResource.value
      ? "搜索内容"
      : hasCompactFilters.value
        ? "搜索"
        : props.resource === "merchants"
          ? "搜索内容"
          : "搜索记录",
);
const searchPlaceholder = computed(() =>
  isAdminDomains.value
    ? "要搜索的域名"
    : isAdminWeixin.value
      ? "名称/APPID"
      : isBlacklist.value
        ? "黑名单内容"
        : isAdminDirectory.value
          ? "UID/用户名/名称"
          : searchLabel.value,
);
const blacklistAddOpen = ref(false);
const blacklistKindField = computed(() =>
  schema.value?.fields.find((f) => f.name === "kind"),
);
function resetListFilters() {
  if (!hasCompactFilters.value || loading.value || busy.value) return;
  query.value = "";
  fieldFilters.value = {};
  page.value = 1;
  selectedIds.value = [];
  void load();
}
function listCellValue(field: ResourceField, row: Record<string, unknown>) {
  if (
    (isBlacklist.value ||
      isAdminDomains.value ||
      isOperationalResource.value) &&
    ["created_at", "expires_at", "last_login_at"].includes(field.name)
  )
    return beijingTimestamp(row[field.name]);
  if (isOriginalMerchantList.value && field.name === "available_minor")
    return "¥" + merchantFieldValue(field.name, row[field.name]);
  if (props.resource === "merchants")
    return merchantFieldValue(field.name, row[field.name]);
  if (field.name === "enabled")
    return consoleValue(field.name, row[field.name]);
  if (field.type === "boolean")
    return row[field.name] === true
      ? "是"
      : row[field.name] === false
        ? "否"
        : "—";
  if (field.type === "select") {
    const option = field.options?.find(
      (o) =>
        String(typeof o === "string" ? o : o.value) === String(row[field.name]),
    );
    if (option) return typeof option === "string" ? option : option.label;
  }
  return displayValue(row[field.name]);
}
function openCreate() {
  if (isBlacklist.value) {
    if (busy.value || loading.value || mode.value !== null) return;
    blacklistAddOpen.value = true;
    return;
  }
  open("create");
}
async function onBlacklistAdded(msg: string) {
  blacklistAddOpen.value = false;
  notice.value = msg;
  await load();
}
function merchantRelatedLinks(id: unknown) {
  const value = String(id ?? "");
  if (!/^[1-9]\d{0,18}$/.test(value) || BigInt(value) > 9223372036854775807n)
    return [];
  return [
    { id: "detail", label: "详情" },
    ...(canDelegate.value ? [{ id: "delegate", label: "一键登录" }] : []),
    ...(canReadMerchantStatistics.value
      ? [{ id: "payment-statistics", label: "支付统计" }]
      : []),
    ...(canManualWithdraw.value
      ? [{ id: "manual-withdrawal", label: "手动提现" }]
      : []),
    ...(canAdjustBalance.value
      ? [{ id: "balance-adjust", label: "加款/扣款" }]
      : []),
    {
      id: "orders",
      label: "订单管理",
      href: `/admin/trade/order?uid=${value}`,
    },
    {
      id: "transfers",
      label: "代付记录",
      href: `/admin/transfer/list?uid=${value}`,
    },
    {
      id: "settlements",
      label: "结算记录",
      href: `/admin/settle/list?uid=${value}`,
    },
    {
      id: "ledger",
      label: "资金明细",
      href: `/admin/merchant/record?uid=${value}`,
    },
    ...(canReadMerchantLogs.value
      ? [{ id: "login-log", label: "登录日志" }]
      : []),
  ];
}
function merchantRelatedAction(action: string, id: unknown) {
  const value = String(id ?? "");
  if (action === "detail") {
    const row = items.value.find((r) => String(r.id) === value);
    if (row && !busy.value) open("detail", row);
    return;
  }
  if (action === "delegate") {
    const row = items.value.find((r) => String(r.id) === value);
    if (row && canDelegate.value && !busy.value) delegation.value = row;
    return;
  }
  if (action === "manual-withdrawal") {
    if (
      !canManualWithdraw.value ||
      busy.value ||
      loading.value ||
      mode.value !== null ||
      !/^[1-9]\d{0,18}$/.test(value) ||
      BigInt(value) > 9223372036854775807n
    )
      return;
    manualWithdrawalReturn =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : undefined;
    manualWithdrawalChanged = false;
    manualWithdrawalID.value = value;
    return;
  }
  if (action === "balance-adjust") {
    if (
      !canAdjustBalance.value ||
      busy.value ||
      loading.value ||
      mode.value !== null ||
      !/^[1-9]\d{0,18}$/.test(value) ||
      BigInt(value) > 9223372036854775807n
    )
      return;
    balanceAdjustReturn =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : undefined;
    balanceAdjustChanged = false;
    balanceAdjustID.value = value;
    return;
  }
  if (action === "payment-statistics") {
    if (
      !canReadMerchantStatistics.value ||
      busy.value ||
      loading.value ||
      mode.value !== null ||
      !/^[1-9]\d{0,18}$/.test(value) ||
      BigInt(value) > 9223372036854775807n
    )
      return;
    merchantStatisticsReturn =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : undefined;
    merchantStatisticsID.value = value;
    return;
  }
  if (
    action !== "login-log" ||
    !canReadMerchantLogs.value ||
    busy.value ||
    loading.value ||
    mode.value !== null ||
    !/^[1-9]\d{0,18}$/.test(value) ||
    BigInt(value) > 9223372036854775807n
  )
    return;
  merchantLogReturn =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : undefined;
  merchantLogID.value = value;
}
function closeMerchantLogs() {
  const target = merchantLogReturn,
    epoch = contextEpoch;
  merchantLogID.value = undefined;
  merchantLogReturn = undefined;
  void nextTick(() => {
    if (epoch === contextEpoch && target?.isConnected) target.focus();
  });
}
function applyMerchantQuery() {
  merchantFilters.value = newMerchantDirectory();
  merchantRouteError.value = "";
  if (props.resource === "merchants") {
    const parsed = readMerchantDirectoryQuery(props.merchantQuery);
    merchantFilters.value = parsed.draft;
    query.value = parsed.search;
    page.value = parsed.page;
    limit.value = parsed.limit;
    merchantRouteError.value = parsed.error;
  }
}
function clearMerchantFilters() {
  merchantFilters.value = newMerchantDirectory();
  merchantRouteError.value = "";
  query.value = "";
  page.value = 1;
  void load();
}
const mode = ref<"create" | "edit" | "detail" | "delete" | "channels" | null>(
    null,
  ),
  selected = ref<Record<string, unknown>>({}),
  values = ref<Record<string, unknown>>({}),
  fieldErrors = ref<Record<string, string>>({});
const groupRiskBusy = ref(false);
const groupDefendBusy = ref(false);
const payRollBusy = ref(false);
const credentialsBusy = ref(false),
  editingBusy = computed(
    () =>
      busy.value ||
      credentialsBusy.value ||
      groupRiskBusy.value ||
      groupDefendBusy.value ||
      payRollBusy.value,
  );
const canManageMerchantCredentials = computed(
  () =>
    props.client.area === "admin" &&
    props.ownSession === true &&
    props.adminClass === "super" &&
    schema.value?.permissions.update === true,
);
function credentialBusy(id: string, value: boolean) {
  if (mode.value === "edit" && String(selected.value.id) === id)
    credentialsBusy.value = value;
}
function discardCredentialDrafts(id: string) {
  if (mode.value === "edit" && String(selected.value.id) === id) {
    values.value.api_key = "";
    values.value.rsa_public_key = "";
  }
}
function observeSignaturePolicy(state: {
  merchant_id: string;
  keytype: 0 | 1 | null;
}) {
  if (
    mode.value === "edit" &&
    String(selected.value.id) === state.merchant_id
  ) {
    selected.value.keytype = state.keytype;
    values.value.keytype = state.keytype;
  }
}
function discardLegacyAlgorithm(id: string, algorithm: "MD5" | "RSA") {
  if (mode.value === "edit" && String(selected.value.id) === id) {
    selected.value.signature_algorithm = algorithm;
    values.value.signature_algorithm = algorithm;
  }
}
let controller: AbortController | undefined,
  detailController: AbortController | undefined,
  contextEpoch = 0;
const unknownMerchantCreation = ref<{ login: string } | null>(null);
const unknownResourceCreation = ref(false);
const merchantCreateSetupIssue = ref("");
const resourcePanel = ref<HTMLElement>();
const formDialog = ref<HTMLElement>();
let pendingFormFocus: HTMLElement | undefined;
let pendingPayRollRowFocus: string | undefined;
function focusResourceDialog() {
  if (
    props.entry ||
    !mode.value ||
    isGroupEditor.value ||
    isMerchantCreationForm.value ||
    isPayRollCustom.value
  )
    return;
  const root = formDialog.value;
  if (!root || root.contains(document.activeElement)) return;
  const field = Array.from(
    root.querySelectorAll<HTMLElement>(
      'input:not([disabled]):not([type="hidden"]),select:not([disabled]),textarea:not([disabled])',
    ),
  ).find((element) => element.tabIndex >= 0 && element.getClientRects().length);
  const target =
    (mode.value === "delete" ? undefined : field) ??
    root.querySelector<HTMLElement>("button:not([disabled])") ??
    root;
  target.focus();
}
function restoreFormFocus() {
  if (mode.value || loading.value || busy.value || payRollBusy.value) return;
  if (resourcePanel.value?.querySelector(".loading-row")) return;
  let target = pendingFormFocus?.isConnected ? pendingFormFocus : undefined;
  if (!target && pendingPayRollRowFocus) {
    target = Array.from(
      resourcePanel.value?.querySelectorAll<HTMLElement>(
        '[data-record-action="channels"]',
      ) || [],
    ).find((button) => button.dataset.recordId === pendingPayRollRowFocus);
    if (
      !target &&
      items.value.some((row) => String(row.id) === pendingPayRollRowFocus)
    )
      return;
    target ??=
      resourcePanel.value?.querySelector<HTMLElement>(
        "[data-resource-create]",
      ) ||
      resourcePanel.value?.querySelector<HTMLElement>(
        "[data-resource-refresh]",
      ) ||
      undefined;
  }
  if (!target && pendingFormFocus) {
    target =
      resourcePanel.value?.querySelector<HTMLElement>(
        "[data-resource-create]",
      ) ||
      resourcePanel.value?.querySelector<HTMLElement>(
        "[data-resource-refresh]",
      ) ||
      undefined;
  }
  if (target?.matches(":disabled")) return;
  target?.focus();
  pendingFormFocus = undefined;
  pendingPayRollRowFocus = undefined;
}
function resetContext() {
  // Clear the existing object too: child components and pending callbacks may
  // still hold it when a route/session replacement changes the resource prop.
  for (const key of Object.keys(values.value)) delete values.value[key];
  contextEpoch++;
  controller?.abort();
  detailController?.abort();
  schema.value = null;
  items.value = [];
  total.value = 0;
  page.value = 1;
  limit.value = 20;
  query.value = "";
  loading.value = false;
  busy.value = false;
  credentialsBusy.value = false;
  groupRiskBusy.value = false;
  groupDefendBusy.value = false;
  payRollBusy.value = false;
  mode.value = null;
  selected.value = {};
  values.value = {};
  fieldErrors.value = {};
  delegation.value = null;
  merchantLogID.value = undefined;
  merchantStatisticsID.value = undefined;
  manualWithdrawalID.value = undefined;
  manualWithdrawalReturn = undefined;
  manualWithdrawalChanged = false;
  merchantStatisticsReturn = undefined;
  groupStatisticsID.value = undefined;
  groupStatisticsReturn = undefined;
  merchantLogReturn = undefined;
  editMerchantRecipient.value = false;
  error.value = "";
  notice.value = "";
  pendingFormFocus = undefined;
  pendingPayRollRowFocus = undefined;
  unknownMerchantCreation.value = null;
  unknownResourceCreation.value = false;
  merchantCreateSetupIssue.value = "";
  applyMerchantQuery();
}
watch(
  mode,
  (next, previous) => {
    if (next === null && previous !== null) {
      if (unknownResourceCreation.value) {
        values.value = {};
        fieldErrors.value = {};
      }
      unknownResourceCreation.value = false;
      if (["merchants", "pay-rolls"].includes(props.resource)) {
        payRollBusy.value = false;
        unknownMerchantCreation.value = null;
        merchantCreateSetupIssue.value = "";
        for (const key of Object.keys(values.value)) delete values.value[key];
        values.value = {};
        fieldErrors.value = {};
      }
      if (props.entry) return;
      pendingFormFocus = groupReturnFocus.value;
      pendingPayRollRowFocus =
        props.resource === "pay-rolls" && previous === "channels"
          ? String(selected.value.id)
          : undefined;
      void nextTick(restoreFormFocus);
    }
  },
  { flush: "sync" },
);
// Focus only after Vue has mounted the drawer; the synchronous watcher above
// is reserved for immediately clearing private drafts when their modal closes.
watch(mode, focusResourceDialog, { flush: "post" });
// Successful creates/reconciliation reload the list while its opener is still
// disabled. Restore only after both the read and write have released it.
watch([loading, busy, payRollBusy], () => void nextTick(restoreFormFocus), {
  flush: "post",
});
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.resource,
    props.adminClass,
    props.ownSession,
    props.entry?.mode,
    props.entry?.id,
    props.entry?.returnPath,
    props.merchantQuery,
  ],
  () => {
    resetContext();
    void load();
  },
  { flush: "sync" },
);
const endpoint = computed(
  () => "/resources/" + encodeURIComponent(props.resource),
);
// Merchant directory CSV export (Pro user/export): same filters as the list,
// non-secret columns only, admin-only. Backend GET …?format=csv.
const canExportMerchants = computed(
  () => props.resource === "merchants" && props.client.area === "admin",
);
const merchantExportPath = computed(() => {
  const q = new URLSearchParams({ format: "csv", q: query.value });
  appendMerchantDirectory(q, merchantFilters.value);
  return endpoint.value + "?" + q.toString();
});
// Generic export (schema.exportable): same filters + search as the list.
const canExport = computed(() => schema.value?.exportable === true);
const exportPath = computed(() => {
  const q = new URLSearchParams({ format: "csv", q: query.value });
  for (const [param, value] of Object.entries(fieldFilters.value))
    if (value) q.set(param, value);
  return endpoint.value + "?" + q.toString();
});
const canBatchDelete = computed(() => schema.value?.batch_delete === true);
const canBatchDomainStatus = computed(
  () => schema.value?.batch_status === true,
);
const confirmDomainStatus = ref<null | boolean>(null);
async function runBatchDomainStatus() {
  const approved = confirmDomainStatus.value;
  if (approved === null || batchBusy.value || !selectedIds.value.length) return;
  batchBusy.value = true;
  error.value = "";
  try {
    const res = await props.client.request<{ updated?: number }>(
      endpoint.value + "/batch-status",
      { method: "POST", body: { ids: selectedIds.value, approved } },
    );
    notice.value = `已${approved ? "通过" : "拒绝"} ${res?.updated ?? selectedIds.value.length} 条授权域名`;
    confirmDomainStatus.value = null;
    selectedIds.value = [];
    await load();
  } catch (e) {
    error.value = message(e);
  } finally {
    batchBusy.value = false;
  }
}
function applyFieldFilter(param: string, value: string) {
  fieldFilters.value = { ...fieldFilters.value, [param]: value };
  page.value = 1;
  selectedIds.value = [];
  void load();
}
function isDefaultGroup(id: unknown): boolean {
  return props.resource === "groups" && String(id) === "0";
}
const selectableItems = computed(() =>
  items.value.filter((row) => !isDefaultGroup(row.id)),
);
function toggleRow(id: string, on: boolean) {
  if (on && isDefaultGroup(id)) return;
  selectedIds.value = on
    ? [...new Set([...selectedIds.value, id])]
    : selectedIds.value.filter((x) => x !== id);
}
const allRowsSelected = computed(
  () =>
    selectableItems.value.length > 0 &&
    selectableItems.value.every((r) =>
      selectedIds.value.includes(String(r.id)),
    ),
);
function toggleAllRows(on: boolean) {
  selectedIds.value = on ? selectableItems.value.map((r) => String(r.id)) : [];
}
async function runBatchDelete() {
  if (batchBusy.value || !selectedIds.value.length) return;
  if (selectedIds.value.some(isDefaultGroup)) {
    selectedIds.value = selectedIds.value.filter((id) => !isDefaultGroup(id));
    confirmBatch.value = false;
    error.value = "默认用户组不可删除，请重新核对所选记录。";
    return;
  }
  batchBusy.value = true;
  error.value = "";
  try {
    const res = await props.client.request<{ deleted?: number }>(
      endpoint.value + "/batch-delete",
      { method: "POST", body: { ids: selectedIds.value } },
    );
    notice.value = `已删除 ${res?.deleted ?? selectedIds.value.length} 条记录`;
    confirmBatch.value = false;
    selectedIds.value = [];
    await load();
  } catch (e) {
    error.value = message(e);
  } finally {
    batchBusy.value = false;
  }
}
const fields = computed(
  () =>
    schema.value?.fields.filter(
      (f) =>
        !f.write_only && f.type !== "password" && !isCredentialField(f.name),
    ) || [],
);
const listFields = computed(() => {
  const visible = resourceListFields(
    props.resource,
    schema.value?.fields || [],
  );
  if (isAdminDomains.value)
    return visible
      .filter((f) =>
        ["merchant_id", "domain", "approved", "created_at"].includes(f.name),
      )
      .map((f) => ({
        ...f,
        label:
          f.name === "merchant_id"
            ? "商户号"
            : f.name === "approved"
              ? "状态"
              : f.label,
      }));
  if (!isBlacklist.value) return visible;
  // The list follows Pro's compact columns; the edit form retains its enabled
  // control and original schema labels, independent from these list labels.
  const names = ["kind", "value", "created_at", "expires_at", "reason"];
  return names.flatMap((name) => {
    const field = visible.find((f) => f.name === name);
    return field
      ? [{ ...field, label: name === "expires_at" ? "过期时间" : field.label }]
      : [];
  });
});
const isOriginalMerchantList = computed(
  () =>
    props.resource === "merchants" &&
    hasOriginalMerchantListFields(schema.value?.fields || []),
);
const isGroupEditor = computed(
  () =>
    props.resource === "groups" &&
    ["create", "edit"].includes(mode.value || ""),
);
const isMerchantCreationForm = computed(
  () =>
    props.resource === "merchants" &&
    mode.value === "create" &&
    schema.value?.fields.some((f) => f.name === "creation_mode"),
);
const isPayRollCustom = computed(
  () =>
    props.resource === "pay-rolls" &&
    ["create", "channels"].includes(mode.value || ""),
);
const isPayRollDraftEditing = computed(() => {
  if (props.resource !== "pay-rolls" || mode.value !== "edit") return false;
  try {
    const record = readPayRollRecord(selected.value);
    return (
      !record.enabled &&
      record.default_route === false &&
      record.channel_ids === ""
    );
  } catch {
    return false;
  }
});
const payRollTitle = computed(() =>
  mode.value === "create" ? "新增轮询组" : "配置通道",
);
async function payRollSaved() {
  if (!isPayRollCustom.value) return;
  mode.value = null;
  notice.value = "轮询组已保存，请核对当前配置。";
  await load();
}
function payRollRecoveryRequired() {
  if (!isPayRollCustom.value) return;
  notice.value =
    "轮询组创建结果尚未核实，请核对最新列表；当前表单不会再次创建。";
  void load();
}
const groupEditorTitle = computed(() =>
  mode.value === "create"
    ? "新增用户组"
    : "编辑用户组 GID:" + String(selected.value.id ?? ""),
);
// Create/edit/detail are long forms, record details and panels: they open as a
// right drawer so the actions stay pinned and the body scrolls. The delete
// confirmation is a short centered dialog.
const asDrawer = computed(
  () => !props.entry && mode.value !== null && mode.value !== "delete",
);
// Two-column forms (merchants) and the tabbed group editor need more width;
// other resource forms are a single label column.
const drawerWidth = computed(() =>
  props.resource === "merchants"
    ? "760px"
    : isGroupEditor.value
      ? "900px"
      : isPayRollCustom.value
        ? "860px"
        : "560px",
);
function closeResourceDialog(event?: Event) {
  if (
    event?.defaultPrevented ||
    props.entry ||
    editingBusy.value ||
    !mode.value
  )
    return;
  const top = Array.from(
    document.querySelectorAll<HTMLElement>(
      '[role="dialog"][aria-modal="true"]',
    ),
  )
    .filter((el) => !el.closest("[inert]") && el.getClientRects().length)
    .at(-1);
  if (top && top !== formDialog.value) return;
  mode.value = null;
}
function trapResourceFocus(event: KeyboardEvent) {
  if (props.entry || !mode.value) return;
  const root = event.currentTarget as HTMLElement;
  const nodes = Array.from(
    root.querySelectorAll<HTMLElement>(
      'button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),a[href],[tabindex="0"]',
    ),
  ).filter((el) => el.tabIndex >= 0 && el.getClientRects().length);
  const first = nodes[0],
    last = nodes.at(-1);
  if (!first || !last) {
    event.preventDefault();
    formDialog.value?.focus();
    return;
  }
  if (!nodes.includes(document.activeElement as HTMLElement)) {
    event.preventDefault();
    (event.shiftKey ? last : first).focus();
    return;
  }
  if (
    first &&
    last &&
    (event.shiftKey
      ? document.activeElement === first
      : document.activeElement === last)
  ) {
    event.preventDefault();
    (event.shiftKey ? last : first).focus();
  }
}
const groupSection = ref("basic");
const groupReturnFocus = ref<HTMLElement>();
const hasGroupProjection = computed(
  () =>
    props.resource === "merchants" &&
    schema.value?.fields.some((f) =>
      ["effective_group_id", "effective_group_name"].includes(f.name),
    ),
);
const hasDomainProjection = computed(
  () =>
    props.resource === "merchants" &&
    schema.value?.fields.some((f) => f.name === "approved_domains"),
);
const hasContactProjection = computed(
  () =>
    props.resource === "merchants" &&
    schema.value?.fields.some((f) => ["phone", "qq"].includes(f.name)),
);
const contactAnchor = computed(
  () =>
    (isOriginalMerchantList.value
      ? listFields.value
      : schema.value?.fields
    )?.find((f) => f.name === "email")?.name ||
    (isOriginalMerchantList.value
      ? listFields.value
      : schema.value?.fields
    )?.find((f) => ["phone", "qq"].includes(f.name))?.name,
);
// 添加商户 asks for no key: the server generates an MD5 key, which is then
// managed under 密钥信息 when editing.
const merchantCreateHidden = new Set([
  "api_key",
  "signature_algorithm",
  "rsa_public_key",
  "keytype",
]);
const formSchema = computed<ResourceSchema | null>(() =>
  schema.value
    ? {
        ...schema.value,
        fields: businessNoticeSettingsFields(
          schema.value.fields
            .filter(
              (f) =>
                !(
                  props.resource === "merchants" &&
                  mode.value === "create" &&
                  merchantCreateHidden.has(f.name)
                ) &&
                !(
                  isMerchantCreationForm.value &&
                  (values.value.creation_mode !== "api"
                    ? [
                        "id",
                        "name",
                        "login_username",
                        "confirm_group_assignment",
                      ].includes(f.name)
                    : [
                        "login_username",
                        "login_password",
                        "confirm_group_assignment",
                      ].includes(f.name))
                ),
            )
            .map((f) =>
              isMerchantCreationForm.value &&
              values.value.creation_mode !== "api" &&
              f.name === "login_password"
                ? { ...f, required: true }
                : isPayRollDraftEditing.value && f.name === "channel_ids"
                  ? { ...f, required: false }
                  : mode.value === "edit" &&
                      props.resource === "merchants" &&
                      selected.value.login_username &&
                      f.name === "login_username"
                    ? { ...f, read_only: true }
                    : props.resource === "merchants" &&
                        mode.value === "create" &&
                        f.name === "fee_mode"
                      ? { ...f, required: true }
                      : f,
            ),
          props.resource === "settings" ? values.value.key : null,
        ),
      }
    : null,
);
const message = (e: unknown) =>
  e instanceof Error ? e.message : "操作失败，请重试";
async function refreshCase() {
  const current = contextEpoch,
    client = props.client,
    path = endpoint.value + "/" + encodeURIComponent(String(selected.value.id));
  await load();
  if (current !== contextEpoch) return;
  detailController?.abort();
  const c = new AbortController();
  detailController = c;
  try {
    const record = await client.request<Record<string, unknown>>(path, {
      signal: c.signal,
    });
    if (current === contextEpoch && !c.signal.aborted) selected.value = record;
  } catch (e) {
    if (current === contextEpoch && !c.signal.aborted) error.value = message(e);
  }
}
async function load() {
  if (props.resource === "weixin-apps" && props.client.area === "admin") return;
  if (
    props.resource === "channels" &&
    props.client.area === "admin" &&
    props.ownSession === true &&
    props.adminClass === "super"
  )
    return;
  merchantStatisticsID.value = undefined;
  merchantStatisticsReturn = undefined;
  groupStatisticsID.value = undefined;
  groupStatisticsReturn = undefined;
  merchantLogID.value = undefined;
  merchantLogReturn = undefined;
  controller?.abort();
  controller = new AbortController();
  const c = controller,
    current = contextEpoch,
    client = props.client,
    path = endpoint.value;
  loading.value = true;
  error.value = "";
  // A list shows a newly read schema together with its first page (see
  // finally), so its toolbar buttons, filters and table appear at once
  // instead of the toolbar changing while the page is still loading.
  let pendingSchema: ResourceSchema | null = null;
  try {
    if (props.resource === "merchants") {
      const problem =
        merchantRouteError.value ||
        merchantDirectoryError(merchantFilters.value, query.value);
      if (problem) {
        items.value = [];
        total.value = 0;
        error.value = problem;
        return;
      }
    }
    let loadedSchema = schema.value;
    if (!loadedSchema) {
      loadedSchema = await client.request<ResourceSchema>(path + "/schema", {
        signal: c.signal,
      });
      if (c.signal.aborted || current !== contextEpoch) return;
      pendingSchema = loadedSchema;
    }
    if (props.entry) {
      schema.value = loadedSchema;
      if (props.entry.mode === "edit" && !props.entry.id) {
        error.value = "资料编号无效";
        return;
      }
      if (
        !(props.entry.mode === "create"
          ? loadedSchema.permissions.create
          : loadedSchema.permissions.update)
      ) {
        error.value = "当前身份不能打开此资料编辑表单";
        return;
      }
      await open(
        props.entry.mode,
        props.entry.mode === "edit" ? { id: props.entry.id } : {},
      );
      return;
    }
    const q = new URLSearchParams({
      page: String(page.value),
      limit: String(limit.value),
      q: query.value,
    });
    if (props.resource === "merchants")
      appendMerchantDirectory(q, merchantFilters.value);
    for (const [param, value] of Object.entries(fieldFilters.value))
      if (value) q.set(param, value);
    const data = await client.request<{
      items: Record<string, unknown>[];
      total: number;
    }>(path + "?" + q, { signal: c.signal });
    if (!c.signal.aborted && current === contextEpoch) {
      const targetPage = listPageWithinTotal(
        page.value,
        data.total || 0,
        limit.value,
      );
      if (targetPage !== page.value) {
        page.value = targetPage;
        await load();
        return;
      }
      items.value = data.items || [];
      total.value = data.total || 0;
    }
  } catch (e) {
    if (!c.signal.aborted && current === contextEpoch) error.value = message(e);
  } finally {
    if (!c.signal.aborted && current === contextEpoch) {
      if (pendingSchema && !schema.value) schema.value = pendingSchema;
      loading.value = false;
    }
  }
}
async function open(
  action: "create" | "edit" | "detail" | "delete" | "channels",
  row: Record<string, unknown> = {},
  section = "basic",
) {
  groupReturnFocus.value =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : undefined;
  groupSection.value = section;
  detailController?.abort();
  const c = new AbortController(),
    current = contextEpoch;
  detailController = c;
  error.value = "";
  notice.value = "";
  if (action === "delete" && isDefaultGroup(row.id)) {
    error.value = "系统自带默认用户组不支持删除";
    return;
  }
  fieldErrors.value = {};
  editMerchantRecipient.value = false;
  mode.value = null;
  values.value = {};
  selected.value = row;
  payRollBusy.value = false;
  if (action === "channels") {
    if (props.resource === "pay-rolls" && schema.value?.permissions.update)
      mode.value = action;
    return;
  }
  if (action === "create") {
    values.value = formValues(schema.value!);
    if (
      props.resource === "merchants" &&
      schema.value!.fields.some((f) => f.name === "creation_mode")
    )
      values.value.creation_mode = "account";
    if (
      props.resource === "groups" &&
      schema.value!.fields.some((field) => field.name === "info") &&
      Object.keys(readGroupPaymentRates(values.value.info)).length === 0
    ) {
      values.value.info = JSON.stringify({
        alipay: { channel: "0", type: "", rate: "" },
        wxpay: { channel: "0", type: "", rate: "" },
      });
    }
    mode.value = action;
    return;
  }
  busy.value = true;
  try {
    const record = await props.client.request<Record<string, unknown>>(
      endpoint.value + "/" + encodeURIComponent(String(row.id)),
      { signal: c.signal },
    );
    if (c.signal.aborted || current !== contextEpoch) return;
    selected.value = record;
    values.value = formValues(schema.value!, selected.value);
    mode.value = action;
  } catch (e) {
    if (!c.signal.aborted && current === contextEpoch) error.value = message(e);
  } finally {
    if (!c.signal.aborted && current === contextEpoch) busy.value = false;
  }
}
async function save() {
  if (
    editingBusy.value ||
    unknownMerchantCreation.value ||
    unknownResourceCreation.value ||
    isPayRollCustom.value
  )
    return;
  const current = contextEpoch;
  if (!schema.value) return;
  if (
    props.resource === "merchants" &&
    !(isMerchantCreationForm.value && values.value.creation_mode === "account")
  ) {
    const username = String(
      values.value.login_username || selected.value.login_username || "",
    );
    const password = String(values.value.login_password || "");
    if (username && !selected.value.login_username && !password) {
      error.value = "设置后台登录账号时请同时填写登录密码";
      return;
    }
    if (password && !username) {
      error.value = "请先填写后台登录账号";
      return;
    }
  }
  fieldErrors.value = validateFields(
    formSchema.value!,
    values.value,
    mode.value === "edit",
  );
  if (
    isMerchantCreationForm.value &&
    values.value.creation_mode === "account"
  ) {
    if (
      !String(values.value.phone || "").trim() &&
      !String(values.value.email || "").trim()
    )
      fieldErrors.value.phone = "请填写手机号或邮箱";
    if (
      !String(values.value.phone || "").trim() &&
      new TextEncoder().encode(String(values.value.email || "").trim()).length >
        100
    )
      fieldErrors.value.email = "邮箱登录账号最多100字节";
    const password = String(values.value.login_password || "");
    if (
      new TextEncoder().encode(password).length < 12 ||
      new TextEncoder().encode(password).length > 1024
    )
      fieldErrors.value.login_password = "登录密码须为12至1024字节";
  }
  if (props.resource === "groups") {
    const rateError = groupPaymentRatesError(values.value.info);
    if (rateError) fieldErrors.value.info = rateError;
  }
  if (isMerchantCreationForm.value && merchantCreateSetupIssue.value)
    fieldErrors.value.settlement_type = merchantCreateSetupIssue.value;
  if (props.resource === "merchants") {
    const key = String(values.value.api_key || "");
    if (
      formSchema.value?.fields.some(
        (f) => f.name === "api_key" && !f.read_only && !f.readonly,
      ) &&
      key &&
      key.length < 24
    )
      fieldErrors.value.api_key = "API 密钥至少需要24个字符";
    if (mode.value === "create")
      Object.assign(fieldErrors.value, createRecipientErrors(values.value));
  }
  if (Object.keys(fieldErrors.value).length) return;
  const creatingResource = mode.value === "create";
  let creationDispatched = false;
  busy.value = true;
  error.value = "";
  try {
    const payload = buildPayload(
      formSchema.value!,
      values.value,
      mode.value === "edit",
    );
    if (props.resource === "merchants") {
      if (!values.value.login_username) delete payload.login_username;
      if (!values.value.login_password) delete payload.login_password;
      if (mode.value === "create") omitUnusedCreateRecipient(payload);
    }
    if (props.resource === "admin-users" && mode.value === "edit") {
      if (payload.username === selected.value.username) delete payload.username;
      else if (payload.username !== undefined)
        payload.expected_username = String(selected.value.username || "");
    }
    await props.client.request(
      endpoint.value +
        (mode.value === "edit"
          ? "/" + encodeURIComponent(String(selected.value.id))
          : ""),
      {
        method: mode.value === "edit" ? "PATCH" : "POST",
        body: payload,
        beforeDispatch: () => {
          creationDispatched = true;
        },
      },
    );
    if (current !== contextEpoch) return;
    notice.value = mode.value === "edit" ? "修改已保存" : "新增已保存";
    mode.value = null;
    if (!props.entry) await load();
  } catch (e) {
    if (current === contextEpoch) {
      if (
        isMerchantCreationForm.value &&
        values.value.creation_mode === "account" &&
        (!(e instanceof ApiError) || !e.responseDecoded || e.status >= 500)
      ) {
        unknownMerchantCreation.value = {
          login: String(values.value.phone || values.value.email || "").trim(),
        };
        delete values.value.login_password;
        delete values.value.settlement_account;
        delete values.value.settlement_name;
        error.value = "添加结果尚未确认，请核对商户账号；不会自动重复添加。";
      } else if (
        creatingResource &&
        creationDispatched &&
        uncertainCreationResult(e)
      ) {
        unknownResourceCreation.value = true;
        for (const field of formSchema.value?.fields || []) {
          if (field.type === "password" || field.write_only)
            delete values.value[field.name];
        }
        error.value =
          "新增结果尚未确认，此表单不会再次提交。请关闭并读取列表核对原记录。";
      } else error.value = message(e);
    }
  } finally {
    if (current === contextEpoch) busy.value = false;
  }
}
async function readCreatedResourceList() {
  if (editingBusy.value || props.entry) return;
  mode.value = null;
  values.value = {};
  fieldErrors.value = {};
  page.value = 1;
  await load();
}
async function reconcileMerchantCreation() {
  const pending = unknownMerchantCreation.value;
  if (!pending || editingBusy.value) return;
  const current = contextEpoch;
  busy.value = true;
  error.value = "";
  try {
    const q = new URLSearchParams({
      column: "login_username",
      q: pending.login,
      page: "1",
      limit: "100",
    });
    const result = await props.client.request<{
      items: Record<string, unknown>[];
    }>(endpoint.value + "?" + q);
    if (current !== contextEpoch || unknownMerchantCreation.value !== pending)
      return;
    const matches = Array.isArray(result.items)
      ? result.items.filter(
          (row) =>
            row.login_username === pending.login &&
            /^[1-9]\d{0,18}$/.test(String(row.id)),
        )
      : [];
    if (matches.length !== 1) {
      error.value =
        "尚未核对到唯一的商户账号，请稍后再次核对或关闭后在商户列表查询。不会重复添加。";
      return;
    }
    const merchantID = String(matches[0]!.id);
    merchantFilters.value = {
      ...newMerchantDirectory(),
      column: "login_username",
    };
    query.value = pending.login;
    page.value = 1;
    mode.value = null;
    notice.value =
      "已找到登录账号对应的商户 #" + merchantID + "，请核对商户资料。";
    await load();
  } catch {
    if (current === contextEpoch)
      error.value = "商户账号暂时无法核对，请稍后重新核对；不会重复添加。";
  } finally {
    if (current === contextEpoch) busy.value = false;
  }
}
async function remove() {
  const current = contextEpoch;
  busy.value = true;
  error.value = "";
  try {
    await props.client.request(
      endpoint.value + "/" + encodeURIComponent(String(selected.value.id)),
      { method: "DELETE" },
    );
    if (current !== contextEpoch) return;
    mode.value = null;
    notice.value = "记录已删除";
    await load();
  } catch (e) {
    if (current === contextEpoch) error.value = message(e);
  } finally {
    if (current === contextEpoch) busy.value = false;
  }
}
onMounted(() => {
  applyMerchantQuery();
  void load();
});
onBeforeUnmount(resetContext);
</script>
<template>
  <template
    v-if="
      resource === 'channels' &&
      client.area === 'admin' &&
      ownSession === true &&
      adminClass === 'super'
    "
  >
    <ParentChannelPage
      :client="client"
      :allowed="ownSession === true && adminClass === 'super'"
    />
    <ChannelStopPanel :client="client" />
  </template>
  <template v-else-if="resource === 'weixin-apps' && client.area === 'admin'">
    <WeixinAppsPage :client="client" />
  </template>
  <template v-else-if="resource === 'pay-types' && client.area === 'admin'">
    <PayTypePage :client="client" />
  </template>
  <template v-else-if="!entry || !mode">
    <RuntimeChannelPolicyPanel
      v-if="
        resource === 'channels' &&
        client.area === 'admin' &&
        ownSession === true &&
        adminClass === 'super'
      "
      :client="client"
      :allowed="ownSession === true && adminClass === 'super'"
    />
    <ChannelStopPanel
      v-if="
        resource === 'channels' &&
        client.area === 'admin' &&
        adminClass === 'super'
      "
      :client="client"
    />
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
    <form
      v-if="!entry && showListFilter"
      class="toolbar list-filter-card panel resource-filter"
      @submit.prevent="
        page = 1;
        load();
      "
    >
      <div class="search-group list-filters-grid">
        <KitField v-if="resource === 'merchants'" label="搜索字段">
          <KitSelect
            v-model="merchantFilters.column"
            :options="merchantDirectoryColumns"
            aria-label="商户搜索字段"
            :disabled="loading || busy"
          />
        </KitField>
        <KitField :label="searchLabel">
          <KitInput
            v-model="query"
            :placeholder="searchPlaceholder"
            :aria-label="'搜索' + (schema?.title || '')"
          />
        </KitField>
        <KitField
          v-for="f in listFilters"
          :key="f.param"
          :label="isBlacklist && f.param === 'kind' ? '类型' : f.label"
        >
          <KitInput
            v-if="f.type === 'positive_id'"
            :model-value="fieldFilters[f.param] || ''"
            @update:model-value="(v: string) => (fieldFilters[f.param] = v)"
            :aria-label="f.label"
            :placeholder="f.label"
            inputmode="numeric"
            :disabled="loading || busy"
          />
          <KitSelect
            v-else
            :model-value="fieldFilters[f.param] || ''"
            :options="f.options"
            :aria-label="f.label"
            :disabled="loading || busy"
            @change="(v: string) => applyFieldFilter(f.param, v)"
          />
        </KitField>
      </div>
      <MerchantDirectoryFilters
        v-if="resource === 'merchants'"
        v-model="merchantFilters"
        :disabled="loading || busy"
        @clear="clearMerchantFilters"
      />
      <div v-else class="pa-filter-actions resource-filter-actions">
        <button
          v-if="hasCompactFilters"
          type="button"
          class="pa-btn"
          :disabled="loading || busy"
          @click="resetListFilters"
        >
          重置
        </button>
        <button
          type="submit"
          class="pa-btn pa-btn--primary"
          :class="{ 'pa-btn--loading': loading }"
          :disabled="loading"
        >
          查询
        </button>
      </div>
    </form>
    <template v-if="resource === 'addon-configs' && !entry">
      <AddonConfigGroups
        :items="items"
        :fields="listFields"
        :entity-label="entityLabel"
        :can-create="schema?.permissions.create === true"
        :can-update="schema?.permissions.update === true"
        :can-delete="schema?.permissions.delete === true"
        :busy="busy"
        :loading="loading"
        :note="schema?.description"
        @create="open('create')"
        @refresh="load"
        @open="(action, row) => open(action, row)"
      />
      <KitPagination
        v-if="total > items.length"
        v-model:page="page"
        v-model:page-size="limit"
        :total="total"
        class="addon-pager"
        @change="load"
      />
    </template>
    <section
      v-else-if="!entry || schema?.description || showBusinessNotices"
      ref="resourcePanel"
      class="panel records"
    >
      <div v-if="!entry && (schema || !loading)" class="table-tools">
        <a
          v-if="
            schema?.permissions.create && resource === 'onboarding-applications'
          "
          class="button-link primary"
          :href="
            client.area === 'merchant'
              ? '/merchant/applyments/create'
              : '/admin/applyments/form'
          "
          ><ShellIcon name="plus" />新增{{ entityLabel }}</a
        ><button
          v-if="
            schema?.permissions.create && resource !== 'onboarding-applications'
          "
          type="button"
          class="primary"
          data-resource-create
          :disabled="busy"
          @click="openCreate"
        >
          <ShellIcon name="plus" />{{
            isBlacklist
              ? "添加"
              : isAdminDirectory
                ? "添加管理员"
                : "新增" + entityLabel
          }}
        </button>
        <span v-if="summaryNoun" class="table-summary"
          >系统共有<strong>{{ total }}</strong
          >个{{ summaryNoun }}</span
        >
        <button
          v-if="canBatchDelete"
          type="button"
          class="pa-btn pa-btn--danger"
          :disabled="busy || batchBusy || !selectedIds.length"
          @click="confirmBatch = true"
        >
          批量删除{{ selectedIds.length ? ` (${selectedIds.length})` : "" }}
        </button>
        <button
          v-if="canBatchDomainStatus"
          type="button"
          class="pa-btn pa-btn--primary"
          :disabled="busy || batchBusy || !selectedIds.length"
          @click="confirmDomainStatus = true"
        >
          批量通过{{ selectedIds.length ? ` (${selectedIds.length})` : "" }}
        </button>
        <button
          v-if="canBatchDomainStatus"
          type="button"
          class="pa-btn"
          :disabled="busy || batchBusy || !selectedIds.length"
          @click="confirmDomainStatus = false"
        >
          批量拒绝{{ selectedIds.length ? ` (${selectedIds.length})` : "" }}
        </button>
        <div class="table-tools-end">
          <CsvExportButton
            v-if="canExportMerchants"
            :client="client"
            :path="merchantExportPath"
            filename="商户列表.csv"
            :disabled="loading || busy"
          />
          <CsvExportButton
            v-else-if="canExport"
            :client="client"
            :path="exportPath"
            :filename="`${resource}.csv`"
            :disabled="loading || busy"
          />
          <button
            type="button"
            data-resource-refresh
            :disabled="loading || busy"
            @click="load"
          >
            <ShellIcon name="refresh" />刷新
          </button>
          <KitHelpPopover
            v-if="schema?.description"
            :text="schema.description"
          />
        </div>
      </div>
      <div v-if="loading" class="loading-row" role="status">正在加载记录…</div>
      <div v-else-if="schema && !entry" class="table-scroll">
        <table>
          <thead>
            <tr>
              <th v-if="canBatchDelete" scope="col" class="select-col">
                <input
                  type="checkbox"
                  aria-label="选择本页全部记录"
                  :checked="allRowsSelected"
                  :disabled="busy || batchBusy || !selectableItems.length"
                  @change="
                    toggleAllRows(($event.target as HTMLInputElement).checked)
                  "
                />
              </th>
              <th scope="col">
                {{
                  resource === "merchants"
                    ? hasGroupProjection
                      ? isOriginalMerchantList
                        ? "商户号/用户组"
                        : "商户号 / 用户组"
                      : "商户号"
                    : resource === "groups"
                      ? "GID"
                      : isAdminDirectory
                        ? "UID"
                        : hasCompactFilters || resource === "pay-rolls"
                          ? "ID"
                          : "编号"
                }}
              </th>
              <th
                v-for="field in listFields"
                :key="field.name"
                scope="col"
                :title="
                  isOriginalMerchantList && field.name === 'created_at'
                    ? '注册时间为北京时间'
                    : isOriginalMerchantList && field.name === 'available_minor'
                      ? '金额单位：元'
                      : undefined
                "
              >
                {{
                  resource === "merchants"
                    ? isOriginalMerchantList
                      ? field.label
                      : hasContactProjection && field.name === contactAnchor
                        ? "联系方式"
                        : field.name === "created_at" &&
                            schema.fields.some((f) => f.name === "url")
                          ? "注册时间 / 域名"
                          : consoleColumnLabel(field.name, field.label)
                    : field.label
                }}
              </th>
              <th v-if="resource === 'groups'" scope="col">通道与费率</th>
              <th scope="col">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in items" :key="String(row.id)">
              <td v-if="canBatchDelete" class="select-col">
                <input
                  type="checkbox"
                  :aria-label="'选择记录 ' + row.id"
                  :checked="selectedIds.includes(String(row.id))"
                  :disabled="busy || batchBusy || isDefaultGroup(row.id)"
                  @change="
                    toggleRow(
                      String(row.id),
                      ($event.target as HTMLInputElement).checked,
                    )
                  "
                />
              </td>
              <td class="resource-record-id">
                {{ row.id }}
                <small
                  v-if="isOriginalMerchantList && typeof row.name === 'string'"
                  class="merchant-balance-detail"
                  >{{ row.name }}</small
                >
                <template v-if="hasGroupProjection">
                  <small class="merchant-balance-detail muted">{{
                    merchantGroupLabel(row)
                  }}</small>
                  <small
                    v-if="
                      typeof row.membership_expires_at === 'string' &&
                      row.membership_expires_at
                    "
                    class="merchant-balance-detail muted"
                    >{{
                      row.group_membership_active === true
                        ? "成员有效期至"
                        : "原成员到期时间"
                    }}（北京时间）：{{
                      consoleValue(
                        "membership_expires_at",
                        row.membership_expires_at,
                      )
                    }}</small
                  >
                </template>
              </td>
              <td
                v-for="field in listFields"
                :key="field.name"
                :class="{ 'pa-order-no': isOrderNumberKey(field.name) }"
              >
                <template v-if="isOriginalMerchantList && field.name === 'pay'">
                  <span
                    class="badge-stack merchant-state-tags"
                    :title="
                      effectiveRiskDescription(
                        row.core_risk,
                        row.enabled === true,
                        row.pay,
                      )
                    "
                  >
                    <KitStatusTag
                      :tone="stateTone('enabled', row.enabled)"
                      :text="row.enabled === true ? '正常' : '封禁'"
                    />
                    <KitStatusTag
                      :tone="stateTone('pay', row.pay)"
                      :text="merchantStateChip('pay', row.pay)"
                    />
                    <KitStatusTag
                      v-if="row.settle !== undefined"
                      :tone="stateTone('settle', row.settle)"
                      :text="merchantStateChip('settle', row.settle)"
                    />
                  </span>
                </template>
                <template
                  v-else-if="
                    hasContactProjection && field.name === contactAnchor
                  "
                >
                  <small
                    v-for="contact in [
                      { key: 'phone', label: '手机' },
                      { key: 'email', label: '邮箱' },
                      { key: 'qq', label: 'QQ' },
                    ].filter(
                      (c) =>
                        fields.some((f) => f.name === c.key) &&
                        typeof row[c.key] === 'string' &&
                        row[c.key],
                    )"
                    :key="contact.key"
                    class="merchant-balance-detail"
                    >{{ contact.label }}：{{ row[contact.key] }}</small
                  >
                  <span v-if="!row.phone && !row.email && !row.qq">—</span>
                </template>
                <KitStatusTag
                  v-else-if="
                    resource === 'domains' && field.name === 'approved'
                  "
                  :tone="row.approved === true ? 'success' : 'warning'"
                  :text="row.approved === true ? '已审核' : '待审核'"
                />
                <KitStatusTag
                  v-else-if="isAdminDirectory && field.name === 'totp_enabled'"
                  :tone="row.totp_enabled === true ? 'success' : 'neutral'"
                  :text="
                    row.totp_enabled === true
                      ? '已开启'
                      : row.totp_enabled === false
                        ? '未开启'
                        : '—'
                  "
                />
                <KitStatusTag
                  v-else-if="field.name === 'enabled'"
                  :tone="stateTone('enabled', row.enabled)"
                  :text="consoleValue('enabled', row.enabled)"
                />
                <KitStatusTag
                  v-else-if="
                    isBlacklist &&
                    field.name === 'expires_at' &&
                    !String(row.expires_at || '')
                  "
                  tone="neutral"
                  text="永久"
                />
                <span
                  v-else
                  class="cell"
                  :class="{
                    'balance-strong':
                      isOriginalMerchantList &&
                      field.name === 'available_minor',
                  }"
                  >{{ listCellValue(field, row) }}</span
                >
                <template
                  v-if="
                    resource === 'merchants' && field.name === 'available_minor'
                  "
                >
                  <small
                    v-if="
                      row.frozen_minor !== undefined &&
                      String(row.frozen_minor) !== '0'
                    "
                    class="merchant-balance-detail muted"
                    >冻结预留：{{
                      consoleValue("frozen_minor", row.frozen_minor)
                    }}</small
                  >
                  <small
                    v-if="
                      row.income_held_minor !== undefined &&
                      String(row.income_held_minor) !== '0'
                    "
                    class="merchant-balance-detail muted"
                    >待解冻收入：{{
                      consoleValue("income_held_minor", row.income_held_minor)
                    }}</small
                  >
                </template>
                <small
                  v-if="
                    resource === 'merchants' &&
                    field.name === 'created_at' &&
                    typeof row.url === 'string' &&
                    row.url
                  "
                  class="merchant-balance-detail"
                  >网站：{{ row.url }}</small
                >
                <template
                  v-if="hasDomainProjection && field.name === 'created_at'"
                >
                  <small
                    v-for="domain in approvedMerchantDomains(
                      row.approved_domains,
                    )"
                    :key="domain"
                    class="merchant-balance-detail muted"
                    >{{ domain }}</small
                  >
                </template>
              </td>
              <td v-if="resource === 'groups'">
                <p v-if="groupDirectoryRates(row).error" class="muted">
                  {{ groupDirectoryRates(row).error }}
                </p>
                <template v-else>
                  <small
                    v-for="rate in groupDirectoryRates(row).lines"
                    :key="rate.label"
                    class="merchant-balance-detail"
                    >{{ rate.label }}：{{ rate.value }}</small
                  >
                  <small class="merchant-balance-detail muted"
                    >继承商户通道配置</small
                  >
                </template>
              </td>
              <td>
                <div class="row-actions">
                  <RouterLink
                    v-if="
                      resource === 'groups' &&
                      client.area === 'admin' &&
                      groupUsersHref(row.id)
                    "
                    :to="groupUsersHref(row.id)!"
                    >用户</RouterLink
                  >
                  <button
                    v-if="canReadGroupStatistics"
                    class="link-button"
                    :disabled="
                      busy || loading || mode !== null || !!groupStatisticsID
                    "
                    @click="openGroupStatistics(row.id)"
                  >
                    统计
                  </button>
                  <button
                    v-if="rowPrimaryLabel"
                    type="button"
                    class="pa-btn pa-btn--primary"
                    :data-record-action="
                      resource === 'pay-rolls' ? 'channels' : undefined
                    "
                    :data-record-id="String(row.id)"
                    :disabled="busy || loading || !schema.permissions.update"
                    @click="
                      open(resource === 'pay-rolls' ? 'channels' : 'edit', row)
                    "
                  >
                    {{ rowPrimaryLabel }}</button
                  ><button
                    v-if="
                      resource !== 'groups' &&
                      resource !== 'merchants' &&
                      !hideDetailResources.has(resource)
                    "
                    class="link-button"
                    :disabled="busy"
                    @click="open('detail', row)"
                  >
                    详情</button
                  ><a
                    v-if="
                      schema.permissions.update &&
                      resource === 'onboarding-applications'
                    "
                    :href="`/${client.area}/applyments/form?id=${encodeURIComponent(String(row.id))}`"
                    >编辑</a
                  ><button
                    v-if="
                      schema.permissions.update &&
                      resource !== 'onboarding-applications'
                    "
                    class="link-button"
                    :class="{
                      'pa-btn pa-btn--primary': resource === 'merchants',
                    }"
                    :disabled="busy"
                    @click="open('edit', row)"
                  >
                    编辑</button
                  ><button
                    v-if="schema.permissions.delete"
                    class="link-button danger-text"
                    :disabled="busy || isDefaultGroup(row.id)"
                    :title="
                      isDefaultGroup(row.id)
                        ? '默认用户组不可删除，请通过编辑修改配置。'
                        : undefined
                    "
                    @click="open('delete', row)"
                  >
                    删除
                  </button>
                  <button
                    v-if="resource === 'merchants' && client.area === 'admin'"
                    type="button"
                    class="pa-btn pa-btn--danger"
                    disabled
                    title="商户不可删除：资金与订单账本不可破坏；请用编辑中的启用开关停用商户。"
                  >
                    删除
                  </button>
                  <button
                    v-if="canDelegate && resource !== 'merchants'"
                    class="link-button"
                    :disabled="busy"
                    @click="delegation = row"
                  >
                    代登录商户
                  </button>
                  <ActionDropdown
                    v-if="resource === 'merchants' && client.area === 'admin'"
                    :label="`商户关联记录 ${row.id}`"
                    :items="merchantRelatedLinks(row.id)"
                    trigger-text="更多"
                    :disabled="
                      busy ||
                      loading ||
                      mode !== null ||
                      !!merchantLogID ||
                      !!merchantStatisticsID ||
                      !!manualWithdrawalID ||
                      !!balanceAdjustID
                    "
                    @action="(action) => merchantRelatedAction(action, row.id)"
                  />
                </div>
              </td>
            </tr>
            <tr v-if="!items.length">
              <td
                :colspan="
                  listFields.length +
                  2 +
                  (resource === 'groups' ? 1 : 0) +
                  (canBatchDelete ? 1 : 0)
                "
                class="empty"
              >
                <KitEmpty text="暂无记录，可通过上方操作添加" />
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <KitPagination
        v-if="!entry"
        v-model:page="page"
        v-model:page-size="limit"
        :total="total"
        @change="load"
      />
      <BusinessNoticeEvents
        v-if="showBusinessNotices"
        :client="client"
        :own-session="ownSession"
        :admin-class="adminClass"
      />
    </section>
    <KitModal
      v-if="confirmBatch"
      title="批量删除确认"
      submit-text="删除"
      danger
      :loading="batchBusy"
      @close="confirmBatch = false"
      @submit="runBatchDelete"
    >
      <p>
        将删除所选
        {{ selectedIds.length }}
        条记录。此操作走各记录的可恢复删除流程，不会物理清除数据。
      </p>
    </KitModal>
    <KitModal
      v-if="confirmDomainStatus !== null"
      :title="confirmDomainStatus ? '批量通过确认' : '批量拒绝确认'"
      :submit-text="confirmDomainStatus ? '通过' : '拒绝'"
      :loading="batchBusy"
      @close="confirmDomainStatus = null"
      @submit="runBatchDomainStatus"
    >
      <p>
        将{{ confirmDomainStatus ? "通过" : "拒绝" }}所选
        {{ selectedIds.length }}
        条授权域名。此操作逐条走后台审核流程并记录审核事件。
      </p>
    </KitModal>
    <BlacklistAddDialog
      v-if="blacklistAddOpen && isBlacklist"
      :client="client"
      :kind-field="blacklistKindField"
      @close="blacklistAddOpen = false"
      @saved="onBlacklistAdded"
    />
  </template>
  <MerchantManualWithdrawal
    v-if="manualWithdrawalID && canManualWithdraw"
    :client="client"
    :merchant-i-d="manualWithdrawalID"
    @close="closeManualWithdrawal"
    @changed="manualWithdrawalChanged = true"
  />
  <MerchantBalanceAdjustDialog
    v-if="balanceAdjustID && canAdjustBalance"
    :client="client"
    :merchant-i-d="balanceAdjustID"
    @close="closeBalanceAdjust"
    @changed="balanceAdjustChanged = true"
  />
  <AccountLoginLogDialog
    v-if="merchantLogID && canReadMerchantLogs"
    :client="client"
    :own-session="ownSession === true"
    :merchant-i-d="merchantLogID"
    @close="closeMerchantLogs"
  />
  <PaymentStatisticsDialog
    v-if="merchantStatisticsID && canReadMerchantStatistics"
    :client="client"
    kind="merchant"
    :subject-id="merchantStatisticsID"
    :own-session="ownSession === true"
    @close="closeMerchantStatistics"
  />
  <GroupStatisticsDialog
    v-if="groupStatisticsID && canReadGroupStatistics"
    :client="client"
    :group-i-d="groupStatisticsID"
    :own-session="ownSession === true"
    @close="closeGroupStatistics"
  />
  <MerchantDelegationPanel
    v-if="delegation && canDelegate"
    :key="String(delegation.id)"
    :client="client"
    :merchant-id="String(delegation.id)"
    :name="String(delegation.name || delegation.id)"
    :can-configure-login="
      resource === 'merchants' && schema?.permissions.update === true
    "
    @close="delegation = null"
    @configure-login="configureDelegatedMerchantLogin"
  />
  <div v-if="entry" class="toolbar">
    <a :href="entry.returnPath">返回进件资料列表</a>
  </div>
  <div
    v-if="mode && schema"
    :class="
      entry
        ? 'resource-entry'
        : 'modal-backdrop' +
          (asDrawer ? ' drawer-backdrop' : '') +
          (isGroupEditor ? ' group-edit-backdrop' : '')
    "
    @keydown.esc="closeResourceDialog"
    @click.self="asDrawer && closeResourceDialog()"
    @keydown.tab="trapResourceFocus"
  >
    <section
      ref="formDialog"
      :class="
        entry
          ? 'panel resource-entry-form'
          : 'modal panel resource-modal' +
            (asDrawer ? ' modal-drawer' : '') +
            (resource === 'merchants' ? ' merchant-edit-modal' : '') +
            (isMerchantCreationForm ? ' merchant-create-modal' : '') +
            (isPayRollCustom ? ' pay-roll-modal' : '') +
            (isGroupEditor ? ' group-edit-modal' : '')
      "
      :style="asDrawer ? { '--drawer-w': drawerWidth } : undefined"
      :role="entry ? undefined : 'dialog'"
      :tabindex="entry ? undefined : -1"
      :aria-modal="entry ? undefined : true"
      :aria-label="
        isPayRollCustom
          ? payRollTitle
          : isMerchantCreationForm
            ? '添加商户'
            : resource === 'admin-users' && ['create', 'edit'].includes(mode)
              ? mode === 'create'
                ? '添加管理员'
                : '编辑管理员'
              : isGroupEditor
                ? groupEditorTitle
                : mode === 'create'
                  ? '新增' + schema.title
                  : mode === 'edit'
                    ? '编辑' + schema.title
                    : mode === 'delete'
                      ? '删除确认'
                      : schema.title + '详情'
      "
    >
      <div v-if="isGroupEditor" class="group-edit-header">
        <h2>{{ groupEditorTitle }}</h2>
        <button
          type="button"
          :disabled="editingBusy"
          aria-label="关闭用户组编辑"
          @click="closeResourceDialog"
        >
          ×
        </button>
      </div>
      <div v-else-if="!entry" class="resource-modal-head">
        <h2>
          <template v-if="isPayRollCustom">{{ payRollTitle }}</template
          ><template v-else-if="isMerchantCreationForm">添加商户</template
          ><template
            v-else-if="
              resource === 'admin-users' && ['create', 'edit'].includes(mode)
            "
            >{{ mode === "create" ? "添加管理员" : "编辑管理员" }}</template
          ><template v-else
            >{{
              mode === "create"
                ? "新增"
                : mode === "edit"
                  ? "编辑"
                  : mode === "delete"
                    ? "删除确认"
                    : ""
            }}{{ schema.title }}{{ mode === "detail" ? "详情" : "" }}</template
          >
        </h2>
        <button
          type="button"
          class="resource-modal-x"
          :disabled="editingBusy"
          :aria-label="mode === 'detail' ? '关闭详情' : '关闭'"
          @click="closeResourceDialog"
        >
          ×
        </button>
      </div>
      <p v-if="entry && schema.description" class="resource-description muted">
        {{ schema.description }}
      </p>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <template v-if="mode === 'detail'"
        ><dl class="record-detail">
          <template v-for="field in fields" :key="field.name"
            ><dt>
              {{
                resource === "merchants"
                  ? consoleColumnLabel(field.name, field.label)
                  : field.label
              }}
            </dt>
            <dd>
              {{
                resource === "merchants"
                  ? merchantFieldValue(field.name, selected[field.name])
                  : displayValue(selected[field.name])
              }}
            </dd></template
          >
        </dl>
        <CasePanel
          v-if="['complaints', 'onboarding-applications'].includes(resource)"
          :client="client"
          :resource="resource"
          :id="String(selected.id)"
          :initial-status="String(selected.status || '')"
          @updated="refreshCase"
        />
        <RuleExecutionPanel
          v-if="resource === 'profitsharing-rules'"
          :id="String(selected.id)"
          :client="client"
          :enabled="!!selected.enabled"
        />
        <CollectionCodePanel
          v-if="resource === 'collection-codes'"
          :client="client"
          :id="String(selected.id)"
        />
        <div class="modal-actions">
          <button @click="closeResourceDialog">关闭</button>
        </div></template
      ><template v-else-if="mode === 'delete'"
        ><p>确认删除编号 {{ selected.id }} 的记录？此操作会立即生效。</p>
        <div class="modal-actions">
          <button :disabled="editingBusy" @click="closeResourceDialog">
            取消</button
          ><button class="danger-button" :disabled="busy" @click="remove">
            {{ busy ? "正在删除…" : "确认删除" }}
          </button>
        </div></template
      >
      <template v-else-if="isPayRollCustom">
        <PayRollCreateForm
          v-if="mode === 'create'"
          :client="client"
          :disabled="busy || loading"
          @busy="payRollBusy = $event"
          @saved="payRollSaved"
          @recovery-required="payRollRecoveryRequired"
        />
        <PayRollChannelEditor
          v-else
          :client="client"
          :roll-i-d="String(selected.id)"
          :disabled="busy || loading"
          @busy="payRollBusy = $event"
          @saved="payRollSaved"
        />
        <div class="modal-actions">
          <button
            type="button"
            :disabled="editingBusy"
            @click="closeResourceDialog"
          >
            关闭
          </button>
        </div>
      </template>
      <form v-else @submit.prevent="save">
        <template v-if="resource === 'merchants' && !isMerchantCreationForm"
          ><p class="muted">
            后台登录账号和密码可同时留空，仅使用
            API。填写登录密码会设置或重置后台登录凭据；已有账号不支持改名。
          </p>
          <p v-if="mode === 'edit' && selected.login_username" class="muted">
            已绑定登录账号：{{
              selected.login_username
            }}。禁用商户会使其登录会话失效。
          </p></template
        >
        <p v-if="resource === 'admin-users' && mode === 'edit'" class="muted">
          管理员账号：{{
            selected.username
          }}。修改用户名后，该管理员需重新登录；动态验证码和资金密码继续有效。
        </p>
        <MerchantCreateFields
          v-if="isMerchantCreationForm"
          :client="client"
          :fields="formSchema!.fields"
          :values="values"
          :errors="fieldErrors"
          :disabled="
            editingBusy || !!unknownMerchantCreation || unknownResourceCreation
          "
          @change="(name, value) => (values[name] = value)"
          @settlement-validity="merchantCreateSetupIssue = $event"
        />
        <MerchantEditorFields
          v-else-if="resource === 'merchants'"
          :key="mode + ':' + String(selected.id || 'new')"
          :client="client"
          :fields="formSchema!.fields"
          :values="values"
          :errors="fieldErrors"
          :editing="mode === 'edit'"
          :disabled="editingBusy"
          :merchant-i-d="String(selected.id || '')"
          :can-manage-credentials="canManageMerchantCredentials"
          :signature-policy="selected.keytype"
          :recipient="selected.settlement_recipient"
          :can-edit-recipient="
            schema.fields.some((f) => f.name === 'settlement_recipient') &&
            schema.permissions.update
          "
          @change="(name, value) => (values[name] = value)"
          @recipient="editMerchantRecipient = !editMerchantRecipient"
          @credential-busy="credentialBusy"
          @credential-drafts-discard="discardCredentialDrafts"
          @signature-policy-observed="observeSignaturePolicy"
          @legacy-algorithm-discard="discardLegacyAlgorithm"
        />
        <GroupEditorFields
          v-else-if="resource === 'groups'"
          :key="mode + ':' + String(selected.id || 'new')"
          :fields="formSchema!.fields"
          :values="values"
          :errors="fieldErrors"
          :editing="mode === 'edit'"
          :disabled="busy || unknownResourceCreation"
          :initial-tab="groupSection"
          :return-focus="groupReturnFocus"
          :client="client"
          @change="(name, value) => (values[name] = value)"
        >
          <template
            v-if="mode === 'edit' && canManageMerchantCredentials"
            #other
          >
            <CoreRiskGroupPanel
              :key="String(selected.id)"
              :client="client"
              :group-i-d="String(selected.id)"
              :allowed="true"
              :disabled="busy"
              @busy="groupRiskBusy = $event"
            />
            <PaymentDefend206GroupPanel
              :key="'defend-' + String(selected.id)"
              :client="client"
              :group-i-d="String(selected.id)"
              :allowed="true"
              :disabled="busy"
              @busy="groupDefendBusy = $event"
            />
          </template>
        </GroupEditorFields>
        <InternalSharingRuleFields
          v-else-if="resource === 'profitsharing-rules'"
          :key="mode + ':' + String(selected.id || 'new')"
          :fields="formSchema!.fields"
          :values="values"
          :errors="fieldErrors"
          :editing="mode === 'edit'"
          :disabled="busy || unknownResourceCreation"
          @change="(name, value) => (values[name] = value)"
        />
        <ResourceFields
          v-else
          :disabled="unknownResourceCreation"
          class="form-rows"
          :fields="formSchema!.fields"
          :values="values"
          :errors="fieldErrors"
          :editing="mode === 'edit'"
          @change="(name, value) => (values[name] = value)"
        />
        <div class="modal-actions">
          <a v-if="entry && !editingBusy" :href="entry.returnPath">返回列表</a
          ><button
            v-if="!entry"
            type="button"
            :disabled="editingBusy"
            @click="closeResourceDialog"
          >
            {{ isMerchantCreationForm ? "关闭" : "取消" }}</button
          ><button
            v-if="unknownResourceCreation && !entry"
            type="button"
            :disabled="editingBusy"
            @click="readCreatedResourceList"
          >
            关闭并核对列表</button
          ><button
            v-if="unknownMerchantCreation"
            type="button"
            :disabled="editingBusy"
            @click="reconcileMerchantCreation"
          >
            核对商户账号</button
          ><button
            class="primary"
            :disabled="
              editingBusy ||
              !!unknownMerchantCreation ||
              unknownResourceCreation ||
              !!merchantCreateSetupIssue
            "
          >
            {{
              credentialsBusy
                ? "正在处理…"
                : busy
                  ? "正在保存…"
                  : isMerchantCreationForm
                    ? "确定添加"
                    : "保存"
            }}
          </button>
        </div>
      </form>
      <SettlementRecipientPanel
        v-if="
          resource === 'merchants' && mode === 'edit' && editMerchantRecipient
        "
        :key="String(selected.id)"
        :client="client"
        :merchant-i-d="String(selected.id)"
        :admin="true"
        :disabled="busy"
        @change="
          (value) => {
            if (value) selected.settlement_recipient = value;
          }
        "
      />
    </section>
  </div>
</template>
<style scoped>
.modal.resource-modal.pay-roll-modal:not(.modal-drawer) {
  width: min(860px, 100%);
  max-height: 92dvh;
  padding: 20px;
  overflow: auto;
}
.modal.resource-modal.merchant-edit-modal.merchant-create-modal:not(
    .modal-drawer
  ) {
  width: min(680px, 100%);
  max-height: 92dvh;
}
.resource-record-id {
  white-space: nowrap;
}
.merchant-balance-detail {
  display: block;
  margin-top: 4px;
  white-space: nowrap;
}
.group-edit-backdrop:not(.drawer-backdrop) {
  align-items: start;
  padding-top: 4dvh;
  padding-bottom: 4dvh;
}
/* Resource create/edit/detail open as a right drawer: the backdrop pins its
   child to the top-right edge with no padding, and the dialog fills the
   window height. Stated here so component order can't leave it centered. */
.modal-backdrop.drawer-backdrop {
  place-items: stretch end;
  padding: 0;
}
/* The scrim's single grid track cannot grow with wide drawer content, so the
   right-pinned drawer stays inside the window. (100vw would include a classic
   Windows scrollbar and put the drawer's right edge under it.) */
.modal-backdrop.drawer-backdrop {
  grid-template-columns: minmax(0, 1fr);
}
.modal-backdrop.drawer-backdrop > .modal.resource-modal.modal-drawer {
  width: min(100%, var(--drawer-w, 560px));
  max-width: 100%;
  height: 100dvh;
  max-height: none;
  border-width: 0 0 0 1px;
  border-radius: 0;
}
/* Keep wide inner content (the group channel table, a two-column form) from
   widening the drawer past --drawer-w; it scrolls inside the body instead. */
.modal.resource-modal.modal-drawer > form {
  min-width: 0;
}
/* .modal.resource-modal sets 480px in style.css, which loads after this
   file, so the wide editors need the same classes to keep their width. As a
   drawer the width comes from --drawer-w instead, so only size the centered
   fallback here. */
.modal.resource-modal.group-edit-modal:not(.modal-drawer) {
  width: min(100%, 1000px);
  max-height: 92dvh;
}
.modal.resource-modal.merchant-edit-modal:not(.modal-drawer) {
  width: min(100%, 900px);
}
/* Head of a drawer/dialog: title left, close × right, both sticky above the
   scrolling body (the shared .modal > h2:first-child sticky rule needs a
   single first child, so the head row carries it here). */
.group-edit-header,
.resource-modal-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}
.modal.resource-modal > .group-edit-header,
.modal.resource-modal > .resource-modal-head {
  position: sticky;
  top: calc(-1 * var(--space-5));
  z-index: 3;
  background: var(--bg-card);
}
.group-edit-header h2,
.resource-modal-head h2 {
  margin: 0;
}
.group-edit-header > button,
.resource-modal-x {
  font-size: 24px;
  line-height: 1;
  padding: 4px 12px;
  border: 0;
  background: transparent;
  color: var(--ink-muted, inherit);
}
.group-edit-header > button:hover:not(:disabled),
.resource-modal-x:hover:not(:disabled) {
  background: var(--bg-hover);
}
</style>
