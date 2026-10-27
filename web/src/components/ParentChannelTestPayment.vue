<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ApiError, type ApiClient } from "../api";
import {
  parentPaymentTestPath,
  parentTestMoney,
  readParentPaymentTest,
  type ParentPaymentTest,
} from "../parent-channel-test";
const props = defineProps<{
  client: ApiClient;
  allowed: boolean;
  channelID: string;
  fingerprint: string;
}>();
const emit = defineEmits<{ close: [] }>();
const name = ref("支付测试"),
  money = ref("1"),
  busy = ref(false),
  unknown = ref(false),
  error = ref(""),
  result = ref<ParentPaymentTest>(),
  modal = ref<HTMLElement>();
const originalClient = props.client,
  originalCSRF = props.client.csrfToken,
  originalID = props.channelID,
  originalFP = props.fingerprint,
  requestID = crypto.randomUUID().replaceAll("-", "");
let alive = true,
  epoch = 0,
  abort: AbortController | undefined;
const opener =
  document.activeElement instanceof HTMLElement
    ? document.activeElement
    : undefined;
function drop() {
  ++epoch;
  abort?.abort();
  result.value = undefined;
  name.value = "";
  money.value = "";
  error.value = "";
  unknown.value = false;
  busy.value = false;
}
function foreground() {
  if (
    !alive ||
    !props.allowed ||
    props.client !== originalClient ||
    props.client.csrfToken !== originalCSRF ||
    props.channelID !== originalID ||
    props.fingerprint !== originalFP
  ) {
    drop();
    emit("close");
    return false;
  }
  return true;
}
function current(n: number) {
  return foreground() && n === epoch;
}
function close() {
  if (!foreground()) return;
  if (busy.value || unknown.value) {
    error.value = "结果未知时请先读取原请求，勿重新提交。";
    return;
  }
  drop();
  emit("close");
  void nextTick(() => opener?.isConnected && opener.focus());
}
function failure(e: unknown) {
  return e instanceof Error ? e.message : "原请求无法核实";
}
async function run(kind: "create" | "read" | "query") {
  if (
    !foreground() ||
    busy.value ||
    (kind === "create" && (unknown.value || result.value)) ||
    (kind === "query" && !result.value?.can_query)
  )
    return;
  let expectedName = name.value.trim(),
    expectedMoney = "";
  try {
    expectedMoney = parentTestMoney(money.value);
  } catch (e) {
    error.value = failure(e);
    return;
  }
  const n = ++epoch;
  abort?.abort();
  abort = new AbortController();
  busy.value = true;
  error.value = "";
  try {
    const path =
      parentPaymentTestPath(
        originalID,
        kind === "create" ? undefined : requestID,
      ) + (kind === "query" ? "/query" : "");
    const raw = await originalClient.request(path, {
      method: kind === "read" ? "GET" : "POST",
      signal: abort.signal,
      body:
        kind === "create"
          ? {
              request_id: requestID,
              name: expectedName,
              money: expectedMoney,
              expected_fingerprint: originalFP,
            }
          : kind === "query"
            ? {}
            : undefined,
    });
    if (!current(n)) return;
    const row = readParentPaymentTest(raw, originalID, requestID);
    if (
      row.name !== expectedName ||
      row.money !== expectedMoney ||
      (result.value &&
        (row.out_trade_no !== result.value.out_trade_no ||
          row.merchant_id !== result.value.merchant_id ||
          (result.value.trade_no !== "" &&
            row.trade_no !== result.value.trade_no)))
    )
      throw new Error("原支付目标不匹配");
    result.value = row;
    unknown.value = false;
  } catch (e) {
    if (current(n)) {
      if (e instanceof ApiError && [401, 403].includes(e.status)) {
        drop();
        emit("close");
        return;
      }
      if (kind === "create")
        unknown.value = !(
          e instanceof ApiError &&
          e.responseDecoded &&
          e.status >= 400 &&
          e.status < 500
        );
      if (
        kind === "read" &&
        e instanceof ApiError &&
        e.responseDecoded &&
        e.status === 404
      ) {
        unknown.value = false;
        error.value = "已确认原请求未创建，请重新确认原通道后操作。";
      } else
        error.value = unknown.value
          ? "结果未知：请读取原请求，勿重复创建。"
          : failure(e);
    }
  } finally {
    if (current(n)) busy.value = false;
  }
}
function keyboard(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    close();
  }
  if (e.key === "Tab") {
    const nodes = modal.value?.querySelectorAll<HTMLElement>(
      "button:not([disabled]),input:not([disabled]),a[href]",
    );
    if (nodes?.length) {
      const first = nodes[0],
        last = nodes[nodes.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.channelID,
    props.fingerprint,
    props.allowed,
  ],
  () => {
    if (!foreground()) return;
  },
  { flush: "sync" },
);
onMounted(() => {
  if (foreground())
    modal.value?.querySelector<HTMLInputElement>("input")?.focus();
});
onBeforeUnmount(() => {
  alive = false;
  drop();
});
const stateName = (state: string) =>
  ({
    blocked: "尚未进入支付",
    prepared: "订单已准备，未获得发送机会",
    processing: "正在核实原支付",
    unknown: "支付结果待核实",
    pending: "等待付款",
    succeeded: "已完成",
    failed: "失败",
    paid: "已支付",
    closed: "已关闭",
  })[state] || "待核实";
</script>
<template>
  <div class="modal-backdrop">
    <section
      ref="modal"
      role="dialog"
      aria-modal="true"
      aria-label="测试支付"
      tabindex="-1"
      class="modal panel parent-payment-test"
      @keydown="keyboard"
      @pointerdown="foreground"
      @focusin="foreground"
    >
      <div class="toolbar">
        <h3>测试支付</h3>
        <button :disabled="busy || unknown" @click="close">关闭</button>
      </div>
      <p class="muted">通道 #{{ channelID }} · 使用已配置的测试收款商户</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <form @submit.prevent="run('create')">
        <label
          >订单名称<input
            v-model="name"
            aria-label="订单名称"
            maxlength="256"
            :disabled="busy || unknown || !!result"
            required /></label
        ><label
          >订单金额<input
            v-model="money"
            aria-label="订单金额"
            inputmode="decimal"
            :disabled="busy || unknown || !!result"
            required
        /></label>
        <div class="toolbar">
          <button type="button" :disabled="busy || unknown" @click="close">
            取消</button
          ><button
            data-parent-test-submit
            type="submit"
            :disabled="busy || unknown || !!result"
          >
            {{ busy ? "正在核实…" : "确定" }}
          </button>
        </div>
      </form>
      <div v-if="unknown" class="test-result">
        <p>原请求号：{{ requestID }}</p>
        <button data-parent-test-read :disabled="busy" @click="run('read')">
          读取原请求
        </button>
      </div>
      <div v-if="result" class="test-result" aria-live="polite">
        <p>{{ stateName(result.state) }}</p>
        <p v-if="result.trade_no">平台订单号：{{ result.trade_no }}</p>
        <p>{{ result.name }} · ¥{{ result.money }}</p>
        <a
          v-if="result.checkout_url"
          :href="result.checkout_url"
          target="_blank"
          rel="noopener noreferrer"
          >打开支付页面</a
        ><button :disabled="busy || !result.can_query" @click="run('query')">
          查询原订单</button
        ><button data-parent-test-read :disabled="busy" @click="run('read')">
          读取原请求
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.parent-payment-test {
  width: min(400px, calc(100vw - 24px));
  max-height: calc(100dvh - 32px);
  overflow: auto;
}
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.parent-payment-test label {
  display: grid;
  gap: 6px;
  margin: 16px 0;
}
.parent-payment-test input {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.test-result {
  border-top: 1px solid var(--border, #e5e7eb);
  margin-top: 16px;
  padding-top: 12px;
  overflow-wrap: anywhere;
}
.test-result button,
.test-result a {
  display: inline-block;
  margin: 6px 10px 6px 0;
}
</style>
