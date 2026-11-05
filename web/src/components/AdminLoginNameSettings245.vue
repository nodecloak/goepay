<script setup lang="ts">
import { onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import KitModal from "./KitModal.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";

const props = defineProps<{ client: ApiClient }>();
const emit = defineEmits<{ reauthenticate: [message: string] }>();
const username = ref(""),
  draft = ref(""),
  password = ref(""),
  editing = ref(false),
  busy = ref(false),
  loading = ref(false),
  uncertain = ref(false),
  error = ref("");
let alive = true,
  epoch = 0,
  controller: AbortController | undefined;
function context() {
  return {
    client: toRaw(props.client),
    csrf: props.client.csrfToken,
    area: props.client.area,
  };
}
let tracked = context();
function same() {
  const now = context();
  return (
    alive &&
    tracked.client === now.client &&
    tracked.csrf === now.csrf &&
    tracked.area === now.area
  );
}
function reset() {
  ++epoch;
  controller?.abort();
  controller = undefined;
  username.value = draft.value = password.value = error.value = "";
  editing.value = busy.value = loading.value = uncertain.value = false;
  tracked = context();
}
function current(run: number, c: AbortController) {
  if (!alive || run !== epoch || c.signal.aborted) return false;
  if (!same()) {
    reset();
    error.value = "会话已变化，请重新读取账户信息";
    return false;
  }
  return true;
}
async function load() {
  if (busy.value || loading.value || editing.value || uncertain.value) return;
  if (!same()) reset();
  if (props.client.area !== "admin" || !props.client.csrfToken) return;
  const run = ++epoch,
    c = new AbortController();
  controller = c;
  loading.value = true;
  error.value = "";
  try {
    const value = await props.client.request<{
      username: string;
      auth_version: string;
    }>("/account/login-name", { signal: c.signal });
    if (!current(run, c)) return;
    if (
      !value ||
      typeof value.username !== "string" ||
      !value.username ||
      typeof value.auth_version !== "string" ||
      !/^[1-9]\d*$/.test(value.auth_version)
    )
      throw Error("账户信息格式无法核对");
    username.value = value.username;
  } catch (e) {
    if (current(run, c))
      error.value = e instanceof Error ? e.message : "读取账户信息失败";
  } finally {
    if (current(run, c)) {
      loading.value = false;
      controller = undefined;
    }
  }
}
function edit() {
  if (!same()) {
    reset();
    void load();
    return;
  }
  if (busy.value || loading.value || uncertain.value || !username.value) return;
  draft.value = username.value;
  password.value = "";
  error.value = "";
  editing.value = true;
}
function cancel() {
  if (busy.value) return;
  editing.value = false;
  draft.value = password.value = "";
  error.value = "";
}
async function save() {
  if (!same()) {
    reset();
    error.value = "会话已变化，请重新读取账户信息";
    return;
  }
  if (!editing.value || busy.value || loading.value || uncertain.value) return;
  error.value = "";
  if (!/^[a-zA-Z0-9_.@-]{3,100}$/.test(draft.value)) {
    error.value = "用户名须为 3 至 100 位字母、数字或 _ . @ -";
    return;
  }
  if (draft.value === username.value) {
    error.value = "请输入与当前不同的新用户名";
    return;
  }
  if (!password.value || password.value.length > 1024) {
    error.value = "请填写当前登录密码（最多 1024 个字符）";
    return;
  }
  const run = ++epoch,
    c = new AbortController(),
    next = draft.value;
  controller = c;
  busy.value = true;
  try {
    const value = await props.client.request<{
      username: string;
      reauthenticate: boolean;
    }>("/account/login-name", {
      method: "POST",
      body: {
        username: next,
        expected_username: username.value,
        current_password: password.value,
      },
      signal: c.signal,
    });
    if (!current(run, c)) return;
    if (!value || value.username !== next || value.reauthenticate !== true)
      throw Error("用户名修改结果无法核对");
    editing.value = false;
    draft.value = password.value = "";
    emit("reauthenticate", "用户名已修改，请使用新用户名重新登录");
  } catch (e) {
    if (!current(run, c)) return;
    const rejected =
      e instanceof ApiError &&
      e.responseDecoded &&
      e.status >= 400 &&
      e.status < 500 &&
      e.status !== 408;
    if (rejected) error.value = e.message;
    else {
      uncertain.value = true;
      editing.value = false;
      draft.value = "";
      error.value =
        "未能确认用户名修改结果，请使用新用户名重新登录核对，不会自动重试。";
    }
  } finally {
    if (current(run, c)) {
      password.value = "";
      busy.value = false;
      controller = undefined;
    }
  }
}
watch(
  () => [props.client, props.client.area, props.client.csrfToken],
  () => {
    reset();
    void load();
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  reset();
  alive = false;
});
</script>
<template>
  <section class="panel admin-login-name245" aria-label="管理员账户">
    <h2>管理员账户</h2>
    <p v-if="loading" role="status">正在读取账户信息…</p>
    <p v-if="error && !editing" class="alert error" role="alert">{{ error }}</p>
    <div class="admin-login-name245-row">
      <span>用户名</span><strong>{{ username || "暂未读取" }}</strong>
      <button
        type="button"
        class="pa-btn"
        aria-label="修改管理员用户名"
        :disabled="busy || loading || uncertain || !username"
        @click="edit"
      >
        修改
      </button>
      <button
        v-if="!username && !uncertain"
        type="button"
        class="pa-btn"
        :disabled="loading"
        @click="load"
      >
        重新读取
      </button>
    </div>
    <p class="muted">
      修改后需使用新用户名重新登录；动态验证码和资金密码继续有效。
    </p>
    <div v-if="$slots.default" class="admin-login-name245-security">
      <slot />
    </div>
    <KitModal
      v-if="editing"
      title="修改管理员用户名"
      submit-text="保存用户名"
      :loading="busy"
      @close="cancel"
      @submit="save"
    >
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <KitField label="新用户名" required
        ><KitInput
          v-model="draft"
          aria-label="新用户名"
          required
          :maxlength="100"
          autocomplete="username"
          :disabled="busy"
      /></KitField>
      <p class="muted">3 至 100 位，支持字母、数字和 _ . @ -。</p>
      <KitField label="当前登录密码" required
        ><KitInput
          v-model="password"
          aria-label="当前登录密码"
          type="password"
          required
          :maxlength="1024"
          autocomplete="current-password"
          :disabled="busy"
      /></KitField>
      <p class="muted">
        保存后会退出该管理员的现有登录会话，请确认新用户名已记住。
      </p>
    </KitModal>
  </section>
</template>
<style scoped>
.admin-login-name245-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  min-width: 0;
}
.admin-login-name245-row strong {
  overflow-wrap: anywhere;
}
.admin-login-name245-security {
  margin-top: var(--space-5);
  padding-top: var(--space-5);
  border-top: 1px solid var(--line);
}
.admin-login-name245-security :deep(.account-pages),
.admin-login-name245-security :deep(.account-section) {
  max-width: none;
}
.admin-login-name245-security :deep(.account-section) {
  padding: 0;
  margin: 0;
  border: 0;
  border-radius: 0;
  background: transparent;
}
.admin-login-name245-security :deep(.account-section > h2) {
  padding: 0;
  margin: 0 0 var(--space-4);
  border: 0;
  font-size: 14px;
}
.admin-login-name245-security :deep(.account-kit-form) {
  max-width: 680px;
}
.admin-login-name245-security
  :deep(.account-section .account-kit-form > label.pa-field) {
  display: grid;
  grid-template-columns: 112px minmax(0, 1fr);
  margin: 0;
}
.admin-login-name245-security :deep(.account-kit-form > p.muted),
.admin-login-name245-security :deep(.account-kit-actions) {
  margin: 0 0 0 128px;
}
@media (max-width: 600px) {
  .admin-login-name245-security
    :deep(.account-section .account-kit-form > label.pa-field) {
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
  }
  .admin-login-name245-security :deep(.account-kit-form > p.muted),
  .admin-login-name245-security :deep(.account-kit-actions) {
    margin-left: 0;
  }
}
</style>
