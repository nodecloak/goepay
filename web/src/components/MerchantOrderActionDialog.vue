<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, nextTick } from "vue";
import { ApiClient } from "../api";
import NotificationDeliveryPanel from "./NotificationDeliveryPanel.vue";
import OrderPrintingPanel from "./OrderPrintingPanel.vue";
const props = defineProps<{
  client: ApiClient;
  tradeNo: string;
  action: "query" | "notify" | "refund" | "print";
  ownSession: boolean;
}>();
const emit = defineEmits<{
  close: [];
  updated: [];
  refund: [row: Record<string, unknown>];
  busy: [value: boolean];
}>();
const loading = ref(true),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  allowed = ref(false),
  executed = ref(false),
  dialog = ref<HTMLElement>();
const order = ref<Record<string, unknown> | null>(null),
  notification = ref<Record<string, unknown> | null>(null);
const controller = new AbortController(),
  client = props.client,
  csrf = client.csrfToken,
  trade = props.tradeNo;
let alive = true;
const valid = () =>
  alive &&
  !controller.signal.aborted &&
  props.ownSession &&
  props.client === client &&
  client.csrfToken === csrf &&
  props.tradeNo === trade;
const title = {
  query: "手动补单",
  notify: "重新通知",
  refund: "核对退款资格",
  print: "打印小票",
};
function setBusy(value: boolean) {
  busy.value = value;
  emit("busy", value);
  if (!value)
    void nextTick(() => {
      if (
        valid() &&
        dialog.value &&
        !dialog.value.contains(document.activeElement)
      )
        dialog.value
          .querySelector<HTMLElement>("button:not([disabled])")
          ?.focus();
    });
}
function close() {
  if (!busy.value) emit("close");
}
async function load() {
  try {
    const data = await client.request<{
      order: Record<string, unknown>;
      charge_verified104?: boolean;
      reporting?: Record<string, unknown>;
      merchant_actions?: Record<string, unknown>;
      notification?: Record<string, unknown>;
      income_hold?: { state?: string };
      can_refund_held_income?: boolean;
    }>(`/orders/${encodeURIComponent(trade)}`, { signal: controller.signal });
    if (!valid()) return;
    if (data.order.trade_no !== trade)
      throw Error("原订单不一致，请重新读取列表");
    order.value = {
      ...data.order,
      ...(typeof data.charge_verified104 === "boolean"
        ? { charge_verified104: data.charge_verified104 }
        : {}),
      income_status: data.income_hold?.state === "held" ? "held" : "none",
      can_refund_held_income: data.can_refund_held_income === true,
    };
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
    notification.value = data.notification || null;
    allowed.value =
      data.merchant_actions?.[props.action] === true &&
      data.order.status === (props.action === "query" ? "pending" : "paid");
    if (!allowed.value) throw Error("当前服务端未允许此操作，请重新读取订单");
    if (props.action === "refund") emit("refund", order.value);
  } catch (e) {
    if (valid())
      error.value = e instanceof Error ? e.message : "原订单读取失败";
  } finally {
    if (valid()) {
      loading.value = false;
      await nextTick();
      dialog.value?.querySelector<HTMLElement>("button")?.focus();
    }
  }
}
async function query() {
  if (
    !valid() ||
    busy.value ||
    loading.value ||
    !allowed.value ||
    executed.value
  )
    return;
  executed.value = true;
  setBusy(true);
  error.value = "";
  try {
    const response = await client.requestResult<Record<string, unknown>>(
      `/orders/${encodeURIComponent(trade)}/query`,
      { method: "POST", body: {}, signal: controller.signal },
    );
    if (!valid()) return;
    if (response.data?.trade_no !== trade) throw Error("查询响应原订单不一致");
    notice.value =
      response.status === 200 && response.data?.status === "paid"
        ? "原通道已核实支付成功，补单完成。"
        : "原通道查询已完成，请核对订单当前状态；本操作没有发起新的支付。";
    emit("updated");
  } catch (e) {
    if (valid())
      error.value =
        "原订单查询结果尚未核实；没有发起新的支付，请重新读取订单后核对。";
  } finally {
    if (valid()) setBusy(false);
  }
}
function trap(event: KeyboardEvent) {
  if (event.key !== "Tab" || !dialog.value) return;
  const all = Array.from(
    dialog.value.querySelectorAll<HTMLElement>(
      "button:not([disabled]),input:not([disabled]),a[href]",
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
function modalKey(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    close();
    return;
  }
  if (
    event.key === "Tab" &&
    dialog.value &&
    !dialog.value.contains(document.activeElement)
  ) {
    const all = Array.from(
      dialog.value.querySelectorAll<HTMLElement>(
        "button:not([disabled]),input:not([disabled]),a[href]",
      ),
    );
    event.preventDefault();
    (event.shiftKey ? all.at(-1) : all[0])?.focus();
  }
}
watch(
  () => [props.client, props.client.csrfToken, props.ownSession, props.tradeNo],
  () => {
    controller.abort();
    emit("busy", false);
    emit("close");
  },
);
onMounted(() => {
  document.addEventListener("keydown", modalKey, true);
  void load();
});
onBeforeUnmount(() => {
  document.removeEventListener("keydown", modalKey, true);
  alive = false;
  controller.abort();
  emit("busy", false);
});
</script>
<template>
  <div class="modal-backdrop" @keydown.esc="close">
    <section
      ref="dialog"
      class="modal panel merchant-order-dialog"
      role="dialog"
      aria-modal="true"
      :aria-label="title[action]"
      @keydown="trap"
    >
      <h2>{{ title[action] }}</h2>
      <p class="original-order-number">
        原订单：<span class="pa-order-no">{{ tradeNo }}</span>
      </p>
      <p v-if="loading" role="status">正在核对原订单权限与状态…</p>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
      <template v-if="!loading && allowed && order">
        <template v-if="action === 'query'"
          ><p>
            将仅查询该订单原支付通道。只有通道已验证支付成功时才补单；不会强制标记支付，也不会发起新的付款。未知结果请读取原订单核对。
          </p>
          <button
            v-if="!executed"
            class="primary"
            :disabled="busy"
            @click="query"
          >
            确认查询原订单
          </button></template
        >
        <NotificationDeliveryPanel
          v-else-if="action === 'notify'"
          :client="client"
          :trade-no="tradeNo"
          :order-status="order.status"
          :allowed="allowed"
          :notification="notification"
          :initial-confirm="true"
          @busy="setBusy"
          @updated="emit('updated')"
        />
        <OrderPrintingPanel
          v-else-if="action === 'print'"
          :client="client"
          :trade-no="tradeNo"
          :own-session="ownSession"
          @busy="setBusy"
        />
      </template>
      <div class="modal-actions">
        <button :disabled="busy" @click="close">关闭</button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.merchant-order-dialog {
  max-height: calc(100vh - 32px);
  overflow-y: auto;
  max-width: 680px;
}
.original-order-number {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
</style>
