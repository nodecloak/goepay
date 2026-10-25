<script lang="ts">
type CleanupBody = { days: number; cutoff: string; external_no: string };
const cleanupIntents = new WeakMap<object, Map<string, CleanupBody>>();
</script>
<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { ApiClient, ApiError } from "../api";
import { beijingTimestamp } from "../console-datetime";
const props = defineProps<{
  client: ApiClient;
  ownSession?: boolean;
  adminClass?: string;
}>();
const emit = defineEmits<{ close: []; updated: [] }>();
const initialCSRF = props.client.csrfToken;
let intents = cleanupIntents.get(props.client);
if (!intents) {
  intents = new Map();
  cleanupIntents.set(props.client, intents);
}
const prior = intents.get(initialCSRF),
  pending = ref<CleanupBody | null>(prior || null),
  days = ref(String(prior?.days ?? 30));
const preview = ref<{ cutoff: string; count: string; days: number } | null>(
    null,
  ),
  busy = ref(false),
  unknown = ref(!!prior),
  done = ref(false),
  error = ref(
    prior ? "原清除结果未知，请核对原清除请求，不要重新创建请求" : "",
  ),
  notice = ref(""),
  dialog = ref<HTMLElement>();
const controller = new AbortController(),
  previousFocus = document.activeElement as HTMLElement | null;
let alive = true;
const canWrite = computed(
  () =>
    props.client.area === "admin" &&
    props.ownSession &&
    props.adminClass === "super" &&
    props.client.csrfToken === initialCSRF,
);
const validDays = computed(
  () => /^[1-9]\d{0,3}$/.test(days.value) && Number(days.value) <= 9999,
);
function remember() {
  if (pending.value) intents!.set(initialCSRF, { ...pending.value });
  else intents!.delete(initialCSRF);
}
watch(days, () => {
  preview.value = null;
  error.value = "";
});
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
  ],
  () => {
    controller.abort();
    busy.value = false;
    preview.value = null;
    error.value = "会话已变化，请关闭后重新进入";
  },
);
async function inspect() {
  if (!canWrite.value || !validDays.value || busy.value || pending.value)
    return;
  busy.value = true;
  error.value = "";
  preview.value = null;
  try {
    const data = await props.client.request<{
      cutoff: string;
      count: string;
      days: number;
    }>("/operations/runtime-log/cleanup?days=" + days.value, {
      signal: controller.signal,
    });
    if (!alive || controller.signal.aborted || !canWrite.value) return;
    if (
      data.days !== Number(days.value) ||
      typeof data.cutoff !== "string" ||
      !Number.isFinite(Date.parse(data.cutoff)) ||
      typeof data.count !== "string" ||
      !/^(0|[1-9]\d*)$/.test(data.count)
    )
      throw new Error("清除范围缺少可信来源，请重新预览");
    preview.value = data;
  } catch (e) {
    if (alive && !controller.signal.aborted)
      error.value = e instanceof Error ? e.message : "清除范围读取失败";
  } finally {
    if (alive) busy.value = false;
  }
}
async function confirm() {
  if (
    !canWrite.value ||
    busy.value ||
    done.value ||
    (!pending.value && !preview.value)
  )
    return;
  const wasUnknown = unknown.value;
  if (!pending.value) {
    pending.value = {
      days: preview.value!.days,
      cutoff: preview.value!.cutoff,
      external_no: "runtime-" + crypto.randomUUID(),
    };
    remember();
  }
  busy.value = true;
  error.value = "";
  try {
    const result = await props.client.request<{
      count: string;
      replayed: boolean;
    }>("/operations/runtime-log/cleanup", {
      method: "POST",
      body: pending.value,
      signal: controller.signal,
    });
    if (!alive || controller.signal.aborted || !canWrite.value) return;
    pending.value = null;
    remember();
    unknown.value = false;
    done.value = true;
    notice.value =
      (result.replayed ? "已找回原清除结果：" : "已清除：") +
      result.count +
      " 条运行日志";
    emit("updated");
  } catch (e) {
    if (!alive || controller.signal.aborted) return;
    if (
      e instanceof ApiError &&
      e.status >= 400 &&
      e.status < 500 &&
      ![408, 429].includes(e.status) &&
      !wasUnknown
    ) {
      pending.value = null;
      remember();
      preview.value = null;
      unknown.value = false;
      error.value =
        e.status === 409 ? "原清除范围已变化，请重新预览" : e.message;
    } else {
      unknown.value = true;
      error.value =
        wasUnknown && e instanceof ApiError
          ? "原清除结果未知：" +
            e.message +
            "；请核对原请求或查看业务审计，不要更换参数"
          : "清除结果未知，请核对原清除请求，不要更改天数或重新创建请求";
    }
  } finally {
    if (alive) busy.value = false;
  }
}
function trap(event: KeyboardEvent) {
  if (event.key === "Escape") {
    if (!busy.value) emit("close");
    return;
  }
  if (event.key !== "Tab" || !dialog.value) return;
  const controls = [
    ...dialog.value.querySelectorAll<HTMLElement>(
      "button:not([disabled]),input:not([disabled])",
    ),
  ];
  if (!controls.length) {
    event.preventDefault();
    return;
  }
  if (event.shiftKey && document.activeElement === controls[0]) {
    event.preventDefault();
    controls.at(-1)!.focus();
  } else if (!event.shiftKey && document.activeElement === controls.at(-1)) {
    event.preventDefault();
    controls[0].focus();
  }
}
onMounted(async () => {
  await nextTick();
  dialog.value
    ?.querySelector<HTMLElement>("input:not([disabled]),button:not([disabled])")
    ?.focus();
});
onBeforeUnmount(() => {
  alive = false;
  controller.abort();
  if (previousFocus?.isConnected) previousFocus.focus();
});
</script>
<template>
  <div class="modal-backdrop">
    <section
      ref="dialog"
      role="dialog"
      aria-modal="true"
      aria-label="清除运行日志"
      class="modal panel runtime-cleanup"
      @keydown="trap"
    >
      <h2>清除运行日志</h2>
      <p>
        清除指定天数前的运行日志显示，原运行记录保留归档。支付账务、登录和业务审计不会清除。
      </p>
      <label
        >保留天数<input
          v-model="days"
          aria-label="保留天数"
          inputmode="numeric"
          maxlength="4"
          :disabled="busy || !!pending || done || !canWrite"
      /></label>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
      <p v-if="preview">
        将清除 {{ beijingTimestamp(preview.cutoff) }}（北京时间）之前的
        {{ preview.count }} 条运行记录。
      </p>
      <p v-if="unknown" class="muted">原请求号：{{ pending?.external_no }}</p>
      <div class="modal-actions">
        <button :disabled="busy" @click="emit('close')">
          {{ unknown ? "关闭并保留原请求" : "取消" }}</button
        ><button
          v-if="!pending && !done"
          :disabled="busy || !canWrite || !validDays"
          @click="inspect"
        >
          预览清除范围</button
        ><button
          v-if="!done"
          class="danger-button"
          :disabled="busy || !canWrite || (!pending && !preview)"
          @click="confirm"
        >
          {{ busy ? "正在处理…" : unknown ? "核对原清除请求" : "确认清除" }}
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.runtime-cleanup {
  max-width: 580px;
  max-height: calc(100dvh - 32px);
  overflow: auto;
  overflow-wrap: anywhere;
}
.runtime-cleanup label {
  display: grid;
  gap: 8px;
}
.runtime-cleanup input {
  min-width: 0;
  width: 100%;
}
</style>
