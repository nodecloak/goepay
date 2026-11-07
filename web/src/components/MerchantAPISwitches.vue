<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  reactive,
  ref,
  watch,
  toRaw,
} from "vue";
import {
  fetchPublicMerchantAccess,
  type PublicMerchantAccess,
} from "../platform-merchant-access";
import { ApiClient, ApiError } from "../api";
import {
  parseMerchantAPISwitches,
  merchantAPISwitchBody,
  type MerchantAPISwitches,
} from "../merchant-api-switches";
import { isPersonalMerchantID } from "../merchant-personal-settings";
const props = defineProps<{
  client: ApiClient;
  merchantId: string;
  ownSession: boolean;
}>();
const confirmButton = ref<HTMLButtonElement>(),
  saveButton = ref<HTMLButtonElement>(),
  errorTarget = ref<HTMLElement>();
const state = ref<MerchantAPISwitches | null>(null),
  platform = ref<PublicMerchantAccess | null>(null),
  draft = reactive({ keylogin: false, refund: false, transfer: false }),
  confirming = ref(false),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
const allowed = computed(
  () =>
    props.ownSession &&
    props.client.area === "merchant" &&
    isPersonalMerchantID(props.merchantId),
);
let epoch = 0,
  controller: AbortController | undefined,
  trackedClient = toRaw(props.client),
  trackedCSRF = props.client.csrfToken,
  trackedID = props.merchantId,
  trackedOwn = props.ownSession;
function setDraft(value?: MerchantAPISwitches) {
  draft.keylogin = value?.keylogin === 1;
  draft.refund = value?.refund === 1;
  draft.transfer = value?.transfer === 1;
}
function sameContext() {
  return (
    trackedClient === toRaw(props.client) &&
    trackedCSRF === props.client.csrfToken &&
    trackedID === props.merchantId &&
    trackedOwn === props.ownSession
  );
}
function clear(message = "") {
  epoch++;
  confirming.value = false;
  controller?.abort();
  controller = undefined;
  state.value = null;
  platform.value = null;
  setDraft();
  loading.value = false;
  busy.value = false;
  error.value = message;
  notice.value = "";
  trackedClient = toRaw(props.client);
  trackedCSRF = props.client.csrfToken;
  trackedID = props.merchantId;
  trackedOwn = props.ownSession;
}
function ensureContext() {
  if (!sameContext()) {
    clear("会话已变化，请刷新页面后核对");
    return false;
  }
  return allowed.value;
}
function valid(run: number, c: AbortController) {
  if (run !== epoch || c.signal.aborted) return false;
  if (!sameContext()) {
    clear("会话已变化，请刷新页面后核对");
    return false;
  }
  return allowed.value;
}
async function load() {
  if (busy.value || loading.value) return;
  clear();
  if (!allowed.value) return;
  const client = toRaw(props.client),
    mid = props.merchantId,
    run = epoch,
    c = new AbortController();
  controller = c;
  loading.value = true;
  try {
    const [value, access] = await Promise.all([
      client.request("/account/api-switches", { signal: c.signal }),
      fetchPublicMerchantAccess(c.signal),
    ]);
    if (!valid(run, c)) return;
    state.value = parseMerchantAPISwitches(value, mid);
    platform.value = access;
    setDraft(state.value);
  } catch {
    if (valid(run, c))
      error.value = "个人或平台功能状态无法确认，请刷新页面后核对";
  } finally {
    if (valid(run, c)) loading.value = false;
  }
}
function cancel() {
  if (!ensureContext() || busy.value || !state.value) return;
  setDraft(state.value);
  error.value = "";
  notice.value = "";
}
function prepareSave() {
  if (!ensureContext() || busy.value || loading.value || !state.value) return;
  confirming.value = true;
  void nextTick(() => {
    if (confirming.value) confirmButton.value?.focus();
  });
}
function cancelConfirmation() {
  confirming.value = false;
  void nextTick(() => saveButton.value?.focus());
}
async function save() {
  if (!ensureContext() || busy.value || loading.value || !state.value) return;
  if (!confirming.value) return;
  confirming.value = false;
  const previous = state.value;
  let body: ReturnType<typeof merchantAPISwitchBody>;
  try {
    body = merchantAPISwitchBody(previous, draft);
  } catch (e) {
    error.value = e instanceof Error ? e.message : "请核对功能开关";
    return;
  }
  const client = toRaw(props.client),
    mid = props.merchantId,
    run = ++epoch,
    c = new AbortController();
  controller?.abort();
  controller = c;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const value = await client.request("/account/api-switches", {
      method: "PATCH",
      body,
      signal: c.signal,
    });
    if (!valid(run, c)) return;
    const next = parseMerchantAPISwitches(value, mid);
    if (
      next.fingerprint === previous.fingerprint ||
      !next.configured ||
      BigInt(next.revision) !== BigInt(previous.revision) + 1n ||
      next.transfer_granted !== previous.transfer_granted ||
      next.keylogin !== body.keylogin ||
      next.refund !== body.refund ||
      next.transfer !== body.transfer
    )
      throw new Error("unconfirmed result");
    state.value = next;
    setDraft(next);
    notice.value = "保存成功";
  } catch (e) {
    if (valid(run, c)) {
      state.value = null;
      setDraft();
      error.value =
        e instanceof ApiError && e.status === 409
          ? "设置已变更，请刷新页面后核对"
          : "保存结果无法确认，请刷新页面后核对";
    }
  } finally {
    if (valid(run, c)) {
      busy.value = false;
      void nextTick(() => {
        if (valid(run, c))
          (state.value ? saveButton.value : errorTarget.value)?.focus();
      });
    }
  }
}
watch(
  () => [
    props.client,
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
onBeforeUnmount(() => clear());
</script>
<template>
  <section class="panel api-switches" aria-label="接口与登录功能设置">
    <div class="switch-heading">
      <h2>接口与登录功能设置</h2>
    </div>
    <p v-if="!allowed" class="muted">功能开关需要商户本人登录后查看和修改。</p>
    <template v-else>
      <p v-if="loading" role="status">正在读取功能开关…</p>
      <p
        ref="errorTarget"
        tabindex="-1"
        v-if="error"
        role="alert"
        class="error"
      >
        {{ error }}
      </p>
      <p v-if="notice" role="status" class="notice">{{ notice }}</p>
      <p v-if="platform?.close_keylogin === 1" class="muted">
        平台已关闭密钥登录，保留您的个人选择；密码登录和原会话继续可用。
      </p>
      <p v-if="platform?.user_refund === 0" class="muted">
        平台已关闭商户自助退款，保留您的个人选择；原退款仍可查询和恢复。
      </p>
      <form class="console-config-form" v-if="state" @submit.prevent="prepareSave">
        <fieldset :disabled="busy || confirming">
          <legend class="sr-only">个人功能开关</legend>
          <label v-if="platform?.close_keylogin === 0" class="switch-row"
            ><input
              v-model="draft.keylogin"
              name="keylogin"
              type="checkbox"
            /><span
              ><strong>开启密钥登录</strong
              ><small
                >允许商户ID与当前商户密钥登录网页后台。已绑定验证器时仍需动态验证码；关闭不影响密码登录和现有会话。</small
              ></span
            ></label
          >
          <label v-if="platform?.user_refund === 1" class="switch-row"
            ><input v-model="draft.refund" name="refund" type="checkbox" /><span
              ><strong>订单退款API接口</strong
              ><small
                >关闭后阻止新的API退款；已发出的原退款仍可查询和恢复。</small
              ></span
            ></label
          >
          <label class="switch-row"
            ><input
              v-model="draft.transfer"
              name="transfer"
              type="checkbox"
            /><span
              ><strong>代付API接口</strong
              ><small
                >仍需管理员、平台和用户组授权。关闭后阻止新的API代付及余额接口；原代付查询与回执恢复继续保留。</small
              ></span
            ></label
          >
        </fieldset>
        <p v-if="!state.transfer_granted" class="muted">
          管理员尚未授予代付API权限；此处开启不会提升该授权。
        </p>
        <p v-if="!state.configured" class="muted">
          当前沿用历史默认设置，保存后按本次选择生效。
        </p>
        <div
          v-if="confirming"
          class="confirm-switches"
          role="group"
          aria-label="确认功能开关"
          @keydown.esc.prevent="cancelConfirmation"
        >
          <p>
            确认保存这些功能开关？已执行资金操作的原始查询和恢复不会重发资金。
          </p>
          <div class="switch-actions">
            <button
              ref="confirmButton"
              type="button"
              class="primary"
              data-action="confirm"
              @click="save"
            >
              确认保存</button
            ><button
              type="button"
              data-action="cancel-confirm"
              @click="cancelConfirmation"
            >
              返回修改
            </button>
          </div>
        </div>
        <div v-else class="switch-actions">
          <button
            ref="saveButton"
            class="primary"
            type="submit"
            :disabled="busy || loading"
          >
            {{ busy ? "正在保存…" : "保存功能开关" }}</button
          ><button
            type="button"
            data-action="cancel"
            :disabled="busy"
            @click="cancel"
          >
            取消修改
          </button>
        </div>
      </form>
      <button v-else type="submit" disabled>保存功能开关</button>
    </template>
  </section>
</template>
<style scoped>
.switch-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.switch-heading h2 {
  margin: 0;
}
.api-switches fieldset {
  border: 0;
  padding: 0;
  margin: 16px 0;
  min-width: 0;
}
.switch-row {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 15px 0;
  border-bottom: 1px solid var(--line);
}
.switch-row input {
  appearance: none;
  position: relative;
  width: 40px;
  height: 22px;
  flex: none;
  margin: 2px 0 0;
  padding: 0;
  border: 1px solid var(--line-control);
  border-radius: 11px;
  background: var(--line-control);
  cursor: pointer;
}
.switch-row input::after {
  content: "";
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--bg-card);
}
.switch-row input:checked {
  border-color: var(--primary-text);
  background: var(--primary);
}
.switch-row input:checked::after {
  left: 20px;
}
.switch-row input:focus-visible {
  outline: 2px solid var(--primary-text);
  outline-offset: 3px;
}
.switch-row input:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
@media (forced-colors: active) {
  .switch-row input {
    appearance: auto;
  }
  .switch-row input::after {
    content: none;
  }
}
.switch-row span {
  min-width: 0;
}
.switch-row strong {
  display: block;
}
.switch-row small {
  display: block;
  color: var(--muted);
  line-height: 1.7;
  margin-top: 6px;
  overflow-wrap: anywhere;
}
.switch-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 16px;
}
.confirm-switches {
  padding: 14px;
  background: var(--surface-alt, var(--bg-raised));
  border: 1px solid var(--line);
  border-radius: 8px;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
}
</style>
