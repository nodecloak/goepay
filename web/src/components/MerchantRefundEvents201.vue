<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  reactive,
  ref,
  toRaw,
  watch,
} from "vue";
import { ApiError, type ApiClient } from "../api";
import { isPersonalMerchantID } from "../merchant-personal-settings";

const props = withDefaults(
  defineProps<{
    client: ApiClient;
    ownSession?: boolean;
    merchantId?: string;
  }>(),
  { ownSession: false, merchantId: "" },
);

type EventType = "refund" | "complain";
const deliveryLabels = {
  none: "尚无投递记录",
  pending: "等待投递",
  delivering: "正在投递",
  delivered: "商户已返回成功响应",
  failed: "投递失败",
  unknown: "投递结果未知",
  skipped: "已跳过投递",
} as const;
type DeliveryState = keyof typeof deliveryLabels;
interface Subscription {
  event_url: string;
  event_types: EventType[];
  fingerprint: string;
  consecutive_failures: number;
  last_delivery_state: DeliveryState;
  subscription_notice: string;
}
interface Update {
  event_url: string;
  event_types: EventType[];
  expected_fingerprint: string;
}
const path = "/account/event-subscription";
const invalid = () => new Error("事件订阅数据无法核对，请刷新页面后核对");
function callbackURL(value: string, enabled: boolean) {
  const url = value.trim();
  if (new TextEncoder().encode(url).length > 500)
    throw new Error("订阅URL最长为500字节，请核对地址");
  if (!url) {
    if (enabled) throw new Error("请填写事件消息订阅URL");
    return "";
  }
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    throw new Error("请输入有效的订阅URL");
  }
  const authority = /^https?:\/\/([^/?#]*)/i.exec(url)?.[1];
  if (
    (parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
    !authority ||
    !parsed.hostname ||
    authority.includes("@") ||
    parsed.username ||
    parsed.password ||
    url.includes("#") ||
    /[\u0000-\u0020\u007f]/.test(url)
  )
    throw new Error("订阅URL须使用http或https，不能包含凭证、片段或空白字符");
  return url;
}
function readSubscription(raw: unknown): Subscription {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw invalid();
  const value = raw as Record<string, unknown>;
  const keys = [
    "event_url",
    "event_types",
    "fingerprint",
    "consecutive_failures",
    "last_delivery_state",
    "subscription_notice",
  ];
  if (
    Object.keys(value).length !== keys.length ||
    keys.some((key) => !Object.hasOwn(value, key)) ||
    typeof value.event_url !== "string" ||
    !Array.isArray(value.event_types) ||
    value.event_types.length > 2 ||
    value.event_types.some(
      (event) => event !== "refund" && event !== "complain",
    ) ||
    new Set(value.event_types as string[]).size !== value.event_types.length ||
    typeof value.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(value.fingerprint) ||
    typeof value.consecutive_failures !== "number" ||
    !Number.isInteger(value.consecutive_failures) ||
    value.consecutive_failures < 0 ||
    value.consecutive_failures > 9 ||
    typeof value.last_delivery_state !== "string" ||
    !Object.hasOwn(deliveryLabels, value.last_delivery_state) ||
    typeof value.subscription_notice !== "string" ||
    new TextEncoder().encode(value.subscription_notice).length > 1000
  )
    throw invalid();
  const eventTypes: EventType[] = [];
  if ((value.event_types as string[]).includes("refund"))
    eventTypes.push("refund");
  if ((value.event_types as string[]).includes("complain"))
    eventTypes.push("complain");
  if (callbackURL(value.event_url, eventTypes.length > 0) !== value.event_url)
    throw invalid();
  return {
    event_url: value.event_url,
    event_types: eventTypes,
    fingerprint: value.fingerprint,
    consecutive_failures: value.consecutive_failures,
    last_delivery_state: value.last_delivery_state as DeliveryState,
    subscription_notice: value.subscription_notice,
  };
}

const state = ref<Subscription | null>(null);
const draft = reactive({ url: "", refund: false, complain: false });
const loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
const confirming = ref<"save" | "disable" | null>(null);
const saveButton = ref<HTMLButtonElement>(),
  disableButton = ref<HTMLButtonElement>();
const confirmButton = ref<HTMLButtonElement>(),
  errorTarget = ref<HTMLElement>();
const urlInput = ref<HTMLInputElement>();
let prepared: Update | undefined;
let epoch = 0,
  active = true,
  controller: AbortController | undefined;
function context() {
  return {
    client: toRaw(props.client),
    area: props.client.area,
    csrf: props.client.csrfToken,
    merchant: props.merchantId,
    own: props.ownSession,
  };
}
let tracked = context();
function isAllowed() {
  return (
    props.ownSession === true &&
    props.client.area === "merchant" &&
    isPersonalMerchantID(props.merchantId) &&
    !!props.client.csrfToken
  );
}
const allowed = computed(isAllowed);
function sameContext(source = tracked) {
  const current = context();
  return (
    active &&
    source.client === current.client &&
    source.area === current.area &&
    source.csrf === current.csrf &&
    source.merchant === current.merchant &&
    source.own === current.own
  );
}
function setDraft(value?: Subscription | null) {
  draft.url = value?.event_url ?? "";
  draft.refund = value?.event_types.includes("refund") ?? false;
  draft.complain = value?.event_types.includes("complain") ?? false;
}
function clear(message = "") {
  ++epoch;
  controller?.abort();
  controller = undefined;
  state.value = null;
  setDraft();
  prepared = undefined;
  confirming.value = null;
  loading.value = false;
  busy.value = false;
  error.value = message;
  notice.value = "";
  tracked = context();
}
function ensureContext() {
  if (!sameContext()) {
    clear("会话已变化，请刷新页面后核对");
    return false;
  }
  return isAllowed();
}
function current(
  run: number,
  abort: AbortController,
  source: ReturnType<typeof context>,
) {
  if (!active || run !== epoch || abort.signal.aborted) return false;
  if (!sameContext(source)) {
    clear("会话已变化，请刷新页面后核对");
    return false;
  }
  return isAllowed();
}
async function load() {
  if (busy.value || loading.value) return;
  clear();
  if (!isAllowed()) return;
  const source = tracked,
    run = epoch,
    abort = new AbortController();
  controller = abort;
  loading.value = true;
  try {
    const raw = await source.client.request<unknown>(path, {
      signal: abort.signal,
    });
    if (!current(run, abort, source)) return;
    state.value = readSubscription(raw);
    setDraft(state.value);
  } catch {
    if (current(run, abort, source))
      error.value = "事件订阅读取失败或格式无法核对，请刷新页面后核对";
  } finally {
    if (current(run, abort, source)) loading.value = false;
  }
}
function cancelDraft() {
  if (!ensureContext() || busy.value || confirming.value || !state.value)
    return;
  setDraft(state.value);
  error.value = "";
  notice.value = "";
}
function prepare(kind: "save" | "disable") {
  if (
    !ensureContext() ||
    busy.value ||
    loading.value ||
    confirming.value ||
    !state.value
  )
    return;
  error.value = "";
  notice.value = "";
  try {
    const refund = kind === "save" && draft.refund;
    const complain = kind === "save" && draft.complain;
    const types: EventType[] = [];
    if (refund) types.push("refund");
    if (complain) types.push("complain");
    prepared = {
      event_url: callbackURL(
        kind === "disable" ? state.value.event_url : draft.url,
        types.length > 0,
      ),
      event_types: types,
      expected_fingerprint: state.value.fingerprint,
    };
    confirming.value = kind;
    void nextTick(() => {
      if (confirming.value === kind) confirmButton.value?.focus();
    });
  } catch (reason) {
    prepared = undefined;
    error.value = reason instanceof Error ? reason.message : "请核对订阅URL";
  }
}
function cancelConfirmation() {
  if (busy.value || !confirming.value) return;
  const kind = confirming.value;
  confirming.value = null;
  prepared = undefined;
  void nextTick(() => {
    if (sameContext())
      (kind === "disable" ? disableButton.value : saveButton.value)?.focus();
  });
}
async function save() {
  if (
    !ensureContext() ||
    busy.value ||
    loading.value ||
    !confirming.value ||
    !prepared ||
    !state.value
  )
    return;
  const body: Update = { ...prepared, event_types: [...prepared.event_types] };
  if (body.expected_fingerprint !== state.value.fingerprint) return;
  const source = tracked,
    run = ++epoch,
    abort = new AbortController();
  controller?.abort();
  controller = abort;
  prepared = undefined;
  confirming.value = null;
  busy.value = true;
  error.value = "";
  notice.value = "";
  let rejected = false;
  try {
    const raw = await source.client.request<unknown>(path, {
      method: "PATCH",
      body,
      signal: abort.signal,
    });
    if (!current(run, abort, source)) return;
    const next = readSubscription(raw);
    if (
      next.event_url !== body.event_url ||
      next.event_types.join(",") !== body.event_types.join(",")
    )
      throw invalid();
    state.value = next;
    setDraft(next);
    notice.value = "订阅设置已保存";
  } catch (reason) {
    if (!current(run, abort, source)) return;
    if (
      reason instanceof ApiError &&
      reason.responseDecoded &&
      [400, 422].includes(reason.status)
    ) {
      rejected = true;
      error.value = reason.message || "订阅设置未被接受，请核对地址和订阅事件";
    } else {
      state.value = null;
      setDraft();
      error.value =
        reason instanceof ApiError && reason.status === 409
          ? "订阅设置已变更，请刷新页面后核对；不会自动重试。"
          : "保存结果无法确认，请刷新页面后核对；不会自动重试。";
    }
  } finally {
    if (current(run, abort, source)) {
      busy.value = false;
      void nextTick(() => {
        if (current(run, abort, source))
          (rejected
            ? urlInput.value
            : state.value
              ? saveButton.value
              : errorTarget.value
          )?.focus();
      });
    }
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.merchantId,
    props.ownSession,
  ],
  () => {
    clear();
    void load();
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  active = false;
  clear();
});
</script>

<template>
  <section
    class="panel account-section refund-event-settings"
    aria-label="事件消息订阅设置"
  >
    <div class="event-heading">
      <h2>事件消息订阅设置</h2>
    </div>
    <p v-if="!allowed" class="muted">事件订阅需要商户本人登录后查看和修改。</p>
    <template v-else>
      <p class="event-guidance">
        事件消息订阅仅支持易支付V2支付插件（RSA 签名验证方式）
      </p>
      <p v-if="loading" role="status">正在读取事件订阅…</p>
      <p
        ref="errorTarget"
        tabindex="-1"
        v-if="error"
        role="alert"
        class="alert error"
      >
        {{ error }}
      </p>
      <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
      <template v-if="state">
        <dl class="event-delivery-facts">
          <div>
            <dt>订阅状态</dt>
            <dd>
              {{
                state.event_types.length
                  ? "已启用：" +
                    state.event_types
                      .map((t) => (t === "refund" ? "订单退款" : "交易投诉"))
                      .join("、")
                  : "未启用"
              }}
            </dd>
          </div>
          <div>
            <dt>最近投递</dt>
            <dd>{{ deliveryLabels[state.last_delivery_state] }}</dd>
          </div>
          <div>
            <dt>连续发送失败</dt>
            <dd>{{ state.consecutive_failures }} 次</dd>
          </div>
        </dl>
        <p
          v-if="state.subscription_notice"
          class="alert event-notice"
          aria-label="订阅通知"
        >
          {{ state.subscription_notice }}
        </p>
      </template>
      <form class="console-config-form" v-if="state" novalidate @submit.prevent="prepare('save')">
        <fieldset :disabled="busy || !!confirming">
          <legend class="sr-only">事件订阅</legend>
          <label class="event-url-label"
            >订阅URL<input
              ref="urlInput"
              v-model="draft.url"
              name="event_url"
              type="url"
              inputmode="url"
              maxlength="500"
              autocomplete="off"
              :spellcheck="false"
              placeholder="请输入以 http:// 或 https:// 开头的消息订阅URL"
          /></label>
          <p class="muted event-url-help">
            使用可公开访问的 HTTP 或 HTTPS
            地址。地址不能包含账号凭证或片段；平台会校验并拒绝内网地址。
          </p>
          <div class="event-choice">
            <span>订阅事件</span>
            <label
              ><input
                v-model="draft.refund"
                name="event_types"
                value="refund"
                type="checkbox"
              /><span>订单退款</span></label
            >
            <label
              ><input
                v-model="draft.complain"
                name="event_types"
                value="complain"
                type="checkbox"
              /><span>交易投诉</span></label
            >
          </div>
          <p class="muted event-scope">
            订单退款：发送在平台提交的退款操作事件，不包括通过API退款的操作。交易投诉：发送渠道投诉的新投诉与状态变化，每个状态仅推送一次。
          </p>
        </fieldset>
        <p class="muted event-guidance">
          保存订阅仅更新设置，不会立即发送消息或测试订阅URL。连续发送失败10次后，系统会自动取消全部事件订阅并保留订阅URL，请检查地址后重新开启。
        </p>
        <div
          v-if="confirming"
          class="event-confirm"
          role="group"
          aria-label="确认事件订阅"
          @keydown.esc.prevent.stop="cancelConfirmation"
        >
          <p>
            {{
              confirming === "disable"
                ? "确认禁用事件订阅？当前已保存的订阅URL会保留，订阅事件将清空。"
                : prepared?.event_types.length
                  ? "确认保存此URL并订阅所选事件？"
                  : "确认保存并关闭全部事件订阅？"
            }}
          </p>
          <div class="event-actions">
            <button
              ref="confirmButton"
              type="button"
              :class="confirming === 'disable' ? 'danger-button' : 'primary'"
              @click="save"
            >
              {{ confirming === "disable" ? "确认禁用" : "确认保存" }}
            </button>
            <button type="button" @click="cancelConfirmation">返回修改</button>
          </div>
        </div>
        <div v-else class="event-actions">
          <button
            ref="saveButton"
            type="submit"
            class="primary"
            :disabled="loading || busy"
          >
            {{ busy ? "正在保存…" : "保存订阅" }}
          </button>
          <button type="button" :disabled="busy" @click="cancelDraft">
            取消修改
          </button>
          <button
            ref="disableButton"
            type="button"
            :disabled="busy || state.event_types.length === 0"
            @click="prepare('disable')"
          >
            禁用订阅
          </button>
        </div>
      </form>
    </template>
  </section>
</template>

<style scoped>
.refund-event-settings {
  overflow-wrap: anywhere;
}
.event-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
}
.event-heading h2 {
  margin: 0;
}
.event-guidance,
.event-url-help,
.event-scope {
  line-height: 1.7;
}
.event-guidance {
  margin: 16px 0;
}
.event-delivery-facts {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px 18px;
  margin: 18px 0;
}
.event-delivery-facts > div {
  min-width: 0;
}
.event-delivery-facts dt {
  color: var(--muted, var(--ink));
  font-size: 13px;
}
.event-delivery-facts dd {
  margin: 6px 0 0;
  font-size: 14px;
}
.event-notice {
  white-space: pre-wrap;
}
.refund-event-settings fieldset {
  border: 0;
  padding: 0;
  margin: 20px 0 0;
  min-width: 0;
}
.event-url-label {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.event-url-label input {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
}
.event-url-help,
.event-scope {
  font-size: 13px;
}
.event-url-help {
  margin: 8px 0 18px;
}
.event-choice,
.event-choice label {
  display: flex;
  align-items: center;
  gap: 12px;
}
.event-choice {
  flex-wrap: wrap;
  gap: 12px 24px;
}
.event-choice input {
  flex: none;
  width: 18px;
  height: 18px;
  margin: 0;
}
.event-scope {
  margin: 8px 0 0;
}
.event-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 16px;
}
.event-confirm {
  padding: 14px;
  border: 1px solid var(--line, var(--line-control));
  border-radius: 6px;
  background: var(--surface-alt, var(--bg-raised));
}
.event-confirm p {
  margin: 0;
}
.refund-event-settings button:focus-visible,
.refund-event-settings input:focus-visible {
  outline: 2px solid var(--primary, var(--focus));
  outline-offset: 3px;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
}
@media (max-width: 600px) {
  .event-delivery-facts {
    grid-template-columns: minmax(0, 1fr);
    gap: 12px;
  }
  .event-delivery-facts > div {
    display: grid;
    grid-template-columns: 100px minmax(0, 1fr);
    gap: 12px;
    align-items: baseline;
  }
  .event-delivery-facts dd {
    margin: 0;
  }
}
</style>
