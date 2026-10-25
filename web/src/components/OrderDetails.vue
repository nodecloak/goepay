<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
import { ApiClient, displayValue } from "../api";
import { consoleColumnLabel, consoleValue } from "../pro-console-fields";
import { operationNotice } from "../order-operations";
import NotificationDeliveryPanel from "./NotificationDeliveryPanel.vue";
import OrderIncomePanel from "./OrderIncomePanel.vue";
import OrderPrintingPanel from "./OrderPrintingPanel.vue";
import PreauthorizationObservation from "./PreauthorizationObservation.vue";
import ScanRecoveryPanel from "./ScanRecoveryPanel.vue";
import type { ScanRecoveryView } from "../scan-recovery";
import {
  canReleaseAuthorization,
  authorizationReleaseNotice,
  type AuthorizationRelease,
  canCaptureAuthorization,
} from "../authorization-release";
const props = defineProps<{
  client: ApiClient;
  tradeNo: string;
  ownSession?: boolean;
}>();
const emit = defineEmits<{
  close: [];
  refund: [order: Record<string, unknown>];
  updated: [];
}>();
const order = ref<Record<string, unknown> | null>(null),
  provider = ref<Record<string, unknown> | null>(null),
  scanRecovery = ref<ScanRecoveryView | null>(null),
  authorization = ref<Record<string, unknown> | null>(null),
  authorizationRelease = ref<AuthorizationRelease | null>(null),
  serverAllowsRelease = ref(false),
  serverAllowsNotification = ref(false),
  serverAllowsCapture = ref(false),
  serverAllowsObservation = ref(false),
  serverAllowsRemainingRelease = ref(false),
  observationBusy = ref(false),
  captureUncertain = ref(false),
  notification = ref<Record<string, unknown> | null>(null),
  incomeHold = ref<Record<string, unknown> | null>(null),
  incomeFreezeAmount = ref<unknown>(),
  canFreezeIncome = ref(false),
  canUnfreezeIncome = ref(false),
  canAdoptIncomeHold = ref(false),
  canRefundHeldIncome = ref(false),
  incomeRefundPending = ref(false),
  incomeBusy = ref(false),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  confirmClose = ref(false),
  confirmRelease = ref(false),
  confirmCapture = ref(false);
const canCapture = computed(
  () =>
    !captureUncertain.value &&
    canCaptureAuthorization(
      serverAllowsCapture.value,
      order.value?.status,
      authorization.value,
    ),
);
const canRelease = computed(
  () =>
    !!authorization.value &&
    canReleaseAuthorization(
      serverAllowsRelease.value,
      order.value?.status,
      authorizationRelease.value,
    ),
);
const canClose = computed(
  () =>
    !!provider.value &&
    provider.value.can_close !== false &&
    provider.value.method !== "preauth" &&
    !authorization.value &&
    order.value?.status === "pending",
);
const authorizationNotice = computed(() =>
  authorization.value?.release_state === "succeeded"
    ? "买家预授权冻结已解除，不代表商户退款或余额增加。"
    : authorization.value?.capture_state === "succeeded" &&
        order.value?.status === "paid"
      ? "预授权付款已捕获，并已核实订单支付。"
      : authorization.value?.capture_state === "none"
        ? "预授权已确认，尚未捕获付款；授权冻结不等于订单已支付。"
        : "预授权捕获状态尚需核实，请查询支付通道；不能将授权冻结或未知捕获当作支付成功。",
);
let controller: AbortController | undefined;
const labels: Record<string, string> = {
  trade_no: "系统订单号",
  out_trade_no: "商户订单号",
  api_trade_no: "接口订单号",
  merchant_id: "商户 ID",
  name: "商品名称",
  type: "支付方式",
  amount: "订单金额（元）",
  merchant_amount: "商户入账额（元）",
  platform_fee: "平台手续费（元）",
  refunded_amount: "已退款金额（元）",
  fee_rate_bps: "费率（万分比）",
  fee_policy: "订单计费策略",
  refund_fee_payer: "退款手续费承担方",
  status: "订单状态",
  notification_state: "通知状态",
  channel_id: "支付通道 ID",
  buyer: "支付账号",
  client_ip: "支付 IP",
  created_at: "创建时间",
  paid_at: "付款时间",
  notify_url: "结果通知地址",
  return_url: "支付后返回地址",
};
async function load() {
  controller?.abort();
  controller = new AbortController();
  const c = controller;
  loading.value = true;
  serverAllowsRelease.value = false;
  serverAllowsNotification.value = false;
  serverAllowsCapture.value = false;
  serverAllowsObservation.value = false;
  serverAllowsRemainingRelease.value = false;
  canFreezeIncome.value = false;
  canUnfreezeIncome.value = false;
  canAdoptIncomeHold.value = false;
  canRefundHeldIncome.value = false;
  incomeRefundPending.value = false;
  try {
    const data = await props.client.request<{
      order: Record<string, unknown>;
      reporting?: Record<string, unknown>;
      provider?: Record<string, unknown>;
      scan_recovery?: ScanRecoveryView;
      authorization?: Record<string, unknown>;
      authorization_release?: AuthorizationRelease;
      can_release_authorization?: boolean;
      can_resend_notification?: boolean;
      notification?: Record<string, unknown>;
      can_capture_authorization?: boolean;
      can_observe_authorization?: boolean;
      can_release_remaining_authorization?: boolean;
      income_hold?: Record<string, unknown> | null;
      income_freeze_amount?: unknown;
      can_freeze_income?: boolean;
      can_unfreeze_income?: boolean;
      can_adopt_income_hold?: boolean;
      can_refund_held_income?: boolean;
      income_refund_pending?: boolean;
    }>(`/orders/${encodeURIComponent(props.tradeNo)}`, { signal: c.signal });
    order.value = { ...data.order };
    for (const key of [
      "api_trade_no",
      "channel_id",
      "buyer",
      "client_ip",
      "notification_state",
      "refunded_amount",
    ])
      if (data.reporting?.[key] !== undefined)
        order.value[key] = data.reporting[key];
    provider.value = data.provider || null;
    scanRecovery.value = data.scan_recovery || null;
    authorization.value = data.authorization || null;
    authorizationRelease.value = data.authorization_release || null;
    serverAllowsRelease.value = data.can_release_authorization === true;
    serverAllowsNotification.value = data.can_resend_notification === true;
    notification.value = data.notification || null;
    serverAllowsCapture.value = data.can_capture_authorization === true;
    serverAllowsObservation.value = data.can_observe_authorization === true;
    serverAllowsRemainingRelease.value =
      data.can_release_remaining_authorization === true;
    incomeHold.value = data.income_hold || null;
    incomeFreezeAmount.value = data.income_freeze_amount;
    canFreezeIncome.value = data.can_freeze_income === true;
    canUnfreezeIncome.value = data.can_unfreeze_income === true;
    canAdoptIncomeHold.value = data.can_adopt_income_hold === true;
    canRefundHeldIncome.value = data.can_refund_held_income === true;
    incomeRefundPending.value = data.income_refund_pending === true;
    if (!canCapture.value) confirmCapture.value = false;
    if (!canRelease.value) confirmRelease.value = false;
    if (!canClose.value) confirmClose.value = false;
  } catch (e) {
    if (!c.signal.aborted)
      error.value = e instanceof Error ? e.message : "订单详情读取失败";
  } finally {
    if (!c.signal.aborted) loading.value = false;
  }
}
async function releaseAuthorization(queryOnly: boolean) {
  if (
    busy.value ||
    observationBusy.value ||
    loading.value ||
    confirmCapture.value
  )
    return;
  if (queryOnly && !authorizationRelease.value) return;
  if (!queryOnly && !canRelease.value) {
    error.value = "当前服务端未允许解除授权，请查询原解除请求";
    confirmRelease.value = false;
    return;
  }
  busy.value = true;
  error.value = "";
  notice.value = "";
  confirmRelease.value = false;
  serverAllowsRelease.value = false;
  try {
    const result = await props.client.requestResult(
      `/orders/${encodeURIComponent(props.tradeNo)}/${queryOnly ? "query-authorization-release" : "release-authorization"}`,
      { method: "POST", body: {} },
    );
    notice.value = authorizationReleaseNotice(result.data, result.status);
  } catch (e) {
    error.value = `授权解除结果尚未核实，请先查询原请求，不应另行发起。${e instanceof Error ? e.message : "通道操作失败"}`;
  } finally {
    await load();
    busy.value = false;
    emit("updated");
  }
}
async function captureAuthorization() {
  if (
    busy.value ||
    observationBusy.value ||
    loading.value ||
    confirmRelease.value ||
    !canCapture.value
  )
    return;
  busy.value = true;
  error.value = "";
  notice.value = "";
  confirmCapture.value = false;
  captureUncertain.value = true;
  serverAllowsCapture.value = false;
  serverAllowsRelease.value = false;
  try {
    const response = await props.client.requestResult<{
      capture_state?: string;
    }>(`/orders/${encodeURIComponent(props.tradeNo)}/capture-authorization`, {
      method: "POST",
      body: {},
    });
    notice.value =
      response.status === 200 && response.data?.capture_state === "succeeded"
        ? "已核实扣取买家授权金额，请核对订单支付状态及账务记录。"
        : "授权扣款结果尚未核实，只能查询原订单，不应再次扣款。";
  } catch (e) {
    error.value = `授权扣款结果尚未核实或当前状态不允许扣款，请查询原订单，不应重复扣取。${e instanceof Error ? e.message : "请求失败"}`;
  } finally {
    await load();
    busy.value = false;
    emit("updated");
  }
}
async function perform(action: "query" | "close") {
  if (action === "close" && !canClose.value) {
    confirmClose.value = false;
    error.value = "预授权或当前订单不支持普通关单";
    return;
  }
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const response = await props.client.requestResult(
      `/orders/${encodeURIComponent(props.tradeNo)}/${action}`,
      { method: "POST", body: {} },
    );
    notice.value = operationNotice(action, response.data, response.status);
    confirmClose.value = false;
    await load();
    emit("updated");
  } catch (e) {
    error.value = e instanceof Error ? e.message : "通道操作失败";
  } finally {
    busy.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => controller?.abort());
</script>
<template>
  <div
    class="modal-backdrop"
    @keydown.esc="!busy && !incomeBusy && emit('close')"
  >
    <section
      class="modal panel resource-modal"
      role="dialog"
      aria-modal="true"
      aria-label="订单详情"
    >
      <h2>订单详情</h2>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
      <div v-if="loading" class="loading-row" role="status">正在读取订单…</div>
      <template v-if="order"
        ><dl class="record-detail">
          <template v-for="(label, key) in labels" :key="key"
            ><dt v-if="order[key] !== undefined">
              {{ consoleColumnLabel(String(key), label) }}
            </dt>
            <dd v-if="order[key] !== undefined">
              {{ consoleValue(String(key), order[key], true) }}
            </dd></template
          >
        </dl>
        <ScanRecoveryPanel :recovery="scanRecovery" />
        <PreauthorizationObservation
          v-if="serverAllowsObservation"
          :client="client"
          :order="order"
          :allowed="serverAllowsObservation"
          :own-session="ownSession === true"
          :release-allowed="serverAllowsRemainingRelease"
          :release-revision="
            String(authorizationRelease?.source_revision ?? '0')
          "
          @busy="observationBusy = $event"
          @released="
            (message) => {
              notice = message;
              load();
              emit('updated');
            }
          "
          :disabled="
            busy || loading || incomeBusy || confirmCapture || confirmRelease
          "
        />
        <OrderIncomePanel
          :client="client"
          :trade-no="tradeNo"
          :hold="incomeHold"
          :freeze-amount="incomeFreezeAmount"
          :can-freeze="canFreezeIncome"
          :can-unfreeze="canUnfreezeIncome"
          :can-adopt="canAdoptIncomeHold"
          :has-pending-refund="incomeRefundPending"
          :disabled="busy || loading"
          @updated="load"
          @busy="incomeBusy = $event"
        />
        <NotificationDeliveryPanel
          :client="client"
          :trade-no="tradeNo"
          :order-status="order.status"
          :allowed="serverAllowsNotification"
          :notification="notification"
          :disabled="busy || loading"
          @updated="load"
        />
        <template v-if="authorization"
          ><h3>预授权记录</h3>
          <p class="muted">{{ authorizationNotice }}</p>
          <dl class="record-detail">
            <template
              v-for="(label, key) in {
                auth_no: '授权编号',
                buyer_id: '授权付款人',
                amount: '授权金额（分）',
                capture_state: '付款捕获状态',
                channel_id: '授权通道 ID',
                release_state: '授权解除状态',
              }"
              :key="key"
              ><dt>{{ label }}</dt>
              <dd>{{ displayValue(authorization[key]) }}</dd></template
            >
          </dl>
          <p class="muted">
            普通关单不适用于预授权。全额解除需核实原冻结未使用；剩余解除需另行查询并确认明确余款。解除不是商户退款，也不会增加商户余额。
          </p>
          <template v-if="authorizationRelease"
            ><h3>原授权解除请求</h3>
            <dl class="record-detail">
              <template
                v-for="(label, key) in {
                  request_no: '原解除请求号',
                  auth_no: '解除对应授权号',
                  amount: '本次解除金额（分）',
                  state: '解除请求状态',
                  sent: '是否已跨越发送边界',
                  channel_id: '解除通道 ID',
                }"
                :key="key"
                ><dt>{{ label }}</dt>
                <dd>{{ displayValue(authorizationRelease[key]) }}</dd></template
              >
            </dl>
            <dl
              v-if="authorizationRelease.source_revision"
              class="record-detail"
            >
              <dt>原冻结金额（分）</dt>
              <dd>{{ displayValue(authorizationRelease.original_amount) }}</dd>
              <dt>剩余解除来源修订</dt>
              <dd>{{ displayValue(authorizationRelease.source_revision) }}</dd>
            </dl>
            <p class="muted">
              处理中或未知时只查询原请求，不会创建新的请求号，也不会再次发送已发送的解除操作。
            </p></template
          >
          <div class="toolbar-actions">
            <button
              v-if="canCapture"
              class="danger-button"
              :disabled="busy || observationBusy || loading || confirmRelease"
              @click="confirmCapture = true"
            >
              扣取买家授权金额
            </button>
            <button
              v-if="canRelease"
              class="danger-button"
              :disabled="busy || observationBusy || loading || confirmCapture"
              @click="confirmRelease = true"
            >
              {{
                authorizationRelease ? "核对并解除原授权" : "解除买家预授权冻结"
              }}</button
            ><button
              v-if="authorizationRelease"
              :disabled="busy || loading"
              @click="releaseAuthorization(true)"
            >
              查询原解除请求
            </button>
          </div>
          <div v-if="confirmCapture && canCapture" class="alert">
            <h3>核对扣取买家授权金额</h3>
            <dl class="record-detail">
              <dt>授权编号</dt>
              <dd>{{ displayValue(authorization.auth_no) }}</dd>
              <dt>本次扣款金额（分）</dt>
              <dd>{{ displayValue(order.amount) }}</dd>
            </dl>
            <p>
              此操作将从买家已冻结的授权金额中实际扣款。请核对订单与授权，服务端会再次验证未扣款、未解除及授权证据；未知结果只能查询原订单，不可重复发送。
            </p>
            <div class="toolbar-actions">
              <button :disabled="busy" @click="confirmCapture = false">
                取消扣款</button
              ><button
                class="danger-button"
                :disabled="
                  busy ||
                  observationBusy ||
                  loading ||
                  confirmRelease ||
                  !canCapture
                "
                @click="captureAuthorization"
              >
                确认扣取授权金额
              </button>
            </div>
          </div>
          <div v-if="confirmRelease && canRelease" class="alert">
            <h3>确认解除买家冻结</h3>
            <dl class="record-detail">
              <dt>授权编号</dt>
              <dd>{{ displayValue(authorization?.auth_no) }}</dd>
              <dt>全额解除金额（分）</dt>
              <dd>
                {{
                  displayValue(
                    authorizationRelease?.amount ?? authorization?.amount,
                  )
                }}
              </dd>
              <dt>原解除请求号</dt>
              <dd>
                {{
                  authorizationRelease?.request_no ||
                  "尚未创建，确认后由服务端生成并持久化"
                }}
              </dd>
            </dl>
            <p>
              将先核对原授权未扣款且全额仍冻结。仅服务端证明尚未发送的原请求才可首次发送；未知结果只查询同一请求。解除成功不代表商户退款或余额增加。
            </p>
            <div class="toolbar-actions">
              <button :disabled="busy" @click="confirmRelease = false">
                取消解除</button
              ><button
                class="danger-button"
                :disabled="busy || observationBusy || loading || !canRelease"
                @click="releaseAuthorization(false)"
              >
                确认核对并解除
              </button>
            </div>
          </div>
        </template>
        <template v-if="provider"
          ><h3>通道请求记录</h3>
          <dl class="record-detail">
            <template
              v-for="(label, key) in {
                channel_id: '通道 ID',
                method: '支付场景',
                state: '请求状态',
                material_type: '支付材料类型',
              }"
              :key="key"
              ><dt>{{ label }}</dt>
              <dd>{{ displayValue(provider[key]) }}</dd></template
            >
          </dl>
          <p class="muted">
            查询只核实通道结果，不会重新提交支付。结果未核实时请勿重复发起新的写操作。
          </p>
          <div class="toolbar-actions">
            <button :disabled="busy || loading" @click="perform('query')">
              查询支付通道</button
            ><button
              v-if="canClose"
              class="danger-button"
              :disabled="busy || loading"
              @click="confirmClose = true"
            >
              关闭通道订单
            </button>
          </div></template
        >
        <p v-else class="muted">该订单尚无可操作的支付通道请求记录。</p>
        <div v-if="confirmClose" class="alert">
          <p>
            确认关闭订单
            {{
              tradeNo
            }}？请核实买家尚未付款。通道结果不明时应继续查询，不应假定关单完成。
          </p>
          <div class="toolbar-actions">
            <button :disabled="busy" @click="confirmClose = false">
              取消关单</button
            ><button
              class="danger-button"
              :disabled="busy"
              @click="perform('close')"
            >
              {{ busy ? "正在处理…" : "确认关闭通道订单" }}
            </button>
          </div>
        </div></template
      >
      <OrderPrintingPanel
        v-if="order && client.area === 'merchant' && ownSession"
        :client="client"
        :trade-no="tradeNo"
        :own-session="!!ownSession"
      />
      <div class="modal-actions">
        <button
          v-if="
            order &&
            ['paid', 'partially_refunded'].includes(String(order.status))
          "
          :disabled="
            busy ||
            loading ||
            incomeBusy ||
            (incomeHold?.state === 'held' && !canRefundHeldIncome)
          "
          :title="
            incomeHold?.state === 'held' && !canRefundHeldIncome
              ? '历史冻结款须先由超级管理员核对专用退款来源'
              : ''
          "
          @click="
            (incomeHold?.state !== 'held' || canRefundHeldIncome) &&
            emit('refund', {
              ...order,
              income_status: incomeHold?.state,
              can_refund_held_income: canRefundHeldIncome,
            })
          "
        >
          申请退款</button
        ><button :disabled="busy || incomeBusy" @click="emit('close')">
          关闭详情
        </button>
      </div>
    </section>
  </div>
</template>
