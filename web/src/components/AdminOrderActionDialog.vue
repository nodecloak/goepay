<script lang="ts">
const pendingOrderIntents = new WeakMap<
  object,
  Map<string, Readonly<Record<string, unknown>>>
>();
</script>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiClient, validAmount } from "../api";
import { orderRefundRemaining104 } from "../order-charge104";
import type { OrderActionID } from "../order-actions";
import { centsMoney } from "../settlement-terms";
import OrderIncomePanel from "./OrderIncomePanel.vue";
import NotificationDeliveryPanel from "./NotificationDeliveryPanel.vue";
const props = defineProps<{
  client: ApiClient;
  tradeNo: string;
  action: OrderActionID;
}>();
const emit = defineEmits<{
  close: [];
  updated: [];
  refund: [row: Record<string, unknown>];
}>();
interface Detail {
  charge_verified104?: boolean;
  order: Record<string, unknown>;
  reporting?: Record<string, unknown>;
  admin_control?: { status: string; archived: boolean; revision: string };
  income_hold?: Record<string, unknown> | null;
  income_freeze_amount?: unknown;
  can_freeze_income?: boolean;
  can_unfreeze_income?: boolean;
  can_refund_held_income?: boolean;
  income_refund_pending?: boolean;
  can_resend_notification?: boolean;
  can_manual_refund?: boolean;
  notification?: Record<string, unknown>;
}
const data = ref<Detail | null>(null),
  loading = ref(true),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  reason = ref(""),
  amount = ref(""),
  ack = ref(false),
  locked = ref(false),
  done = ref(false);
const controller = new AbortController(),
  key = props.tradeNo + "/" + props.action;
const dialogElement = ref<HTMLElement>();
const previousFocus =
  typeof document !== "undefined"
    ? (document.activeElement as HTMLElement | null)
    : null;
function trapFocus(event: KeyboardEvent) {
  if (event.key !== "Tab" || !dialogElement.value) return;
  const items = Array.from(
    dialogElement.value.querySelectorAll<HTMLElement>(
      "input:not([disabled]):not([readonly]),select:not([disabled]),textarea:not([disabled]),button:not([disabled]),a[href]",
    ),
  );
  if (!items.length) return;
  const first = items[0],
    last = items[items.length - 1];
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
let store = pendingOrderIntents.get(props.client);
if (!store) {
  store = new Map();
  pendingOrderIntents.set(props.client, store);
}
let originalBody = store.get(key),
  externalNo =
    typeof originalBody?.external_no === "string"
      ? originalBody.external_no
      : crypto.randomUUID();
const titles: Record<OrderActionID, string> = {
  "mark-paid": "改已完成确认",
  "mark-unpaid": "改未完成确认",
  "api-refund": "API退款确认",
  "manual-refund": "手动退款确认",
  freeze: "冻结订单确认",
  unfreeze: "解冻订单确认",
  notify: "重新通知确认",
  fill: "手动补单确认",
  delete: "删除订单确认",
  restore: "恢复订单确认",
};
const title = computed(() => titles[props.action]);
const ordinary = computed(
  () => !["api-refund", "freeze", "unfreeze", "notify"].includes(props.action),
);
const manualBlocked = computed(
  () =>
    props.action === "manual-refund" && data.value?.can_manual_refund !== true,
);
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const result = await props.client.request<Detail>(
      `/orders/${encodeURIComponent(props.tradeNo)}`,
      { signal: controller.signal },
    );
    if (controller.signal.aborted) return;
    if (typeof result.charge_verified104 === "boolean")
      result.order = {
        ...result.order,
        charge_verified104: result.charge_verified104,
      };
    data.value = result;
    if (!amount.value) {
      const remaining = orderRefundRemaining104({
        ...result.order,
        refunded_amount: result.reporting?.refunded_amount ?? "0",
      });
      if (remaining !== undefined)
        amount.value = centsMoney(remaining.toString());
    }
    if (props.action === "api-refund" && result.order.status !== "paid") {
      error.value =
        "当前只有后台状态标记，尚无已核实的支付入账，不能进行 API 退款。";
      return;
    }
    if (props.action === "api-refund" && result.charge_verified104 === false) {
      error.value = "实际支付金额无法核对，请重新读取原订单";
      return;
    }
    if (props.action === "api-refund")
      emit("refund", {
        ...result.order,
        ...result.reporting,
        income_status: result.income_hold?.state,
        can_refund_held_income: result.can_refund_held_income,
      });
  } catch (e) {
    if (!controller.signal.aborted)
      error.value = e instanceof Error ? e.message : "订单读取失败";
  } finally {
    if (!controller.signal.aborted) {
      loading.value = false;
      await nextTick();
      if (!controller.signal.aborted)
        dialogElement.value
          ?.querySelector<HTMLElement>(
            "input:not([disabled]):not([readonly]),textarea:not([disabled]),button:not([disabled])",
          )
          ?.focus();
    }
  }
}
if (originalBody) {
  reason.value = String(originalBody.reason ?? "");
  if (typeof originalBody.amount === "string")
    amount.value = originalBody.amount;
  ack.value = originalBody.offline_confirmed === true;
  locked.value = true;
}
async function submit() {
  if (
    busy.value ||
    loading.value ||
    !data.value ||
    manualBlocked.value ||
    done.value
  )
    return;
  if (!reason.value.trim()) {
    error.value = "请填写操作原因";
    return;
  }
  if (
    props.action === "manual-refund" &&
    (!validAmount(amount.value) || !ack.value)
  ) {
    error.value = "请核对退款金额并确认已完成线下退款";
    return;
  }
  let endpoint =
    props.action === "mark-paid" || props.action === "mark-unpaid"
      ? "admin-status"
      : props.action === "delete"
        ? "archive"
        : props.action === "fill"
          ? "manual-complete"
          : props.action;
  if (!originalBody) {
    const body: Record<string, unknown> = {
      external_no: externalNo,
      reason: reason.value.trim(),
    };
    if (props.action === "manual-refund") {
      body.amount = amount.value;
      body.offline_confirmed = ack.value;
    } else {
      body.expected_revision = data.value.admin_control?.revision ?? "0";
      if (endpoint === "admin-status")
        body.status = props.action === "mark-paid" ? "paid" : "pending";
    }
    originalBody = Object.freeze(body);
    store!.set(key, originalBody);
  }
  locked.value = true;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const result = await props.client.requestResult<Record<string, unknown>>(
      `/orders/${encodeURIComponent(props.tradeNo)}/${endpoint}`,
      { method: "POST", body: originalBody, signal: controller.signal },
    );
    if (controller.signal.aborted) return;
    if (
      props.action === "fill" &&
      (result.status === 202 || result.data?.completed !== true)
    ) {
      notice.value =
        "原通道尚未证实支付成功，未补记完成或入账。保留本请求号核对后重试。";
      return;
    }
    if (
      props.action === "manual-refund" &&
      (result.data?.offline !== true ||
        result.data?.provider_refund_sent !== false)
    ) {
      throw new Error("手动退款结果尚未核实，请保留原请求号查询");
    }
    done.value = true;
    store!.delete(key);
    notice.value =
      props.action === "manual-refund"
        ? "手动退款已记账。支付通道未发起退款，线下退还由管理员确认。"
        : props.action === "fill"
          ? "原订单支付已核实，入账与后台标记已恢复。"
          : props.action === "delete"
            ? "订单已从后台列表删除，账务、支付事实和审计仍保留，可在已删除订单中恢复。"
            : props.action === "restore"
              ? "订单已恢复显示。"
              : "后台状态标记已更新，通道支付事实和账本未改变。";
    emit("updated");
  } catch (e) {
    if (!controller.signal.aborted)
      error.value = `${e instanceof Error ? e.message : "操作结果尚未核实"}。请保留原请求号重试，勿重复创建。`;
  } finally {
    busy.value = false;
  }
}
async function panelUpdated() {
  emit("updated");
  await load();
}
onMounted(load);
onBeforeUnmount(() => {
  controller.abort();
  if (
    previousFocus?.isConnected &&
    dialogElement.value?.contains(document.activeElement)
  )
    previousFocus.focus();
});
</script>
<template>
  <div class="modal-backdrop" @keydown.esc="!busy && emit('close')">
    <section
      ref="dialogElement"
      class="modal panel order-action-dialog"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
      @keydown="trapFocus"
    >
      <h2>{{ title }}</h2>
      <p class="muted">
        系统订单号：<span class="pa-order-no">{{ tradeNo }}</span>
      </p>
      <div v-if="loading" role="status">正在读取原订单…</div>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
      <template v-if="data && !loading">
        <OrderIncomePanel
          v-if="action === 'freeze' || action === 'unfreeze'"
          :key="tradeNo + '/' + action"
          :client="client"
          :trade-no="tradeNo"
          :hold="data.income_hold"
          :freeze-amount="data.income_freeze_amount"
          :can-freeze="data.can_freeze_income === true"
          :can-unfreeze="data.can_unfreeze_income === true"
          :has-pending-refund="data.income_refund_pending"
          :initial-action="action"
          @busy="busy = $event"
          @updated="panelUpdated"
        />
        <NotificationDeliveryPanel
          v-else-if="action === 'notify'"
          :client="client"
          :trade-no="tradeNo"
          :order-status="data.order.status"
          :allowed="data.can_resend_notification === true"
          :notification="data.notification"
          :initial-confirm="true"
          @busy="busy = $event"
          @updated="panelUpdated"
        />
        <form v-else-if="ordinary" @submit.prevent="submit">
          <p
            v-if="action === 'mark-paid' || action === 'mark-unpaid'"
            class="alert"
          >
            仅修改后台订单标记，不增减商户余额，也不把标记作为通道付款凭证。核实通道并补齐入账请使用“手动补单”。
          </p>
          <p v-if="action === 'fill'" class="alert">
            核对原支付通道，只有确认原单已支付才补齐入账与后台标记；未知结果不会改成完成，也不会重新扣款。
          </p>
          <p v-if="action === 'delete'" class="alert">
            确认从后台订单列表删除此订单？账务、退款和支付查询依据保留，可通过“显示已删除订单”恢复。
          </p>
          <p v-if="action === 'restore'" class="alert">
            恢复原订单在后台列表中的显示，不新增订单或资金流水。
          </p>
          <template v-if="action === 'manual-refund'"
            ><p class="alert">
              此操作从原订单资金中结转退款，不调用微信或支付宝退款接口。请确认已在线下退还买家。
            </p>
            <p v-if="manualBlocked" role="alert">
              仅本人超级管理员可处理已支付订单的手动退款。
            </p>
            <label
              >退款金额（元）<input
                v-model="amount"
                inputmode="decimal"
                required
                :disabled="locked || busy || manualBlocked" /></label
            ><label class="offline-confirm"
              ><input
                v-model="ack"
                type="checkbox"
                :disabled="locked || busy || manualBlocked"
              />我确认已完成线下退款，本次只记录账务，不向支付通道发起退款</label
            ></template
          >
          <label
            >操作原因<input
              v-model="reason"
              required
              maxlength="80"
              :disabled="locked || busy || done"
          /></label>
          <p class="muted">操作请求号：{{ externalNo }}</p>
          <div class="modal-actions">
            <button type="button" :disabled="busy" @click="emit('close')">
              {{ done ? "关闭" : "取消" }}</button
            ><button
              v-if="!done"
              class="primary"
              :disabled="busy || manualBlocked"
            >
              {{
                busy
                  ? "正在处理…"
                  : locked
                    ? "重试原请求"
                    : "确认" +
                      (action === "delete"
                        ? "删除订单"
                        : action === "restore"
                          ? "恢复订单"
                          : action === "manual-refund"
                            ? "手动退款"
                            : action === "fill"
                              ? "手动补单"
                              : "修改状态")
              }}
            </button>
          </div>
        </form>
      </template>
      <div v-if="!ordinary || !data" class="modal-actions">
        <button type="button" :disabled="busy" @click="emit('close')">
          关闭
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.order-action-dialog {
  max-width: 600px;
  max-height: calc(100dvh - 32px);
  overflow: auto;
  min-width: 0;
  overflow-wrap: anywhere;
}
.order-action-dialog input:not([type="checkbox"]) {
  width: 100%;
  box-sizing: border-box;
}
.offline-confirm {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin: 14px 0;
}
.offline-confirm input {
  flex: none;
  margin-top: 4px;
}
.modal-actions {
  flex-wrap: wrap;
}
</style>
