<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { ApiClient } from "../api";
const props = defineProps<{
  client: ApiClient;
  allowed?: boolean;
  algorithm?: unknown;
  fingerprint?: unknown;
  hasKey?: boolean;
}>();
const emit = defineEmits<{ refresh: [] }>();
const open = ref(false),
  busy = ref(false),
  password = ref(""),
  privateKey = ref(""),
  publicKey = ref(""),
  error = ref(""),
  uncertain = ref(false);
const dialog = ref<HTMLElement>(),
  trigger = ref<HTMLButtonElement>();
let epoch = 0,
  controller: AbortController | undefined;
const available = computed(
  () =>
    props.allowed === true &&
    props.client.area === "merchant" &&
    Boolean(props.client.csrfToken) &&
    ["MD5", "RSA"].includes(String(props.algorithm)) &&
    typeof props.fingerprint === "string" &&
    /^[0-9a-f]{64}$/.test(props.fingerprint),
);
function clear() {
  ++epoch;
  controller?.abort();
  controller = undefined;
  open.value = false;
  busy.value = false;
  password.value = "";
  privateKey.value = "";
  publicKey.value = "";
  error.value = "";
  uncertain.value = false;
}
function close() {
  const changed = Boolean(privateKey.value) || busy.value;
  clear();
  if (changed) emit("refresh");
  else void nextTick(() => trigger.value?.focus());
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.allowed,
    props.algorithm,
    props.fingerprint,
    props.hasKey,
  ],
  clear,
  { flush: "sync" },
);
onBeforeUnmount(clear);
function show() {
  clear();
  if (available.value) {
    open.value = true;
    void nextTick(() =>
      dialog.value?.querySelector<HTMLInputElement>("input")?.focus(),
    );
  }
}
function trap(e: KeyboardEvent) {
  if (e.key !== "Tab") return;
  const items = [
    ...(dialog.value?.querySelectorAll<HTMLElement>(
      "input,button:not(:disabled)",
    ) || []),
  ];
  const first = items[0],
    last = items.at(-1);
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last?.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first?.focus();
  }
}
function pem(
  raw: unknown,
  type: string,
  min: number,
  max: number,
): raw is string {
  return (
    typeof raw === "string" &&
    raw.length >= min &&
    raw.length <= max &&
    new RegExp(
      `^-----BEGIN ${type}-----\\r?\\n[A-Za-z0-9+/=\\r\\n]+\\r?\\n-----END ${type}-----\\r?\\n?$`,
    ).test(raw)
  );
}
async function generate() {
  if (!available.value || busy.value || !password.value || uncertain.value)
    return;
  const current = ++epoch,
    client = props.client,
    csrf = client.csrfToken,
    expected = props.fingerprint;
  controller = new AbortController();
  busy.value = true;
  error.value = "";
  try {
    const raw = await client.request<Record<string, unknown>>(
      "/account/rsa-key/rotate",
      {
        method: "POST",
        body: {
          current_password: password.value,
          expected_fingerprint: expected,
        },
        signal: controller.signal,
      },
    );
    if (
      current !== epoch ||
      client !== props.client ||
      csrf !== client.csrfToken ||
      !available.value
    )
      return;
    if (
      !raw ||
      raw.algorithm !== props.algorithm ||
      typeof raw.fingerprint !== "string" ||
      !/^[0-9a-f]{64}$/.test(raw.fingerprint) ||
      raw.fingerprint === expected ||
      !pem(raw.public_key, "PUBLIC KEY", 400, 8192) ||
      !pem(raw.private_key, "PRIVATE KEY", 1600, 16384)
    )
      throw Error("invalid result");
    privateKey.value = raw.private_key;
    publicKey.value = raw.public_key;
    open.value = false;
    password.value = "";
  } catch {
    if (current !== epoch) return;
    password.value = "";
    open.value = false;
    uncertain.value = true;
    error.value =
      "未能确认 RSA 密钥生成结果。请刷新接入信息，核对当前公钥后再明确发起新的重置；不会自动重试。";
    emit("refresh");
  } finally {
    if (current === epoch) {
      busy.value = false;
      password.value = "";
    }
  }
}
</script>
<template>
  <section v-if="available" class="panel account-section">
    <h2>商户 RSA 密钥对</h2>
    <p v-if="algorithm === 'MD5'" class="muted">
      当前仍使用 MD5 签名。此操作仅生成备用 RSA
      公钥，不会改变签名算法，也不会重置 API 密钥。
    </p>
    <p v-else class="muted">
      重置后旧 RSA 签名立即失效，请准备同步更新您的接入系统。此操作不会重置 MD5
      API 密钥。
    </p>
    <p class="muted">
      RSA 2048 位；公钥为 PKIX，私钥为
      PKCS8。私钥仅成功生成后展示一次，关闭或离开页面即清空，平台不保存私钥。
    </p>
    <div v-if="error" role="alert" class="alert error">{{ error }}</div>
    <button
      v-if="!privateKey && !uncertain"
      ref="trigger"
      :disabled="busy"
      @click="show"
    >
      {{ hasKey ? "重置" : "生成" }} RSA 密钥对
    </button>
    <template v-if="privateKey">
      <label
        >新 RSA 公钥<textarea
          aria-label="新 RSA 公钥"
          :value="publicKey"
          readonly
          rows="5"
          autocomplete="off"
          spellcheck="false"
        />
      </label>
      <label
        >新 RSA 私钥<textarea
          aria-label="新 RSA 私钥"
          :value="privateKey"
          readonly
          rows="8"
          autocomplete="off"
          spellcheck="false"
        />
      </label>
      <p class="muted">
        请保存到您自己的安全密钥管理位置；关闭后无法再次查询此私钥。
      </p>
      <button @click="close">已保存，关闭私钥</button>
    </template>
    <div v-if="open" class="modal-backdrop">
      <section
        ref="dialog"
        role="dialog"
        aria-modal="true"
        aria-label="确认生成 RSA 密钥对"
        class="modal panel"
        @keydown.esc="close"
        @keydown="trap"
      >
        <h2>{{ hasKey ? "重置" : "生成" }} RSA 密钥对</h2>
        <p>
          {{
            algorithm === "RSA"
              ? "旧 RSA 签名将立即失效。"
              : "签名算法保持 MD5，此密钥仅作备用。"
          }}私钥仅在本次成功响应展示。
        </p>
        <form @submit.prevent="generate">
          <div class="form-rows">
            <label
              ><span class="field-label field-label--req">确认当前登录密码</span
              ><input
                v-model="password"
                type="password"
                required
                maxlength="1024"
                autocomplete="current-password"
                autofocus
            /></label>
          </div>
          <div class="modal-actions">
            <button type="button" @click="close">关闭</button
            ><button class="danger-button" :disabled="busy">
              {{ busy ? "正在生成…" : "确认生成 RSA 密钥对" }}
            </button>
          </div>
        </form>
      </section>
    </div>
  </section>
</template>
