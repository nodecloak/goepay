<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import ParentChannelSubject from "./ParentChannelSubject.vue";
import { ApiError, type ApiClient } from "../api";
import {
  channelLimitKeys,
  normalizeChannelLimits,
  readRuntimeChannelLimits,
  readRuntimeChannelLimitsResult,
  runtimeChannelLimitsPath,
  type ChannelLimitFields,
  type RuntimeChannelLimits,
} from "../runtime-channel-limits";
const props = defineProps<{
  client: ApiClient;
  allowed: boolean;
  channelID: string;
  subjectEditor?: boolean;
}>();
const subjectLocked = ref(false);
const emit = defineEmits<{ close: []; contextInvalid: [] }>();
const blank = (): ChannelLimitFields => ({
  daytop: "",
  daymaxorder: "",
  paymin: "",
  paymax: "",
  timestart: "",
  timestop: "",
  rate_limit_seconds: "",
  rate_limit_count: "",
});
const draft = reactive(blank()),
  target = ref<RuntimeChannelLimits>(),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  confirmation = ref<ChannelLimitFields>();
const modal = ref<HTMLElement>(),
  firstInput = ref<HTMLInputElement>(),
  errorTarget = ref<HTMLElement>(),
  closeButton = ref<HTMLButtonElement>(),
  backButton = ref<HTMLButtonElement>(),
  confirmGroup = ref<HTMLElement>();
let active = true,
  epoch = 0,
  controller: AbortController | undefined,
  contextClient = props.client,
  contextCSRF = props.client.csrfToken,
  contextID = props.channelID,
  preserveDraft = false;
function reset() {
  ++epoch;
  controller?.abort();
  controller = undefined;
  target.value = undefined;
  confirmation.value = undefined;
  loading.value = false;
  busy.value = false;
  error.value = "";
  notice.value = "";
  Object.assign(draft, blank());
  preserveDraft = false;
  contextClient = props.client;
  contextCSRF = props.client.csrfToken;
  contextID = props.channelID;
}
function ensureSession() {
  if (
    props.client === contextClient &&
    props.client.csrfToken === contextCSRF &&
    props.channelID === contextID
  )
    return true;
  reset();
  emit("contextInvalid");
  return false;
}
function begin() {
  controller?.abort();
  const c = new AbortController();
  controller = c;
  return { c, current: ++epoch };
}
function valid(current: number, c: AbortController) {
  return (
    active &&
    current === epoch &&
    !c.signal.aborted &&
    props.allowed &&
    props.client.area === "admin" &&
    ensureSession()
  );
}
async function load() {
  if (
    !ensureSession() ||
    busy.value ||
    !props.allowed ||
    props.client.area !== "admin"
  )
    return;
  const id = props.channelID,
    { c, current } = begin();
  loading.value = true;
  target.value = undefined;
  confirmation.value = undefined;
  error.value = "";
  notice.value = "";
  try {
    const raw = await props.client.request<unknown>(
      runtimeChannelLimitsPath(id),
      { signal: c.signal },
    );
    if (!valid(current, c) || props.channelID !== id) return;
    const state = readRuntimeChannelLimits(raw);
    if (state.channel_id !== id) throw Error("wrong target");
    target.value = state;
    if (preserveDraft)
      notice.value = "已刷新当前状态；草稿已保留，请重新核对并确认。";
    else for (const key of channelLimitKeys) draft[key] = state[key];
    preserveDraft = false;
  } catch {
    if (valid(current, c))
      error.value = "当前父通道限额无法核实，请刷新页面后重新确认。";
  } finally {
    if (valid(current, c)) {
      loading.value = false;
      await nextTick();
      if (valid(current, c))
        (error.value ? errorTarget.value : firstInput.value)?.focus();
    }
  }
}
async function confirm() {
  if (
    !ensureSession() ||
    busy.value ||
    loading.value ||
    !target.value?.can_update
  )
    return;
  try {
    confirmation.value = normalizeChannelLimits(draft);
    error.value = "";
    notice.value = "";
    await nextTick();
    backButton.value?.focus();
  } catch {
    error.value =
      "金额须为非负元且最多两位小数，次数为非负整数；最小值不能大于非零最大值，时段须成对填写0至23。";
    await nextTick();
    firstInput.value?.focus();
  }
}
async function back() {
  if (!ensureSession() || busy.value) return;
  confirmation.value = undefined;
  await nextTick();
  firstInput.value?.focus();
}
async function close() {
  if (subjectLocked.value) return;
  if (!ensureSession() || busy.value) return;
  ++epoch;
  controller?.abort();
  emit("close");
}
async function submit() {
  if (!ensureSession()) return;
  const before = target.value,
    desired = confirmation.value,
    id = props.channelID;
  if (busy.value || !before?.can_update || !desired || before.channel_id !== id)
    return;
  const { c, current } = begin();
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    await nextTick();
    if (!valid(current, c) || props.channelID !== id) return;
    confirmGroup.value?.focus();
    const raw = await props.client.request<unknown>(
      runtimeChannelLimitsPath(id),
      {
        method: "PATCH",
        body: { ...desired, expected_fingerprint: before.fingerprint },
        signal: c.signal,
      },
    );
    if (!valid(current, c) || props.channelID !== id) return;
    const after = readRuntimeChannelLimitsResult(raw, before, desired);
    target.value = after;
    for (const key of channelLimitKeys) draft[key] = after[key];
    confirmation.value = undefined;
    notice.value = "限额设置已保存。";
  } catch (e) {
    if (valid(current, c)) {
      target.value = undefined;
      confirmation.value = undefined;
      preserveDraft = true;
      error.value =
        e instanceof ApiError && e.status === 409
          ? "通道配置已变更，请刷新页面后核对；不会自动重试。"
          : "本次限额修改未能确认，请刷新页面核对当前状态；不要直接重复提交。";
    }
  } finally {
    if (valid(current, c)) {
      busy.value = false;
      await nextTick();
      if (valid(current, c))
        (error.value ? errorTarget.value : closeButton.value)?.focus();
    }
  }
}
const windowText = computed(() => {
  const v = confirmation.value;
  if (!v) return "";
  return v.timestart === "" || v.timestart === v.timestop
    ? "全天不限"
    : `${v.timestart}:00 至 ${v.timestop}:59（含首尾整小时）`;
});
const unlimited = (v: string) => (Number(v) === 0 ? "不限" : v);
const observationTime = (v: string) =>
  new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  }).format(new Date(v));
function keydown(event: KeyboardEvent) {
  if (busy.value && ["Tab", "Enter", "Escape"].includes(event.key)) {
    event.preventDefault();
    event.stopImmediatePropagation();
    confirmGroup.value?.focus();
    return;
  }
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopImmediatePropagation();
    void close();
    return;
  }
  if (event.key !== "Tab" || !modal.value) return;
  const nodes = Array.from(
      modal.value.querySelectorAll<HTMLElement>(
        "button:not([disabled]),input:not([disabled]),textarea:not([disabled]),select:not([disabled]),a[href]",
      ),
    ).filter((el) => el.getClientRects().length > 0),
    first = nodes[0],
    last = nodes.at(-1);
  if (!nodes.includes(document.activeElement as HTMLElement)) {
    event.preventDefault();
    (event.shiftKey ? last : first)?.focus();
    return;
  }
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.allowed,
    props.channelID,
  ],
  () => {
    reset();
    void load();
  },
  { immediate: true, flush: "sync" },
);
onMounted(() => document.addEventListener("keydown", keydown, true));
onBeforeUnmount(() => {
  active = false;
  ++epoch;
  controller?.abort();
  document.removeEventListener("keydown", keydown, true);
});
</script>
<template>
  <div
    v-if="allowed && client.area === 'admin'"
    class="modal-backdrop drawer-backdrop"
    @click.self="close"
  >
    <section
      ref="modal"
      class="modal modal-drawer panel channel-limits-modal"
      role="dialog"
      aria-modal="true"
      aria-label="父通道限额设置"
    >
      <h2>{{ subjectEditor ? "父通道高级设置" : "父通道限额设置" }}</h2>
      <p class="target">通道ID：{{ channelID }}</p>
      <p class="muted">
        同一父通道跨用户组、直连和轮询共用限制。开放时间按 UTC+8 判断。
      </p>
      <p v-if="loading" role="status">正在核对当前父通道…</p>
      <p
        ref="errorTarget"
        tabindex="-1"
        v-if="error"
        class="alert error"
        role="alert"
      >
        {{ error }}
      </p>
      <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
      <div v-if="target" class="usage" aria-label="父通道当前用量">
        <p>
          <strong>{{ target.usage.day }} · UTC+8</strong
          ><span class="muted"
            >成功实收 {{ target.usage.successful_amount }} 元 · 成功
            {{ target.usage.successful_count }} 笔</span
          >
        </p>
        <p>
          {{
            target.usage.day_paused
              ? "已暂停新单（日额或日笔数已达阈值）"
              : "成功日阈值未达"
          }}<span class="muted"
            >当前频率窗口已发起 {{ target.usage.recent_starts }} 笔{{
              target.usage.rate_limited ? "，已达频率限制" : ""
            }}</span
          >
        </p>
        <p class="muted">
          统计时刻（UTC+8）：{{
            observationTime(target.usage.as_of)
          }}。日额按成功实收 realmoney 统计，退款不释放；待支付订单不预占日额。
        </p>
      </div>
      <form v-if="target && !confirmation" @submit.prevent="confirm">
        <fieldset :disabled="busy || !target.can_update">
          <div class="limits-grid">
            <label
              >单日限额（成功实收元）<input
                ref="firstInput"
                v-model="draft.daytop"
                inputmode="decimal"
                maxlength="24"
                autocomplete="off"
            /></label>
            <label
              >单日限笔（成功笔数）<input
                v-model="draft.daymaxorder"
                inputmode="numeric"
                maxlength="19"
                autocomplete="off"
            /></label>
            <label
              >单笔最小（原请求money元）<input
                v-model="draft.paymin"
                inputmode="decimal"
                maxlength="24"
                autocomplete="off"
            /></label>
            <label
              >单笔最大（原请求money元）<input
                v-model="draft.paymax"
                inputmode="decimal"
                maxlength="24"
                autocomplete="off"
            /></label>
            <label
              >开放起始小时（UTC+8）<input
                v-model="draft.timestart"
                inputmode="numeric"
                maxlength="2"
                autocomplete="off"
            /></label>
            <label
              >开放结束小时（UTC+8）<input
                v-model="draft.timestop"
                inputmode="numeric"
                maxlength="2"
                autocomplete="off"
            /></label>
            <label
              >频率窗口（秒）<input
                v-model="draft.rate_limit_seconds"
                inputmode="numeric"
                maxlength="10"
                autocomplete="off"
            /></label>
            <label
              >窗口内发起笔数<input
                v-model="draft.rate_limit_count"
                inputmode="numeric"
                maxlength="19"
                autocomplete="off"
            /></label>
          </div>
          <p class="muted">
            金额和笔数填0或留空不限；单笔按原请求money判断。时段成对填写0至23，含首尾整小时，起止相等为全天不限。频率任一项为0或留空不限。
          </p>
          <p class="muted">
            成功日额达到阈值后暂停新单；已发订单仍可正常成功，累计实收可能超过阈值。次日自动按新一天统计。
          </p>
          <button type="submit" class="primary">保存限额设置</button>
        </fieldset>
      </form>
      <div
        v-if="confirmation"
        ref="confirmGroup"
        class="alert limits-confirm"
        role="group"
        aria-label="确认父通道限额"
        tabindex="-1"
      >
        <p>通道ID：{{ channelID }}</p>
        <p>
          成功日额：{{ unlimited(confirmation.daytop)
          }}{{ Number(confirmation.daytop) > 0 ? " 元" : "" }}；成功日笔：{{
            unlimited(confirmation.daymaxorder)
          }}。
        </p>
        <p>
          单笔原请求money：最小 {{ unlimited(confirmation.paymin) }}，最大
          {{ unlimited(confirmation.paymax) }}（元）。
        </p>
        <p>开放时间：{{ windowText }} · UTC+8</p>
        <p>
          发起频率：{{
            confirmation.rate_limit_seconds === "0" ||
            confirmation.rate_limit_count === "0"
              ? "不限"
              : `${confirmation.rate_limit_seconds} 秒内 ${confirmation.rate_limit_count} 笔`
          }}。
        </p>
        <p class="muted">
          新设置影响之后的新选路；原订单的通道、费用和已消耗的发起次数保持原记录。
        </p>
        <div class="toolbar-actions">
          <button ref="backButton" type="button" :disabled="busy" @click="back">
            返回修改</button
          ><button
            type="button"
            class="primary"
            :disabled="busy"
            @click="submit"
          >
            {{ busy ? "正在保存…" : "确定保存" }}
          </button>
        </div>
      </div>
      <ParentChannelSubject
        v-if="subjectEditor"
        :client="client"
        :allowed="allowed"
        :channel-i-d="channelID"
        @locked="subjectLocked = $event"
        @context-invalid="emit('contextInvalid')"
      />
      <div class="toolbar-actions bottom">
        <button ref="closeButton" type="button" :disabled="busy" @click="close">
          关闭
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.channel-limits-modal {
  --drawer-w: 600px;
  overscroll-behavior: contain;
  overflow-wrap: anywhere;
  min-width: 0;
}
.channel-limits-modal h2 {
  margin-top: 0;
}
.target {
  font-variant-numeric: tabular-nums;
}
.usage {
  background: var(--surface-soft, var(--bg-raised));
  border: 1px solid var(--border, var(--line));
  border-radius: 6px;
  padding: 12px;
  margin: 16px 0;
}
.usage p {
  margin: 0 0 8px;
}
.usage p:last-child {
  margin: 0;
}
.usage span {
  display: block;
  margin-top: 4px;
}
.channel-limits-modal fieldset {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
.limits-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px 20px;
}
.limits-grid label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
  margin: 0;
}
.limits-grid input {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
  margin: 0;
}
.channel-limits-modal > .toolbar-actions.bottom:not(:last-child) {
  margin-top: 16px;
}
.limits-confirm:focus {
  outline: 2px solid var(--primary, var(--focus));
  outline-offset: 2px;
}
@media (max-width: 540px) {
  .limits-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .channel-limits-modal {
    padding: 18px;
  }
  /* The global full-bleed head/foot bars use a -20px (var(--space-5))
     horizontal margin that assumes the modal's default 20px padding. This
     panel tightens its padding to 18px below 540px, so pull the bars back to
     -18px to keep them flush instead of overflowing by 2px each side. */
  .channel-limits-modal > :is(h2, h3):first-child,
  .channel-limits-modal > :is(.modal-actions, .toolbar-actions):last-child {
    margin-left: -18px;
    margin-right: -18px;
    padding-left: 18px;
    padding-right: 18px;
  }
}
</style>
