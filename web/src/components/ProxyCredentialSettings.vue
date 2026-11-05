<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import CredentialActionControl from "./CredentialActionControl.vue";
const props = withDefaults(
  defineProps<{
    client: ApiClient;
    ownSession?: boolean;
    adminClass?: string;
  }>(),
  { ownSession: false, adminClass: "" },
);
interface Metadata {
  password_set: boolean;
  apikey_set: boolean;
  fingerprint: string;
}
type Field = "password" | "apikey";
const state = ref<Metadata>(),
  draft = ref<Record<Field, string>>({ password: "", apikey: "" }),
  editing = ref<Field>(),
  clearing = ref<Field>(),
  loading = ref(false),
  busy = ref(false),
  unresolvedSave = ref(false),
  error = ref(""),
  notice = ref("");
const apikeyInput = ref<HTMLInputElement>(),
  passwordInput = ref<HTMLInputElement>();
const test = ref({ type: "http", server: "", port: "", user: "" }),
  testing = ref(false),
  testResult = ref("");
let active = true,
  epoch = 0,
  controller: AbortController | undefined;
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
const { confirmDiscard: confirmDraftDiscard } = useSettingsDraft212(
  "代理密码和接口密钥",
  () =>
    !!draft.value.password ||
    !!draft.value.apikey ||
    !!clearing.value ||
    unresolvedSave.value,
);
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
function clear(message = "", preserveUnresolved = false) {
  ++epoch;
  controller?.abort();
  controller = undefined;
  state.value = undefined;
  draft.value = { password: "", apikey: "" };
  editing.value = undefined;
  clearing.value = undefined;
  loading.value = false;
  busy.value = false;
  error.value = message;
  notice.value = "";
  if (!preserveUnresolved) unresolvedSave.value = false;
  tracked = context();
}
function current(
  run: number,
  c: AbortController,
  source: ReturnType<typeof context>,
) {
  if (!active || run !== epoch || c.signal.aborted) return false;
  if (!same(source)) {
    clear("会话已变化，请刷新页面后核对");
    return false;
  }
  return isAllowed();
}
function read(raw: unknown): Metadata {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw Error("代理密钥状态格式无法核对");
  const value = raw as Record<string, unknown>;
  if (
    Object.keys(value).length !== 3 ||
    typeof value.password_set !== "boolean" ||
    typeof value.apikey_set !== "boolean" ||
    typeof value.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(value.fingerprint)
  )
    throw Error("代理密钥状态格式无法核对");
  return {
    password_set: value.password_set,
    apikey_set: value.apikey_set,
    fingerprint: value.fingerprint,
  };
}
async function load() {
  if (busy.value || loading.value) return;
  const discardEpoch = epoch;
  if (
    !(await confirmDraftDiscard()) ||
    discardEpoch !== epoch ||
    busy.value ||
    loading.value ||
    !isAllowed()
  )
    return;
  clear("", true);
  if (!isAllowed()) return;
  const source = tracked,
    run = epoch,
    c = new AbortController();
  controller = c;
  loading.value = true;
  try {
    const raw = await source.client.request<unknown>(
      "/settings/proxy-credentials",
      { signal: c.signal },
    );
    if (current(run, c, source)) {
      state.value = read(raw);
      unresolvedSave.value = false;
    }
  } catch {
    if (current(run, c, source))
      error.value = "代理密钥状态读取失败，请刷新页面后核对";
  } finally {
    if (current(run, c, source)) loading.value = false;
  }
}
function edit(field: Field) {
  if (!same() || !isAllowed() || fieldBlocked(field) || !state.value) return;
  editing.value = field;
  clearing.value = undefined;
  draft.value = { password: "", apikey: "" };
  error.value = "";
  notice.value = "";
  void nextTick(() => {
    if (same() && editing.value === field)
      (field === "apikey" ? apikeyInput : passwordInput).value?.focus();
  });
}
function cancelEdit() {
  if (busy.value) return;
  editing.value = undefined;
  clearing.value = undefined;
  draft.value = { password: "", apikey: "" };
  unresolvedSave.value = false;
  error.value = "";
}
function fieldBlocked(field: Field) {
  return (
    busy.value ||
    loading.value ||
    (!!editing.value && editing.value !== field) ||
    (!!clearing.value && clearing.value !== field)
  );
}
function fieldAction(field: Field) {
  return editing.value === field
    ? "replace"
    : clearing.value === field
      ? "clear"
      : "keep";
}
function setAction(field: Field, action: "keep" | "replace" | "clear") {
  if (!same() || !isAllowed() || !state.value || fieldBlocked(field)) return;
  if (action === "keep") cancelEdit();
  else if (action === "replace") edit(field);
  else if (
    field === "password" ? state.value.password_set : state.value.apikey_set
  ) {
    cancelEdit();
    clearing.value = field;
    notice.value = "";
  }
}
function randomKey() {
  const chars = "ABCDEFGHJKMNPQRSTWXYZabcdefhijkmnprstwxyz2345678";
  const bytes = new Uint32Array(20);
  crypto.getRandomValues(bytes);
  draft.value.apikey = Array.from(bytes, (n) => chars[n % chars.length]).join(
    "",
  );
}
async function save(field: Field, action: "replace" | "clear") {
  if (!same() || !isAllowed() || busy.value || loading.value || !state.value)
    return;
  if (fieldAction(field) !== action) return;
  const body: Record<string, unknown> = {
    expected_fingerprint: state.value.fingerprint,
  };
  if (action === "replace") {
    if (!/^[\x20-\x7e]{1,256}$/.test(draft.value[field])) {
      error.value = "代理密钥须为1到256位，不能包含换行或控制字符";
      return;
    }
    body[field] = draft.value[field];
  } else {
    body[field === "apikey" ? "clear_apikey" : "clear_password"] = true;
  }
  const before = state.value,
    source = tracked,
    run = ++epoch,
    c = new AbortController();
  controller?.abort();
  controller = c;
  busy.value = true;
  unresolvedSave.value = true;
  editing.value = undefined;
  clearing.value = undefined;
  draft.value = { password: "", apikey: "" };
  error.value = "";
  notice.value = "";
  try {
    const raw = await source.client.request<unknown>(
      "/settings/proxy-credentials",
      { method: "PATCH", body, signal: c.signal },
    );
    if (!current(run, c, source)) return;
    const next = read(raw);
    if (next.fingerprint === before.fingerprint)
      throw Error("unconfirmed proxy credential operation");
    state.value = next;
    unresolvedSave.value = false;
    notice.value =
      (field === "apikey" ? "API接口密钥" : "代理密码") +
      (action === "replace" ? "已保存" : "已清除");
  } catch (reason) {
    if (!current(run, c, source)) return;
    if (
      reason instanceof ApiError &&
      reason.responseDecoded &&
      [400, 422].includes(reason.status)
    ) {
      unresolvedSave.value = false;
      error.value = "密钥操作未被接受，请核对后重新输入";
    } else {
      state.value = undefined;
      error.value =
        reason instanceof ApiError && reason.status === 409
          ? "代理密钥状态已变化，请刷新页面后核对；不会自动重试。"
          : "本次操作结果无法确认，请刷新页面后核对；不会自动重试。";
    }
  } finally {
    if (current(run, c, source)) busy.value = false;
  }
}
async function runTest() {
  if (!isAllowed() || testing.value) return;
  testResult.value = "";
  if (
    !["http", "https", "sock4", "sock5", "sock5h"].includes(test.value.type) ||
    !test.value.server ||
    !/^[1-9]\d*$/.test(test.value.port) ||
    Number(test.value.port) > 65535
  ) {
    testResult.value = "请填写代理IP与1-65535端口，并选择协议。";
    return;
  }
  testing.value = true;
  try {
    const raw = (await props.client.request("/settings/proxy-test", {
      method: "POST",
      body: {
        type: test.value.type,
        server: test.value.server,
        port: test.value.port,
        user: test.value.user,
      },
    })) as { reachable?: boolean; ip?: string };
    testResult.value = raw.reachable
      ? "连接成功，出口IP：" + (raw.ip || "")
      : "代理连接失败，请检查代理配置。";
  } catch {
    testResult.value = "代理连接测试失败，请稍后重试。";
  } finally {
    testing.value = false;
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
  <section class="panel proxy-credentials" aria-label="中转代理密钥">
    <div class="proxy-heading">
      <h2>代理密码与API接口密钥</h2>
    </div>
    <p v-if="!allowed" class="muted">请使用超级管理员本人会话配置代理密钥。</p>
    <template v-else>
      <p v-if="loading" role="status">正在读取代理密钥状态…</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
      <template v-if="state">
        <p class="muted">
          旧密钥不会显示，加密保存，仅在支付成功异步回调走中转代理时使用。
        </p>
        <div class="proxy-secret">
          <CredentialActionControl
            :model-value="fieldAction('password')"
            :configured="state.password_set"
            label="代理密码"
            :disabled="fieldBlocked('password')"
            @update:model-value="setAction('password', $event)"
          />
          <form
            v-if="editing === 'password' || clearing === 'password'"
            novalidate
            @submit.prevent="
              save('password', clearing === 'password' ? 'clear' : 'replace')
            "
          >
            <label v-if="editing === 'password'"
              >新的代理密码<input
                ref="passwordInput"
                v-model="draft.password"
                name="proxy_pwd"
                type="password"
                autocomplete="new-password"
                maxlength="256"
                :spellcheck="false"
                :disabled="busy"
            /></label>
            <p v-if="clearing === 'password'" class="muted">
              点击保存后才会清除代理密码，API接口密钥不受影响。
            </p>
            <div class="proxy-actions">
              <button type="submit" class="primary" :disabled="busy">
                保存代理密码
              </button>
            </div>
          </form>
        </div>
        <div class="proxy-secret">
          <CredentialActionControl
            :model-value="fieldAction('apikey')"
            :configured="state.apikey_set"
            label="API接口密钥"
            :disabled="fieldBlocked('apikey')"
            @update:model-value="setAction('apikey', $event)"
          />
          <form
            v-if="editing === 'apikey' || clearing === 'apikey'"
            novalidate
            @submit.prevent="
              save('apikey', clearing === 'apikey' ? 'clear' : 'replace')
            "
          >
            <label v-if="editing === 'apikey'"
              >新的API接口密钥<input
                ref="apikeyInput"
                v-model="draft.apikey"
                name="proxy_apikey"
                type="text"
                autocomplete="off"
                maxlength="256"
                :spellcheck="false"
                :disabled="busy"
            /></label>
            <p v-if="clearing === 'apikey'" class="muted">
              点击保存后才会清除API接口密钥，代理密码不受影响。
            </p>
            <div class="proxy-actions">
              <button
                v-if="editing === 'apikey'"
                type="button"
                :disabled="busy"
                @click="randomKey"
              >
                随机</button
              ><button type="submit" class="primary" :disabled="busy">
                保存API接口密钥
              </button>
            </div>
          </form>
        </div>
      </template>
      <div class="proxy-secret proxy-test">
        <p>代理服务器连通性测试（使用上方已保存的代理密码）</p>
        <div class="proxy-test-fields">
          <select v-model="test.type" aria-label="代理协议" :disabled="testing">
            <option value="http">HTTP</option>
            <option value="https">HTTPS</option>
            <option value="sock4">SOCK4</option>
            <option value="sock5">SOCK5</option>
            <option value="sock5h">SOCK5H</option>
          </select>
          <input
            v-model="test.server"
            aria-label="代理IP"
            placeholder="代理IP"
            :disabled="testing"
          />
          <input
            v-model="test.port"
            aria-label="代理端口"
            inputmode="numeric"
            placeholder="代理端口"
            :disabled="testing"
          />
          <input
            v-model="test.user"
            aria-label="代理账号"
            placeholder="代理账号（可选）"
            :disabled="testing"
          />
        </div>
        <div class="proxy-actions">
          <button type="button" :disabled="testing" @click="runTest">
            {{ testing ? "正在测试…" : "测试连通性" }}
          </button>
        </div>
        <p v-if="testResult" class="muted" role="status">{{ testResult }}</p>
      </div>
      <p class="muted proxy-footer">
        本功能开启后，在支付成功异步回调的时候，使用中转代理访问商户网站，可解决一些只能国内访问的网站回调问题，也可以防止本站服务器IP泄露。代理服务器可用CCProxy等自建；代理API需自行搭建对接站点，修改接口密钥后需重新对接。
      </p>
    </template>
  </section>
</template>
<style scoped>
.proxy-credentials {
  overflow-wrap: anywhere;
}
.proxy-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
}
.proxy-heading h2 {
  margin: 0;
}
.proxy-secret {
  margin-top: 18px;
  padding-top: 14px;
  border-top: 1px solid var(--line, var(--line-control));
}
.proxy-secret label {
  display: grid;
  gap: 8px;
  margin-top: 12px;
  min-width: 0;
}
.proxy-secret input {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
}
.proxy-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 12px;
}
.proxy-footer {
  margin-top: 20px;
  line-height: 1.7;
}
.proxy-test-fields {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 12px;
}
.proxy-test-fields input,
.proxy-test-fields select {
  min-width: 0;
  flex: 1 1 140px;
}
</style>
