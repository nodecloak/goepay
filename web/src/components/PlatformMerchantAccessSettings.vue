<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import {
  parsePlatformMerchantAccess,
  platformMerchantAccessBody,
  type AccessField,
  type PlatformMerchantAccess,
} from "../platform-merchant-access";
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
  field: AccessField;
}>();
const emit = defineEmits<{ busy: [value: boolean] }>();
const state = ref<PlatformMerchantAccess | null>(null),
  enabled = ref(false),
  loading = ref(false),
  busy = ref(false),
  confirming = ref(false),
  error = ref(""),
  notice = ref("");
const saveButton = ref<HTMLButtonElement>(),
  confirmButton = ref<HTMLButtonElement>(),
  reloadButton = ref<HTMLButtonElement>();
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.ownSession,
);
const label = computed(() =>
  props.field === "close_keylogin" ? "密钥登录" : "开启商户自助退款",
);
let epoch = 0,
  alive = true,
  controller: AbortController | undefined;
let tracked = capture();
function capture() {
  return {
    client: toRaw(props.client),
    csrf: toRaw(props.client).csrfToken,
    own: props.ownSession,
    role: props.adminClass,
    field: props.field,
  };
}
function same() {
  const v = capture();
  return Object.keys(v).every(
    (k) => v[k as keyof typeof v] === tracked[k as keyof typeof v],
  );
}
function clear(message = "") {
  epoch++;
  controller?.abort();
  state.value = null;
  enabled.value = false;
  confirming.value = false;
  loading.value = false;
  busy.value = false;
  error.value = message;
  notice.value = "";
  tracked = capture();
}
function ensure() {
  if (!same()) {
    clear("会话已变化，请重新读取平台功能设置");
    return false;
  }
  return alive && allowed.value;
}
function valid(run: number, c: AbortController) {
  if (!alive || run !== epoch || c.signal.aborted) return false;
  return ensure();
}
function accept(value: PlatformMerchantAccess) {
  state.value = value;
  enabled.value =
    props.field === "close_keylogin"
      ? value.close_keylogin === "0"
      : value.user_refund === "1";
}
async function load() {
  if (busy.value || loading.value) return;
  clear();
  if (!allowed.value) return;
  const run = epoch,
    c = new AbortController(),
    client = tracked.client;
  controller = c;
  loading.value = true;
  try {
    const v = await client.request("/settings/merchant-access", {
      signal: c.signal,
    });
    if (valid(run, c)) accept(parsePlatformMerchantAccess(v));
  } catch {
    if (valid(run, c)) error.value = "平台功能状态无法确认，请重新读取";
  } finally {
    if (valid(run, c)) loading.value = false;
  }
}
function prepare() {
  if (!ensure() || busy.value || loading.value || !state.value) return;
  confirming.value = true;
  void nextTick(() => {
    if (confirming.value) confirmButton.value?.focus();
  });
}
function cancel() {
  if (!ensure() || busy.value) return;
  confirming.value = false;
  void nextTick(() => saveButton.value?.focus());
}
async function save() {
  if (
    !ensure() ||
    busy.value ||
    loading.value ||
    !state.value ||
    !confirming.value
  )
    return;
  const previous = state.value,
    body = platformMerchantAccessBody(previous, props.field, enabled.value),
    client = tracked.client,
    run = ++epoch,
    c = new AbortController();
  controller?.abort();
  controller = c;
  confirming.value = false;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const value = await client.request("/settings/merchant-access", {
      method: "PATCH",
      body,
      signal: c.signal,
    });
    if (!valid(run, c)) return;
    const next = parsePlatformMerchantAccess(value);
    if (
      next.close_keylogin !== body.close_keylogin ||
      next.user_refund !== body.user_refund ||
      next.fingerprint === previous.fingerprint
    )
      throw Error("unconfirmed");
    accept(next);
    notice.value = "平台功能设置已保存";
  } catch (e) {
    if (valid(run, c)) {
      state.value = null;
      enabled.value = false;
      error.value =
        e instanceof ApiError && e.status === 409
          ? "设置已变化，请重新读取并核对后保存"
          : "保存结果无法确认，请重新读取后核对";
    }
  } finally {
    if (valid(run, c)) {
      busy.value = false;
      void nextTick(() => {
        if (valid(run, c))
          (state.value ? saveButton.value : reloadButton.value)?.focus();
      });
    }
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.adminClass,
    props.ownSession,
    props.field,
  ],
  () => {
    clear();
    void load();
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  clear();
});
watch(busy, (value) => emit("busy", value), { flush: "sync" });
</script>
<template>
  <section class="platform-access" :aria-label="label">
    <h3>{{ label }}</h3>
    <p v-if="!allowed" class="muted">此设置需要超级管理员本人会话。</p>
    <template v-else>
      <button
        ref="reloadButton"
        type="button"
        :disabled="busy || loading"
        @click="load"
      >
        重新读取平台功能
      </button>
      <p v-if="loading" role="status">正在读取平台功能…</p>
      <p v-if="error" role="alert" class="error">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <template v-if="state">
        <label class="platform-switch"
          ><input
            :name="field"
            v-model="enabled"
            type="checkbox"
            role="switch"
            :aria-checked="enabled"
            :disabled="busy || confirming"
            @change="ensure"
          /><span>{{ label }}</span></label
        >
        <p class="muted">
          {{
            field === "close_keylogin"
              ? "关闭后禁止新的商户密钥登录；密码登录和已有会话继续可用。"
              : "关闭后禁止商户新的 API 和后台自助退款；管理员授权退款及原退款的查询与恢复继续可用。"
          }}
        </p>
        <div
          v-if="confirming"
          role="group"
          aria-label="确认平台功能设置"
          @keydown.esc.prevent="cancel"
        >
          <p>
            确认{{ enabled ? "开启" : "关闭"
            }}{{ label }}？另一项平台功能将保留本次读取的状态。
          </p>
          <button
            ref="confirmButton"
            type="button"
            data-platform-access-confirm
            @click="save"
          >
            确认保存平台功能
          </button>
          <button type="button" @click="cancel">返回修改</button>
        </div>
        <button
          v-else
          ref="saveButton"
          type="button"
          data-platform-access-save
          :disabled="busy || loading"
          @click="prepare"
        >
          {{ busy ? "正在保存…" : "保存平台功能" }}
        </button>
      </template>
    </template>
  </section>
</template>
<style scoped>
.platform-access {
  border: 1px solid var(--border, var(--line-control));
  border-radius: 8px;
  padding: 16px;
  margin: 16px 0;
  overflow-wrap: anywhere;
}
.platform-access h3 {
  margin: 0 0 12px;
}
.platform-switch {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 16px 0;
}
.platform-switch input {
  appearance: none;
  width: 40px;
  min-width: 40px;
  height: 22px;
  border-radius: 12px;
  background: var(--line-control);
  border: 0;
  position: relative;
  cursor: pointer;
}
.platform-switch input::before {
  content: "";
  position: absolute;
  width: 16px;
  height: 16px;
  top: 3px;
  left: 3px;
  border-radius: 50%;
  background: var(--bg-card);
}
.platform-switch input:checked {
  background: var(--primary);
}
.platform-switch input:checked::before {
  left: 21px;
}
.platform-switch input:focus-visible {
  outline: 3px solid var(--focus);
  outline-offset: 3px;
}
.platform-switch input:disabled {
  opacity: 0.6;
  cursor: default;
}
.platform-access button {
  margin: 4px 8px 4px 0;
  max-width: 100%;
}
</style>
