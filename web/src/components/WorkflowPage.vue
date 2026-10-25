<script setup lang="ts">
import { RouterLink } from "vue-router";
import { readLinkedMerchantRoute } from "../list-filters";
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";
import { ApiClient, ApiError, validAmount } from "../api";
import type { Area } from "../menu";
import { displayValue } from "../api";
import {
  readFundsAvailability,
  type FundsAvailability,
} from "../funds-availability";
import FundsAvailabilitySummary from "./FundsAvailabilitySummary.vue";
import SettlementRecipientPanel from "./SettlementRecipientPanel.vue";
import SettlementPayoutPanel from "./SettlementPayoutPanel.vue";
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
}>();
const routeContext = readLinkedMerchantRoute(
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
const payoutRequestId = ref("");
const form = ref({
  merchant_id: "",
  recipient_id: "",
  amount: "",
  external_no: "",
});
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
}
watch(() => form.value.merchant_id, invalidateFunds, { flush: "sync" });
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
    const data = await props.client.request<{
      items: Record<string, unknown>[];
      total: number;
    }>(
      endpoint +
        "?" +
        new URLSearchParams({
          page: String(page.value),
          limit: String(limit.value),
          ...(props.area === "admin" && routeContext.merchantID
            ? { merchant_id: routeContext.merchantID }
            : {}),
        }),
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
  settlementRecipient.value = null;
  form.value = {
    merchant_id: "",
    recipient_id: "",
    amount: "",
    external_no: crypto.randomUUID(),
  };
  error.value = "";
  dialog.value = "create";
  invalidateFunds();
  if (props.area === "merchant") void loadFunds();
}
async function submit() {
  if (busy.value) return;
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
    await props.client.request(endpoint, { method: "POST", body });
    notice.value = "申请已提交，请根据记录状态核实处理结果";
    dialog.value = null;
    if (!props.application) await load();
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
function openDecision(
  row: Record<string, unknown>,
  action: "approve" | "reject",
) {
  selected.value = row;
  batchMode.value = false;
  dialog.value = action;
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
  let completed = 0;
  try {
    for (const id of ids) {
      await props.client.request(
        `${endpoint}/${encodeURIComponent(id)}/${action}`,
        { method: "POST", body: {} },
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
onMounted(() => {
  if (
    props.application &&
    props.area === "merchant" &&
    props.workflow === "settlements"
  )
    openCreate();
  else void load();
});
onBeforeUnmount(() => {
  controller?.abort();
  invalidateFunds();
});
</script>
<template>
  <section v-if="!application || !dialog" class="panel records">
    <p
      v-if="area === 'admin' && routeContext.merchantID && !routeContext.error"
      class="resource-description"
    >
      商户号：{{ routeContext.merchantID }}
    </p>
    <RouterLink
      v-if="
        workflow === 'settlements' &&
        (routeContext.merchantID || routeContext.error)
      "
      :to="`/${area}/settle/list`"
      >清空筛选</RouterLink
    >
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
    <p class="resource-description muted">
      {{
        workflow === "settlements"
          ? "结算申请会独立预占申请总额；核准后仍需另行准备并明确发送钱包出款，审批通过不代表支付成功。缺失费用、资金归属或收款快照的历史记录不能出款；银行卡不适用本钱包流程。拒绝或已证明未发送的取消会释放对应冻结资金。"
          : workflow === "profitsharing"
            ? "站内分账申请由已支付订单和启用规则生成，核准后转入站内收款商户账户。生成申请时同样由服务端执行 T+0/T+1 可出账政策，本页面不手动创建分账或执行上游渠道分账。"
            : "站内转账仅在本平台商户账户之间转移余额。请核对商户编号和金额。"
      }}
    </p>
    <div class="toolbar">
      <div class="toolbar-actions">
        <button v-if="!application" :disabled="loading" @click="load">
          刷新</button
        ><button
          v-if="workflow !== 'profitsharing'"
          class="primary"
          :disabled="busy"
          @click="openCreate"
        >
          {{ workflow === "settlements" ? "申请结算" : "发起站内转账" }}
        </button>
      </div>
      <button
        v-if="area === 'admin' && workflow === 'settlements'"
        :disabled="!checked.length || busy"
        @click="
          batchMode = true;
          dialog = 'approve';
        "
      >
        核准所选 {{ checked.length }} 条申请
      </button>
      <a
        v-if="
          area === 'admin' &&
          workflow === 'settlements' &&
          checked.length &&
          !busy
        "
        :href="
          '/admin/settle/batch?request_ids=' +
          encodeURIComponent(checked.join(','))
        "
        >将所选 {{ checked.length }} 条申请生成结算批次</a
      >
    </div>
    <div v-if="loading" class="loading-row" role="status">正在加载申请…</div>
    <div v-else-if="!application" class="table-scroll">
      <table>
        <thead>
          <tr>
            <th v-if="area === 'admin' && workflow === 'settlements'">选择</th>
            <th v-for="column in columns" :key="column.key">
              {{ column.label }}
            </th>
            <th v-if="area === 'admin' || workflow === 'settlements'">操作</th>
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
              {{ displayValue(row[column.key]) }}
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
    <div v-if="!application" class="pagination">
      <span>共 {{ total }} 条申请</span>
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
    <a href="/merchant/settle/list">提现记录</a>
    <p v-if="notice" class="muted">业务请求号：{{ form.external_no }}</p>
  </div>
  <div
    v-if="dialog"
    :class="application ? 'workflow-application' : 'modal-backdrop'"
  >
    <section
      :class="application ? 'panel workflow-application-form' : 'modal panel'"
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
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <form v-if="dialog === 'create'" @submit.prevent="submit">
        <label v-if="area === 'admin'"
          >付款商户 ID<input
            v-model="form.merchant_id"
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
            required
            inputmode="numeric"
        /></label>
        <SettlementRecipientPanel
          v-if="workflow === 'settlements'"
          :client="client"
          :admin="area === 'admin'"
          :merchant-i-d="form.merchant_id"
          :disabled="busy"
          @change="recipientChanged"
        />
        <label
          >金额（元）<input
            v-model="form.amount"
            required
            inputmode="decimal" /></label
        ><label
          >业务请求号<input v-model="form.external_no" required maxlength="128"
        /></label>
        <p class="muted">
          重复请求必须保留相同请求号。以记录状态和资金明细为准。
        </p>
        <div
          v-if="workflow === 'settlements' && settlementPreview"
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
          <a v-if="application && !busy" href="/merchant/settle/list"
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
            class="primary"
            :disabled="busy || !hasCurrentFunds || !hasRecipient"
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
