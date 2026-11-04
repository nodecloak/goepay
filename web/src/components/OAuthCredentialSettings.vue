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
  qq_set: boolean;
  wx_set: boolean;
  alipay_set: boolean;
  telegram_set: boolean;
  fingerprint: string;
}
type Field = "qq" | "wx" | "alipay" | "telegram";
const rows: { field: Field; title: string; hint: string }[] = [
  {
    field: "qq",
    title: "QQ互联 APPKEY",
    hint: "QQ互联应用的 APP KEY，加密保存。",
  },
  {
    field: "wx",
    title: "微信开放平台 AppSecret",
    hint: "微信网站应用的 AppSecret 或中转密钥，加密保存。",
  },
  {
    field: "alipay",
    title: "支付宝开放平台应用密钥",
    hint: "支付宝第三方登录应用的密钥或中转密钥，加密保存。",
  },
  {
    field: "telegram",
    title: "Telegram Bot Token",
    hint: "通过 @BotFather 获取的 Bot Token，加密保存。",
  },
];
const state = ref<Metadata>(),
  draft = ref<Record<Field, string>>({
    qq: "",
    wx: "",
    alipay: "",
    telegram: "",
  }),
  editing = ref<Field>(),
  clearing = ref<Field>(),
  loading = ref(false),
  busy = ref(false),
  unresolvedSave = ref(false),
  error = ref(""),
  notice = ref("");
const inputs = ref<Record<Field, HTMLInputElement | undefined>>({
  qq: undefined,
  wx: undefined,
  alipay: undefined,
  telegram: undefined,
});
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
  "快捷登录应用密钥",
  () =>
    Object.values(draft.value).some(Boolean) ||
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
function setFromField(field: Field, meta: Metadata): boolean {
  return field === "qq"
    ? meta.qq_set
    : field === "wx"
      ? meta.wx_set
      : field === "alipay"
        ? meta.alipay_set
        : meta.telegram_set;
}
function clear(message = "", preserveUnresolved = false) {
  ++epoch;
  controller?.abort();
  controller = undefined;
  state.value = undefined;
  draft.value = { qq: "", wx: "", alipay: "", telegram: "" };
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
    clear("会话已变化，请重新读取快捷登录密钥状态");
    return false;
  }
  return isAllowed();
}
function read(raw: unknown): Metadata {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw Error("快捷登录密钥状态格式无法核对");
  const value = raw as Record<string, unknown>;
  if (
    Object.keys(value).length !== 5 ||
    typeof value.qq_set !== "boolean" ||
    typeof value.wx_set !== "boolean" ||
    typeof value.alipay_set !== "boolean" ||
    typeof value.telegram_set !== "boolean" ||
    typeof value.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(value.fingerprint)
  )
    throw Error("快捷登录密钥状态格式无法核对");
  return {
    qq_set: value.qq_set,
    wx_set: value.wx_set,
    alipay_set: value.alipay_set,
    telegram_set: value.telegram_set,
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
      "/settings/oauth-credentials",
      { signal: c.signal },
    );
    if (current(run, c, source)) {
      state.value = read(raw);
      unresolvedSave.value = false;
    }
  } catch {
    if (current(run, c, source))
      error.value = "快捷登录密钥状态读取失败，请重新读取";
  } finally {
    if (current(run, c, source)) loading.value = false;
  }
}
function edit(field: Field) {
  if (!same() || !isAllowed() || fieldBlocked(field) || !state.value) return;
  editing.value = field;
  clearing.value = undefined;
  draft.value = { qq: "", wx: "", alipay: "", telegram: "" };
  error.value = "";
  notice.value = "";
  void nextTick(() => {
    if (same() && editing.value === field) inputs.value[field]?.focus();
  });
}
function cancelEdit() {
  if (busy.value) return;
  editing.value = undefined;
  clearing.value = undefined;
  draft.value = { qq: "", wx: "", alipay: "", telegram: "" };
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
  else if (setFromField(field, state.value)) {
    cancelEdit();
    clearing.value = field;
    notice.value = "";
  }
}
async function save(field: Field, action: "replace" | "clear") {
  if (!same() || !isAllowed() || busy.value || loading.value || !state.value)
    return;
  if (fieldAction(field) !== action) return;
  const body: Record<string, unknown> = {
    field,
    expected_fingerprint: state.value.fingerprint,
  };
  if (action === "replace") {
    if (!/^[\x20-\x7e]{1,512}$/.test(draft.value[field])) {
      error.value = "密钥须为1到512位，不能包含换行或控制字符";
      return;
    }
    body.value = draft.value[field];
  } else {
    body.clear = true;
  }
  const before = state.value,
    source = tracked,
    run = ++epoch,
    c = new AbortController(),
    title = rows.find((r) => r.field === field)!.title;
  controller?.abort();
  controller = c;
  busy.value = true;
  unresolvedSave.value = true;
  editing.value = undefined;
  clearing.value = undefined;
  draft.value = { qq: "", wx: "", alipay: "", telegram: "" };
  error.value = "";
  notice.value = "";
  try {
    const raw = await source.client.request<unknown>(
      "/settings/oauth-credentials",
      { method: "PATCH", body, signal: c.signal },
    );
    if (!current(run, c, source)) return;
    const next = read(raw);
    if (next.fingerprint === before.fingerprint)
      throw Error("unconfirmed oauth credential operation");
    state.value = next;
    unresolvedSave.value = false;
    notice.value = title + (action === "replace" ? "已保存" : "已清除");
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
          ? "快捷登录密钥状态已变化，请重新读取并核对；不会自动重试。"
          : "本次操作结果无法确认，请重新读取核对；不会自动重试。";
    }
  } finally {
    if (current(run, c, source)) busy.value = false;
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
  <section class="panel oauth-credentials" aria-label="快捷登录密钥">
    <div class="oauth-heading">
      <h2>各平台登录密钥</h2>
      <button
        v-if="allowed"
        type="button"
        :disabled="loading || busy"
        @click="load"
      >
        重新读取快捷登录密钥状态
      </button>
    </div>
    <p v-if="!allowed" class="muted">
      请使用超级管理员本人会话配置快捷登录密钥。
    </p>
    <template v-else>
      <p v-if="loading" role="status">正在读取快捷登录密钥状态…</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
      <p class="muted">
        旧密钥不会显示，加密保存。各平台的开关与 APPID /
        中转地址在上方字段中配置。
      </p>
      <template v-if="state">
        <div v-for="row in rows" :key="row.field" class="oauth-secret">
          <CredentialActionControl
            :model-value="fieldAction(row.field)"
            :configured="setFromField(row.field, state)"
            :label="row.title"
            :disabled="fieldBlocked(row.field)"
            @update:model-value="setAction(row.field, $event)"
          />
          <form
            v-if="editing === row.field || clearing === row.field"
            novalidate
            @submit.prevent="
              save(row.field, clearing === row.field ? 'clear' : 'replace')
            "
          >
            <label v-if="editing === row.field"
              >新的{{ row.title
              }}<input
                :ref="(el) => (inputs[row.field] = el as HTMLInputElement)"
                v-model="draft[row.field]"
                type="password"
                autocomplete="new-password"
                maxlength="512"
                :spellcheck="false"
                :disabled="busy"
            /></label>
            <small class="muted">{{
              clearing === row.field
                ? "点击保存后才会清除当前密钥，其它平台密钥不受影响。"
                : row.hint
            }}</small>
            <div class="oauth-actions">
              <button type="submit" class="primary" :disabled="busy">
                保存{{ row.title }}
              </button>
            </div>
          </form>
        </div>
      </template>
    </template>
  </section>
</template>
<style scoped>
.oauth-credentials {
  overflow-wrap: anywhere;
}
.oauth-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
}
.oauth-heading h2 {
  margin: 0;
}
.oauth-secret {
  margin-top: 18px;
  padding-top: 14px;
  border-top: 1px solid var(--line, var(--line-control));
}
.oauth-secret label {
  display: grid;
  gap: 8px;
  margin-top: 12px;
  min-width: 0;
}
.oauth-secret input {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
}
.oauth-secret small {
  display: block;
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.6;
}
.oauth-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 12px;
}
</style>
