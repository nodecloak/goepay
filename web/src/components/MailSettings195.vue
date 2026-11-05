<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch, nextTick } from "vue";
import { ApiClient, ApiError } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import { readMailAccount195, type MailAccount195 } from "../mail-accounts195";
import MailFrequency194 from "./MailFrequency194.vue";
import MailTest195 from "./MailTest195.vue";
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
}>();
const emit = defineEmits<{ busy: [boolean] }>();
const tab = ref<"all" | "channel" | "rate">("all"),
  busy = ref(false),
  rateBusy = ref(false),
  testBusy = ref(false),
  testDraftDirty = ref(false),
  loading = ref(false),
  error = ref(""),
  notice = ref(""),
  state = ref<MailAccount195 | null>(null),
  opened = ref(false);
const draft = ref({
  host: "",
  port: "465",
  username: "",
  password: "",
  enabled: false,
});
const hasDraft = ref(false),
  unresolvedDraft = ref(false),
  visitedRate = ref(true);
const draftDirty = computed(
  () =>
    unresolvedDraft.value ||
    (hasDraft.value &&
      (!state.value ||
        draft.value.host !== state.value.config.host ||
        draft.value.port !== state.value.config.port ||
        draft.value.username !== state.value.config.username ||
        draft.value.enabled !== (state.value.status === 1) ||
        !!draft.value.password)),
);
const { confirmDiscard: confirmDraftDiscard } = useSettingsDraft212(
  "SMTP账号",
  () => draftDirty.value,
);
watch(tab, (value) => {
  if (value === "rate") visitedRate.value = true;
});
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.ownSession,
);
let epoch = 0,
  alive = true,
  ac: AbortController | undefined,
  readyClient: ApiClient | undefined,
  readyCSRF = "";
const dialogElement = ref<HTMLElement>();
let previousFocus: HTMLElement | null = null;
function trapFocus(event: KeyboardEvent) {
  if (event.key !== "Tab" || !dialogElement.value) return;
  const items = Array.from(
    dialogElement.value.querySelectorAll<HTMLElement>(
      "input:not([disabled]),button:not([disabled])",
    ),
  );
  const first = items[0],
    last = items.at(-1);
  if (!first || !last) return;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
function reset() {
  epoch++;
  ac?.abort();
  state.value = null;
  draft.value = {
    host: "",
    port: "465",
    username: "",
    password: "",
    enabled: false,
  };
  opened.value = false;
  busy.value = false;
  loading.value = false;
  readyClient = undefined;
  readyCSRF = "";
  hasDraft.value = false;
  unresolvedDraft.value = false;
}
function current(
  run: number,
  client: ApiClient,
  csrf: string,
  signal: AbortSignal,
) {
  if (!alive || run !== epoch || signal.aborted) return false;
  if (
    !allowed.value ||
    client !== toRaw(props.client) ||
    csrf !== toRaw(props.client).csrfToken
  ) {
    reset();
    error.value = "会话已变化，请刷新页面后核对";
    return false;
  }
  return true;
}
async function load() {
  if (!allowed.value || busy.value || loading.value) return;
  const discardEpoch = epoch,
    discardClient = toRaw(props.client),
    discardCSRF = discardClient.csrfToken;
  if (
    !(await confirmDraftDiscard(["测试邮件"])) ||
    !alive ||
    discardEpoch !== epoch ||
    !allowed.value ||
    busy.value ||
    loading.value ||
    discardClient !== toRaw(props.client) ||
    discardCSRF !== toRaw(props.client).csrfToken
  )
    return;
  const wasUnresolved = unresolvedDraft.value;
  reset();
  unresolvedDraft.value = wasUnresolved;
  error.value = "";
  notice.value = "";
  const run = epoch,
    client = toRaw(props.client),
    csrf = client.csrfToken,
    control = new AbortController();
  ac = control;
  loading.value = true;
  try {
    const v = readMailAccount195(
      await client.request("/settings/mail-channels/smtp", {
        signal: control.signal,
      }),
    );
    if (!current(run, client, csrf, control.signal)) return;
    state.value = v;
    unresolvedDraft.value = false;
    readyClient = client;
    readyCSRF = csrf;
  } catch {
    if (current(run, client, csrf, control.signal))
      error.value = "SMTP账号状态无法确认，请刷新页面后核对";
  } finally {
    if (current(run, client, csrf, control.signal)) loading.value = false;
  }
}
function close() {
  if (busy.value) return;
  draft.value = {
    host: "",
    port: "465",
    username: "",
    password: "",
    enabled: false,
  };
  opened.value = false;
  hasDraft.value = false;
  unresolvedDraft.value = false;
}
function suspend() {
  if (!busy.value) opened.value = false;
}
async function open() {
  if (
    busy.value ||
    !state.value ||
    readyClient !== toRaw(props.client) ||
    readyCSRF !== toRaw(props.client).csrfToken
  ) {
    reset();
    error.value = "会话已变化，请刷新页面后核对";
    return;
  }
  const c = state.value.config;
  previousFocus = document.activeElement as HTMLElement | null;
  if (!hasDraft.value)
    draft.value = { ...c, password: "", enabled: state.value.status === 1 };
  hasDraft.value = true;
  opened.value = true;
  error.value = "";
  notice.value = "";
  await nextTick();
  document.getElementById("mail195-host")?.focus();
}
async function save() {
  if (!allowed.value || busy.value || !state.value) return;
  if (
    readyClient !== toRaw(props.client) ||
    readyCSRF !== toRaw(props.client).csrfToken
  ) {
    reset();
    error.value = "会话已变化，请刷新页面后核对";
    return;
  }
  if (testDraftDirty.value) {
    error.value =
      "请先核对原测试邮件结果，或取消未提交的测试邮件，再修改SMTP账号。";
    return;
  }
  const originalDraft = draft.value;
  const before = state.value,
    config = {
      host: draft.value.host,
      port: draft.value.port,
      username: draft.value.username,
    },
    same = Object.entries(config).every(
      ([k, v]) => v === before.config[k as keyof typeof config],
    );
  if (
    !config.host ||
    !config.username ||
    !/^[1-9]\d*$/.test(config.port) ||
    Number(config.port) > 65535 ||
    (!draft.value.password && (!same || !before.password_set))
  ) {
    error.value =
      "请填写完整SMTP账号；更换服务器、端口或账号时请重新填写邮箱密码";
    return;
  }
  const body: any = {
    name: "smtp",
    status: draft.value.enabled ? 1 : 0,
    config,
    expected_fingerprint: before.fingerprint,
  };
  if (draft.value.password)
    body.config = { ...config, password: draft.value.password };
  else body.keep_password = true;
  const run = ++epoch,
    client = readyClient,
    csrf = readyCSRF,
    control = new AbortController();
  ac?.abort();
  ac = control;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = readMailAccount195(
      await client.request("/settings/mail-channels/smtp", {
        method: "PATCH",
        body,
        signal: control.signal,
      }),
    );
    if (!current(run, client, csrf, control.signal)) return;
    if (
      !v.configured ||
      v.fingerprint === before.fingerprint ||
      BigInt(v.revision) !== BigInt(before.revision) + 1n ||
      v.status !== body.status ||
      !v.password_set ||
      Object.entries(config).some(
        ([k, x]) => v.config[k as keyof typeof config] !== x,
      )
    )
      throw Error("save result mismatch");
    state.value = v;
    opened.value = false;
    hasDraft.value = false;
    unresolvedDraft.value = false;
    notice.value = "保存成功";
  } catch (e) {
    if (current(run, client, csrf, control.signal)) {
      unresolvedDraft.value = true;
      if (
        e instanceof ApiError &&
        e.responseDecoded &&
        e.status >= 400 &&
        e.status < 500 &&
        e.status !== 409
      ) {
        error.value = "保存未完成，请核对草稿后重试。" + e.message;
      } else {
        state.value = null;
        opened.value = false;
        error.value = "保存结果无法确认，当前草稿仍保留；请刷新页面后核对";
      }
    }
  } finally {
    originalDraft.password = "";
    if (body.config) delete body.config.password;
    if (current(run, client, csrf, control.signal)) busy.value = false;
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.adminClass,
    props.ownSession,
  ],
  () => {
    reset();
    void load();
  },
  { immediate: true, flush: "sync" },
);
watch(
  () => busy.value || rateBusy.value || opened.value || testBusy.value,
  (v) => emit("busy", v),
  { flush: "sync" },
);
watch(opened, (v) => {
  if (!v)
    void nextTick(() => previousFocus?.isConnected && previousFocus.focus());
});
onBeforeUnmount(() => {
  alive = false;
  reset();
  emit("busy", false);
});
</script>
<template>
  <section aria-label="邮箱配置">
    <div role="tablist" aria-label="邮箱配置分类" class="settings-tabs">
      <button
        role="tab"
        :aria-selected="tab === 'all'"
        :disabled="busy || rateBusy || opened || testBusy"
        @click="tab = 'all'"
      >
        查看全部
      </button>
      <button
        role="tab"
        :aria-selected="tab === 'channel'"
        :disabled="busy || rateBusy || opened || testBusy"
        @click="tab = 'channel'"
      >
        邮箱渠道配置
      </button>
      <button
        role="tab"
        :aria-selected="tab === 'rate'"
        :disabled="busy || rateBusy || opened || testBusy"
        @click="tab = 'rate'"
      >
        发送频率配置
      </button>
    </div>
    <MailFrequency194
      v-if="visitedRate"
      v-show="tab !== 'channel'"
      :client="client"
      :admin-class="adminClass"
      :own-session="ownSession"
      @busy="rateBusy = $event"
    />
    <div v-show="tab !== 'rate'">
      <p v-if="!allowed" class="muted">账号设置需要超级管理员本人会话。</p>
      <template v-else>
        <p v-if="loading" role="status">正在读取邮箱渠道…</p>
        <p v-if="error" role="alert" class="alert error">{{ error }}</p>
        <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
        <template v-if="state">
          <p v-if="!state.configured" class="muted">
            尚未接管在线账号，实际发信沿用部署配置；保存后使用本页账号。
          </p>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>邮箱渠道名称</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                <tr>
                  <td>SMTP发信</td>
                  <td>{{ state.status === 1 ? "已开启" : "已关闭" }}</td>
                  <td>
                    <button
                      @click="open"
                      :disabled="busy || opened || testBusy"
                    >
                      {{ draftDirty ? "继续编辑草稿" : "账号设置" }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <MailTest195
            :client="client"
            :account="state"
            :allowed="allowed"
            @busy="testBusy = $event"
            @dirty="testDraftDirty = $event"
          />
        </template>
      </template>
    </div>
    <div
      v-if="opened"
      class="modal-backdrop"
      @keydown.esc.prevent="close"
      @keydown="trapFocus"
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label="SMTP发信账号设置"
        ref="dialogElement"
        class="modal panel"
      >
        <h2>SMTP发信账号设置</h2>
        <form @submit.prevent="save" autocomplete="off">
          <div class="form-rows">
            <label
              ><span class="field-label">启用状态</span
              ><span class="mail195-switch"
                ><input
                  type="checkbox"
                  aria-label="启用状态"
                  v-model="draft.enabled"
                  :disabled="busy"
                />{{ draft.enabled ? "开启" : "关闭" }}</span
              ></label
            >
            <label for="mail195-host"
              ><span class="field-label field-label--req">SMTP服务器</span
              ><input
                id="mail195-host"
                aria-label="SMTP服务器"
                name="host"
                v-model="draft.host"
                required
                maxlength="253"
                :disabled="busy"
            /></label>
            <label
              ><span class="field-label field-label--req">SMTP端口</span
              ><input
                aria-label="SMTP端口"
                name="port"
                v-model="draft.port"
                inputmode="numeric"
                pattern="[1-9][0-9]*"
                required
                maxlength="5"
                :disabled="busy"
            /></label>
            <label
              ><span class="field-label field-label--req">邮箱账号</span
              ><input
                aria-label="邮箱账号"
                name="username"
                v-model="draft.username"
                type="email"
                required
                maxlength="254"
                :disabled="busy"
            /></label>
            <label
              ><span class="field-label">邮箱密码</span
              ><input
                aria-label="邮箱密码"
                name="password"
                v-model="draft.password"
                type="password"
                autocomplete="new-password"
                maxlength="1024"
                :placeholder="
                  state?.password_set
                    ? '已设置，留空保留原密码'
                    : '填写邮箱密码或授权码'
                "
                :disabled="busy"
            /></label>
          </div>
          <p class="muted">常用端口465(SSL)或587(TLS)。QQ邮箱使用授权码。</p>
          <p v-if="error" role="alert" class="alert error">{{ error }}</p>
          <div class="modal-actions">
            <button type="button" :disabled="busy" @click="suspend">
              收起并保留草稿
            </button>
            <button type="button" :disabled="busy" @click="close">取消</button
            ><button class="primary" type="submit" :disabled="busy">
              {{ busy ? "保存中…" : "保存" }}
            </button>
          </div>
        </form>
      </section>
    </div>
  </section>
</template>
<style scoped>
.settings-tabs {
  display: flex;
  overflow-x: auto;
  gap: 8px;
  border-bottom: 1px solid var(--line);
  margin: 12px 0 24px;
  padding: 0 0 8px;
}
.settings-tabs button {
  white-space: nowrap;
  background: none;
  border-color: transparent;
  flex-shrink: 0;
}
.settings-tabs button[aria-selected="true"] {
  color: var(--accent);
  border-bottom-color: var(--accent);
  border-radius: 0;
}
/* The enable toggle keeps its checkbox and its 开启/关闭 text on one line in
   the control column. */
.mail195-switch {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}
</style>
