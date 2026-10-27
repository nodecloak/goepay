<script lang="ts">
const pendingUpdates183 = new WeakMap<object, Map<string, string>>();
</script>
<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  toRaw,
  watch,
} from "vue";
import { ApiClient } from "../api";
import {
  parseSystemUpdate183,
  parseUpdateStatus183,
  updateStateLabels183,
  updateCheckLabels183,
  type SystemUpdate183,
  type UpdateStatus183,
} from "../system-update183";
const props = defineProps<{
  client: ApiClient;
  ownSession?: boolean;
  adminClass?: string;
}>();
const client = toRaw(props.client),
  csrf = client.csrfToken;
let pending = pendingUpdates183.get(client);
if (!pending) {
  pending = new Map();
  pendingUpdates183.set(client, pending);
}
const view = ref<SystemUpdate183 | null>(null),
  status = ref<UpdateStatus183 | null>(null),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  uncertain = ref(!!pending.get(csrf)),
  intent = ref(pending.get(csrf) || ""),
  confirmation = ref<"prepare" | "cancel" | "retry" | null>(null),
  ack = ref(false),
  dialog = ref<HTMLElement>();
const canWriteNow = () =>
  props.ownSession === true &&
  props.adminClass === "super" &&
  client.area === "admin" &&
  client === toRaw(props.client) &&
  client.csrfToken === csrf;
const canWrite = computed(canWriteNow);
let alive = true,
  epoch = 0,
  controller: AbortController | undefined,
  timer: ReturnType<typeof setTimeout> | undefined,
  previousFocus: HTMLElement | null = null;
const current = (n: number) =>
  alive &&
  n === epoch &&
  client === toRaw(props.client) &&
  csrf === client.csrfToken;
let confirmed: { intent: string; state: string; version: string } | undefined;
function remember() {
  if (intent.value) pending!.set(csrf, intent.value);
  else pending!.delete(csrf);
}
function stop() {
  controller?.abort();
  if (timer) clearTimeout(timer);
  timer = undefined;
  epoch++;
}
function revokeContext() {
  stop();
  view.value = null;
  status.value = null;
  confirmation.value = null;
  confirmed = undefined;
  intent.value = "";
  pending!.delete(csrf);
  loading.value = false;
  busy.value = false;
  uncertain.value = false;
  error.value = "会话上下文已变化，请重新进入页面";
}
function checkContext() {
  if (client === toRaw(props.client) && csrf === client.csrfToken) return true;
  revokeContext();
  return false;
}
function accept(raw: unknown, expected: string) {
  const s = parseUpdateStatus183(raw);
  if (s.intent !== expected) throw Error("返回结果不属于本次更新");
  status.value = s;
  intent.value = s.state === "none" ? "" : s.intent;
  remember();
  return s;
}
async function inspect(poll = false) {
  if (!checkContext()) return;
  if (busy.value || loading.value || !alive) return;
  stop();
  const n = epoch,
    c = new AbortController();
  controller = c;
  loading.value = true;
  error.value = "";
  try {
    const raw = await client.request("/system-update", { signal: c.signal });
    if (!current(n)) return;
    const next = parseSystemUpdate183(raw);
    view.value = next;
    const saved = intent.value;
    if (saved) {
      const original = await client.request(
        "/system-update/preparations/" + saved,
        { signal: c.signal },
      );
      if (!current(n)) return;
      accept(original, saved);
    } else {
      status.value = next.preparation;
      if (next.preparation.state !== "none") {
        intent.value = next.preparation.intent;
        remember();
      }
    }
    uncertain.value = false;
  } catch (e) {
    if (current(n)) {
      error.value = e instanceof Error ? e.message : "版本检测失败";
      if (intent.value) uncertain.value = true;
    }
  } finally {
    if (current(n)) {
      loading.value = false;
      if (status.value?.state === "preparing" && !uncertain.value)
        timer = setTimeout(() => inspect(true), 1000);
    }
  }
}
function show(action: "prepare" | "cancel" | "retry") {
  if (
    !checkContext() ||
    !canWriteNow() ||
    busy.value ||
    loading.value ||
    uncertain.value
  )
    return;
  if (action === "prepare" && !view.value?.can_prepare) return;
  if (action === "cancel" && !status.value?.can_cancel) return;
  if (action === "retry" && !status.value?.can_retry) return;
  previousFocus = document.activeElement as HTMLElement;
  stop();
  confirmed = {
    intent:
      action === "prepare" ? view.value!.fingerprint : status.value!.intent,
    state: status.value!.state_fingerprint,
    version:
      action === "prepare"
        ? view.value!.latest_version
        : status.value!.target?.version || "",
  };
  ack.value = false;
  confirmation.value = action;
  nextTick(() => dialog.value?.focus());
}
function close() {
  if (busy.value) return;
  confirmation.value = null;
  confirmed = undefined;
  if (status.value?.state === "preparing")
    timer = setTimeout(() => inspect(true), 1000);
  nextTick(() => previousFocus?.focus());
}
function keydown(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    close();
  }
  if (e.key !== "Tab" || !dialog.value) return;
  const nodes = [
    ...dialog.value.querySelectorAll<HTMLElement>(
      'button:not(:disabled),input:not(:disabled),[tabindex="0"]',
    ),
  ];
  if (!nodes.length) {
    e.preventDefault();
    return;
  }
  const first = nodes[0]!,
    last = nodes[nodes.length - 1]!;
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first.focus();
  }
}
async function submit() {
  if (!checkContext()) return;
  const action = confirmation.value;
  if (
    !action ||
    !ack.value ||
    !canWriteNow() ||
    busy.value ||
    !view.value ||
    !status.value
  )
    return;
  stop();
  const n = epoch,
    c = new AbortController();
  controller = c;
  busy.value = true;
  error.value = "";
  if (!confirmed) {
    busy.value = false;
    return;
  }
  const original = confirmed.intent;
  intent.value = original;
  remember();
  const path =
    action === "prepare"
      ? "/system-update/prepare"
      : "/system-update/preparations/" + original + "/" + action;
  const body =
    action === "prepare"
      ? { intent: original, expected_fingerprint: original }
      : action === "retry"
        ? {
            expected_fingerprint: original,
            expected_state: confirmed.state,
          }
        : { expected_state: confirmed.state };
  try {
    const raw = await client.request(path, {
      method: "POST",
      body,
      signal: c.signal,
    });
    if (!current(n)) return;
    accept(raw, original);
    uncertain.value = false;
    confirmation.value = null;
  } catch (e) {
    if (current(n)) {
      uncertain.value = true;
      confirmation.value = null;
      error.value = e instanceof Error ? e.message : "提交结果待核对";
    }
  } finally {
    if (current(n)) {
      busy.value = false;
      nextTick(() => previousFocus?.focus());
      if (status.value?.state === "preparing" && !uncertain.value)
        timer = setTimeout(() => inspect(true), 1000);
    }
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
  ],
  revokeContext,
);
onMounted(() => {
  window.addEventListener("focus", checkContext);
  window.addEventListener("pageshow", checkContext);
  document.addEventListener("visibilitychange", checkContext);
  inspect();
});
onBeforeUnmount(() => {
  window.removeEventListener("focus", checkContext);
  window.removeEventListener("pageshow", checkContext);
  document.removeEventListener("visibilitychange", checkContext);
  alive = false;
  stop();
});
</script>
<template>
  <section class="panel system-update-page">
    <header class="update-heading">
      <h2>系统更新</h2>
      <button :disabled="loading || busy" @click="inspect()">
        {{ loading ? "正在检测…" : "重新检测" }}
      </button>
    </header>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <p v-if="uncertain" role="status">
      提交结果尚未确认，请先核对原更新结果。<button
        :disabled="busy || loading"
        @click="inspect()"
      >
        核对原更新结果
      </button>
    </p>
    <template v-if="view">
      <div class="version-columns">
        <div>
          <span>当前版本</span><strong>{{ view.current_version }}</strong
          ><small v-if="view.current_source"
            >构建来源 {{ view.current_source }}</small
          >
        </div>
        <div>
          <span>最新版本</span
          ><strong>{{ view.latest_version || "暂无版本信息" }}</strong
          ><small>{{ view.latest_version_name }}</small>
        </div>
      </div>
      <p role="status">{{ updateCheckLabels183[view.check_status] }}</p>
      <button
        v-if="view.can_prepare && canWrite"
        data-action="prepare"
        class="primary"
        :disabled="busy || loading || uncertain"
        @click="show('prepare')"
      >
        立即更新
      </button>
      <div v-if="view.update_log" class="update-record">
        <h3>更新记录</h3>
        <pre>{{ view.update_log }}</pre>
      </div>
    </template>
    <div
      v-if="status && status.state !== 'none'"
      class="preparation"
      aria-live="polite"
    >
      <h3>{{ updateStateLabels183[status.state] }}</h3>
      <p v-if="status.target">目标版本 {{ status.target.version }}</p>
      <div
        v-if="
          status.state === 'preparing' ||
          status.state === 'prepared' ||
          status.state === 'applied'
        "
        class="actual-progress"
      >
        <label
          >读取更新包
          <progress
            :max="status.progress.total_bytes || 1"
            :value="status.progress.read_bytes"
          />{{ status.progress.read_bytes }} /
          {{ status.progress.total_bytes }} 字节</label
        >
        <label
          >校验文件
          <progress
            :max="status.progress.total_files || 1"
            :value="status.progress.verified_files"
          />{{ status.progress.verified_files }} /
          {{ status.progress.total_files }}</label
        >
      </div>
      <p v-if="status.state === 'prepared'">
        更新包已验证并准备完成。请在维护窗口备份数据库、使用本地部署流程应用更新并重启；运行版本及数据库匹配后才会显示更新已部署。
      </p>
      <p v-if="status.state === 'unknown' || uncertain">
        请先核对本次更新结果，再决定是否重试。
      </p>
      <p v-if="status.error_code">
        准备未完成，请检查本地更新包及部署配置后重新检测。
      </p>
      <div class="update-buttons">
        <button
          v-if="!uncertain && status.state === 'unknown'"
          :disabled="busy || loading"
          @click="inspect()"
        >
          核对原更新结果</button
        ><button
          v-if="canWrite && status.can_retry"
          data-action="retry"
          :disabled="busy || loading || uncertain"
          @click="show('retry')"
        >
          重试准备</button
        ><button
          v-if="canWrite && status.can_cancel"
          data-action="cancel"
          :disabled="busy || loading || uncertain"
          @click="show('cancel')"
        >
          取消更新
        </button>
      </div>
    </div>
    <p v-if="!view && !loading && !error">暂无版本信息，请重新检测。</p>
    <Teleport to="body"
      ><div v-if="confirmation" class="update-overlay" @keydown="keydown">
        <section
          ref="dialog"
          class="update-dialog panel"
          role="dialog"
          aria-modal="true"
          aria-labelledby="update-confirm-title"
          tabindex="-1"
        >
          <h3 id="update-confirm-title">
            {{
              confirmation === "cancel"
                ? "取消更新"
                : confirmation === "retry"
                  ? "确认重试准备"
                  : "确认更新"
            }}
          </h3>
          <p>
            {{
              confirmation === "cancel"
                ? "撤回当前尚未部署的准备结果。"
                : `准备 ${confirmed?.version} 更新包，完成后按本地维护流程部署。`
            }}
          </p>
          <label class="ack"
            ><input
              v-model="ack"
              type="checkbox"
              :disabled="busy"
            />我已核对版本和本次操作</label
          >
          <div class="update-buttons">
            <button :disabled="busy" @click="close">返回</button
            ><button
              data-action="confirm"
              class="primary"
              :disabled="!ack || busy || !canWrite"
              @click="submit"
            >
              {{ busy ? "正在提交…" : "确认" }}
            </button>
          </div>
        </section>
      </div></Teleport
    >
  </section>
</template>
<style scoped>
.system-update-page {
  padding: 24px;
}
.update-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.update-heading h2 {
  margin: 0;
}
.version-columns {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 24px;
  margin: 24px 0;
}
.version-columns > div {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 20px;
  border: 1px solid #e7eaf0;
  border-radius: 5px;
  min-width: 0;
}
.version-columns span {
  color: #6b7280;
}
.version-columns strong {
  font-size: 24px;
  font-weight: 500;
}
.version-columns small {
  overflow-wrap: anywhere;
  color: #6b7280;
}
.update-record,
.preparation {
  border-top: 1px solid #edf0f5;
  margin-top: 24px;
  padding-top: 16px;
}
.update-record pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-family: inherit;
  line-height: 1.8;
}
.actual-progress label {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  margin: 12px 0;
}
.actual-progress progress {
  flex: 1;
  min-width: 120px;
  accent-color: #409eff;
}
.update-buttons {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  flex-wrap: wrap;
  margin-top: 20px;
}
.update-overlay {
  position: fixed;
  inset: 0;
  z-index: 1500;
  background: #0006;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
}
.update-dialog {
  width: 440px;
  max-width: 100%;
  box-sizing: border-box;
  padding: 24px;
  max-height: 90vh;
  overflow: auto;
}
.update-dialog p {
  line-height: 1.8;
}
.ack {
  display: flex;
  gap: 8px;
  align-items: center;
}
.error {
  color: #c0392b;
}
@media (max-width: 600px) {
  .system-update-page {
    padding: 16px;
  }
  .version-columns {
    grid-template-columns: 1fr;
    gap: 12px;
  }
  .actual-progress label {
    display: grid;
  }
  .update-dialog {
    padding: 20px;
  }
}
</style>
