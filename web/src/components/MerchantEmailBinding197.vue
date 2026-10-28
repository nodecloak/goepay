<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { ApiClient, ApiError } from "../api";
const props = defineProps<{ client: ApiClient }>();
const emit = defineEmits<{ reauthenticate: [] }>();
type Metadata = {
  email: string;
  verified: boolean;
  email_enabled: boolean;
  requires: "email" | "password";
  fingerprint: string;
  email_interval?: number;
};
type Flow = {
  flow_id: string;
  phase: "old" | "new" | "closed" | "completed";
  original_email: string;
  email: string;
  delivery: "" | "unknown" | "sent" | "failed";
  expires_at: string;
  reauthenticate: boolean;
  send_id?: string;
  sent_at?: string;
};
const meta = ref<Metadata | null>(null),
  flow = ref<Flow | null>(null),
  opened = ref(false),
  busy = ref(false),
  loading = ref(false),
  error = ref(""),
  notice = ref(""),
  uncertain = ref(false);
const password = ref(""),
  code = ref(""),
  email = ref(""),
  flowID = ref(""),
  sendID = ref("");
const dialog = ref<HTMLElement>(),
  trigger = ref<HTMLButtonElement>();
const now = ref(Date.now());
const timer = setInterval(() => {
  now.value = Date.now();
}, 1000);
let epoch = 0,
  controller: AbortController | undefined,
  sourceClient: ApiClient | undefined,
  sourceCSRF = "";
const available = computed(
  () =>
    meta.value?.email_enabled === true &&
    sourceClient === props.client &&
    sourceCSRF === props.client.csrfToken &&
    props.client.area === "merchant",
);
const stage = computed(() =>
  flow.value?.phase === "new"
    ? "new"
    : flow.value?.phase === "old"
      ? "old"
      : "password",
);
const expired = computed(() =>
  Boolean(flow.value && Date.parse(flow.value.expires_at) <= now.value),
);
const remaining = computed(() => {
  const sent = flow.value?.sent_at ? Date.parse(flow.value.sent_at) : 0;
  return sent
    ? Math.max(
        0,
        Math.ceil(
          (sent + (meta.value?.email_interval || 60) * 1000 - now.value) / 1000,
        ),
      )
    : 0;
});
const canSend = computed(
  () =>
    !busy.value &&
    !uncertain.value &&
    !expired.value &&
    remaining.value === 0 &&
    flow.value?.delivery !== "unknown",
);
function randomID() {
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  return btoa(String.fromCharCode(...bytes))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replaceAll("=", "");
}
function clearDialog() {
  opened.value = false;
  flow.value = null;
  flowID.value = "";
  sendID.value = "";
  password.value = "";
  code.value = "";
  email.value = "";
  uncertain.value = false;
  busy.value = false;
}
function reset() {
  ++epoch;
  controller?.abort();
  controller = undefined;
  clearDialog();
  loading.value = false;
  meta.value = null;
  error.value = "";
  notice.value = "";
  sourceClient = undefined;
  sourceCSRF = "";
}
function current(turn: number, client: ApiClient, csrf: string) {
  return (
    turn === epoch &&
    props.client === client &&
    client.csrfToken === csrf &&
    client.area === "merchant"
  );
}
function readFlow(value: unknown): Flow {
  const v = value as Flow;
  if (
    !v ||
    v.flow_id !== flowID.value ||
    !["old", "new", "closed", "completed"].includes(v.phase) ||
    !["", "unknown", "sent", "failed"].includes(v.delivery) ||
    typeof v.original_email !== "string" ||
    typeof v.email !== "string" ||
    !Number.isFinite(Date.parse(v.expires_at)) ||
    typeof v.reauthenticate !== "boolean"
  )
    throw Error("邮箱验证状态无效，请查询原验证流程");
  return v;
}
async function load() {
  const turn = epoch,
    client = props.client,
    csrf = client.csrfToken;
  if (client.area !== "merchant" || !csrf) return;
  controller = new AbortController();
  loading.value = true;
  try {
    const v = await client.request<Metadata>("/account/email", {
      signal: controller.signal,
    });
    if (!current(turn, client, csrf)) return;
    if (
      !v ||
      typeof v.email !== "string" ||
      typeof v.verified !== "boolean" ||
      typeof v.email_enabled !== "boolean" ||
      !["email", "password"].includes(v.requires) ||
      !/^[a-f0-9]{64}$/.test(v.fingerprint) ||
      v.verified !== (v.email !== "") ||
      v.requires !== (v.verified ? "email" : "password")
    )
      throw Error("绑定邮箱状态无效");
    meta.value = v;
    sourceClient = client;
    sourceCSRF = csrf;
  } catch (e) {
    if (current(turn, client, csrf))
      error.value = e instanceof Error ? e.message : "读取绑定邮箱失败";
  } finally {
    if (current(turn, client, csrf)) loading.value = false;
  }
}
async function focus() {
  await nextTick();
  dialog.value
    ?.querySelector<HTMLInputElement>(
      "input:not(:disabled),button:not(:disabled)",
    )
    ?.focus();
}
function setFlow(v: Flow) {
  const prior = stage.value;
  flow.value = v;
  uncertain.value = false;
  if (v.send_id === sendID.value && v.delivery !== "unknown") sendID.value = "";
  if (v.email) email.value = v.email;
  if (prior !== stage.value) {
    code.value = "";
    sendID.value = "";
    void focus();
  }
  if (v.delivery === "failed")
    error.value =
      "发信结果未能确认。请勿重复提交本次发送；可查询状态或稍后重新获取验证码。";
  if (v.phase === "completed" && v.reauthenticate) {
    clearDialog();
    notice.value = "修改绑定成功，请重新登录";
    emit("reauthenticate");
  }
  if (v.phase === "closed") {
    clearDialog();
    void nextTick(() => trigger.value?.focus());
  }
}
async function action(
  path: string,
  body: Record<string, string>,
  method: "POST" | "DELETE" | "GET" = "POST",
) {
  if (busy.value) return;
  const turn = epoch,
    client = props.client,
    csrf = client.csrfToken;
  busy.value = true;
  error.value = "";
  try {
    const value = await client.request<unknown>(path, {
      method,
      body: method === "GET" || method === "DELETE" ? undefined : body,
      signal: controller?.signal,
    });
    if (!current(turn, client, csrf)) return;
    setFlow(readFlow(value));
  } catch (e) {
    if (!current(turn, client, csrf)) return;
    if (
      method === "DELETE" &&
      e instanceof ApiError &&
      e.responseDecoded &&
      [409, 422].includes(e.status)
    ) {
      // An expired or no-longer-applicable flow cannot authorize this context.
      // A lost response or temporary access rejection still requires a query.
      clearDialog();
      error.value = "";
      void nextTick(() => trigger.value?.focus());
      return;
    }
    code.value = "";
    password.value = "";
    uncertain.value = !(
      e instanceof ApiError &&
      e.responseDecoded &&
      e.status < 500
    );
    error.value = uncertain.value
      ? "操作结果未能确认，请先查询原验证流程。"
      : e instanceof Error
        ? e.message
        : "邮箱验证失败";
  } finally {
    if (current(turn, client, csrf)) {
      busy.value = false;
      password.value = "";
    }
  }
}
async function begin() {
  if (!meta.value || !available.value || busy.value) return;
  if (!flowID.value) flowID.value = randomID();
  await action("/account/email", {
    flow_id: flowID.value,
    expected_fingerprint: meta.value.fingerprint,
    current_password: password.value,
  });
}
async function show() {
  if (!available.value) return;
  clearDialog();
  error.value = "";
  notice.value = "";
  opened.value = true;
  if (meta.value?.requires === "email") await begin();
  await focus();
}
async function send() {
  if (!canSend.value || !flow.value) return;
  if (!sendID.value) sendID.value = randomID();
  const old = stage.value === "old";
  await action(
    `/account/email/${flowID.value}/${old ? "old-code" : "new-code"}`,
    old
      ? { send_id: sendID.value }
      : { send_id: sendID.value, email: email.value.trim().toLowerCase() },
  );
  if (!uncertain.value) sendID.value = "";
}
async function submit() {
  if (uncertain.value || busy.value || expired.value) return;
  if (!flow.value) {
    await begin();
    return;
  }
  await action(
    `/account/email/${flowID.value}/${stage.value === "old" ? "old-verify" : "complete"}`,
    { code: code.value },
  );
}
async function query() {
  if (!flowID.value) return;
  await action(`/account/email/${flowID.value}`, {}, "GET");
}
async function close() {
  if (busy.value) return;
  if (flowID.value) {
    await action(`/account/email/${flowID.value}`, {}, "DELETE");
  } else {
    clearDialog();
    await nextTick();
    trigger.value?.focus();
  }
}
function trap(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    void close();
    return;
  }
  if (e.key !== "Tab" || !dialog.value) return;
  const nodes = [
    ...dialog.value.querySelectorAll<HTMLElement>(
      "input:not(:disabled),button:not(:disabled)",
    ),
  ];
  const first = nodes[0],
    last = nodes.at(-1);
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last?.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first?.focus();
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
  clearInterval(timer);
});
</script>

<template>
  <div class="email-binding197">
    <div class="email-binding197-row">
      <span>绑定邮箱</span
      ><span>{{ loading ? "正在读取…" : meta?.email || "未绑定" }}</span
      ><button
        ref="trigger"
        type="button"
        data-email197="change"
        class="link-button"
        :disabled="!available || busy"
        @click="show"
      >
        修改
      </button>
    </div>
    <p v-if="meta && !meta.email_enabled" class="muted">
      暂未启用邮箱验证，请联系管理员配置发信邮箱。
    </p>
    <p v-if="error && !opened" class="alert error" role="alert">
      {{ error }} <button type="button" @click="load">重新读取</button>
    </p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <div v-if="opened" class="modal-backdrop" @keydown="trap">
      <section
        ref="dialog"
        class="modal panel email-binding197-dialog"
        role="dialog"
        aria-modal="true"
        :aria-label="stage === 'new' ? '修改绑定邮箱' : '安全验证'"
        :aria-busy="busy"
      >
        <h2>{{ stage === "new" ? "修改绑定邮箱" : "安全验证" }}</h2>
        <p v-if="error" class="alert error" role="alert">{{ error }}</p>
        <p v-if="expired" class="alert error" role="alert">
          验证已过期，请取消后重新验证。
        </p>
        <form @submit.prevent="submit">
          <template v-if="stage === 'password'"
            ><p>请先确认当前登录密码，再绑定新邮箱。</p>
            <label
              >当前登录密码<input
                v-model="password"
                type="password"
                autocomplete="current-password"
                required
                maxlength="1024"
                :disabled="busy || uncertain" /></label
          ></template>
          <template v-else>
            <label v-if="stage === 'old'"
              >验证方式<input value="邮箱验证" readonly
            /></label>
            <label v-if="stage === 'old'"
              >原绑定邮箱<input :value="flow?.original_email" readonly
            /></label>
            <label v-else
              >新邮箱<input
                v-model="email"
                type="email"
                autocomplete="email"
                required
                maxlength="254"
                :readonly="Boolean(flow?.email)"
                :disabled="busy || uncertain"
            /></label>
            <div class="email-binding197-verification">
              <label for="email197-code">验证码</label
              ><span class="email-binding197-code"
                ><input
                  id="email197-code"
                  v-model="code"
                  inputmode="numeric"
                  autocomplete="one-time-code"
                  pattern="[0-9]{7}"
                  minlength="7"
                  maxlength="7"
                  required
                  :disabled="busy || uncertain || expired"
                /><button
                  type="button"
                  data-email197="send"
                  :disabled="!canSend || (stage === 'new' && !email.trim())"
                  @click="send"
                >
                  {{ remaining ? `${remaining} 秒` : "获取验证码" }}
                </button></span
              >
            </div>
            <p class="muted">
              {{
                stage === "old"
                  ? "请先验证原绑定邮箱。"
                  : "请填写新邮箱并获取验证码。"
              }}验证流程在 10 分钟后过期。
            </p>
          </template>
          <div class="modal-actions">
            <button type="button" :disabled="busy" @click="close">取消</button
            ><button
              v-if="flowID"
              type="button"
              :disabled="busy"
              @click="query"
            >
              查询验证状态</button
            ><button class="primary" :disabled="busy || uncertain || expired">
              {{ busy ? "处理中…" : stage === "new" ? "确认修改" : "下一步" }}
            </button>
          </div>
        </form>
      </section>
    </div>
  </div>
</template>
<style scoped>
.email-binding197-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border, var(--line));
  font-size: 14px;
}
.email-binding197-row > span:first-child {
  min-width: 84px;
  color: var(--muted, var(--ink));
}
.email-binding197-row > span:nth-child(2) {
  overflow-wrap: anywhere;
}
.email-binding197-row button {
  margin-left: auto;
  min-height: var(--control-h);
}
.email-binding197-code {
  display: flex;
  gap: 8px;
  align-items: center;
}
.email-binding197-code input {
  min-width: 0;
  flex: 1;
}
.email-binding197-code button {
  white-space: nowrap;
  min-height: 40px;
}
.email-binding197-dialog {
  max-width: 500px;
  width: calc(100vw - 32px);
}
.email-binding197-dialog label {
  display: grid;
  gap: 8px;
  margin: 16px 0;
}
.email-binding197-dialog .modal-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}
@media (max-width: 480px) {
  .email-binding197-row {
    gap: 8px;
  }
  .email-binding197-dialog .modal-actions button {
    flex: 1;
  }
}
@media (prefers-reduced-motion: reduce) {
  .email-binding197-dialog {
    transition: none;
  }
}
</style>
