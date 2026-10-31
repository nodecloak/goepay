<script setup lang="ts">
import { RouterLink, useRouter } from "vue-router";
import { readLinkedMerchantRoute } from "../list-filters";
import ShellIcon from "./ShellIcon.vue";
import {
  ref,
  computed,
  watch,
  onMounted,
  onBeforeUnmount,
  nextTick,
  inject,
} from "vue";
import {
  freshSettlementFilters,
  readSettlementRoute,
  settlementFiltersError,
  settlementFilterQuery,
} from "../settlement-filters";
import SettlementListFilters from "./SettlementListFilters.vue";
import KitPagination from "./KitPagination.vue";
import KitStatusTag from "./KitStatusTag.vue";
import ActionDropdown from "./ActionDropdown.vue";
import KitField from "./KitField.vue";
import KitSelect from "./KitSelect.vue";
import SettlementStatistics from "./SettlementStatistics.vue";
import { ApiClient, ApiError, validAmount } from "../api";
import {
  fundsPrincipal212,
  fundsScope212,
  FundsIntentStore212,
  fundsDefinitiveRefusal212,
  verifiedWorkflow212,
  type FundsIntent212,
} from "../funds-intent212";
import type { Area } from "../menu";
import { displayValue } from "../api";
import {
  readFundsAvailability,
  type FundsAvailability,
} from "../funds-availability";
import FundsAvailabilitySummary from "./FundsAvailabilitySummary.vue";
import KitHelpPopover from "./KitHelpPopover.vue";
import SettlementRecipientPanel from "./SettlementRecipientPanel.vue";
import SettlementPayoutPanel from "./SettlementPayoutPanel.vue";
import SettlementListTable from "./SettlementListTable.vue";
import InternalTransferListTable from "./InternalTransferListTable.vue";
import AdminInternalTransferListTable from "./AdminInternalTransferListTable.vue";
import { hasOriginalSettlementProjection } from "../settlement-list";
import { payoutFundingLabel, payoutStateLabel } from "../settlement-payout";
import {
  settlementSnapshotLabel,
  type SettlementRecipient,
} from "../settlement-recipient";
import {
  centsMoney,
  readSettlementPreview,
  type SettlementPreview,
} from "../settlement-terms";
const props = defineProps<{
  client: ApiClient;
  workflow: "settlements" | "internal-transfers" | "profitsharing";
  area: Area;
  application?: boolean;
  initialQuery?: Record<string, unknown>;
  selfID?: string;
  boundMerchantID?: string;
}>();
const emit = defineEmits<{ busy: [value: boolean]; submitted: [] }>();
const internalMerchantList = computed(
  () => props.workflow === "internal-transfers" && props.area === "merchant",
);
const internalAdminList = computed(
  () => props.workflow === "internal-transfers" && props.area === "admin",
);
const listNote = computed(() =>
  props.workflow === "settlements"
    ? "结算申请会独立预占申请总额；核准后仍需另行准备并明确发送钱包出款，审批通过不代表支付成功。缺失费用、资金归属或收款快照的历史记录不能出款；银行卡不适用本钱包流程。拒绝或已证明未发送的取消会释放对应冻结资金。"
    : props.workflow === "profitsharing"
      ? "站内分账申请由已支付订单和启用规则生成，核准后转入站内收款商户账户。生成申请时同样由服务端执行 T+0/T+1 可出账政策，本页面不手动创建分账或执行上游渠道分账。"
      : "站内转账仅在本平台商户账户之间转移余额。请核对商户编号和金额。",
);
const internalDraft = ref({ direction: "", status: "" });
const internalApplied = ref({ direction: "", status: "" });
function applyInternalFilters() {
  internalApplied.value = { ...internalDraft.value };
  page.value = 1;
  void load();
}
function resetInternalFilters() {
  internalDraft.value = { direction: "", status: "" };
  applyInternalFilters();
}
const adminInternalDraft = ref({
  merchant_id: "",
  recipient_id: "",
  status: "",
  from: "",
  to: "",
});
const adminInternalApplied = ref({ ...adminInternalDraft.value });
const adminInternalError = ref("");
const idOrEmpty = (v: string) => v === "" || /^[1-9]\d{0,18}$/.test(v);
function applyAdminInternalFilters() {
  if (
    !idOrEmpty(adminInternalDraft.value.merchant_id) ||
    !idOrEmpty(adminInternalDraft.value.recipient_id)
  ) {
    adminInternalError.value = "发起方/收款方商户号格式无效";
    return;
  }
  if (
    adminInternalDraft.value.from &&
    adminInternalDraft.value.to &&
    adminInternalDraft.value.from > adminInternalDraft.value.to
  ) {
    adminInternalError.value = "开始日期不能晚于结束日期";
    return;
  }
  adminInternalError.value = "";
  adminInternalApplied.value = { ...adminInternalDraft.value };
  page.value = 1;
  void load();
}
function resetAdminInternalFilters() {
  adminInternalDraft.value = {
    merchant_id: "",
    recipient_id: "",
    status: "",
    from: "",
    to: "",
  };
  adminInternalError.value = "";
  applyAdminInternalFilters();
}
const settlementRoute = readSettlementRoute(
  props.initialQuery || {},
  props.area,
  props.selfID || "",
);
const settlementDraft = ref({ ...settlementRoute.filters }),
  settlementApplied = ref({ ...settlementRoute.filters }),
  settlementRouteError = ref(settlementRoute.error),
  statisticsOpen = ref(false);
const router = useRouter();
let statisticsOpener: HTMLElement | undefined;
function openStatistics() {
  statisticsOpener =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : undefined;
  statisticsOpen.value = true;
}
function closeStatistics() {
  const opener = statisticsOpener;
  statisticsOpen.value = false;
  statisticsOpener = undefined;
  void nextTick(() => {
    if (opener?.isConnected) opener.focus();
  });
}
function applySettlementFilters() {
  const message = settlementFiltersError(settlementDraft.value);
  if (message) {
    error.value = message;
    return;
  }
  settlementApplied.value = { ...settlementDraft.value };
  settlementRouteError.value = "";
  statisticsOpen.value = false;
  page.value = 1;
  void load();
}
function resetSettlementFilters() {
  settlementDraft.value = freshSettlementFilters();
  settlementApplied.value = freshSettlementFilters();
  settlementRouteError.value = "";
  statisticsOpen.value = false;
  page.value = 1;
  if (props.initialQuery && Object.keys(props.initialQuery).length && router) {
    void router.replace("/" + props.area + "/settle/list");
  } else void load();
}
const routeContext =
  props.workflow === "settlements"
    ? {
        merchantID: settlementRoute.filters.merchant_id,
        page: settlementRoute.page,
        limit: settlementRoute.limit,
        error: settlementRoute.error,
      }
    : readLinkedMerchantRoute(
        props.initialQuery || {},
        props.area,
        props.selfID || "",
      );
const items = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  page = ref(routeContext.page),
  limit = ref(routeContext.limit),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  dialog = ref<"create" | "approve" | "reject" | null>(null),
  selected = ref<Record<string, unknown>>({}),
  checked = ref<string[]>([]),
  batchMode = ref(false);
const rejectReason = ref("");
watch(busy, (value) => emit("busy", value), { flush: "sync" });
const validBoundMerchant = computed(
  () =>
    props.boundMerchantID === undefined ||
    (/^[1-9]\d{0,18}$/.test(props.boundMerchantID) &&
      BigInt(props.boundMerchantID) <= 9223372036854775807n),
);
const settlementRecordsHref = computed(
  () =>
    "/" +
    props.area +
    "/settle/list" +
    (props.area === "admin" && props.boundMerchantID && validBoundMerchant.value
      ? "?uid=" + props.boundMerchantID
      : ""),
);
const payoutRequestId = ref("");
const form = ref({
  merchant_id: "",
  recipient_id: "",
  amount: "",
  external_no: "",
});
const principalContext212 = inject(fundsPrincipal212, undefined);
const pendingIntent212 = ref<FundsIntent212 | null>(null),
  restoringIntent212 = ref(false),
  intentStorageError212 = ref("");
let intentStore212: FundsIntentStore212 | null = null,
  intentEpoch212 = 0;
const intentMerchant212 = () =>
  props.area === "admin"
    ? form.value.merchant_id
    : props.selfID || principalContext212?.value?.id || "";
const intentNotice212 = computed(() =>
  pendingIntent212.value?.phase === "accepted"
    ? `原申请已登记（${pendingIntent212.value.record?.id}），审批或外部付款结果请以原记录为准。`
    : "上次申请结果尚未核实；原金额、收款对象/版本和请求号已锁定，请查询原申请或重试原请求。",
);
async function restoreIntent212() {
  const stamp = ++intentEpoch212,
    payer = intentMerchant212();
  intentStore212 = null;
  pendingIntent212.value = null;
  intentStorageError212.value = "";
  if (props.workflow === "profitsharing" || !/^[1-9]\d{0,18}$/.test(payer))
    return;
  const context =
    principalContext212?.value ||
    (props.selfID && props.client.csrfToken
      ? {
          area: props.area,
          id: props.selfID,
          issuer: "",
          csrf: props.client.csrfToken,
        }
      : null);
  if (!context) return;
  restoringIntent212.value = true;
  try {
    const scope = await fundsScope212(context);
    if (stamp !== intentEpoch212 || dialog.value !== "create") return;
    const store = new FundsIntentStore212(scope, props.workflow, payer),
      saved = store.read();
    intentStore212 = store;
    pendingIntent212.value = saved;
    if (saved)
      form.value = {
        merchant_id: props.area === "admin" ? payer : "",
        recipient_id: saved.body.recipient_id || "",
        amount: saved.body.amount,
        external_no: saved.body.external_no,
      };
  } catch (e) {
    if (stamp === intentEpoch212)
      intentStorageError212.value =
        e instanceof Error ? e.message : "无法保存原资金请求";
  } finally {
    if (stamp === intentEpoch212) restoringIntent212.value = false;
  }
}
watch(
  () => [
    principalContext212?.value,
    props.client,
    props.selfID,
    props.boundMerchantID,
  ],
  () => {
    intentEpoch212++;
    intentStore212 = null;
    pendingIntent212.value = null;
    restoringIntent212.value = false;
    intentStorageError212.value = "";
    if (dialog.value === "create") {
      dialog.value = null;
      form.value.amount = "";
      form.value.external_no = "";
    }
  },
  { flush: "sync" },
);
let controller: AbortController | undefined,
  fundsController: AbortController | undefined,
  fundsEpoch = 0;
const funds = ref<FundsAvailability | null>(null),
  fundsLoading = ref(false),
  fundsError = ref(""),
  fundsMerchant = ref("");
const settlementPreview = ref<SettlementPreview | null>(null);
const settlementRecipient = ref<SettlementRecipient | null>(null);
const hasRecipient = computed(
  () =>
    props.workflow !== "settlements" ||
    settlementRecipient.value?.state === "configured",
);
function recipientChanged(v: SettlementRecipient | null) {
  settlementRecipient.value = v;
  settlementPreview.value = null;
  previewBinding = "";
}
let previewBinding = "";
watch(
  form,
  () => {
    settlementPreview.value = null;
    previewBinding = "";
  },
  { deep: true, flush: "sync" },
);
const currentBinding = () =>
  JSON.stringify([form.value, settlementRecipient.value?.config_id || ""]);
const hasCurrentFunds = computed(
  () =>
    !!funds.value &&
    !fundsLoading.value &&
    !fundsError.value &&
    (props.area === "merchant" ||
      fundsMerchant.value === form.value.merchant_id),
);
function invalidateFunds() {
  settlementPreview.value = null;
  previewBinding = "";
  fundsEpoch++;
  fundsController?.abort();
  funds.value = null;
  fundsLoading.value = false;
  fundsError.value = "";
  fundsMerchant.value = "";
  livePreview.value = null;
  liveError.value = "";
}
// Pro settle/apply previews 手续费 / 实际到账 inline as the amount is typed, and
// a 全部 button fills the full withdrawable amount. The preview reads the same
// read-only server quote (/workflows/settlement-policy) the submit step re-verifies.
const livePreview = ref<SettlementPreview | null>(null),
  liveLoading = ref(false),
  liveError = ref("");
let liveEpoch = 0,
  liveController: AbortController | undefined,
  liveTimer: ReturnType<typeof setTimeout> | undefined;
function fillAll() {
  if (!funds.value || !hasCurrentFunds.value) return;
  form.value.amount = funds.value.available_money;
}
function scheduleLivePreview() {
  if (props.workflow !== "settlements") return;
  liveEpoch++;
  liveController?.abort();
  livePreview.value = null;
  liveError.value = "";
  if (liveTimer) clearTimeout(liveTimer);
  const amount = form.value.amount;
  if (!validAmount(amount) || !hasRecipient.value || !hasCurrentFunds.value)
    return;
  const epoch = liveEpoch,
    merchant = form.value.merchant_id;
  liveTimer = setTimeout(async () => {
    const q = new URLSearchParams({ amount });
    if (props.area === "admin") q.set("merchant_id", merchant);
    liveController = new AbortController();
    liveLoading.value = true;
    try {
      const raw = await props.client.request(
        "/workflows/settlement-policy?" + q,
        { signal: liveController.signal },
      );
      if (epoch !== liveEpoch) return;
      livePreview.value = readSettlementPreview(raw, amount);
    } catch (e) {
      if (epoch === liveEpoch)
        liveError.value =
          e instanceof Error ? e.message : "费用预览读取失败，请核对金额";
    } finally {
      if (epoch === liveEpoch) liveLoading.value = false;
    }
  }, 350);
}
watch(
  () => [
    form.value.amount,
    form.value.merchant_id,
    hasRecipient.value,
    funds.value,
  ],
  scheduleLivePreview,
  { flush: "post" },
);
watch(
  () => form.value.merchant_id,
  () => {
    invalidateFunds();
    if (props.area === "admin" && dialog.value === "create")
      void restoreIntent212();
  },
  { flush: "sync" },
);
watch(dialog, (value) => {
  if (value !== "create") invalidateFunds();
});
async function loadFunds() {
  const merchant = form.value.merchant_id;
  invalidateFunds();
  if (props.area === "admin" && !/^[1-9]\d*$/.test(merchant)) {
    fundsError.value = "请输入有效的付款商户 ID 后查询资金政策";
    return;
  }
  const epoch = fundsEpoch;
  fundsLoading.value = true;
  fundsController = new AbortController();
  try {
    const data = await props.client.request(
      "/workflows/funds-availability" +
        (props.area === "admin"
          ? "?merchant_id=" + encodeURIComponent(merchant)
          : ""),
      { signal: fundsController.signal },
    );
    if (
      epoch === fundsEpoch &&
      dialog.value === "create" &&
      (props.area === "merchant" || merchant === form.value.merchant_id)
    ) {
      funds.value = readFundsAvailability(data);
      fundsMerchant.value = merchant;
    }
  } catch (e) {
    if (epoch === fundsEpoch)
      fundsError.value =
        e instanceof Error ? e.message : "资金政策读取失败，请重试";
  } finally {
    if (epoch === fundsEpoch) fundsLoading.value = false;
  }
}
const endpoint = "/workflows/" + props.workflow;
const columns = [
  { key: "id", label: "编号" },
  { key: "external_no", label: "请求号" },
  { key: "merchant_id", label: "商户 ID" },
  ...(props.workflow !== "settlements"
    ? [{ key: "recipient_id", label: "收款商户 ID" }]
    : []),
  { key: "amount", label: "金额（元）" },
  ...(props.workflow === "settlements"
    ? [
        { key: "settlement_fee_money", label: "申请手续费（元）" },
        { key: "settlement_net_money", label: "预计净额（元）" },
        { key: "settlement_recipient_label", label: "申请收款快照" },
        { key: "settlement_funding_label", label: "申请资金归属" },
        { key: "settlement_payout_label", label: "独立出款结果" },
      ]
    : []),
  { key: "status", label: "状态" },
  { key: "created_at", label: "申请时间" },
];
async function load() {
  checked.value = [];
  items.value = [];
  total.value = 0;
  const routeError =
    props.workflow === "settlements"
      ? settlementRouteError.value
      : routeContext.error;
  if (routeError) {
    error.value = routeError;
    items.value = [];
    total.value = 0;
    loading.value = false;
    return;
  }
  controller?.abort();
  controller = new AbortController();
  const c = controller;
  loading.value = true;
  error.value = "";
  try {
    const data = await props.client.request<{
      items: Record<string, unknown>[];
      total: number;
    }>(
      endpoint +
        "?" +
        (props.workflow === "settlements"
          ? settlementFilterQuery(
              settlementApplied.value,
              props.area,
              page.value,
              limit.value,
            )
          : new URLSearchParams({
              page: String(page.value),
              limit: String(limit.value),
              ...(internalMerchantList.value
                ? {
                    ...(internalApplied.value.direction
                      ? { direction: internalApplied.value.direction }
                      : {}),
                    ...(internalApplied.value.status
                      ? { status: internalApplied.value.status }
                      : {}),
                  }
                : {}),
              ...(internalAdminList.value
                ? {
                    ...(adminInternalApplied.value.merchant_id
                      ? { merchant_id: adminInternalApplied.value.merchant_id }
                      : {}),
                    ...(adminInternalApplied.value.recipient_id
                      ? {
                          recipient_id: adminInternalApplied.value.recipient_id,
                        }
                      : {}),
                    ...(adminInternalApplied.value.status
                      ? { status: adminInternalApplied.value.status }
                      : {}),
                    ...(adminInternalApplied.value.from
                      ? { from: adminInternalApplied.value.from }
                      : {}),
                    ...(adminInternalApplied.value.to
                      ? { to: adminInternalApplied.value.to }
                      : {}),
                  }
                : {}),
              ...(props.area === "admin" && routeContext.merchantID
                ? { merchant_id: routeContext.merchantID }
                : {}),
            })),
      { signal: c.signal },
    );
    if (!c.signal.aborted) {
      items.value = data.items.map((row) => {
        if (props.workflow !== "settlements") return row;
        const terms = row.settlement as Record<string, unknown> | null;
        return {
          ...row,
          settlement_fee_money: terms
            ? centsMoney(terms.fee_amount)
            : "历史未记录",
          settlement_net_money: terms
            ? centsMoney(terms.net_amount)
            : "历史未记录",
          settlement_recipient_label: settlementSnapshotLabel(
            row.settlement_recipient,
          ),
          settlement_funding_label: payoutFundingLabel(row.settlement_funding),
          settlement_payout_label:
            row.payout && typeof row.payout === "object"
              ? payoutStateLabel(
                  String((row.payout as { state?: unknown }).state),
                )
              : "尚未核对出款绑定",
        };
      });
      total.value = data.total;
      checked.value = [];
    }
  } catch (e) {
    if (!c.signal.aborted)
      error.value = e instanceof Error ? e.message : "读取失败，请重试";
  } finally {
    if (!c.signal.aborted) loading.value = false;
  }
}
function openCreate() {
  if (props.area === "admin" && !validBoundMerchant.value) {
    error.value = "所选商户号无效，请关闭后重新选择";
    return;
  }
  settlementRecipient.value = null;
  form.value = {
    merchant_id: props.area === "admin" ? props.boundMerchantID || "" : "",
    recipient_id: "",
    amount: "",
    external_no: crypto.randomUUID(),
  };
  error.value = "";
  dialog.value = "create";
  invalidateFunds();
  if (props.area === "merchant") void loadFunds();
  void restoreIntent212();
}
async function submit() {
  if (
    busy.value ||
    restoringIntent212.value ||
    pendingIntent212.value ||
    intentStorageError212.value
  )
    return;
  if (
    props.area === "admin" &&
    props.boundMerchantID !== undefined &&
    (!validBoundMerchant.value ||
      form.value.merchant_id !== props.boundMerchantID)
  ) {
    error.value = "提现商户已变化，请关闭后重新选择";
    return;
  }
  if (!hasRecipient.value) {
    error.value = "请先核对并配置当前商户的收款版本";
    return;
  }
  if (!hasCurrentFunds.value) {
    fundsError.value = "请先查询当前付款商户的可出账金额";
    return;
  }
  if (!validAmount(form.value.amount)) {
    error.value = "请输入大于 0 且最多两位小数的金额";
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    if (
      props.workflow === "settlements" &&
      (!settlementPreview.value || previewBinding !== currentBinding())
    ) {
      const binding = currentBinding(),
        q = new URLSearchParams({ amount: form.value.amount });
      if (props.area === "admin") q.set("merchant_id", form.value.merchant_id);
      const raw = await props.client.request(
        "/workflows/settlement-policy?" + q,
      );
      if (dialog.value !== "create" || currentBinding() !== binding) return;
      settlementPreview.value = readSettlementPreview(raw, form.value.amount);
      previewBinding = binding;
      return;
    }
    const body: Record<string, string> = {
      amount: form.value.amount,
      external_no: form.value.external_no,
    };
    if (props.area === "admin") body.merchant_id = form.value.merchant_id;
    if (props.workflow === "internal-transfers")
      body.recipient_id = form.value.recipient_id;
    if (props.workflow === "settlements" && settlementPreview.value) {
      body.expected_fee_cents = settlementPreview.value.fee_cents;
      body.expected_net_cents = settlementPreview.value.net_cents;
      body.recipient_config_id = settlementRecipient.value!.config_id;
    }
    if (!intentStore212)
      throw new Error("本人会话或本地保存状态尚未核实，尚未发送资金请求");
    await sendOriginalIntent212({ target: endpoint, body });
  } catch (e) {
    error.value = e instanceof Error ? e.message : "提交失败";
    if (
      props.workflow === "settlements" &&
      e instanceof ApiError &&
      e.status === 409
    ) {
      settlementPreview.value = null;
      previewBinding = "";
    }
  } finally {
    busy.value = false;
  }
}
async function sendOriginalIntent212(
  original: Pick<FundsIntent212, "target" | "body">,
  retry = false,
) {
  const stamp = intentEpoch212,
    store = intentStore212,
    currentClient = props.client;
  if (!store) throw new Error("原申请尚未恢复，不能发送");
  let dispatched = false;
  let saved = retry ? (original as FundsIntent212) : null;
  try {
    const result = await currentClient.request<Record<string, unknown>>(
      original.target,
      {
        method: "POST",
        body: original.body,
        beforeDispatch: () => {
          if (stamp !== intentEpoch212 || currentClient !== props.client)
            throw new Error("本人会话或原申请已变化，尚未发送");
          if (!retry) {
            saved = store.begin(original.target, { ...original.body });
            pendingIntent212.value = saved;
          }
          dispatched = true;
        },
      },
    );
    if (stamp !== intentEpoch212 || currentClient !== props.client) return;
    if (!saved) throw new Error("原申请发送状态无法核对");
    const record = verifiedWorkflow212(result, saved, saved.object);
    pendingIntent212.value = store.accept(record);
    // A complete response confirms this application was registered (not paid).
    // Ordinary successful submissions need no recovery acknowledgement later.
    if (!retry) {
      store.clear();
      pendingIntent212.value = null;
    }
    notice.value = "申请已提交并登记，请根据原记录状态核实处理结果";
    dialog.value = null;
    emit("submitted");
    if (!props.application) await load();
  } catch (e) {
    if (stamp !== intentEpoch212 || currentClient !== props.client) return;
    if (!dispatched) {
      error.value =
        (retry
          ? "本次核对或授权未完成，尚未重发；之前的原申请结果仍待核实，已保留原请求。"
          : "申请尚未发送，请修正或完成核对后提交。") +
        (e instanceof Error ? e.message : "请求未发送");
    } else if (!retry && saved && fundsDefinitiveRefusal212(e, saved)) {
      store.clear();
      pendingIntent212.value = null;
      error.value =
        "本次申请已明确拒绝，未重新发送。" +
        (e instanceof Error ? e.message : "请求拒绝");
      settlementPreview.value = null;
      previewBinding = "";
    } else
      error.value =
        "申请结果尚未核实。已保存原金额、收款对象/版本和请求号，请查询或重试原请求。" +
        (e instanceof Error ? e.message : "请求失败");
  }
}
async function retryOriginalIntent212() {
  if (
    busy.value ||
    !pendingIntent212.value ||
    pendingIntent212.value.phase !== "unknown"
  )
    return;
  busy.value = true;
  error.value = "";
  try {
    await sendOriginalIntent212(pendingIntent212.value, true);
  } catch (e) {
    error.value = e instanceof Error ? e.message : "原请求重试未确认";
  } finally {
    busy.value = false;
  }
}
async function queryOriginalIntent212() {
  const saved = pendingIntent212.value,
    store = intentStore212,
    stamp = intentEpoch212;
  if (busy.value || !saved || !store) return;
  busy.value = true;
  error.value = "";
  try {
    const q = new URLSearchParams({ external_no: saved.body.external_no });
    if (props.area === "admin") q.set("merchant_id", saved.object);
    const data = await props.client.request(`${saved.target}/lookup?${q}`);
    if (stamp !== intentEpoch212) return;
    pendingIntent212.value = store.accept(
      verifiedWorkflow212(data, saved, saved.object),
    );
  } catch (e) {
    if (stamp === intentEpoch212)
      error.value =
        e instanceof ApiError && e.status === 404
          ? "尚未查到原申请，不代表未受理；继续保留原请求号，不要重新创建。"
          : e instanceof Error
            ? e.message
            : "原申请查询失败";
  } finally {
    if (stamp === intentEpoch212) busy.value = false;
  }
}
async function finishOriginalIntent212() {
  if (
    busy.value ||
    !intentStore212 ||
    pendingIntent212.value?.phase !== "accepted" ||
    !pendingIntent212.value.record ||
    !["pending", "approved", "rejected"].includes(
      pendingIntent212.value.record.status,
    )
  )
    return;
  try {
    intentStore212.clear();
    pendingIntent212.value = null;
    openCreate();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "原申请状态未能保存";
  }
}
function openDecision(
  row: Record<string, unknown>,
  action: "approve" | "reject",
) {
  selected.value = row;
  batchMode.value = false;
  dialog.value = action;
  rejectReason.value = "";
  error.value = "";
}
async function decide() {
  const action = dialog.value;
  if (action !== "approve" && action !== "reject") return;
  busy.value = true;
  error.value = "";
  const ids = batchMode.value
    ? [...checked.value]
    : [String(selected.value.id)];
  // A 拒绝原因 is only carried on a single admin internal-transfer rejection; it
  // is an optional, trimmed note stored with the review's immutable event.
  const reason =
    action === "reject" &&
    !batchMode.value &&
    props.workflow === "internal-transfers"
      ? rejectReason.value.trim()
      : "";
  let completed = 0;
  try {
    for (const id of ids) {
      await props.client.request(
        `${endpoint}/${encodeURIComponent(id)}/${action}`,
        { method: "POST", body: reason ? { reason } : {} },
      );
      completed++;
    }
    notice.value = `已${action === "approve" ? "核准" : "拒绝"} ${completed} 条账务申请`;
    dialog.value = null;
    await load();
  } catch (e) {
    const failure = `已处理 ${completed} 条，后续操作停止。${e instanceof Error ? e.message : "请重新核实记录"}`;
    if (completed) {
      dialog.value = null;
      await load();
    }
    error.value = failure;
  } finally {
    busy.value = false;
  }
}
function check(id: string, enabled: boolean) {
  checked.value = enabled
    ? [...checked.value, id]
    : checked.value.filter((x) => x !== id);
}
// Pro settle/list keeps all batch actions in one 批量修改 dropdown. GOEPAY's two
// admin batch actions (核准所选 / 生成结算批次) collapse into one 批量操作 dropdown;
// picking one opens its confirm for the current selection.
const batchHref = computed(() =>
  checked.value.length
    ? "/admin/settle/batch?request_ids=" +
      encodeURIComponent(checked.value.join(","))
    : "",
);
const batchMenuItems = computed(() => [
  { id: "approve", label: "核准所选 " + checked.value.length + " 条申请" },
  {
    id: "batch",
    label: "生成结算批次（" + checked.value.length + " 条）",
    href: batchHref.value || undefined,
  },
]);
function pickBatch(id: string) {
  if (!checked.value.length || busy.value || loading.value) return;
  if (id === "approve") {
    batchMode.value = true;
    dialog.value = "approve";
  }
  // id === "batch" navigates via the menu item href.
}
function workflowTone(
  status: string,
): "success" | "warning" | "danger" | "info" | "neutral" {
  return (
    (
      {
        pending: "warning",
        approved: "success",
        succeeded: "success",
        paid: "success",
        rejected: "danger",
        failed: "danger",
      } as Record<string, "success" | "warning" | "danger" | "info" | "neutral">
    )[status] ?? "neutral"
  );
}
onMounted(() => {
  if (props.application && props.workflow === "settlements") openCreate();
  else void load();
});
onBeforeUnmount(() => {
  controller?.abort();
  invalidateFunds();
});
</script>
<template>
  <template v-if="!application || !dialog">
    <div class="list-page">
      <form
        v-if="internalMerchantList && !application"
        class="toolbar list-filter-card panel internal-transfer-filters"
        aria-label="站内转账筛选"
        @submit.prevent="applyInternalFilters"
      >
        <div class="pa-filter-fields list-filters-grid">
          <KitField label="方向">
            <KitSelect
              v-model="internalDraft.direction"
              :options="[
                { value: '', label: '全部方向' },
                { value: 'out', label: '转出' },
                { value: 'in', label: '转入' },
              ]"
              :disabled="loading || busy"
              aria-label="方向"
            />
          </KitField>
          <KitField label="状态">
            <KitSelect
              v-model="internalDraft.status"
              :options="[
                { value: '', label: '全部状态' },
                { value: 'pending', label: '待审核' },
                { value: 'approved', label: '成功' },
                { value: 'rejected', label: '已拒绝' },
              ]"
              :disabled="loading || busy"
              aria-label="状态"
            />
          </KitField>
        </div>
        <div class="pa-filter-actions">
          <button
            type="button"
            class="pa-btn"
            data-action="reset-internal-transfers"
            :disabled="loading || busy"
            @click="resetInternalFilters"
          >
            重置
          </button>
          <button
            type="submit"
            class="pa-btn pa-btn--primary"
            :disabled="loading || busy"
          >
            查询
          </button>
        </div>
      </form>
      <form
        v-if="internalAdminList && !application"
        class="toolbar list-filter-card panel internal-transfer-filters"
        aria-label="站内转账筛选"
        @submit.prevent="applyAdminInternalFilters"
      >
        <div class="pa-filter-fields list-filters-grid">
          <KitField label="发起方商户号">
            <input
              v-model="adminInternalDraft.merchant_id"
              inputmode="numeric"
              placeholder="发起方商户号"
              aria-label="发起方商户号"
            />
          </KitField>
          <KitField label="收款方商户号">
            <input
              v-model="adminInternalDraft.recipient_id"
              inputmode="numeric"
              placeholder="收款方商户号"
              aria-label="收款方商户号"
            />
          </KitField>
          <KitField label="状态">
            <KitSelect
              v-model="adminInternalDraft.status"
              :options="[
                { value: '', label: '全部状态' },
                { value: 'pending', label: '待审核' },
                { value: 'approved', label: '成功' },
                { value: 'rejected', label: '已拒绝' },
              ]"
              :disabled="loading || busy"
              aria-label="状态"
            />
          </KitField>
          <KitField label="开始日期">
            <input
              v-model="adminInternalDraft.from"
              type="date"
              aria-label="开始日期"
            />
          </KitField>
          <KitField label="结束日期">
            <input
              v-model="adminInternalDraft.to"
              type="date"
              aria-label="结束日期"
            />
          </KitField>
        </div>
        <p v-if="adminInternalError" class="alert error" role="alert">
          {{ adminInternalError }}
        </p>
        <div class="pa-filter-actions">
          <button
            type="button"
            class="pa-btn"
            data-action="reset-admin-internal-transfers"
            :disabled="loading || busy"
            @click="resetAdminInternalFilters"
          >
            重置
          </button>
          <button
            type="submit"
            class="pa-btn pa-btn--primary"
            :disabled="loading || busy"
          >
            查询
          </button>
        </div>
      </form>
      <SettlementListFilters
        v-if="workflow === 'settlements' && !application"
        v-model="settlementDraft"
        :area="area"
        :loading="loading || busy"
        @submit="applySettlementFilters"
        @reset="resetSettlementFilters"
      />
      <p
        v-if="
          area === 'admin' &&
          (workflow === 'settlements'
            ? settlementApplied.merchant_id && !settlementRouteError
            : routeContext.merchantID && !routeContext.error)
        "
        class="resource-description"
      >
        商户号：{{
          workflow === "settlements"
            ? settlementApplied.merchant_id
            : routeContext.merchantID
        }}
      </p>
      <RouterLink
        v-if="
          workflow === 'settlements' &&
          (routeContext.merchantID || routeContext.error)
        "
        :to="`/${area}/settle/list`"
        >清空筛选</RouterLink
      >
      <div v-if="error && !dialog" class="alert error" role="alert">
        {{ error }}
      </div>
      <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
      <section class="panel records">
        <div class="table-tools">
          <button
            v-if="workflow !== 'profitsharing'"
            type="button"
            class="primary"
            :disabled="busy"
            @click="openCreate"
          >
            <ShellIcon name="plus" />{{
              workflow === "settlements" ? "申请结算" : "发起站内转账"
            }}
          </button>
          <button
            v-if="workflow === 'settlements' && !application"
            type="button"
            class="soft-button"
            :disabled="loading || !!settlementRouteError"
            @click="openStatistics"
          >
            结算统计
          </button>
          <div class="table-tools-end">
            <ActionDropdown
              v-if="
                area === 'admin' && workflow === 'settlements' && !application
              "
              label="批量操作"
              trigger-text="批量操作"
              :items="batchMenuItems"
              :disabled="!checked.length || busy || loading"
              @action="pickBatch"
            />
            <button v-if="!application" :disabled="loading" @click="load">
              <ShellIcon name="refresh" />刷新
            </button>
            <KitHelpPopover v-if="!application" :text="listNote" />
          </div>
        </div>
        <div v-if="loading" class="loading-row" role="status">
          {{
            internalMerchantList || internalAdminList
              ? "正在加载站内转账…"
              : "正在加载申请…"
          }}
        </div>
        <InternalTransferListTable
          v-else-if="internalMerchantList && !application"
          :items="items"
        />
        <AdminInternalTransferListTable
          v-else-if="internalAdminList && !application"
          :items="items"
          :busy="busy"
          @decision="openDecision"
        />
        <SettlementListTable
          v-else-if="
            !application &&
            workflow === 'settlements' &&
            hasOriginalSettlementProjection(items)
          "
          :items="items"
          :area="area"
          :checked="checked"
          :busy="busy"
          :client="client"
          @check="check"
          @decision="openDecision"
          @payout="(id) => (payoutRequestId = id)"
        />
        <div v-else-if="!application" class="table-scroll">
          <table>
            <thead>
              <tr>
                <th v-if="area === 'admin' && workflow === 'settlements'">
                  选择
                </th>
                <th v-for="column in columns" :key="column.key">
                  {{ column.label }}
                </th>
                <th v-if="area === 'admin' || workflow === 'settlements'">
                  操作
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in items" :key="String(row.id)">
                <td v-if="area === 'admin' && workflow === 'settlements'">
                  <input
                    type="checkbox"
                    :aria-label="'选择申请 ' + row.id"
                    :disabled="row.status !== 'pending'"
                    :checked="checked.includes(String(row.id))"
                    @change="
                      check(
                        String(row.id),
                        ($event.target as HTMLInputElement).checked,
                      )
                    "
                  />
                </td>
                <td v-for="column in columns" :key="column.key">
                  <KitStatusTag
                    v-if="column.key === 'status'"
                    :text="displayValue(row.status)"
                    :tone="workflowTone(String(row.status))"
                  />
                  <template v-else>{{
                    displayValue(row[column.key])
                  }}</template>
                </td>
                <td v-if="area === 'admin' || workflow === 'settlements'">
                  <div
                    v-if="area === 'admin' && row.status === 'pending'"
                    class="row-actions"
                  >
                    <button
                      class="link-button"
                      @click="openDecision(row, 'approve')"
                    >
                      核准</button
                    ><button
                      class="link-button danger-text"
                      @click="openDecision(row, 'reject')"
                    >
                      拒绝
                    </button>
                  </div>
                  <button
                    v-else-if="
                      workflow === 'settlements' && row.status === 'approved'
                    "
                    class="link-button"
                    @click="payoutRequestId = String(row.id)"
                  >
                    核对结算出款
                  </button>
                  <span v-else class="muted">已处理</span>
                </td>
              </tr>
              <tr v-if="!items.length">
                <td class="empty" :colspan="columns.length + 2">暂无申请</td>
              </tr>
            </tbody>
          </table>
        </div>
        <KitPagination
          v-if="!application"
          v-model:page="page"
          v-model:page-size="limit"
          :total="total"
          @change="load"
        />
      </section>
    </div>
  </template>
  <SettlementStatistics
    v-if="statisticsOpen"
    :client="client"
    :query="settlementFilterQuery(settlementApplied, area).toString()"
    @close="closeStatistics"
  />
  <SettlementPayoutPanel
    v-if="workflow === 'settlements' && payoutRequestId"
    :key="payoutRequestId"
    :client="client"
    :area="area"
    :request-id="payoutRequestId"
    @close="payoutRequestId = ''"
    @changed="load"
  />
  <div v-if="application" class="toolbar">
    <a :href="settlementRecordsHref">提现记录</a>
    <p v-if="notice" class="muted">业务请求号：{{ form.external_no }}</p>
  </div>
  <div
    v-if="dialog"
    :class="
      application
        ? 'workflow-application'
        : 'modal-backdrop' + (dialog === 'create' ? ' drawer-backdrop' : '')
    "
  >
    <section
      :class="
        application
          ? 'panel workflow-application-form'
          : 'modal panel' + (dialog === 'create' ? ' modal-drawer' : '')
      "
      :style="
        !application && dialog === 'create'
          ? { '--drawer-w': '560px' }
          : undefined
      "
      :role="application ? undefined : 'dialog'"
      :aria-modal="application ? undefined : true"
      :aria-label="
        application
          ? '申请提现'
          : dialog === 'create'
            ? '新增账务申请'
            : '审批确认'
      "
    >
      <div v-if="!application" class="workflow-modal-head">
        <h2>
          {{
            dialog === "create"
              ? workflow === "settlements"
                ? application
                  ? "申请提现"
                  : "申请结算"
                : "发起站内转账"
              : dialog === "approve"
                ? "核准账务申请"
                : "拒绝账务申请"
          }}
        </h2>
        <button
          type="button"
          class="workflow-modal-x"
          :disabled="busy"
          aria-label="关闭"
          @click="dialog = null"
        >
          ×
        </button>
      </div>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <form v-if="dialog === 'create'" @submit.prevent="submit">
        <p v-if="restoringIntent212" role="status">正在恢复本人原资金请求…</p>
        <p v-if="intentStorageError212" class="alert error" role="alert">
          {{ intentStorageError212 }}
        </p>
        <div v-if="pendingIntent212" class="alert" role="status">
          <p>{{ intentNotice212 }}</p>
          <p v-if="pendingIntent212.body.recipient_config_id">
            原收款版本：{{
              pendingIntent212.body.recipient_config_id
            }}；重试仅使用此版本，不采用后来修改的收款资料。
          </p>
          <div class="toolbar-actions">
            <button
              type="button"
              :disabled="busy"
              @click="queryOriginalIntent212"
            >
              查询原申请
            </button>
            <button
              v-if="pendingIntent212.phase === 'unknown'"
              type="button"
              :disabled="busy"
              @click="retryOriginalIntent212"
            >
              重试原申请请求
            </button>
            <button
              v-if="pendingIntent212.phase === 'accepted'"
              type="button"
              :disabled="busy"
              @click="finishOriginalIntent212"
            >
              已核对记录，开始新申请
            </button>
          </div>
        </div>
        <label v-if="area === 'admin'"
          >付款商户 ID<input
            v-model="form.merchant_id"
            :readonly="
              boundMerchantID !== undefined ||
              !!pendingIntent212 ||
              busy ||
              restoringIntent212
            "
            required
            inputmode="numeric" /></label
        ><button
          type="button"
          :disabled="fundsLoading || busy"
          @click="loadFunds"
        >
          {{ area === "admin" ? "查询可出账金额" : "刷新可出账金额" }}
        </button>
        <p v-if="fundsLoading" role="status" class="muted">
          正在查询资金政策与可出账金额…
        </p>
        <p v-if="fundsError" role="alert" class="alert error">
          {{ fundsError }}
        </p>
        <FundsAvailabilitySummary
          v-if="funds && hasCurrentFunds"
          :funds="funds"
        />
        <p v-else-if="!fundsLoading && !fundsError" class="muted">
          请先查询付款商户的资金政策，未查询时不能提交。
        </p>
        <label v-if="workflow === 'internal-transfers'"
          >收款商户 ID<input
            v-model="form.recipient_id"
            :readonly="!!pendingIntent212 || busy || restoringIntent212"
            required
            inputmode="numeric"
        /></label>
        <SettlementRecipientPanel
          v-if="workflow === 'settlements'"
          :client="client"
          :admin="area === 'admin'"
          :merchant-i-d="form.merchant_id"
          :disabled="busy || !!pendingIntent212 || restoringIntent212"
          @change="recipientChanged"
        />
        <label
          >金额（元）<span class="settle-amount-row"
            ><input
              v-model="form.amount"
              :readonly="!!pendingIntent212 || busy || restoringIntent212"
              required
              inputmode="decimal"
              aria-label="金额（元）"
            /><button
              v-if="workflow === 'settlements'"
              type="button"
              class="settle-all-btn"
              :disabled="
                busy ||
                !hasCurrentFunds ||
                !!pendingIntent212 ||
                restoringIntent212
              "
              @click="fillAll"
            >
              全部
            </button></span
          ></label
        >
        <p
          v-if="workflow === 'settlements' && liveLoading"
          class="muted settle-live"
          role="status"
        >
          正在预估手续费与实际到账…
        </p>
        <div
          v-else-if="workflow === 'settlements' && livePreview"
          class="settle-live"
          role="status"
        >
          <span>手续费：{{ livePreview.fee_money }} 元</span
          ><span>实际到账：{{ livePreview.net_money }} 元</span>
        </div>
        <p
          v-else-if="workflow === 'settlements' && liveError"
          class="muted settle-live"
        >
          {{ liveError }}
        </p>
        <label
          >业务请求号<input
            v-model="form.external_no"
            :readonly="!!pendingIntent212 || busy || restoringIntent212"
            required
            maxlength="128"
        /></label>
        <p class="muted">
          重复请求必须保留相同请求号。以记录状态和资金明细为准。
        </p>
        <div
          v-if="
            workflow === 'settlements' && settlementPreview && !pendingIntent212
          "
          class="alert"
          role="status"
        >
          <p>预计手续费：{{ settlementPreview.fee_money }} 元</p>
          <p>预计结算净额：{{ settlementPreview.net_money }} 元</p>
          <p>需留存可出账余额：{{ settlementPreview.retained_money }} 元</p>
          <p>
            将预占申请总额。此步骤和后续审批均不代表外部出款，提交时服务端重新校验；费用改变将拒绝本次确认。
          </p>
          <button
            type="button"
            :disabled="busy"
            @click="
              settlementPreview = null;
              previewBinding = '';
              submit();
            "
          >
            重新核对费用
          </button>
        </div>
        <div class="modal-actions">
          <a v-if="application && !busy" :href="settlementRecordsHref"
            >返回提现记录</a
          >
          <button
            v-if="!application"
            type="button"
            :disabled="busy"
            @click="dialog = null"
          >
            取消</button
          ><button
            v-if="!pendingIntent212"
            class="primary"
            :disabled="
              busy ||
              !hasCurrentFunds ||
              !hasRecipient ||
              restoringIntent212 ||
              !!intentStorageError212
            "
          >
            {{
              busy
                ? "正在处理…"
                : workflow === "settlements" && settlementPreview
                  ? "按已核对费用提交"
                  : "确认提交"
            }}
          </button>
        </div>
      </form>
      <template v-else
        ><p>
          {{
            batchMode
              ? "将按顺序处理所选 " +
                checked.length +
                " 条申请，失败时停止后续处理。"
              : "请核对申请编号 " + selected.id + " 的账务信息。"
          }}
        </p>
        <label
          v-if="
            dialog === 'reject' &&
            !batchMode &&
            workflow === 'internal-transfers'
          "
          >拒绝原因（选填）<textarea
            v-model="rejectReason"
            rows="2"
            maxlength="500"
            placeholder="填写拒绝原因，将记录在审核事件中"
            aria-label="拒绝原因"
          ></textarea>
        </label>
        <div class="modal-actions">
          <button :disabled="busy" @click="dialog = null">取消</button
          ><button class="primary" :disabled="busy" @click="decide">
            {{
              busy
                ? "正在处理…"
                : "确认" + (dialog === "approve" ? "核准" : "拒绝")
            }}
          </button>
        </div></template
      >
    </section>
  </div>
</template>
<style scoped>
.internal-transfer-filters > .filter-inline > select {
  min-width: 160px;
}
.settle-amount-row {
  display: flex;
  gap: var(--space-2, 8px);
  align-items: stretch;
}
.settle-amount-row > input {
  flex: 1 1 auto;
  min-width: 0;
}
.settle-all-btn {
  flex: 0 0 auto;
  white-space: nowrap;
}
.settle-live {
  display: flex;
  gap: var(--space-4, 16px);
  margin: var(--space-2, 8px) 0 0;
  font-size: 13px;
}
.settle-live > span {
  color: var(--ink, inherit);
}
/* Head of the account-申请 dialog: title left, close × right, sticky above
   the scrolling body. */
.workflow-modal-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}
.modal > .workflow-modal-head {
  position: sticky;
  top: calc(-1 * var(--space-5));
  z-index: 3;
  background: var(--bg-card);
}
.workflow-modal-head h2 {
  margin: 0;
}
.workflow-modal-x {
  font-size: 24px;
  line-height: 1;
  padding: 4px 12px;
  border: 0;
  background: transparent;
  color: var(--ink-muted, inherit);
}
.workflow-modal-x:hover:not(:disabled) {
  background: var(--bg-hover);
}
</style>
