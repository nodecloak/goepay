<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import type { MailAccount195 } from "../mail-accounts195";
const props = defineProps<{
  client: ApiClient;
  account: MailAccount195;
  allowed: boolean;
}>();
const emit = defineEmits<{ busy: [boolean]; dirty: [boolean] }>();
const opened = ref(false),
  busy = ref(false),
  to = ref(""),
  error = ref(""),
  status = ref<"accepted" | "unknown" | null>(null),
  pending = ref<{ id: string; to: string; fingerprint: string } | null>(null),
  dialog = ref<HTMLElement>();
const available = computed(
  () =>
    props.allowed && props.account.test_available && props.account.status === 1,
);
const draftDirty = computed(
  () =>
    (!!opened.value && !!to.value) || busy.value || status.value === "unknown",
);
useSettingsDraft212("测试邮件", () => draftDirty.value);
watch(draftDirty, (value) => emit("dirty", value), {
  immediate: true,
  flush: "sync",
});
let epoch = 0,
  alive = true,
  ac: AbortController | undefined,
  readyClient = toRaw(props.client),
  readyCSRF = readyClient.csrfToken,
  previousFocus: HTMLElement | null = null;
function reset() {
  epoch++;
  ac?.abort();
  pending.value = null;
  status.value = null;
  to.value = "";
  opened.value = false;
  busy.value = false;
  error.value = "";
  readyClient = toRaw(props.client);
  readyCSRF = readyClient.csrfToken;
}
function current(run: number, signal?: AbortSignal) {
  if (!alive || run !== epoch || signal?.aborted) return false;
  if (
    !available.value ||
    readyClient !== toRaw(props.client) ||
    readyCSRF !== toRaw(props.client).csrfToken
  ) {
    reset();
    error.value = "会话已变化，请重新读取邮件账号";
    return false;
  }
  return true;
}
async function open(fresh = false) {
  if (!current(epoch) || busy.value) return;
  if (fresh) {
    pending.value = null;
    status.value = null;
    to.value = "";
  }
  previousFocus = document.activeElement as HTMLElement | null;
  opened.value = true;
  error.value = "";
  await nextTick();
  dialog.value
    ?.querySelector<HTMLElement>("input:not([disabled]),button:not([disabled])")
    ?.focus();
}
function close() {
  if (busy.value) return;
  opened.value = false;
  to.value = "";
  void nextTick(() => previousFocus?.isConnected && previousFocus.focus());
}
function trap(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    close();
    return;
  }
  if (event.key !== "Tab" || !dialog.value) return;
  const nodes = Array.from(
      dialog.value.querySelectorAll<HTMLElement>(
        "input:not([disabled]),button:not([disabled])",
      ),
    ),
    first = nodes[0],
    last = nodes.at(-1);
  if (!first || !last) return;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
function result(raw: unknown, id: string) {
  const v = raw as { request_id?: string; status?: string };
  if (
    !v ||
    v.request_id !== id ||
    (v.status !== "accepted" && v.status !== "unknown")
  )
    throw Error("原发送结果无法确认");
  return v.status;
}
async function act(query: boolean) {
  if (busy.value || !current(epoch)) return;
  if (!query) {
    if (pending.value) return;
    const target = to.value.trim();
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(target)) {
      error.value = "请填写收件邮箱地址";
      return;
    }
    pending.value = {
      id: crypto.randomUUID(),
      to: target,
      fingerprint: props.account.fingerprint,
    };
    to.value = "";
  }
  const original = pending.value;
  if (!original) return;
  const run = ++epoch,
    control = new AbortController();
  ac?.abort();
  ac = control;
  busy.value = true;
  error.value = "";
  status.value = "unknown";
  try {
    const raw = query
      ? await readyClient.request(
          "/settings/mail-channels/smtp/tests/" + original.id,
          { signal: control.signal },
        )
      : await readyClient.request("/settings/mail-channels/smtp/test", {
          method: "POST",
          body: {
            request_id: original.id,
            to: original.to,
            expected_fingerprint: original.fingerprint,
          },
          signal: control.signal,
        });
    if (current(run, control.signal)) status.value = result(raw, original.id);
  } catch {
    if (current(run, control.signal))
      error.value = "发送结果无法确认，请查询原发送结果，不要重复发送";
  } finally {
    if (current(run, control.signal)) busy.value = false;
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.allowed,
    props.account.fingerprint,
  ],
  reset,
  {
    flush: "sync",
  },
);
watch(
  () => opened.value || busy.value,
  (v) => emit("busy", v),
  { flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  reset();
  emit("busy", false);
  emit("dirty", false);
});
</script>
<template>
  <div v-if="available" class="toolbar">
    <button @click="open()" :disabled="busy || opened">
      {{ pending ? "查看原测试结果" : "发送测试邮件" }}</button
    ><button
      v-if="status === 'accepted'"
      @click="open(true)"
      :disabled="busy || opened"
    >
      发送新的测试邮件
    </button>
  </div>
  <p v-if="error && !opened" role="alert" class="alert error">{{ error }}</p>
  <div v-if="opened" class="modal-backdrop" @keydown="trap">
    <section
      ref="dialog"
      role="dialog"
      aria-modal="true"
      aria-label="发送测试邮件"
      class="modal panel"
    >
      <h2>发送测试邮件</h2>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="status === 'unknown'" role="status">
        发送结果尚未确认，请查询原发送结果，不要重复发送。
      </p>
      <p v-if="status === 'accepted'" role="status">
        SMTP已接受，实际送达以收件箱为准。
      </p>
      <form
        v-if="!pending"
        aria-label="发送测试邮件"
        @submit.prevent="act(false)"
      >
        <div class="form-rows">
          <label
            ><span class="field-label field-label--req">收件邮箱地址</span
            ><input
              aria-label="收件邮箱地址"
              v-model="to"
              type="email"
              maxlength="254"
              required
              :disabled="busy"
          /></label>
        </div>
        <div class="modal-actions">
          <button type="button" @click="close" :disabled="busy">取消</button
          ><button class="primary" type="submit" :disabled="busy">发送</button>
        </div>
      </form>
      <div v-else class="modal-actions">
        <button type="button" @click="close" :disabled="busy">关闭</button
        ><button type="button" @click="act(true)" :disabled="busy">
          {{ busy ? "正在确认…" : "查询原发送结果" }}
        </button>
      </div>
    </section>
  </div>
</template>
