<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { ApiClient, validAmount } from "../api";
import { nativeClient202, nativeScan202 } from "../native-scan202";

interface CashierResult {
  request_id: string;
  trade_no: string;
  money: string;
  charge_money: string;
  net_money: string;
  fee_money: string;
  remark: string;
  type: string;
  state: string;
  can_apply: boolean;
  can_query: boolean;
}
const props = defineProps<{
  client: ApiClient;
  ownSession: boolean;
  merchantID: string;
}>();
const amount = ref(""),
  remark = ref(""),
  draft = ref(""),
  editing = ref(false),
  scanning = ref(false),
  code = ref("");
const busy = ref(false),
  nativeBusy = ref(false),
  ready = ref(false),
  error = ref(""),
  pending = ref(""),
  result = ref<CashierResult | null>(null);
const video = ref<HTMLVideoElement | null>(null),
  remarkInput = ref<HTMLInputElement | null>(null);
const initialCSRF = props.client.csrfToken,
  storageKey = "goepay.scan192." + props.merchantID;
const terminal = computed(
  () =>
    !!result.value && ["paid", "closed", "failed"].includes(result.value.state),
);
const canStart = computed(
  () =>
    props.ownSession &&
    ready.value &&
    !busy.value &&
    !nativeBusy.value &&
    !pending.value &&
    props.client.csrfToken === initialCSRF,
);
let alive = true,
  epoch = 0,
  controller: AbortController | undefined,
  stream: MediaStream | undefined,
  frame = 0;
let scanEpoch202 = 0,
  scanController202: AbortController | undefined;
const keys = ["1", "2", "3", "4", "5", "6", "7", "8", "9", ".", "0", "⌫"];
function keypad(key: string) {
  if (!canStart.value) return;
  if (key === "⌫") {
    amount.value = amount.value.slice(0, -1);
    return;
  }
  let next = amount.value + key;
  if (key === "." && !amount.value) next = "0.";
  if (/^\d{0,6}(?:\.\d{0,2})?$/.test(next))
    amount.value = next.replace(/^0+(?=\d)/, "");
}
function stopCamera() {
  cancelAnimationFrame(frame);
  stream?.getTracks().forEach((t) => t.stop());
  stream = undefined;
  if (video.value) video.value.srcObject = null;
}
function cancelScan() {
  cancelNative202();
  stopCamera();
  scanning.value = false;
  code.value = "";
}
function cancelNative202() {
  scanEpoch202++;
  scanController202?.abort();
  scanController202 = undefined;
  nativeBusy.value = false;
}
async function startScan202() {
  if (!canStart.value || !validAmount(amount.value)) return;
  cancelNative202();
  scanning.value = true;
  code.value = "";
  error.value = "";
  if (nativeClient202() === "browser") return;
  const client = props.client,
    csrf = client.csrfToken,
    merchant = props.merchantID,
    run = scanEpoch202,
    ac = new AbortController();
  scanController202 = ac;
  nativeBusy.value = true;
  const current = () =>
    alive &&
    run === scanEpoch202 &&
    !ac.signal.aborted &&
    props.client === client &&
    client.csrfToken === csrf &&
    props.merchantID === merchant &&
    props.ownSession &&
    scanning.value &&
    !pending.value;
  try {
    const value = await nativeScan202(client, ac.signal);
    if (!current()) return;
    nativeBusy.value = false;
    if (value === null) return;
    code.value = value.trim();
    await prepare();
  } catch (e) {
    if (current())
      error.value =
        e instanceof Error ? e.message : "原生扫码未完成，可输入付款码";
  } finally {
    if (alive && run === scanEpoch202) nativeBusy.value = false;
  }
}
function remember(key: string) {
  try {
    sessionStorage.setItem(storageKey, key);
    pending.value = key;
    return true;
  } catch {
    code.value = "";
    error.value = "无法安全保存原请求号，请检查浏览器存储设置后重试";
    return false;
  }
}
function readResult(value: CashierResult) {
  if (
    !value ||
    value.request_id !== pending.value ||
    ![
      "unprepared",
      "prepared",
      "created",
      "submitting",
      "pending",
      "unknown",
      "paid",
      "closed",
      "failed",
    ].includes(value.state) ||
    typeof value.can_apply !== "boolean" ||
    typeof value.can_query !== "boolean" ||
    ![value.money, value.charge_money, value.net_money, value.fee_money].every(
      (x) => typeof x === "string" && /^\d+\.\d{2}$/.test(x),
    )
  )
    throw new Error("原收款结果无法确认，请继续恢复同一请求");
  result.value = value;
  if (terminal.value) {
    code.value = "";
    cancelScan();
  }
}
async function perform(task: (signal: AbortSignal) => Promise<void>) {
  if (busy.value || !props.ownSession || props.client.csrfToken !== initialCSRF)
    return;
  busy.value = true;
  error.value = "";
  const run = ++epoch,
    ac = new AbortController();
  controller = ac;
  try {
    await task(ac.signal);
  } catch (e) {
    if (alive && run === epoch)
      error.value =
        e instanceof Error ? e.message : "原收款结果未知，请恢复同一请求";
  } finally {
    if (alive && run === epoch) busy.value = false;
  }
}
async function prepare() {
  if (
    !canStart.value ||
    !validAmount(amount.value) ||
    Number(amount.value) > 999999.99
  )
    return;
  if (!/^(?:1[0-5]\d{16}|(?:2[5-9]|30)\d{14,22})$/.test(code.value)) {
    error.value = "请输入微信或支付宝付款码";
    return;
  }
  cancelNative202();
  stopCamera();
  const key = crypto.randomUUID();
  if (!remember(key)) return;
  const body = {
    request_id: key,
    money: Number(amount.value).toFixed(2),
    remark: remark.value,
    auth_code: code.value,
  };
  await perform(async (signal) => {
    const v = await props.client.request<CashierResult>("/scanpay/prepare", {
      method: "POST",
      body,
      signal,
    });
    if (alive && !signal.aborted) {
      readResult(v);
      scanning.value = false;
    }
  });
}
async function apply() {
  if (!result.value?.can_apply || !code.value || !pending.value) return;
  const key = pending.value,
    body = { auth_code: code.value };
  // Clear the input before I/O. A lost response permits original reads/queries,
  // never another Scan request or a replacement bearer code.
  code.value = "";
  await perform(async (signal) => {
    const v = await props.client.request<CashierResult>(
      `/scanpay/intents/${key}/apply`,
      { method: "POST", body, signal },
    );
    if (alive && !signal.aborted) readResult(v);
  });
}
async function recover(query = false) {
  if (!pending.value) return;
  code.value = "";
  cancelScan();
  const path = `/scanpay/intents/${pending.value}` + (query ? "/query" : "");
  await perform(async (signal) => {
    const v = await props.client.request<CashierResult>(
      path,
      query ? { method: "POST", body: {}, signal } : { signal },
    );
    if (alive && !signal.aborted) readResult(v);
  });
}
async function close() {
  if (!pending.value || result.value?.can_query) return;
  code.value = "";
  cancelScan();
  await perform(async (signal) => {
    const v = await props.client.request<CashierResult>(
      `/scanpay/intents/${pending.value}/close`,
      { method: "POST", body: {}, signal },
    );
    if (alive && !signal.aborted) readResult(v);
  });
}
function reset() {
  if (!terminal.value || busy.value) return;
  sessionStorage.removeItem(storageKey);
  pending.value = "";
  result.value = null;
  amount.value = "";
  remark.value = "";
  error.value = "";
  code.value = "";
}
function editRemark() {
  if (!canStart.value) return;
  draft.value = remark.value;
  editing.value = true;
  void nextTick(() => remarkInput.value?.focus());
}
async function camera() {
  if (!canStart.value || !scanning.value) return;
  cancelNative202();
  error.value = "";
  stopCamera();
  type Detector = {
    detect: (v: HTMLVideoElement) => Promise<{ rawValue: string }[]>;
  };
  const DetectorClass = (
    window as unknown as {
      BarcodeDetector?: new (o: { formats: string[] }) => Detector;
    }
  ).BarcodeDetector;
  if (!DetectorClass || !navigator.mediaDevices?.getUserMedia) {
    error.value = "当前浏览器不支持摄像头识别，可使用扫码枪或输入付款码";
    return;
  }
  const run = epoch,
    cameraRun202 = scanEpoch202;
  try {
    const acquired = await navigator.mediaDevices.getUserMedia({
      video: { facingMode: "environment" },
      audio: false,
    });
    if (
      !alive ||
      !scanning.value ||
      run !== epoch ||
      cameraRun202 !== scanEpoch202
    ) {
      acquired.getTracks().forEach((t) => t.stop());
      return;
    }
    stream = acquired;
    await nextTick();
    if (!video.value) {
      stopCamera();
      return;
    }
    video.value.srcObject = stream;
    await video.value.play();
    const detector = new DetectorClass({
      formats: ["qr_code", "code_128", "ean_13"],
    });
    const currentStream202 = stream;
    const scan = async () => {
      if (
        !alive ||
        !scanning.value ||
        !stream ||
        !video.value ||
        cameraRun202 !== scanEpoch202 ||
        stream !== currentStream202
      )
        return;
      try {
        const rows = await detector.detect(video.value);
        if (
          !alive ||
          !scanning.value ||
          cameraRun202 !== scanEpoch202 ||
          stream !== currentStream202
        )
          return;
        if (rows.length) {
          code.value = rows[0].rawValue;
          stopCamera();
          return;
        }
      } catch {
        error.value = "识别失败，请输入付款码";
        stopCamera();
        return;
      }
      frame = requestAnimationFrame(() => void scan());
    };
    void scan();
  } catch {
    stopCamera();
    if (alive && run === epoch && cameraRun202 === scanEpoch202)
      error.value = "无法打开摄像头，可使用扫码枪或输入付款码";
  }
}
onMounted(async () => {
  try {
    const saved = sessionStorage.getItem(storageKey);
    if (saved && /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(saved))
      pending.value = saved;
  } catch {
    error.value = "浏览器无法保存原请求号，暂不可发起收款";
    return;
  }
  const ac = new AbortController(),
    run = epoch;
  controller = ac;
  try {
    const cap = await props.client.request<{
      can_prepare: boolean;
      platform_open: boolean;
      pay_types: string[];
    }>("/scanpay/capabilities", { signal: ac.signal });
    if (alive && !ac.signal.aborted && run === epoch) {
      ready.value =
        cap.can_prepare === true &&
        cap.platform_open === true &&
        Array.isArray(cap.pay_types) &&
        cap.pay_types.some((x) => x === "wxpay" || x === "alipay");
      if (!ready.value) error.value = "付款码收款暂未开启或无可用支付通道";
    }
  } catch {
    if (alive && !ac.signal.aborted && run === epoch)
      error.value = "收款能力无法确认，请重新进入";
  }
});
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.ownSession,
    props.merchantID,
  ],
  () => {
    epoch++;
    cancelNative202();
    controller?.abort();
    stopCamera();
    code.value = "";
    ready.value = false;
    busy.value = false;
    error.value = "会话已变化，请重新进入收款页";
  },
  { flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  cancelNative202();
  controller?.abort();
  stopCamera();
  code.value = "";
});
</script>

<template>
  <main class="scan-cashier192">
    <header>
      <RouterLink to="/merchant/onecode">‹ 返回聚合收款</RouterLink>
      <h1>付款码收款</h1>
    </header>
    <section class="scan-card192">
      <p v-if="!ownSession" class="alert warning">
        请使用商户本人会话收款，代登录不可发起扣款。
      </p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <template v-if="!pending">
        <label class="scan-amount192"
          >收款金额<span
            >¥
            <input
              aria-label="收款金额"
              :value="amount"
              placeholder="0.00"
              readonly
              inputmode="decimal" /></span
        ></label>
        <button class="remark192" :disabled="!canStart" @click="editRemark">
          {{ remark || "添加备注" }}
        </button>
        <div class="scan-keypad192">
          <button
            v-for="key in keys"
            :key="key"
            :aria-label="'金额键 ' + key"
            :disabled="!canStart"
            @click="keypad(key)"
          >
            {{ key }}
          </button>
        </div>
        <button
          class="primary scan-main192"
          :disabled="!canStart || !validAmount(amount)"
          @click="startScan202"
        >
          扫一扫收款
        </button>
      </template>
      <template v-else>
        <h2 v-if="result?.state === 'paid'" class="scan-success192">
          收款成功
        </h2>
        <h2 v-else-if="result?.state === 'closed'">已取消收款</h2>
        <h2 v-else-if="result?.state === 'failed'">收款失败</h2>
        <h2 v-else>
          {{ result?.can_apply && code ? "确认收款" : "收款结果待确认" }}
        </h2>
        <template v-if="result">
          <p>收款金额 ¥{{ result.money }}</p>
          <p class="scan-charge192">付款人实付 ¥{{ result.charge_money }}</p>
          <p class="muted">
            手续费 ¥{{ result.fee_money }} · 商户入账 ¥{{ result.net_money }}
          </p>
          <p v-if="result.remark">备注：{{ result.remark }}</p>
          <p v-if="result.trade_no" class="scan-trade192">
            订单号：{{ result.trade_no }}
          </p>
        </template>
        <button
          v-if="result?.can_apply && code"
          class="primary scan-main192"
          :disabled="busy"
          @click="apply"
        >
          确认收款 ¥{{ result.charge_money }}
        </button>
        <button v-if="!terminal" :disabled="busy" @click="recover()">
          恢复原收款结果
        </button>
        <button
          v-if="result?.can_query && !terminal"
          :disabled="busy"
          @click="recover(true)"
        >
          查询原支付通道
        </button>
        <button
          v-if="
            !result ||
            (['prepared', 'unprepared'].includes(result.state) &&
              !result.can_query)
          "
          :disabled="busy"
          @click="close"
        >
          取消本次收款
        </button>
        <button
          v-if="terminal"
          class="primary scan-main192"
          :disabled="busy"
          @click="reset"
        >
          继续收款
        </button>
        <p v-if="!terminal" class="muted">
          结果未确认前，请恢复或查询本笔收款，避免重复向付款人扣款。
        </p>
      </template>
    </section>
    <div v-if="editing" class="scan-overlay192" @keydown.esc="editing = false">
      <section
        role="dialog"
        aria-modal="true"
        aria-label="添加收款备注"
        class="scan-dialog192"
      >
        <h2>添加备注</h2>
        <label
          >收款备注<input
            ref="remarkInput"
            v-model="draft"
            maxlength="80"
            @keydown.enter="
              remark = draft;
              editing = false;
            "
        /></label>
        <div class="scan-dialog-actions192">
          <button @click="editing = false">取消备注</button
          ><button
            class="primary"
            @click="
              remark = draft;
              editing = false;
            "
          >
            确认备注
          </button>
        </div>
      </section>
    </div>
    <div
      v-if="scanning && !pending"
      class="scan-overlay192"
      @keydown.esc="cancelScan"
    >
      <form
        class="scan-dialog192"
        role="dialog"
        aria-modal="true"
        aria-label="扫描付款码"
        @submit.prevent="prepare"
      >
        <h2>扫描付款码</h2>
        <p>请顾客出示微信或支付宝付款码</p>
        <p v-if="nativeBusy" role="status">正在等待原生扫码结果…</p>
        <video ref="video" muted playsinline /><label
          >付款码<input
            v-model="code"
            inputmode="numeric"
            autocomplete="off"
            maxlength="24"
            :disabled="nativeBusy"
        /></label>
        <p v-if="error" role="alert" class="alert error">{{ error }}</p>
        <button type="button" :disabled="busy || nativeBusy" @click="camera">
          使用摄像头
        </button>
        <div class="scan-dialog-actions192">
          <button type="button" :disabled="busy" @click="cancelScan">
            取消扫码</button
          ><button class="primary" :disabled="busy || nativeBusy || !code">
            核对收款
          </button>
        </div>
      </form>
    </div>
  </main>
</template>
<style scoped>
.scan-cashier192 {
  min-height: 100dvh;
  background: var(--bg-raised);
  padding: 20px 16px;
  color: var(--ink);
  box-sizing: border-box;
}
.scan-cashier192 header {
  max-width: 460px;
  margin: auto auto 18px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.scan-cashier192 h1 {
  font-size: 18px;
  font-weight: 500;
  margin: 0;
}
.scan-cashier192 a {
  color: var(--ink);
  font-size: 14px;
  text-decoration: none;
}
.scan-card192 {
  max-width: 460px;
  margin: auto;
  background: var(--bg-card);
  border-radius: 8px;
  padding: 24px;
  box-sizing: border-box;
}
.scan-amount192 {
  display: block;
  font-size: 15px;
}
.scan-amount192 span {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 34px;
  border-bottom: 1px solid var(--line);
  margin-top: 16px;
  padding: 8px 0;
}
.scan-amount192 input {
  border: 0 !important;
  background: none !important;
  box-shadow: none !important;
  padding: 0;
  width: 100%;
  min-width: 0;
  font-size: 42px;
  font-weight: 500;
  color: var(--ink);
}
.remark192 {
  border: 0 !important;
  padding: 12px 0 !important;
  color: var(--ink-muted) !important;
  box-shadow: none !important;
  background: none !important;
}
.scan-keypad192 {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 1px;
  background: var(--line);
  margin: 12px -8px 20px;
}
.scan-keypad192 button {
  border: 0;
  border-radius: 0;
  font-size: 26px;
  height: 65px;
  background: var(--bg-card);
  box-shadow: none;
}
.scan-main192 {
  width: 100%;
  margin: 12px 0;
  min-height: 46px;
  background: var(--primary) !important;
  border-color: var(--primary-text) !important;
  color: var(--on-primary);
}
.scan-charge192 {
  font-size: 24px;
}
.scan-success192 {
  color: var(--success-text);
}
.scan-trade192 {
  overflow-wrap: anywhere;
}
.scan-card192 > button:not(.remark192) {
  margin: 6px 0;
}
.scan-overlay192 {
  position: fixed;
  inset: 0;
  z-index: 1300;
  background: var(--scrim);
  display: grid;
  place-items: center;
  padding: 16px;
}
.scan-dialog192 {
  width: min(100%, 380px);
  box-sizing: border-box;
  background: var(--bg-card);
  padding: 22px;
  border-radius: 8px;
}
.scan-dialog192 h2 {
  font-size: 18px;
  font-weight: 500;
  margin: 0 0 18px;
}
.scan-dialog192 label {
  display: block;
}
.scan-dialog192 input {
  display: block;
  width: 100%;
  box-sizing: border-box;
  margin: 10px 0;
}
.scan-dialog-actions192 {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 16px;
}
.scan-dialog192 video {
  max-width: 100%;
  max-height: 200px;
  display: block;
}
.scan-dialog192 video:not([src]) {
  height: auto;
}
.scan-card192 .muted {
  font-size: 13px;
  line-height: 1.6;
}
@media (max-width: 480px) {
  .scan-cashier192 {
    padding: 16px 12px;
  }
  .scan-card192 {
    padding: 20px;
  }
  .scan-keypad192 button {
    height: 68px;
  }
}
</style>
