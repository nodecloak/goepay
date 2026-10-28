<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient, ApiError } from "../api";
const props = withDefaults(
  defineProps<{
    client: ApiClient;
    ownSession?: boolean;
    adminClass?: string;
  }>(),
  { ownSession: false, adminClass: "" },
);
interface Metadata {
  configured: boolean;
  fingerprint: string;
}
interface Update {
  action: "replace" | "clear";
  key?: string;
  expected_fingerprint: string;
}
const state = ref<Metadata>(),
  draft = ref(""),
  editing = ref(false),
  confirming = ref<"replace" | "clear">(),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
const refresh = ref<HTMLButtonElement>(),
  saveButton = ref<HTMLButtonElement>(),
  clearButton = ref<HTMLButtonElement>(),
  cancelButton = ref<HTMLButtonElement>(),
  keyInput = ref<HTMLInputElement>();
let active = true,
  epoch = 0,
  controller: AbortController | undefined,
  prepared: Update | undefined;
function context() {
  return {
    client: toRaw(props.client),
    area: props.client.area,
    csrf: props.client.csrfToken,
    own: props.ownSession,
    adminClass: props.adminClass,
  };
}
let tracked = context();
function isAllowed() {
  return (
    props.ownSession === true &&
    props.adminClass === "super" &&
    props.client.area === "admin" &&
    !!props.client.csrfToken
  );
}
const allowed = computed(isAllowed);
function same(source = tracked) {
  const now = context();
  return (
    active &&
    source.client === now.client &&
    source.area === now.area &&
    source.csrf === now.csrf &&
    source.own === now.own &&
    source.adminClass === now.adminClass
  );
}
function clear(message = "") {
  ++epoch;
  controller?.abort();
  controller = undefined;
  state.value = undefined;
  draft.value = "";
  editing.value = false;
  confirming.value = undefined;
  prepared = undefined;
  loading.value = false;
  busy.value = false;
  error.value = message;
  notice.value = "";
  tracked = context();
}
function ensure() {
  if (!same()) {
    clear("会话已变化，请重新读取密钥状态");
    return false;
  }
  return isAllowed();
}
function current(
  run: number,
  c: AbortController,
  source: ReturnType<typeof context>,
) {
  if (!active || run !== epoch || c.signal.aborted) return false;
  if (!same(source)) {
    clear("会话已变化，请重新读取密钥状态");
    return false;
  }
  return isAllowed();
}
function read(raw: unknown): Metadata {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw Error("验证码密钥状态格式无法核对");
  const value = raw as Record<string, unknown>;
  if (
    Object.keys(value).length !== 2 ||
    typeof value.configured !== "boolean" ||
    typeof value.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(value.fingerprint)
  )
    throw Error("验证码密钥状态格式无法核对");
  return { configured: value.configured, fingerprint: value.fingerprint };
}
async function load() {
  if (busy.value || loading.value) return;
  clear();
  if (!isAllowed()) return;
  const source = tracked,
    run = epoch,
    c = new AbortController();
  controller = c;
  loading.value = true;
  try {
    const raw = await source.client.request<unknown>("/settings/captcha", {
      signal: c.signal,
    });
    if (current(run, c, source)) state.value = read(raw);
  } catch {
    if (current(run, c, source))
      error.value = "验证码密钥状态读取失败，请重新读取密钥状态";
  } finally {
    if (current(run, c, source)) loading.value = false;
  }
}
function edit() {
  if (!ensure() || busy.value || !state.value) return;
  editing.value = true;
  draft.value = "";
  notice.value = "";
  error.value = "";
  void nextTick(() => {
    if (same() && editing.value) keyInput.value?.focus();
  });
}
function cancelEdit() {
  if (!ensure() || busy.value) return;
  draft.value = "";
  editing.value = false;
  confirming.value = undefined;
  prepared = undefined;
  error.value = "";
  notice.value = "";
}
function prepare(action: "replace" | "clear") {
  if (
    !ensure() ||
    busy.value ||
    loading.value ||
    confirming.value ||
    !state.value
  )
    return;
  if (action === "replace" && !/^[\x21-\x7e]{16,256}$/.test(draft.value)) {
    error.value = "密钥须为16到256位，不能包含空白或换行";
    return;
  }
  if (action === "clear" && !state.value.configured) return;
  error.value = "";
  notice.value = "";
  prepared = {
    action,
    expected_fingerprint: state.value.fingerprint,
    ...(action === "replace" ? { key: draft.value } : {}),
  };
  confirming.value = action;
  void nextTick(() => {
    if (confirming.value === action && same()) cancelButton.value?.focus();
  });
}
function cancelConfirmation() {
  if (busy.value || !confirming.value) return;
  const action = confirming.value;
  confirming.value = undefined;
  prepared = undefined;
  void nextTick(() => {
    if (same())
      (action === "replace" ? saveButton.value : clearButton.value)?.focus();
  });
}
async function save() {
  if (
    !ensure() ||
    busy.value ||
    loading.value ||
    !state.value ||
    !confirming.value ||
    !prepared
  )
    return;
  const before = state.value,
    body = { ...prepared },
    source = tracked,
    run = ++epoch,
    c = new AbortController();
  controller?.abort();
  controller = c;
  prepared = undefined;
  draft.value = "";
  editing.value = false;
  confirming.value = undefined;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const raw = await source.client.request<unknown>("/settings/captcha", {
      method: "PATCH",
      body,
      signal: c.signal,
    });
    if (!current(run, c, source)) return;
    const next = read(raw);
    if (
      next.fingerprint === before.fingerprint ||
      next.configured !== (body.action === "replace")
    )
      throw Error("unconfirmed CAPTCHA key operation");
    state.value = next;
    notice.value =
      body.action === "replace" ? "验证码密钥已保存" : "验证码密钥已清除";
  } catch (reason) {
    if (!current(run, c, source)) return;
    if (
      reason instanceof ApiError &&
      reason.responseDecoded &&
      [400, 422].includes(reason.status)
    ) {
      error.value = "密钥操作未被接受，请核对后重新输入";
    } else {
      state.value = undefined;
      error.value =
        reason instanceof ApiError && reason.status === 409
          ? "密钥状态已变化，请重新读取密钥状态并核对；不会自动重试。"
          : "本次密钥操作结果无法确认，请重新读取密钥状态核对；不会自动重试。";
    }
  } finally {
    if (current(run, c, source)) {
      busy.value = false;
      void nextTick(() => {
        if (current(run, c, source) && !state.value) refresh.value?.focus();
      });
    }
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
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
  <section class="panel captcha-key203" aria-label="极验验证码密钥">
    <div class="captcha-heading">
      <h2>极验验证码密钥</h2>
      <button
        v-if="allowed"
        ref="refresh"
        type="button"
        :disabled="loading || busy || !!confirming"
        @click="load"
      >
        重新读取密钥状态
      </button>
    </div>
    <p v-if="!allowed" class="muted">
      请使用超级管理员本人会话配置验证码密钥。
    </p>
    <template v-else>
      <p v-if="loading" role="status">正在读取密钥状态…</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
      <template v-if="state"
        ><p>
          当前密钥：<strong>{{
            state.configured ? "已配置" : "未配置"
          }}</strong>
        </p>
        <p class="muted">
          旧密钥不会显示。验证码ID、版本和各场景开关在网站配置中设置；保存密钥不会执行验证码校验。
        </p>
        <form v-if="editing" novalidate @submit.prevent="prepare('replace')">
          <label
            >新的极验验证码密钥<input
              ref="keyInput"
              v-model="draft"
              name="new_captcha_key"
              type="password"
              autocomplete="new-password"
              maxlength="256"
              :spellcheck="false"
              :disabled="busy || !!confirming"
          /></label>
          <div v-if="!confirming" class="captcha-actions">
            <button
              ref="saveButton"
              type="submit"
              class="primary"
              :disabled="busy"
            >
              保存新密钥</button
            ><button type="button" :disabled="busy" @click="cancelEdit">
              取消编辑
            </button>
          </div>
        </form>
        <div v-else-if="!confirming" class="captcha-actions">
          <button
            type="button"
            class="primary"
            :disabled="busy || loading"
            @click="edit"
          >
            {{ state.configured ? "替换密钥" : "设置密钥" }}</button
          ><button
            v-if="state.configured"
            ref="clearButton"
            type="button"
            :disabled="busy || loading"
            @click="prepare('clear')"
          >
            清除密钥
          </button>
        </div>
        <div
          v-if="confirming"
          class="captcha-confirm"
          role="group"
          aria-label="确认验证码密钥操作"
          @keydown.esc.prevent.stop="cancelConfirmation"
        >
          <p>
            {{
              confirming === "clear"
                ? "确认清除验证码密钥？需要验证码的操作将在重新配置后才能继续。"
                : "确认保存新的验证码密钥？旧密钥会被替换，现有验证码需要重新获取。"
            }}
          </p>
          <div class="captcha-actions">
            <button type="button" class="primary" @click="save">
              {{ confirming === "clear" ? "确认清除" : "确认保存" }}</button
            ><button
              ref="cancelButton"
              type="button"
              @click="cancelConfirmation"
            >
              返回修改
            </button>
          </div>
        </div>
      </template>
    </template>
  </section>
</template>
<style scoped>
.captcha-key203 {
  overflow-wrap: anywhere;
}
.captcha-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
}
.captcha-heading h2 {
  margin: 0;
}
.captcha-key203 label {
  display: grid;
  gap: 8px;
  margin-top: 18px;
  min-width: 0;
}
.captcha-key203 input {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
}
.captcha-key203 p {
  line-height: 1.7;
}
.captcha-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 16px;
}
.captcha-confirm {
  margin-top: 18px;
  padding: 14px;
  background: var(--surface-alt, var(--bg-raised));
  border: 1px solid var(--line, var(--line-control));
  border-radius: 6px;
}
.captcha-confirm p {
  margin: 0;
}
.captcha-key203 button:focus-visible,
.captcha-key203 input:focus-visible {
  outline: 2px solid var(--primary, var(--focus));
  outline-offset: 3px;
}
</style>
