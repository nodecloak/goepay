<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from "vue";
import { ApiError, type ApiClient } from "../api";
import {
  readSignaturePolicy,
  readSignaturePolicyResult,
  signaturePolicyLabel,
  signaturePolicyPath,
  type SignaturePolicy,
} from "../merchant-signature-policy";
const props = defineProps<{
  client: ApiClient;
  reload: number;
  merchantID?: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  busy: [merchantID: string, value: boolean];
  observed: [state: SignaturePolicy];
  discardLegacyAlgorithm: [merchantID: string, algorithm: "MD5" | "RSA"];
}>();
const state = ref<SignaturePolicy>(),
  selection = ref(""),
  loading = ref(false),
  busy = ref(false),
  confirming = ref(false),
  error = ref(""),
  notice = ref("");
const select = ref<HTMLSelectElement>(),
  refresh = ref<HTMLButtonElement>(),
  cancelButton = ref<HTMLButtonElement>(),
  confirmGroup = ref<HTMLDivElement>();
let epoch = 0,
  active = true,
  controller: AbortController | undefined;
async function load() {
  if (busy.value) return;
  controller?.abort();
  const c = new AbortController(),
    current = ++epoch;
  controller = c;
  state.value = undefined;
  selection.value = "";
  confirming.value = false;
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const raw = await props.client.request<unknown>(
      signaturePolicyPath(props.client.area, props.merchantID),
      { signal: c.signal },
    );
    if (!active || c.signal.aborted || current !== epoch) return;
    const parsed = readSignaturePolicy(raw);
    if (
      props.client.area === "admin" &&
      parsed.merchant_id !== props.merchantID
    )
      throw new Error("签名方式目标不一致");
    state.value = parsed;
    emit("observed", parsed);
    selection.value =
      state.value.keytype === null ? "" : String(state.value.keytype);
  } catch (e) {
    if (active && !c.signal.aborted && current === epoch)
      error.value =
        e instanceof ApiError && e.status === 403
          ? props.client.area === "merchant"
            ? "签名方式涉及接入安全，需要商户本人登录后查看。"
            : "当前管理员没有读取签名方式的权限，请联系超级管理员核对授权。"
          : "签名方式读取失败，请刷新重试。";
  } finally {
    if (active && current === epoch) loading.value = false;
  }
}
async function confirm() {
  const before = state.value;
  if (
    props.disabled ||
    busy.value ||
    loading.value ||
    !before?.can_update ||
    !["0", "1"].includes(selection.value) ||
    (selection.value === "1" && !before.rsa_configured) ||
    String(before.keytype) === selection.value
  )
    return;
  error.value = "";
  notice.value = "";
  confirming.value = true;
  await nextTick();
  cancelButton.value?.focus();
}
async function cancel() {
  if (busy.value) return;
  confirming.value = false;
  selection.value =
    state.value?.keytype === null ? "" : String(state.value?.keytype ?? "");
  await nextTick();
  select.value?.focus();
}
async function submit() {
  const before = state.value,
    desired = selection.value;
  if (
    props.disabled ||
    busy.value ||
    !confirming.value ||
    !before?.can_update ||
    !["0", "1"].includes(desired) ||
    (desired === "1" && !before.rsa_configured)
  )
    return;
  const c = new AbortController(),
    current = ++epoch;
  controller?.abort();
  controller = c;
  busy.value = true;
  emit("busy", before.merchant_id, true);
  error.value = "";
  notice.value = "";
  try {
    emit("discardLegacyAlgorithm", before.merchant_id, before.algorithm);
    await nextTick();
    if (!active || c.signal.aborted || current !== epoch) return;
    confirmGroup.value?.focus();
    const raw = await props.client.request<unknown>(
      signaturePolicyPath(props.client.area, props.merchantID),
      {
        method: "PATCH",
        body: {
          keytype: Number(desired),
          expected_fingerprint: before.fingerprint,
        },
        signal: c.signal,
      },
    );
    if (!active || c.signal.aborted || current !== epoch) return;
    state.value = readSignaturePolicyResult(
      raw,
      before,
      Number(desired) as 0 | 1,
    );
    emit("observed", state.value);
    confirming.value = false;
    notice.value = "签名方式已保存";
  } catch (e) {
    if (active && !c.signal.aborted && current === epoch) {
      state.value = undefined;
      selection.value = "";
      confirming.value = false;
      error.value =
        e instanceof ApiError && e.status === 409
          ? "签名配置已变更，请刷新后重新确认；不会自动重试。"
          : "本次签名方式修改未能确认，请刷新当前状态；不要直接重复提交。";
    }
  } finally {
    if (active && current === epoch) {
      busy.value = false;
      emit("busy", before.merchant_id, false);
      await nextTick();
      if (active && current === epoch)
        (error.value ? refresh.value : select.value)?.focus();
    }
  }
}
function keydown(e: KeyboardEvent) {
  if (busy.value && ["Tab", "Enter", "Escape"].includes(e.key)) {
    e.preventDefault();
    e.stopPropagation();
  } else if (e.key === "Escape" && confirming.value) {
    e.preventDefault();
    e.stopPropagation();
    void cancel();
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.reload,
    props.merchantID,
  ],
  () => {
    controller?.abort();
    ++epoch;
    busy.value = false;
    state.value = undefined;
    void load();
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  active = false;
  ++epoch;
  controller?.abort();
  state.value = undefined;
});
</script>
<template>
  <section
    class="panel account-section signature-policy"
    role="region"
    aria-label="签名方式设置"
    @keydown="keydown"
  >
    <h2>签名方式</h2>
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
    <p v-if="loading" role="status">正在读取签名方式…</p>
    <template v-else-if="state">
      <p>当前方式：{{ signaturePolicyLabel(state) }}</p>
      <p class="muted">
        MD5+RSA允许两种签名方式；V2接口使用RSA。仅RSA将停止接受MD5及V1密钥查询。
      </p>
      <div v-if="state.can_update">
        <label for="merchant-signature-policy">签名方式</label
        ><select
          id="merchant-signature-policy"
          ref="select"
          v-model="selection"
          :disabled="disabled || busy || confirming"
        >
          <option v-if="state.keytype === null" value="" disabled>
            {{ signaturePolicyLabel(state) }}
          </option>
          <option value="0">MD5+RSA兼容</option>
          <option value="1" :disabled="!state.rsa_configured">仅RSA</option>
        </select>
      </div>
      <p v-if="!state.rsa_configured" class="muted">
        请先生成商户RSA密钥对，再启用仅RSA。
      </p>
      <button
        v-if="state.can_update && !confirming"
        type="button"
        class="primary"
        :disabled="
          disabled ||
          busy ||
          !['0', '1'].includes(selection) ||
          String(state.keytype) === selection
        "
        @click="confirm"
      >
        保存签名方式
      </button>
      <div
        v-if="confirming"
        ref="confirmGroup"
        class="signature-confirm alert"
        role="group"
        aria-label="确认修改签名方式"
        tabindex="-1"
      >
        <p>目标商户：{{ state.merchant_id }}</p>
        <p>
          确定改为{{
            selection === "1" ? "仅RSA" : "MD5+RSA兼容"
          }}？请同步核对接入配置。
        </p>
        <div class="toolbar-actions">
          <button
            ref="cancelButton"
            type="button"
            :disabled="disabled || busy"
            @click="cancel"
          >
            取消</button
          ><button
            type="button"
            class="primary"
            :disabled="disabled || busy"
            @click="submit"
          >
            {{ busy ? "正在保存…" : "确定修改" }}
          </button>
        </div>
      </div>
    </template>
    <button
      v-if="!loading && !confirming"
      ref="refresh"
      type="button"
      :disabled="disabled || busy"
      @click="load"
    >
      刷新签名方式
    </button>
  </section>
</template>
<style scoped>
.signature-policy {
  overflow-wrap: anywhere;
}
.signature-policy select {
  display: block;
  width: 100%;
  margin-top: 8px;
}
.signature-policy > button {
  margin: 8px 8px 0 0;
}
.signature-confirm:focus {
  outline: 2px solid var(--primary, var(--focus));
  outline-offset: 2px;
}
</style>
