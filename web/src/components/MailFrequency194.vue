<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import {
  mailFields194,
  mailValues194,
  readMailPolicy194,
  type MailPolicy194,
  type MailValues194,
} from "../mail-frequency194";
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
}>();
const emit = defineEmits<{ busy: [value: boolean] }>();
const state = ref<MailPolicy194 | null>(null),
  draft = ref<MailValues194 | null>(null),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.ownSession,
);
const unresolvedDraft = ref(false);
const { confirmDiscard: confirmDraftDiscard } = useSettingsDraft212(
  "邮件发送频率",
  () =>
    unresolvedDraft.value ||
    (!!state.value &&
      !!draft.value &&
      mailFields194.some(
        ([key]) => String(draft.value![key]) !== String(state.value![key]),
      )),
);
let epoch = 0,
  alive = true,
  controller: AbortController | undefined,
  readyClient: ApiClient | undefined,
  readyCSRF = "";
function clear() {
  epoch++;
  controller?.abort();
  state.value = null;
  draft.value = null;
  loading.value = false;
  busy.value = false;
  readyClient = undefined;
  readyCSRF = "";
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
    toRaw(props.client) !== client ||
    toRaw(props.client).csrfToken !== csrf
  ) {
    clear();
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
    !(await confirmDraftDiscard()) ||
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
  clear();
  unresolvedDraft.value = wasUnresolved;
  error.value = "";
  notice.value = "";
  const run = epoch,
    client = toRaw(props.client),
    csrf = client.csrfToken,
    ac = new AbortController();
  controller = ac;
  loading.value = true;
  try {
    const v = readMailPolicy194(
      await client.request("/settings/mail-frequency", { signal: ac.signal }),
    );
    if (!current(run, client, csrf, ac.signal)) return;
    state.value = v;
    draft.value = mailValues194(v);
    unresolvedDraft.value = false;
    readyClient = client;
    readyCSRF = csrf;
  } catch {
    if (current(run, client, csrf, ac.signal))
      error.value = "邮件频率配置无法确认，请刷新页面后核对";
  } finally {
    if (current(run, client, csrf, ac.signal)) loading.value = false;
  }
}
async function save() {
  if (!allowed.value || !state.value || !draft.value || busy.value) return;
  if (
    readyClient !== toRaw(props.client) ||
    readyCSRF !== toRaw(props.client).csrfToken
  ) {
    clear();
    error.value = "会话已变化，请刷新页面后核对";
    return;
  }
  let values: MailValues194;
  try {
    values = mailValues194(draft.value);
  } catch (e) {
    error.value = (e as Error).message;
    return;
  }
  const before = state.value,
    client = readyClient,
    csrf = readyCSRF,
    run = ++epoch,
    ac = new AbortController();
  controller?.abort();
  controller = ac;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const saved = readMailPolicy194(
      await client.request("/settings/mail-frequency", {
        method: "PATCH",
        body: { ...values, expected_fingerprint: before.fingerprint },
        signal: ac.signal,
      }),
    );
    if (!current(run, client, csrf, ac.signal)) return;
    if (
      !saved.configured ||
      saved.fingerprint === before.fingerprint ||
      BigInt(saved.revision) !== BigInt(before.revision) + 1n ||
      mailFields194.some(([k]) => saved[k] !== values[k])
    )
      throw Error("保存结果无法确认，请刷新页面后核对");
    state.value = saved;
    draft.value = mailValues194(saved);
    unresolvedDraft.value = false;
    notice.value = "保存成功";
  } catch {
    if (current(run, client, csrf, ac.signal)) {
      state.value = null;
      draft.value = null;
      unresolvedDraft.value = true;
      error.value = "保存结果无法确认，请刷新页面后核对";
    }
  } finally {
    if (current(run, client, csrf, ac.signal)) busy.value = false;
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
    clear();
    void load();
  },
  { immediate: true, flush: "sync" },
);
watch(busy, (v) => emit("busy", v), { flush: "sync" });
onBeforeUnmount(() => {
  alive = false;
  clear();
  emit("busy", false);
});
</script>
<template>
  <section aria-label="发送频率配置">
    <h3>发送频率配置</h3>
    <p v-if="!allowed" class="muted">修改需要超级管理员本人会话。</p>
    <template v-else>
      <p v-if="loading" role="status">正在读取邮件频率配置…</p>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
      <template v-if="state && draft">
        <p v-if="!state.configured" class="muted">
          当前实际生效：同一邮箱间隔{{
            state.effective.mail_interval_email
          }}秒、每日{{ state.effective.mail_daily_email }}次；同一IP每日{{
            state.effective.mail_daily_ip
          }}次。保存后启用下列设置，已有发送次数继续计入。
        </p>
        <form class="console-config-form" aria-label="邮件发送频率配置" @submit.prevent="save">
          <div class="settings-fields">
            <div
              v-for="[key, label, max] in mailFields194"
              :key="key"
              class="setting-field"
            >
              <label :for="`mail194-${key}`">{{ label }}</label>
              <input
                :id="`mail194-${key}`"
                v-model="draft[key]"
                :name="key"
                :aria-label="label"
                type="text"
                inputmode="numeric"
                pattern="[1-9][0-9]*"
                :maxlength="String(max).length"
                required
                :disabled="busy"
              />
            </div>
          </div>
          <button type="submit" class="primary" :disabled="busy">
            {{ busy ? "保存中…" : "保存" }}
          </button>
        </form>
      </template>
    </template>
  </section>
</template>
