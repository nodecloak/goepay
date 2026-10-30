<script lang="ts">
type BlacklistBody = {
  kind: "ip" | "buyer";
  expected_value: string;
  days: number;
  reason: string;
  external_no: string;
};
const blacklistIntents = new WeakMap<object, Map<string, BlacklistBody>>();
</script>
<script setup lang="ts">
import {
  ref,
  computed,
  onMounted,
  onBeforeUnmount,
  nextTick,
  watch,
} from "vue";
import { ApiClient, ApiError } from "../api";
const props = defineProps<{
  client: ApiClient;
  tradeNo: string;
  kind: "ip" | "buyer";
  expected: string;
  ownSession: boolean;
  adminClass?: string;
}>();
const emit = defineEmits<{ close: []; updated: [] }>();
const initialCSRF = props.client.csrfToken,
  key = JSON.stringify([initialCSRF, props.tradeNo, props.kind]);
let intents = blacklistIntents.get(props.client);
if (!intents) {
  intents = new Map();
  blacklistIntents.set(props.client, intents);
}
const pending = ref<BlacklistBody | null>(intents.get(key) || null),
  value = ref(pending.value?.expected_value || props.expected),
  days = ref(String(pending.value?.days ?? 0)),
  reason = ref(pending.value?.reason || ""),
  unknown = ref(!!pending.value),
  loading = ref(true),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  done = ref(false),
  invalid = ref(false),
  dialog = ref<HTMLElement>();
const label = computed(() => (props.kind === "ip" ? "IP地址" : "支付账号")),
  canWrite = computed(
    () =>
      props.client.area === "admin" &&
      props.ownSession &&
      ["super", "operator"].includes(props.adminClass || "") &&
      props.client.csrfToken === initialCSRF,
  );
const controller = new AbortController();
let alive = true;
const previousFocus = document.activeElement as HTMLElement | null;
function remember() {
  if (pending.value) intents!.set(key, { ...pending.value });
  else intents!.delete(key);
}
async function load() {
  try {
    const data = await props.client.request<{
      order: { trade_no: string };
      reporting?: Record<string, unknown>;
    }>(`/orders/${encodeURIComponent(props.tradeNo)}`, {
      signal: controller.signal,
    });
    if (!alive || controller.signal.aborted) return;
    const source =
      data.reporting?.[props.kind === "ip" ? "client_ip" : "buyer"];
    if (
      data.order?.trade_no !== props.tradeNo ||
      (!pending.value &&
        (typeof source !== "string" || !source || source !== props.expected))
    ) {
      invalid.value = true;
      error.value = "订单中的原支付来源已变化，请关闭并重新读取订单";
    }
  } catch (e) {
    if (alive && !controller.signal.aborted) {
      invalid.value = true;
      error.value = e instanceof Error ? e.message : "原订单读取失败";
    }
  } finally {
    if (alive && !controller.signal.aborted) {
      loading.value = false;
      await nextTick();
      dialog.value
        ?.querySelector<HTMLElement>(
          "input:not([disabled]):not([readonly]),textarea:not([disabled]),button:not([disabled])",
        )
        ?.focus();
    }
  }
}
async function confirm() {
  if (
    !canWrite.value ||
    busy.value ||
    loading.value ||
    done.value ||
    invalid.value
  )
    return;
  if (!pending.value) {
    if (
      !/^(0|[1-9][0-9]{0,4})$/.test(days.value) ||
      Number(days.value) > 36500
    ) {
      error.value = "有效期应为0至36500的整数天，0为永久";
      return;
    }
    pending.value = {
      kind: props.kind,
      expected_value: value.value,
      days: Number(days.value),
      reason: reason.value.trim(),
      external_no: crypto.randomUUID(),
    };
    remember();
  }
  busy.value = true;
  error.value = "";
  try {
    const result = await props.client.request<{
      replayed: boolean;
      enabled: boolean;
      expires_at: string;
    }>(`/orders/${encodeURIComponent(props.tradeNo)}/blacklist`, {
      method: "POST",
      body: pending.value,
      signal: controller.signal,
    });
    if (!alive || controller.signal.aborted) return;
    pending.value = null;
    remember();
    unknown.value = false;
    done.value = true;
    notice.value =
      (result.replayed ? "已找回原黑名单记录" : "已加入黑名单") +
      (result.enabled ? "" : "，原记录已停用");
    emit("updated");
  } catch (e) {
    if (!alive || controller.signal.aborted) return;
    if (
      e instanceof ApiError &&
      e.status >= 400 &&
      e.status < 500 &&
      e.status !== 408 &&
      e.status !== 429
    ) {
      pending.value = null;
      remember();
      unknown.value = false;
      invalid.value = true;
      error.value =
        e.status === 409 ? "订单来源或请求已变化，请重新读取订单" : e.message;
    } else {
      unknown.value = true;
      error.value = "操作结果未知，请核对原请求，不要重复创建或更改有效期";
    }
  } finally {
    if (alive) busy.value = false;
  }
}
function trap(event: KeyboardEvent) {
  if (event.key !== "Tab" || !dialog.value) return;
  const items = Array.from(
    dialog.value.querySelectorAll<HTMLElement>(
      "input:not([disabled]),textarea:not([disabled]),button:not([disabled])",
    ),
  );
  if (event.shiftKey && document.activeElement === items[0]) {
    event.preventDefault();
    items.at(-1)?.focus();
  } else if (!event.shiftKey && document.activeElement === items.at(-1)) {
    event.preventDefault();
    items[0]?.focus();
  }
}
watch(
  () => [props.client, props.client.csrfToken, props.ownSession],
  () => {
    controller.abort();
    invalid.value = true;
    busy.value = false;
    loading.value = false;
    error.value = "会话已变化，请关闭重新进入";
  },
);
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  controller.abort();
  if (previousFocus?.isConnected) previousFocus.focus();
});
</script>
<template>
  <div class="modal-backdrop" @keydown.esc="!busy && emit('close')">
    <section
      ref="dialog"
      class="modal panel blacklist-dialog"
      role="dialog"
      aria-modal="true"
      :aria-label="'添加黑名单 - ' + label"
      @keydown="trap"
    >
      <h2>添加黑名单 - {{ label }}</h2>
      <p class="muted">
        系统订单号：<span class="pa-order-no">{{ tradeNo }}</span
        >。将本订单的{{
          label
        }}加入全局支付黑名单；0天为永久，停用或删除可解除拦截。
      </p>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
      <p v-if="loading" role="status">正在核对原支付来源…</p>
      <label
        >{{ label }}<input :value="value" readonly autocomplete="off" /></label
      ><label
        >有效期（天）<input
          v-model="days"
          inputmode="numeric"
          maxlength="5"
          :disabled="loading || busy || unknown || done || invalid" /></label
      ><label
        >备注<textarea
          v-model="reason"
          maxlength="1000"
          placeholder="选填"
          :disabled="loading || busy || unknown || done || invalid"
        />
      </label>
      <div class="modal-actions">
        <button type="button" :disabled="busy" @click="emit('close')">
          {{ unknown ? "关闭并保留原请求" : "关闭" }}</button
        ><button
          v-if="!done"
          type="button"
          class="primary"
          :disabled="busy || loading || invalid || !canWrite"
          @click="confirm"
        >
          {{ busy ? "正在提交…" : unknown ? "核对原请求" : "确认拉黑" }}
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.blacklist-dialog {
  max-width: 580px;
  max-height: calc(100dvh - 32px);
  overflow: auto;
  overflow-wrap: anywhere;
}
.blacklist-dialog label {
  display: grid;
  gap: 8px;
}
.blacklist-dialog input,
.blacklist-dialog textarea {
  width: 100%;
  min-width: 0;
}
.blacklist-dialog textarea {
  min-height: 72px;
}
.modal-actions {
  flex-wrap: wrap;
}
</style>
