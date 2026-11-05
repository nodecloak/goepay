<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiError, type ApiClient } from "../api";
import {
  merchantCredentialPath,
  readMerchantCredentialState,
  readMerchantCredentialResult,
  type MerchantCredentialState,
  type MerchantCredentialOperation,
} from "../merchant-credentials";
const props = defineProps<{
  client: ApiClient;
  merchantID: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  busy: [merchantID: string, value: boolean];
  discardDrafts: [merchantID: string];
}>();
const state = ref<MerchantCredentialState>(),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  message = ref(""),
  confirmation = ref<MerchantCredentialOperation>(),
  result = ref<{ operation: MerchantCredentialOperation; secret: string }>();
const resetButton = ref<HTMLButtonElement>(),
  errorTarget = ref<HTMLElement>(),
  rsaButton = ref<HTMLButtonElement>(),
  cancelButton = ref<HTMLButtonElement>(),
  confirmGroup = ref<HTMLDivElement>(),
  resultField = ref<HTMLInputElement | HTMLTextAreaElement>();
let active = true,
  epoch = 0,
  controller: AbortController | undefined;
const rsaTitle = computed(() =>
    state.value?.public_key ? "重置商户RSA密钥对" : "生成商户RSA密钥对",
  ),
  title = computed(() =>
    confirmation.value === "reset-md5" ? "重置MD5密钥" : rsaTitle.value,
  );
function clearSecret() {
  result.value = undefined;
  message.value = "";
}
async function focusOperation(operation?: MerchantCredentialOperation) {
  await nextTick();
  if (active)
    (operation === "generate-rsa"
      ? rsaButton.value
      : resetButton.value
    )?.focus();
}
async function load() {
  if (busy.value) return;
  controller?.abort();
  const c = new AbortController();
  controller = c;
  const current = ++epoch;
  loading.value = true;
  error.value = "";
  confirmation.value = undefined;
  clearSecret();
  state.value = undefined;
  try {
    const data = await props.client.request<unknown>(
      merchantCredentialPath(props.merchantID),
      { signal: c.signal },
    );
    if (!active || c.signal.aborted || current !== epoch) return;
    state.value = readMerchantCredentialState(data, props.merchantID);
  } catch {
    if (active && !c.signal.aborted && current === epoch)
      error.value = "当前密钥信息读取失败，请刷新重试";
  } finally {
    if (active && current === epoch) loading.value = false;
  }
}
async function confirm(operation: MerchantCredentialOperation) {
  if (props.disabled || loading.value || busy.value || !state.value) return;
  clearSecret();
  error.value = "";
  confirmation.value = operation;
  await nextTick();
  cancelButton.value?.focus();
}
function cancel() {
  const operation = confirmation.value;
  confirmation.value = undefined;
  void focusOperation(operation);
}
function hideSecret() {
  const operation = result.value?.operation;
  clearSecret();
  void focusOperation(operation);
}
async function submit() {
  if (props.disabled || busy.value || !state.value || !confirmation.value)
    return;
  const operation = confirmation.value,
    merchantID = props.merchantID,
    before = state.value,
    current = ++epoch,
    c = new AbortController();
  controller?.abort();
  controller = c;
  busy.value = true;
  emit("busy", props.merchantID, true);
  error.value = "";
  try {
    emit("discardDrafts", merchantID);
    await nextTick();
    if (!active || c.signal.aborted || current !== epoch) return;
    confirmGroup.value?.focus();
    const data = await props.client.request<unknown>(
      merchantCredentialPath(props.merchantID) + "/" + operation,
      {
        method: "POST",
        body: { expected_fingerprint: before.fingerprint },
        signal: c.signal,
      },
    );
    if (!active || c.signal.aborted || current !== epoch) return;
    const parsed = readMerchantCredentialResult(data, before, operation);
    state.value = parsed.state;
    confirmation.value = undefined;
    result.value = {
      operation,
      secret: operation === "reset-md5" ? parsed.apiKey! : parsed.privateKey!,
    };
  } catch (e) {
    if (active && !c.signal.aborted && current === epoch) {
      confirmation.value = undefined;
      state.value = undefined;
      clearSecret();
      error.value =
        e instanceof ApiError && e.status === 409
          ? "密钥状态已变更，请刷新页面后明确重新确认；原请求不会自动重试。"
          : "本次密钥变更未能确认，请刷新页面核对当前状态；不要直接重复生成。";
    }
  } finally {
    if (active && current === epoch) {
      busy.value = false;
      emit("busy", props.merchantID, false);
      await nextTick();
      if (active && current === epoch) {
        if (result.value) resultField.value?.focus();
        else if (error.value) errorTarget.value?.focus();
      }
    }
  }
}
async function copySecret() {
  const current = epoch,
    original = result.value;
  if (!original) return;
  try {
    await navigator.clipboard.writeText(original.secret);
    if (active && current === epoch && result.value === original)
      message.value = "已复制，请妥善保存";
  } catch {
    if (active && current === epoch && result.value === original)
      message.value = "复制失败，请手动保存当前显示的密钥";
  }
}
function downloadPrivate() {
  if (result.value?.operation !== "generate-rsa") return;
  const url = URL.createObjectURL(
      new Blob([result.value.secret], { type: "application/x-pem-file" }),
    ),
    a = document.createElement("a");
  a.href = url;
  a.download = "merchant-" + props.merchantID + "-private.pem";
  a.click();
  queueMicrotask(() => URL.revokeObjectURL(url));
}
function keydown(event: KeyboardEvent) {
  if (busy.value && ["Tab", "Enter"].includes(event.key)) {
    event.preventDefault();
    event.stopPropagation();
    return;
  }
  if (
    event.key === "Enter" &&
    result.value &&
    event.target instanceof HTMLElement &&
    ["INPUT", "TEXTAREA"].includes(event.target.tagName)
  ) {
    event.preventDefault();
    event.stopPropagation();
    return;
  }
  if (
    event.key === "Escape" &&
    (busy.value || confirmation.value || result.value)
  ) {
    event.preventDefault();
    event.stopPropagation();
    if (busy.value) return;
    if (confirmation.value) cancel();
    else hideSecret();
    return;
  }
  if (event.key !== "Tab" || (!confirmation.value && !result.value)) return;
  const group = event.currentTarget as HTMLElement,
    targets = Array.from(
      group.querySelectorAll<HTMLElement>(
        "button:not([disabled]),input:not([disabled]),textarea:not([disabled])",
      ),
    ).filter((e) => e.tabIndex >= 0 && e.getClientRects().length),
    first = targets[0],
    last = targets.at(-1);
  if (
    first &&
    last &&
    (event.shiftKey
      ? document.activeElement === first
      : document.activeElement === last)
  ) {
    event.preventDefault();
    (event.shiftKey ? last : first).focus();
  }
}
onMounted(load);
onBeforeUnmount(() => {
  active = false;
  epoch++;
  controller?.abort();
  confirmation.value = undefined;
  clearSecret();
  state.value = undefined;
  emit("busy", props.merchantID, false);
});
</script>
<template>
  <section class="merchant-credentials" aria-label="商户密钥操作">
    <h3>密钥操作</h3>
    <p class="muted">
      目标商户：{{
        merchantID
      }}。重置会立即更换接口密钥，请同步商户支付配置；签名算法保持不变。
    </p>
    <p
      ref="errorTarget"
      tabindex="-1"
      v-if="error"
      role="alert"
      class="alert error"
    >
      {{ error }}
    </p>
    <p v-if="loading" role="status">正在读取密钥状态…</p>
    <div class="credential-actions">
      <button
        ref="resetButton"
        type="button"
        :disabled="
          disabled || loading || busy || !state || !!confirmation || !!result
        "
        @click="confirm('reset-md5')"
      >
        重置MD5密钥
      </button>
      <button
        ref="rsaButton"
        type="button"
        :disabled="
          disabled || loading || busy || !state || !!confirmation || !!result
        "
        @click="confirm('generate-rsa')"
      >
        {{ rsaTitle }}
      </button>
    </div>
    <div
      v-if="confirmation"
      ref="confirmGroup"
      role="group"
      tabindex="-1"
      :aria-busy="busy"
      :aria-label="'确认' + title"
      class="credential-confirm"
      @keydown="keydown"
    >
      <p>
        是否确认{{ title }}？目标商户：{{
          merchantID
        }}。新密钥仅在本次成功后显示，未保存的密钥草稿将清空。
      </p>
      <p v-if="busy" role="status">正在处理，请等待原请求结果…</p>
      <div class="credential-actions">
        <button
          ref="cancelButton"
          type="button"
          :disabled="busy"
          @click="cancel"
        >
          取消</button
        ><button
          class="primary"
          type="button"
          :disabled="disabled || busy"
          @click="submit"
        >
          {{ confirmation === "reset-md5" ? "确定重置" : "确定生成" }}
        </button>
      </div>
    </div>
    <div v-if="result" class="credential-result" @keydown="keydown">
      <p role="status">
        密钥已更换。请保存后再关闭；关闭、切换商户或离开页面后不会再次显示。
      </p>
      <label v-if="result.operation === 'reset-md5'"
        >新MD5密钥（仅本次显示）<input
          ref="resultField"
          :value="result.secret"
          readonly
          autocomplete="off"
          spellcheck="false"
      /></label>
      <label v-else
        >商户RSA私钥（仅本次显示）<textarea
          ref="resultField"
          :value="result.secret"
          readonly
          rows="9"
          autocomplete="off"
          spellcheck="false"
        />
      </label>
      <div class="credential-actions">
        <button type="button" @click="copySecret">复制新密钥</button
        ><button
          v-if="result.operation === 'generate-rsa'"
          type="button"
          @click="downloadPrivate"
        >
          下载私钥</button
        ><button type="button" @click="hideSecret">关闭密钥显示</button>
      </div>
      <p v-if="message" role="status">{{ message }}</p>
    </div>
    <details v-if="state?.public_key && !result">
      <summary>查看当前商户RSA公钥</summary>
      <textarea
        aria-label="当前商户RSA公钥"
        :value="state.public_key"
        readonly
        rows="5"
      />
    </details>
  </section>
</template>
<style scoped>
.merchant-credentials {
  border-top: 1px solid var(--line, var(--line));
  margin-top: 20px;
  padding-top: 16px;
}
.merchant-credentials h3 {
  font-size: 14px;
  font-weight: 500;
  margin: 0 0 10px;
}
.credential-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.credential-confirm,
.credential-result {
  margin-top: 14px;
  padding: 12px;
  border: 1px solid var(--line-control);
  border-radius: 6px;
  background: var(--bg-raised);
}
.merchant-credentials textarea {
  width: 100%;
  max-width: 100%;
  resize: vertical;
  font:
    12px/1.6 Consolas,
    monospace;
}
.merchant-credentials details {
  margin-top: 14px;
}
.merchant-credentials summary {
  cursor: pointer;
}
.merchant-credentials p {
  line-height: 1.6;
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .credential-actions > button {
    white-space: normal;
    max-width: 100%;
  }
  .credential-result input {
    font-size: 12px;
  }
}
</style>
