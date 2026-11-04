<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import KitModal from "./KitModal.vue";
import KitSelect from "./KitSelect.vue";
import KitSwitch from "./KitSwitch.vue";
import CredentialActionControl from "./CredentialActionControl.vue";

const props = defineProps<{
  client: ApiClient;
  kind: "robot" | "telegram" | "qqbot";
  ownSession: boolean;
  adminClass?: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{ busy: [boolean] }>();
interface State {
  revision: string;
  enabled: boolean;
  app_id_masked: string;
  secret_present: boolean;
  bound: boolean;
  recipient_masked: string;
  phone_masked: string;
  token_source: string;
}
interface Attempt {
  id: string;
  challenge: string;
  expires_at: string;
}
const state = ref<State>(),
  enabled = ref(false),
  action = ref("keep"),
  appID = ref(""),
  secret = ref(""),
  phone = ref(""),
  tokenSource = ref("dedicated"),
  loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  confirming = ref<"save" | "unbind">(),
  error = ref(""),
  notice = ref(""),
  binding = ref<Attempt>(),
  remaining = ref(0);
const title = computed(() =>
  props.kind === "robot"
    ? "管理员群机器人配置"
    : props.kind === "telegram"
      ? "管理员Telegram配置"
      : "管理员QQ机器人配置",
);
const path = computed(() => `/settings/notice-accounts/${props.kind}`);
const callbackURL = computed(
  () => `${window.location.origin}/payment/qqbot/webhook`,
);
const accountLabel = computed(() =>
  props.kind === "robot"
    ? "群机器人Webhook地址"
    : props.kind === "telegram"
      ? "管理员Telegram Chat Id"
      : "管理员QQ机器人AppID",
);
const secretLabel = computed(() =>
  props.kind === "telegram" ? "Telegram Bot Token" : "管理员QQ机器人AppSecret",
);
const allowed = computed(
  () =>
    props.ownSession &&
    props.adminClass === "super" &&
    props.client.area === "admin" &&
    !!props.client.csrfToken,
);
const dirty = computed(
  () =>
    !!state.value &&
    (enabled.value !== state.value.enabled ||
      action.value !== "keep" ||
      !!appID.value ||
      !!secret.value ||
      !!phone.value),
);
const editingBlocked = computed(
  () =>
    busy.value ||
    loading.value ||
    uncertain.value ||
    !!confirming.value ||
    !!binding.value ||
    props.disabled,
);
const canBind = computed(
  () =>
    allowed.value &&
    state.value?.enabled &&
    state.value.secret_present &&
    !state.value.bound &&
    !dirty.value &&
    !editingBlocked.value,
);
const { confirmDiscard } = useSettingsDraft212(
  title.value,
  () => dirty.value || uncertain.value || !!binding.value,
);
let alive = true,
  epoch = 0,
  controller: AbortController | undefined,
  pollTimer: ReturnType<typeof setTimeout> | undefined,
  expiryTimer: ReturnType<typeof setTimeout> | undefined,
  clockTimer: ReturnType<typeof setInterval> | undefined;
function context() {
  return [
    toRaw(props.client),
    props.client.area,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
    props.kind,
  ] as const;
}
function current(run: number, c: readonly unknown[]) {
  return (
    alive &&
    epoch === run &&
    c.every((v, i) => v === context()[i]) &&
    allowed.value
  );
}
function clearBinding() {
  clearTimeout(pollTimer);
  clearTimeout(expiryTimer);
  clearInterval(clockTimer);
  pollTimer = undefined;
  expiryTimer = undefined;
  clockTimer = undefined;
  binding.value = undefined;
  remaining.value = 0;
}
function clearPrivate() {
  appID.value = "";
  secret.value = "";
  phone.value = "";
  action.value = "keep";
  tokenSource.value =
    state.value?.token_source === "oauth" ? "oauth" : "dedicated";
  confirming.value = undefined;
}
function setCredentialAction(next: "keep" | "replace" | "clear") {
  if (editingBlocked.value || (next === "clear" && enabled.value)) return;
  action.value = next;
  if (next !== "replace") {
    appID.value = "";
    secret.value = "";
    phone.value = "";
    tokenSource.value =
      state.value?.token_source === "oauth" ? "oauth" : "dedicated";
  }
}
function parse(v: State): State {
  if (
    !v ||
    typeof v.revision !== "string" ||
    !/^(0|[1-9]\d{0,18})$/.test(v.revision) ||
    typeof v.enabled !== "boolean" ||
    typeof v.secret_present !== "boolean" ||
    typeof v.bound !== "boolean" ||
    ![
      v.app_id_masked,
      v.recipient_masked,
      v.phone_masked,
      v.token_source,
    ].every((x) => typeof x === "string") ||
    !["", "oauth", "dedicated"].includes(v.token_source)
  )
    throw Error("配置响应不完整，请重新读取");
  return {
    revision: v.revision,
    enabled: v.enabled,
    app_id_masked: v.app_id_masked,
    secret_present: v.secret_present,
    bound: v.bound,
    recipient_masked: v.recipient_masked,
    phone_masked: v.phone_masked,
    token_source: v.token_source,
  };
}
function apply(v: State) {
  state.value = parse(v);
  enabled.value = state.value.enabled;
  uncertain.value = false;
  clearPrivate();
}
function begin() {
  const run = ++epoch,
    c = context();
  controller?.abort();
  controller = new AbortController();
  return { run, c, signal: controller.signal };
}
async function load(force = false) {
  if (
    !allowed.value ||
    busy.value ||
    loading.value ||
    binding.value ||
    (!force && !(await confirmDiscard()))
  )
    return;
  const { run, c, signal } = begin();
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = await props.client.request<State>(path.value, { signal });
    if (current(run, c)) apply(v);
  } catch (e) {
    if (current(run, c)) {
      state.value = undefined;
      clearPrivate();
      error.value = e instanceof Error ? e.message : "读取失败";
    }
  } finally {
    if (current(run, c)) loading.value = false;
  }
}
function writable() {
  return (
    allowed.value &&
    !!state.value &&
    !busy.value &&
    !loading.value &&
    !uncertain.value &&
    !props.disabled
  );
}
function check() {
  if (!writable() || binding.value || !dirty.value) return;
  error.value = "";
  notice.value = "";
  if (enabled.value && action.value === "clear") {
    error.value = "清除凭据前请先关闭开关。";
    return;
  }
  if (
    action.value === "replace" &&
    ((!appID.value.trim() &&
      !(props.kind === "robot" && !enabled.value && !!phone.value.trim())) ||
      (props.kind !== "robot" &&
        !(props.kind === "telegram" && tokenSource.value === "oauth") &&
        !secret.value))
  ) {
    error.value = "请填写完整的账号与凭据后再保存。";
    return;
  }
  if (
    enabled.value &&
    action.value === "keep" &&
    !state.value!.secret_present
  ) {
    error.value = "请先填写完整凭据后再开启。";
    return;
  }
  confirming.value = "save";
}
function failMutation(e: unknown) {
  uncertain.value = !(
    e instanceof ApiError &&
    e.responseDecoded &&
    [400, 422].includes(e.status)
  );
  error.value = uncertain.value
    ? "操作结果尚未确认，请重新读取配置后核对；不会自动重试。"
    : e instanceof Error
      ? e.message
      : "配置未被接受";
}
async function save() {
  if (!writable() || confirming.value !== "save" || binding.value) return;
  const { run, c, signal } = begin();
  const body = {
    expected_revision: state.value!.revision,
    enabled: enabled.value,
    credential_action: action.value,
    app_id: action.value === "replace" ? appID.value : "",
    secret:
      action.value === "replace" &&
      props.kind !== "robot" &&
      !(props.kind === "telegram" && tokenSource.value === "oauth")
        ? secret.value
        : "",
    phone:
      action.value === "replace" && props.kind === "robot" ? phone.value : "",
    token_source:
      action.value === "replace" && props.kind === "telegram"
        ? tokenSource.value
        : "",
  };
  busy.value = true;
  emit("busy", true);
  error.value = "";
  clearPrivate();
  try {
    const v = await props.client.request<State>(path.value, {
      method: "PUT",
      body,
      signal,
    });
    if (current(run, c)) {
      apply(v);
      notice.value = "配置已保存。";
    }
  } catch (e) {
    if (current(run, c)) failMutation(e);
  } finally {
    if (current(run, c)) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
function expireBinding() {
  ++epoch;
  controller?.abort();
  clearBinding();
  notice.value = "本次绑定码已过期，请重新读取配置后再开始绑定。";
}
function schedulePoll(run: number, c: readonly unknown[]) {
  pollTimer = setTimeout(() => {
    void poll(run, c);
  }, 2000);
}
async function poll(run: number, c: readonly unknown[]) {
  if (!current(run, c) || !binding.value) return;
  controller = new AbortController();
  try {
    const v = parse(
      await props.client.request<State>(path.value, {
        signal: controller.signal,
      }),
    );
    if (!current(run, c) || !binding.value) return;
    if (v.bound) {
      clearBinding();
      apply(v);
      notice.value = "QQ机器人已绑定。";
    } else if (v.revision !== state.value?.revision || !v.enabled) {
      clearBinding();
      apply(v);
      notice.value = "配置已变化，请重新核对后开始绑定。";
    } else schedulePoll(run, c);
  } catch {
    if (current(run, c)) {
      clearBinding();
      uncertain.value = true;
      error.value = "绑定状态尚未确认，请重新读取配置后核对；不会自动重试。";
    }
  }
}
async function startBinding() {
  if (!canBind.value || props.kind !== "qqbot") return;
  const { run, c, signal } = begin();
  busy.value = true;
  emit("busy", true);
  error.value = "";
  notice.value = "";
  try {
    const v = await props.client.request<Attempt>(path.value + "/binding", {
      method: "POST",
      body: { expected_revision: state.value!.revision },
      signal,
    });
    if (current(run, c)) {
      const expires = Date.parse(v?.expires_at);
      if (
        !v ||
        !/^[a-f0-9]{48}$/.test(v.id) ||
        !/^GOEPAY-[a-f0-9]{20}$/.test(v.challenge) ||
        !Number.isFinite(expires) ||
        expires <= Date.now()
      )
        throw Error("绑定响应不完整");
      const end = Math.min(expires, Date.now() + 300000);
      binding.value = {
        id: v.id,
        challenge: v.challenge,
        expires_at: v.expires_at,
      };
      const tick = () => {
        remaining.value = Math.max(0, Math.ceil((end - Date.now()) / 1000));
      };
      tick();
      clockTimer = setInterval(tick, 1000);
      expiryTimer = setTimeout(expireBinding, end - Date.now());
      schedulePoll(run, c);
    }
  } catch (e) {
    if (current(run, c)) {
      clearBinding();
      failMutation(e);
    }
  } finally {
    if (current(run, c)) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
async function cancelBinding() {
  if (!writable() || !binding.value) return;
  const id = binding.value.id;
  const { run, c, signal } = begin();
  clearBinding();
  busy.value = true;
  emit("busy", true);
  error.value = "";
  notice.value = "";
  try {
    const v = await props.client.request<{ cancelled: boolean }>(
      path.value + "/binding/" + id,
      { method: "DELETE", signal },
    );
    if (current(run, c)) {
      if (v?.cancelled !== true) throw Error("取消结果不完整");
      notice.value = "本次绑定窗口已取消。";
    }
  } catch (e) {
    if (current(run, c)) failMutation(e);
  } finally {
    if (current(run, c)) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
function confirmUnbind() {
  if (writable() && !dirty.value && state.value?.bound && !binding.value)
    confirming.value = "unbind";
}
async function unbind() {
  if (!writable() || confirming.value !== "unbind" || !state.value?.bound)
    return;
  const { run, c, signal } = begin();
  busy.value = true;
  emit("busy", true);
  confirming.value = undefined;
  error.value = "";
  notice.value = "";
  try {
    const v = await props.client.request<State>(path.value + "/binding", {
      method: "DELETE",
      body: { expected_revision: state.value.revision },
      signal,
    });
    if (current(run, c)) {
      apply(v);
      notice.value = "QQ绑定已解除。";
    }
  } catch (e) {
    if (current(run, c)) failMutation(e);
  } finally {
    if (current(run, c)) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
function discard() {
  if (!state.value || busy.value || uncertain.value || binding.value) return;
  enabled.value = state.value.enabled;
  clearPrivate();
  error.value = "";
  notice.value = "";
}
async function copyCallback() {
  try {
    await navigator.clipboard.writeText(callbackURL.value);
    notice.value = "Webhook URL已复制。";
  } catch {
    error.value = "复制失败，请选中Webhook URL手动复制。";
  }
}
watch(action, (value) => {
  if (value !== "replace") {
    appID.value = "";
    secret.value = "";
    phone.value = "";
  }
});
watch(tokenSource, (value) => {
  if (value === "oauth") secret.value = "";
});
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
    props.kind,
  ],
  () => {
    ++epoch;
    controller?.abort();
    clearBinding();
    state.value = undefined;
    clearPrivate();
    loading.value = false;
    busy.value = false;
    uncertain.value = false;
    error.value = "";
    notice.value = "";
    emit("busy", false);
    if (allowed.value) void load(true);
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  ++epoch;
  controller?.abort();
  clearBinding();
  clearPrivate();
  emit("busy", false);
});
</script>

<template>
  <section class="notification-bots231" :aria-label="title">
    <p v-if="!allowed" class="alert">请使用超级管理员本人会话管理配置。</p>
    <template v-else>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <button
        type="button"
        :disabled="busy || loading || !!binding || disabled"
        @click="load()"
      >
        重新读取{{ title }}
      </button>
      <p v-if="loading" role="status">正在读取配置…</p>
      <form v-if="state" @submit.prevent="check">
        <fieldset :disabled="editingBlocked">
          <div class="bot-row231">
            <span>{{ title }}开关</span
            ><KitSwitch
              v-model="enabled"
              :aria-label="`${title}开关`"
              :label="enabled ? '开启' : '关闭'"
              :disabled="editingBlocked"
            />
          </div>
          <CredentialActionControl
            :model-value="action"
            :configured="state.secret_present || !!state.phone_masked"
            :label="`${title}凭据`"
            :disabled="editingBlocked"
            :clear-disabled="enabled"
            clear-hint="请先关闭开关，再清除凭据。"
            @update:model-value="setCredentialAction"
          />
          <label class="bot-row231"
            ><span>{{ accountLabel }}</span
            ><input
              v-model="appID"
              :type="kind === 'robot' ? 'password' : 'text'"
              :aria-label="accountLabel"
              autocomplete="off"
              :maxlength="kind === 'robot' ? 4096 : 128"
              :placeholder="state.app_id_masked || '未配置'"
              :disabled="action !== 'replace' || editingBlocked"
              :required="
                action === 'replace' &&
                (kind !== 'robot' || enabled || !phone.trim())
              "
          /></label>
          <label v-if="kind === 'robot'" class="bot-row231"
            ><span>管理员手机号码</span
            ><input
              v-model="phone"
              type="tel"
              aria-label="管理员手机号码"
              autocomplete="off"
              maxlength="32"
              :placeholder="state.phone_masked || '未配置（可留空）'"
              :disabled="action !== 'replace' || editingBlocked"
          /></label>
          <label v-if="kind === 'telegram'" class="bot-row231"
            ><span>Token来源</span
            ><KitSelect
              v-model="tokenSource"
              aria-label="Telegram Token来源"
              :disabled="action !== 'replace' || editingBlocked"
              :options="[
                { value: 'dedicated', label: '填写独立Token' },
                { value: 'oauth', label: '复用快捷登录Telegram Token' },
              ]"
          /></label>
          <label v-if="kind !== 'robot'" class="bot-row231"
            ><span>{{ secretLabel }}</span
            ><input
              v-model="secret"
              type="password"
              :aria-label="secretLabel"
              autocomplete="new-password"
              maxlength="1024"
              :placeholder="
                kind === 'telegram' && tokenSource === 'oauth'
                  ? '使用快捷登录中保存的Token'
                  : state.secret_present
                    ? '已保存（不回显）'
                    : '未配置'
              "
              :disabled="
                action !== 'replace' ||
                editingBlocked ||
                (kind === 'telegram' && tokenSource === 'oauth')
              "
              :required="
                action === 'replace' &&
                !(kind === 'telegram' && tokenSource === 'oauth')
              "
          /></label>
          <p
            v-if="
              kind === 'robot' && !state.secret_present && state.phone_masked
            "
            class="muted"
          >
            管理员手机已保存；Webhook未配置。开启群机器人前请填写完整Webhook
            URL。
          </p>
          <p v-else class="muted">
            {{
              state.secret_present
                ? "凭据已配置，加密保存，不回显原值。"
                : "凭据未配置，请点击“设置”填写。"
            }}未修改的凭据无需重填；停用后可清除。
          </p>
          <p v-if="kind === 'robot'" class="muted">
            支持企业微信、钉钉和飞书群机器人。管理员手机可用于群机器人@和管理员全部交易投诉短信；短信仅支持中国大陆手机号。
          </p>
          <p v-if="kind === 'telegram'" class="muted">
            Chat
            Id可填写管理员或群组会话ID。选择复用时，请先在快捷登录配置中保存Telegram
            Token。
          </p>
          <p v-if="kind === 'qqbot'" class="muted">
            填写QQ机器人AppID和AppSecret并保存，再开始绑定。替换凭据会解除原绑定。
          </p>
          <div class="bot-actions231">
            <button type="submit" :disabled="editingBlocked || !dirty">
              保存{{ title }}</button
            ><button
              type="button"
              :disabled="editingBlocked || !dirty"
              @click="discard"
            >
              取消编辑
            </button>
          </div>
        </fieldset>
      </form>
      <template v-if="state && kind === 'qqbot'">
        <label class="bot-row231"
          ><span>Webhook URL</span
          ><span class="bot-callback231"
            ><input
              :value="callbackURL"
              aria-label="QQ机器人Webhook URL"
              readonly
            /><button
              type="button"
              :disabled="busy || disabled"
              @click="copyCallback"
            >
              复制Webhook URL
            </button></span
          ></label
        >
        <p role="status">
          {{ state.bound ? `已绑定 ${state.recipient_masked}` : "未绑定" }}
        </p>
        <p class="muted">
          在QQ开放平台为该机器人订阅Webhook的C2C消息事件。开始绑定后，将下方完整绑定码私聊发送给机器人，五分钟内有效。
        </p>
        <div class="bot-actions231">
          <button type="button" :disabled="!canBind" @click="startBinding">
            开始QQ绑定</button
          ><button
            type="button"
            :disabled="editingBlocked || dirty || !state.bound"
            @click="confirmUnbind"
          >
            解除QQ绑定
          </button>
        </div>
        <div v-if="binding" class="bot-binding231" role="status">
          <p>请向机器人私聊发送本次绑定码：</p>
          <code>{{ binding.challenge }}</code>
          <p>剩余 {{ remaining }} 秒。绑定码仅保留在当前页面内存中。</p>
          <button
            type="button"
            :disabled="busy || disabled"
            @click="cancelBinding"
          >
            取消本次绑定
          </button>
        </div>
      </template>
      <KitModal
        v-if="confirming"
        :title="confirming === 'save' ? `确认保存${title}` : '确认解除QQ绑定'"
        :submit-text="confirming === 'save' ? '确认保存' : '确认解绑'"
        :loading="busy"
        @close="confirming = undefined"
        @submit="confirming === 'save' ? save() : unbind()"
      >
        <p v-if="confirming === 'save'">
          将{{ enabled ? "开启" : "关闭" }}配置并{{
            action === "replace"
              ? "替换"
              : action === "clear"
                ? "清除"
                : "保留"
          }}凭据。{{
            kind === "qqbot" && action !== "keep" ? "原QQ绑定将解除。" : ""
          }}保存不会发送消息或开始绑定。
        </p>
        <p v-else>解除当前QQ接收人绑定后，需要重新开始绑定才能接收QQ消息。</p>
      </KitModal>
    </template>
  </section>
</template>

<style scoped>
.notification-bots231 fieldset {
  border: 0;
  padding: 0;
  margin: 0;
}
.bot-row231 {
  display: grid;
  grid-template-columns: 240px minmax(0, 1fr);
  align-items: center;
  gap: 18px;
  margin: 16px 0;
}
.bot-row231 > span:first-child {
  font-weight: 500;
}
.bot-row231 input {
  width: 100%;
  max-width: 640px;
  min-width: 0;
}
.notification-bots231 p {
  line-height: 1.7;
}
.bot-actions231,
.bot-callback231 {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
}
.bot-callback231 input {
  flex: 1 1 280px;
}
.bot-binding231 {
  margin-top: 16px;
  padding: 12px 16px;
  border: 1px solid var(--border, #d1d5db);
  border-radius: 6px;
}
.bot-binding231 code {
  user-select: text;
  overflow-wrap: anywhere;
  font-size: 1rem;
}
@media (max-width: 760px) {
  .bot-row231 {
    grid-template-columns: 1fr;
    gap: 8px;
  }
}
</style>
