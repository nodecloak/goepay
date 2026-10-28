<script setup lang="ts">
import { RouterLink, useRouter } from "vue-router";
import {
  readTransferRoute,
  freshTransferFilters,
  transferFiltersError,
  transferFiltersQuery,
} from "../transfer-filters";
import TransferListFilters from "./TransferListFilters.vue";
import {
  computed,
  ref,
  watch,
  onMounted,
  onBeforeUnmount,
  nextTick,
} from "vue";
import { ApiClient, displayValue } from "../api";
import type { Area } from "../menu";
import { safePaymentUrl } from "../commerce";
import DataTable from "./DataTable.vue";
import TransferListTable from "./TransferListTable.vue";
import { hasOriginalTransferProjection } from "../transfer-list";
import CsvExportButton from "./CsvExportButton.vue";
import ShellIcon from "./ShellIcon.vue";
import bankCodes from "../data/wechat-bank-codes.json";
import bankNames from "../data/wechat-bank-names.json";
import {
  transferTypeLabel,
  transferPolicyError,
  transferNotice,
  bankChannelProduct,
  bankProductLabel,
  transferReadActions,
} from "../transfers";
import TransferPolicySummary from "./TransferPolicySummary.vue";
import {
  readFundsAvailability,
  type FundsAvailability,
} from "../funds-availability";
const props = defineProps<{
  client: ApiClient;
  area: Area;
  initialQuery?: Record<string, unknown>;
  selfID?: string;
}>();
interface Capabilities extends FundsAvailability {
  writes_enabled: boolean;
  platform_enabled: boolean;
  effective_enabled?: boolean;
  merchant_enabled: boolean;
  transfer_rate: string;
  channels: {
    id: string;
    types: string[];
    bank_product?: string;
    bank_code_required?: boolean;
  }[];
  available_money: string;
  minimum_money?: string;
  maximum_money?: string;
  daily_recipient_limit?: number;
  default_remark?: string;
  default_title?: string;
}
const routeContext = readTransferRoute(
  props.initialQuery || {},
  props.area,
  props.selfID || "",
);
const router = useRouter(),
  baseRoute =
    props.area === "admin" ? "/admin/transfer/list" : "/merchant/transfer";
const draftFilters = ref({ ...routeContext.filters }),
  appliedFilters = ref({ ...routeContext.filters });
const draftError = computed(() => transferFiltersError(draftFilters.value));
const items = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  page = ref(routeContext.page),
  limit = ref(routeContext.limit),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  creating = ref(false),
  confirming = ref(false),
  capabilities = ref<Capabilities | null>(null),
  capLoading = ref(false),
  capMerchant = ref(""),
  selected = ref<Record<string, unknown> | null>(null),
  recipient = ref<Record<string, unknown>>({}),
  proof = ref<Record<string, unknown> | null>(null),
  proofConfirm = ref(false);
const form = ref({
  merchant_id: "",
  channel_id: "",
  type: "alipay",
  account: "",
  name: "",
  amount: "",
  remark: "",
  external_no: "",
  bank_code: "",
});
let controller: AbortController | undefined,
  capEpoch = 0;
let active = true;
const detailDialog = ref<HTMLElement>(),
  detailClose = ref<HTMLButtonElement>();
const recordsPanel = ref<HTMLElement>();
let detailOpener: HTMLElement | undefined;
let detailReturn: { id: string; label: string } | undefined;
function closeDetails() {
  if (busy.value) return;
  const target = detailOpener;
  const remembered = detailReturn;
  selected.value = null;
  recipient.value = {};
  proof.value = null;
  proofConfirm.value = false;
  detailOpener = undefined;
  detailReturn = undefined;
  void nextTick(() => {
    if (!active) return;
    if (target?.isConnected) target.focus();
    else {
      const row = Array.from(
        recordsPanel.value?.querySelectorAll<HTMLElement>(
          "tr[data-transfer-id]",
        ) || [],
      ).find((node) => node.dataset.transferId === remembered?.id);
      const rowButton = Array.from(
        row?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") || [],
      ).find((button) => button.textContent?.trim() === remembered?.label);
      (
        rowButton ||
        recordsPanel.value?.querySelector<HTMLButtonElement>(
          ".table-tools button:not(:disabled)",
        )
      )?.focus();
    }
  });
}
function detailKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    closeDetails();
    return;
  }
  if (event.key !== "Tab") return;
  const nodes = Array.from(
      detailDialog.value?.querySelectorAll<HTMLElement>(
        "button:not(:disabled),a[href],input:not(:disabled),select:not(:disabled),textarea:not(:disabled)",
      ) || [],
    ),
    first = nodes[0],
    last = nodes.at(-1);
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}
function transferQuery() {
  return transferFiltersQuery(appliedFilters.value, page.value, limit.value);
}
function applyFilters() {
  if (busy.value || loading.value || routeContext.error || draftError.value)
    return;
  appliedFilters.value = { ...draftFilters.value };
  page.value = 1;
  void load();
}
function resetFilters() {
  if (busy.value || loading.value) return;
  if (Object.keys(props.initialQuery || {}).length) {
    void router.replace(baseRoute);
    return;
  }
  draftFilters.value = freshTransferFilters();
  appliedFilters.value = freshTransferFilters();
  page.value = 1;
  void load();
}
const csvPath = computed(() => {
  if (routeContext.error || draftError.value) return "";
  const q = transferQuery();
  q.set("format", "csv");
  return `/transfers?${q}`;
});
const columns = [
  { key: "id", label: "编号" },
  { key: "merchant_id", label: "商户 ID" },
  { key: "funding_source", label: "资金来源" },
  { key: "settlement_request_id", label: "原结算申请" },
  { key: "transfer_no", label: "业务付款单号" },
  { key: "amount", label: "付款金额（分）" },
  { key: "fee_amount", label: "付款手续费（分）" },
  { key: "cost_amount", label: "出账本金及费用（分）" },
  { key: "channel_id", label: "付款通道 ID" },
  { key: "status", label: "状态" },
  { key: "ledger_status", label: "历史账本状态" },
  { key: "bank_lifecycle", label: "银行生命周期" },
];
const readActions = computed(() => transferReadActions(selected.value));
const availableTypes = computed(() =>
  [
    ...new Set(capabilities.value?.channels.flatMap((c) => c.types) || []),
  ].filter(
    (t) =>
      ["alipay", "wxpay", "bank"].includes(t) &&
      (t !== "bank" ||
        capabilities.value?.channels.some((c) => !!bankChannelProduct(c))),
  ),
);
const defaultDescription = (type: string) =>
  (type === "alipay"
    ? capabilities.value?.default_title
    : capabilities.value?.default_remark) || "";
const effectiveDescription = computed(
  () => form.value.remark || defaultDescription(form.value.type),
);
watch(
  () => form.value.type,
  (type, old) => {
    if (!form.value.remark || form.value.remark === defaultDescription(old))
      form.value.remark = defaultDescription(type);
    if (type !== "bank") form.value.bank_code = "";
  },
);
const channels = computed(
  () =>
    capabilities.value?.channels.filter(
      (c) =>
        c.types.includes(form.value.type) &&
        (form.value.type !== "bank" || !!bankChannelProduct(c)),
    ) || [],
);
const selectedBankProduct = computed(() =>
  form.value.type === "bank"
    ? bankChannelProduct(
        channels.value.find((c) => c.id === form.value.channel_id),
      )
    : undefined,
);
const bankCodeRequired = computed(
  () => form.value.type === "bank" && selectedBankProduct.value === "wxpay_v2",
);
const unknownBankCapabilities = computed(() =>
  capabilities.value?.channels.some(
    (c) => c.types.includes("bank") && !bankChannelProduct(c),
  ),
);
watch(
  () => form.value.channel_id,
  () => {
    if (!bankCodeRequired.value) form.value.bank_code = "";
  },
);
const canSubmit = computed(
  () =>
    !!capabilities.value?.writes_enabled &&
    channels.value.some((c) => c.id === form.value.channel_id) &&
    (props.area === "merchant" || capMerchant.value === form.value.merchant_id),
);
const message = (e: unknown) =>
  e instanceof Error ? e.message : "请求失败，请重试";
async function load() {
  if (routeContext.error) {
    error.value = routeContext.error;
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
    const q = transferQuery();
    const data = await props.client.request<{
      items: Record<string, unknown>[];
      total: number;
    }>("/transfers?" + q, { signal: c.signal });
    if (!c.signal.aborted) {
      items.value = data.items;
      total.value = data.total;
    }
  } catch (e) {
    if (!c.signal.aborted) error.value = message(e);
  } finally {
    if (!c.signal.aborted) loading.value = false;
  }
}
async function loadCapabilities() {
  const merchant = form.value.merchant_id;
  if (props.area === "admin" && !/^[1-9]\d*$/.test(merchant)) {
    error.value = "请输入有效商户 ID";
    return;
  }
  const epoch = ++capEpoch;
  capLoading.value = true;
  error.value = "";
  capabilities.value = null;
  try {
    const data = await props.client.request<Capabilities>(
      "/transfers/capabilities" +
        (props.area === "admin"
          ? "?merchant_id=" + encodeURIComponent(merchant)
          : ""),
    );
    if (epoch === capEpoch) {
      capabilities.value = { ...data, ...readFundsAvailability(data) };
      capMerchant.value = merchant;
      form.value.type = availableTypes.value.includes(form.value.type)
        ? form.value.type
        : availableTypes.value[0] || "";
      form.value.channel_id = channels.value[0]?.id || "";
      if (!form.value.remark)
        form.value.remark = defaultDescription(form.value.type);
    }
  } catch (e) {
    if (epoch === capEpoch) error.value = message(e);
  } finally {
    if (epoch === capEpoch) capLoading.value = false;
  }
}
async function openCreate() {
  creating.value = true;
  confirming.value = false;
  error.value = "";
  notice.value = "";
  capabilities.value = null;
  if (!form.value.external_no) form.value.external_no = crypto.randomUUID();
  if (props.area === "merchant") await loadCapabilities();
}
function check() {
  error.value = "";
  if (!canSubmit.value) {
    error.value = "付款权限或可用通道尚未确认，请重新读取付款配置";
    return;
  }
  const policyError = transferPolicyError(
    form.value.amount,
    capabilities.value?.minimum_money,
    capabilities.value?.maximum_money,
  );
  if (policyError) {
    error.value = policyError;
    return;
  }
  if (!form.value.account.trim()) {
    error.value = "请填写收款账号";
    return;
  }
  if (
    form.value.type === "bank" &&
    (!selectedBankProduct.value ||
      !form.value.name.trim() ||
      (bankCodeRequired.value &&
        !Object.values(bankCodes).includes(form.value.bank_code)))
  ) {
    error.value = bankCodeRequired.value
      ? "银行卡付款必须填写收款人姓名并选择开户银行"
      : "银行卡付款须确认产品能力并填写真实姓名";
    return;
  }
  if (form.value.type === "bank" && !/^\d{12,32}$/.test(form.value.account)) {
    error.value = "请输入 12 至 32 位数字银行卡号";
    return;
  }
  if (form.value.type === "wxpay" && !effectiveDescription.value.trim()) {
    error.value = "微信付款必须填写付款说明";
    return;
  }
  confirming.value = true;
}
async function submit() {
  if (!canSubmit.value) return;
  busy.value = true;
  error.value = "";
  try {
    const body: Record<string, string> = {
      channel_id: form.value.channel_id,
      type: form.value.type,
      account: form.value.account,
      name: form.value.name,
      amount: form.value.amount,
      remark: effectiveDescription.value,
      external_no: form.value.external_no,
    };
    if (bankCodeRequired.value) body.bank_code = form.value.bank_code;
    if (props.area === "admin") body.merchant_id = form.value.merchant_id;
    const result = await props.client.requestResult<Record<string, unknown>>(
      "/transfers",
      { method: "POST", body },
    );
    notice.value = transferNotice(result.data, result.status);
    creating.value = false;
    confirming.value = false;
    form.value.external_no = "";
    await load();
    if (result.data?.id) await details(result.data, notice.value);
  } catch (e) {
    confirming.value = false;
    error.value = `付款结果尚未核实。请保留原业务请求号 ${form.value.external_no}，先通过付款记录查询。${message(e)}`;
  } finally {
    busy.value = false;
  }
}
async function details(
  row: Record<string, unknown>,
  submissionNotice?: string,
) {
  detailOpener =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : undefined;
  const label = detailOpener?.textContent?.trim() || "";
  detailReturn =
    detailOpener?.closest<HTMLElement>("tr[data-transfer-id]")?.dataset
      .transferId === String(row.id) && ["付款详情", "查询状态"].includes(label)
      ? { id: String(row.id), label }
      : undefined;
  busy.value = true;
  error.value = "";
  try {
    const data = await props.client.request<{
      transfer: Record<string, unknown>;
      recipient: Record<string, unknown>;
    }>(`/transfers/${encodeURIComponent(String(row.id))}`);
    if (!active) return false;
    if (
      String(data.transfer.id) !== String(row.id) ||
      (row.merchant_id !== undefined &&
        String(data.transfer.merchant_id) !== String(row.merchant_id)) ||
      (row.amount !== undefined && data.transfer.amount !== row.amount)
    )
      throw Error("付款详情与原记录不一致，请刷新核对");
    selected.value = data.transfer;
    notice.value = submissionNotice ?? transferNotice(data.transfer, 200);
    recipient.value = data.recipient;
    proof.value = null;
    proofConfirm.value = false;
    void nextTick(() => detailClose.value?.focus());
    return true;
  } catch (e) {
    if (active) error.value = message(e);
    return false;
  } finally {
    busy.value = false;
  }
}
async function queryRecord(row: Record<string, unknown>) {
  if (busy.value) return;
  if ((await details(row)) && active) await queryTransfer();
}
async function queryTransfer() {
  if (!selected.value || busy.value || !readActions.value.query) return;
  busy.value = true;
  error.value = "";
  try {
    const result = await props.client.requestResult<Record<string, unknown>>(
      `/transfers/${encodeURIComponent(String(selected.value.id))}/query`,
      { method: "POST", body: {} },
    );
    notice.value = transferNotice(result.data, result.status);
    if (result.data) selected.value = result.data;
    await load();
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
async function requestProof() {
  if (!selected.value || busy.value || !readActions.value.proof) return;
  busy.value = true;
  error.value = "";
  try {
    const result = await props.client.requestResult<Record<string, unknown>>(
      `/transfers/${encodeURIComponent(String(selected.value.id))}/proof`,
      { method: "POST", body: {} },
    );
    proof.value = result.data;
    proofConfirm.value = false;
    notice.value =
      result.status === 202
        ? "凭证结果尚未核实，请通过原付款记录继续查询。"
        : `凭证状态：${displayValue(result.data?.state)}，请以已就绪的下载链接为准。`;
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => {
  active = false;
  detailOpener = undefined;
  detailReturn = undefined;
  controller?.abort();
  capEpoch++;
});
</script>
<template>
  <p
    v-if="area === 'admin' && appliedFilters.merchant_id && !routeContext.error"
    class="resource-description"
  >
    商户号：{{ appliedFilters.merchant_id }}
  </p>
  <RouterLink
    v-if="appliedFilters.merchant_id || routeContext.error"
    :to="baseRoute"
    >清空筛选</RouterLink
  >
  <div v-if="error" class="alert error" role="alert">{{ error }}</div>
  <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
  <form class="toolbar list-filter-card panel" @submit.prevent="applyFilters">
    <TransferListFilters
      v-model="draftFilters"
      :area="area"
      :loading="loading || busy"
      :blocked="!!routeContext.error"
      @reset="resetFilters"
    />
  </form>
  <section ref="recordsPanel" class="panel records">
    <div class="table-tools">
      <button
        type="button"
        class="primary"
        :disabled="busy"
        @click="openCreate"
      >
        <ShellIcon name="plus" />申请付款
      </button>
      <a :href="`/${area}/transfer/batches`" class="button soft-button"
        >批量转账</a
      >
      <div class="table-tools-end">
        <CsvExportButton
          :client="client"
          :path="csvPath"
          filename="transfers-filtered.csv"
          :disabled="loading || !!routeContext.error || !!draftError"
        />
      </div>
    </div>
    <div v-if="loading" class="loading-row" role="status">
      正在读取付款记录…
    </div>
    <TransferListTable
      v-else-if="hasOriginalTransferProjection(items)"
      :items="items"
      :area="area"
      :busy="busy"
      @details="details"
      @query="queryRecord"
    />
    <DataTable v-else :columns="columns" :items="items"
      ><template #actions="{ row }"
        ><button class="link-button" :disabled="busy" @click="details(row)">
          付款详情
        </button></template
      ></DataTable
    >
    <div class="pagination">
      <span>共 {{ total }} 条记录</span>
      <div>
        <button
          :disabled="page <= 1 || loading"
          @click="
            page--;
            load();
          "
        >
          上一页</button
        ><span>第 {{ page }} 页</span
        ><button
          :disabled="page * limit >= total || loading"
          @click="
            page++;
            load();
          "
        >
          下一页
        </button>
      </div>
    </div>
  </section>
  <div v-if="creating" class="modal-backdrop">
    <section
      class="modal panel resource-modal"
      role="dialog"
      aria-modal="true"
      aria-label="申请付款"
    >
      <h2>{{ confirming ? "核对付款" : "申请付款" }}</h2>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <template v-if="confirming"
        ><dl class="record-detail">
          <dt>收款方式</dt>
          <dd>{{ transferTypeLabel[form.type] || form.type }}</dd>
          <dt>收款账号</dt>
          <dd>{{ form.account }}</dd>
          <dt>收款人姓名</dt>
          <dd>{{ form.name || "未填写" }}</dd>
          <dt>付款金额（元）</dt>
          <dd>{{ form.amount }}</dd>
          <dt>付款费率</dt>
          <dd>{{ capabilities?.transfer_rate }}%</dd>
          <dt>付款说明</dt>
          <dd>{{ effectiveDescription }}</dd>
          <template v-if="form.type === 'bank'"
            ><dt>银行卡产品</dt>
            <dd>
              {{
                selectedBankProduct
                  ? bankProductLabel[selectedBankProduct]
                  : "能力尚未核实"
              }}
            </dd></template
          >
          <template v-if="bankCodeRequired"
            ><dt>开户银行编号</dt>
            <dd>{{ form.bank_code }}</dd></template
          >
          <dt>业务请求号</dt>
          <dd>{{ form.external_no }}</dd>
        </dl>
        <p v-if="selectedBankProduct === 'alipay_bankcard'" class="muted">
          支付宝银行卡仅对私账户，需核对真实姓名，无需微信银行编号。
        </p>
        <TransferPolicySummary v-if="capabilities" :policy="capabilities" />
        <p class="muted">
          确认后向通道提交付款，并预占本金及配置手续费。结果未知时保留预占，只能查询核实，不应创建新的付款重试。
        </p>
        <div class="modal-actions">
          <button :disabled="busy" @click="confirming = false">返回核对</button
          ><button
            class="danger-button"
            :disabled="busy || !canSubmit"
            @click="submit"
          >
            {{ busy ? "正在提交…" : "确认提交付款" }}
          </button>
        </div></template
      >
      <form v-else @submit.prevent="check">
        <label v-if="area === 'admin'"
          >付款商户 ID<input
            v-model="form.merchant_id"
            required
            inputmode="numeric"
            @input="
              capabilities = null;
              capEpoch++;
            " /></label
        ><button
          v-if="area === 'admin'"
          type="button"
          :disabled="capLoading"
          @click="loadCapabilities"
        >
          读取付款权限
        </button>
        <div v-if="capLoading" role="status" class="muted">
          正在读取付款配置…
        </div>
        <template v-if="capabilities"
          ><p class="muted">
            付款费率
            {{ capabilities.transfer_rate }}%
          </p>
          <TransferPolicySummary :policy="capabilities" />
          <p v-if="unknownBankCapabilities" class="muted">
            部分银行卡产品能力尚未核实，已禁止使用。
          </p>
          <p v-if="!capabilities.writes_enabled" class="alert error">
            {{
              !(capabilities.effective_enabled ?? capabilities.platform_enabled)
                ? capabilities.platform_enabled
                  ? "当前用户组付款功能未启用"
                  : "平台付款功能未启用"
                : !capabilities.merchant_enabled
                  ? "该商户付款功能未启用"
                  : "当前运行环境或通道未启用付款"
            }}
          </p>
          <label
            >收款方式<select
              v-model="form.type"
              required
              @change="form.channel_id = channels[0]?.id || ''"
            >
              <option v-for="type in availableTypes" :key="type" :value="type">
                {{ transferTypeLabel[type] }}
              </option>
            </select></label
          ><label
            >付款通道<select v-model="form.channel_id" required>
              <option
                v-for="channel in channels"
                :key="channel.id"
                :value="channel.id"
              >
                {{ channel.id }}
              </option>
            </select></label
          >
          <p v-if="form.type === 'bank' && selectedBankProduct" class="muted">
            {{ bankProductLabel[selectedBankProduct]
            }}{{
              selectedBankProduct === "alipay_bankcard"
                ? "：仅对私账户，无需微信银行编号。"
                : "：必须选择微信开户银行编号。"
            }}
          </p>
          <label v-if="bankCodeRequired"
            >开户银行编号<select v-model="form.bank_code" required>
              <option value="">请选择开户银行</option>
              <option
                v-for="(code, bank) in bankCodes"
                :key="code"
                :value="code"
              >
                {{ bankNames[bank] || bank }}（{{ code }}）
              </option>
            </select></label
          ><label
            >收款账号<input
              v-model="form.account"
              required
              maxlength="256"
              autocomplete="off" /></label
          ><label
            >收款人姓名<input
              v-model="form.name"
              :required="form.type === 'bank'"
              maxlength="100"
              autocomplete="off" /></label
          ><label
            >付款金额（元）<input
              v-model="form.amount"
              required
              inputmode="decimal" /></label
          ><label
            >付款说明{{ form.type === "wxpay" ? "（必填）" : ""
            }}<textarea
              v-model="form.remark"
              :required="
                form.type === 'wxpay' && !defaultDescription(form.type)
              "
              :placeholder="defaultDescription(form.type)"
              maxlength="32"
              rows="3"
            /></label
          ><label
            >业务请求号<input
              v-model="form.external_no"
              required
              maxlength="128" /></label
        ></template>
        <div class="modal-actions">
          <button type="button" :disabled="busy" @click="creating = false">
            取消</button
          ><button class="primary" :disabled="busy || !canSubmit">
            核对付款信息
          </button>
        </div>
      </form>
    </section>
  </div>
  <div
    v-if="selected && !creating"
    class="modal-backdrop"
    @click.self="closeDetails"
  >
    <section
      ref="detailDialog"
      class="modal panel resource-modal"
      role="dialog"
      aria-modal="true"
      aria-label="付款详情"
      @keydown="detailKeydown"
    >
      <h2>付款详情</h2>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
      <dl class="record-detail">
        <template
          v-for="(label, key) in {
            id: '编号',
            transfer_no: '业务付款单号',
            provider_transfer_no: '通道付款单号',
            channel_id: '付款通道',
            funding_source: '资金来源（settlement 为原结算专属资金）',
            settlement_request_id: '原结算申请编号',
            send_state: '结算发送证明',
            type: '收款方式',
            amount: '付款金额（分）',
            fee_amount: '付款手续费（分）',
            cost_amount: '出账本金及费用（分）',
            status: '付款状态',
            ledger_status: '历史账本状态（不代表银行最终到账）',
            bank_lifecycle: '银行生命周期',
            bank_product: '银行卡产品',
            bank_estimated_fee_known: '是否存在银行手续费估算',
            bank_estimated_fee_amount: '预计银行手续费（分，仅估算）',
            bank_fee_known: '银行手续费已核实',
            bank_fee_amount: '银行手续费（分）',
            bank_service_fee_held: '服务费暂存（分）',
            bank_returned_principal: '已返还本金（分）',
            bank_returned_service_fee: '已返还服务费（分）',
            bank_returned_fee: '已返还银行手续费（分）',
            bank_next_check_at: '下次银行核验时间',
            bank_reversal_journals: '退票记账流水编号',
          }"
          :key="key"
          ><dt>{{ label }}</dt>
          <dd>
            {{
              key === "bank_estimated_fee_amount" &&
              selected.bank_estimated_fee_known !== true
                ? "尚无估算"
                : key === "bank_fee_amount" &&
                    selected.type === "bank" &&
                    selected.bank_fee_known !== true
                  ? "尚未核实"
                  : displayValue(selected[key])
            }}
          </dd></template
        >
        <dt>收款账号</dt>
        <dd>{{ displayValue(recipient.account) }}</dd>
        <dt>收款姓名</dt>
        <dd>{{ displayValue(recipient.name) }}</dd>
        <template
          v-if="
            selected.type === 'bank' && selected.bank_product === 'wxpay_v2'
          "
          ><dt>开户银行编号</dt>
          <dd>{{ displayValue(recipient.bank_code) }}</dd></template
        >
      </dl>
      <p v-if="selected.type === 'bank'" class="muted">
        预计费用仅用于提示，估算不代表实际扣费或退费。缺少核实证据时不能认定资金已返还。
      </p>
      <p v-if="selected.funding_source === 'settlement'" class="alert">
        此 TF
        使用原结算申请的独立冻结资金，不会再次预占可用余额；审批通过不代表已出款。尚未发送的绑定须在原结算申请详情操作。
      </p>
      <p class="muted">
        查询只核实原付款结果，不重新提交付款。结果未知时不得按失败处理或再次付款。
      </p>
      <div class="toolbar-actions">
        <button
          v-if="readActions.query"
          :disabled="busy"
          @click="queryTransfer"
        >
          查询通道付款结果</button
        ><button
          v-if="readActions.proof"
          :disabled="busy"
          @click="proof ? requestProof() : (proofConfirm = true)"
        >
          {{ proof ? "查询付款凭证" : "申请付款凭证" }}
        </button>
      </div>
      <div v-if="proofConfirm" class="alert">
        <p>
          首次操作将向支付宝申请付款凭证，后续通过已保存的凭证编号查询。确认继续？
        </p>
        <div class="toolbar-actions">
          <button :disabled="busy" @click="proofConfirm = false">取消</button
          ><button :disabled="busy" @click="requestProof">确认申请凭证</button>
        </div>
      </div>
      <dl v-if="proof" class="record-detail">
        <dt>凭证编号</dt>
        <dd>{{ displayValue(proof.file_id) }}</dd>
        <dt>凭证状态</dt>
        <dd>{{ displayValue(proof.state) }}</dd>
        <dt v-if="safePaymentUrl(proof.download_url)">下载</dt>
        <dd v-if="safePaymentUrl(proof.download_url)">
          <a
            :href="safePaymentUrl(proof.download_url)"
            target="_blank"
            rel="noopener noreferrer"
            >下载付款凭证</a
          >
        </dd>
      </dl>
      <div class="modal-actions">
        <button ref="detailClose" :disabled="busy" @click="closeDetails">
          关闭详情
        </button>
      </div>
    </section>
  </div>
</template>
