<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient } from "../api";
import {
  cleanupCounts229,
  cleanupCutoff229,
  cleanupDateBounds229,
  cleanupDateDaysAgo229,
  parseCleanupReport229,
  type CleanupReport229,
} from "../data-cleanup229";
import KitModal from "./KitModal.vue";
import KitStatusTag from "./KitStatusTag.vue";
import RuntimeLogCleanup from "./RuntimeLogCleanup.vue";

const props = defineProps<{
  client: ApiClient;
  ownSession?: boolean;
  adminClass?: string;
}>();
const date = ref(""),
  busy = ref(false),
  confirmation = ref(false),
  runtimeOpen = ref(false),
  unknown = ref(false),
  error = ref(""),
  notice = ref(""),
  preview = ref<CleanupReport229>();
const previewButton = ref<HTMLButtonElement>();
const canWrite = computed(
  () =>
    props.client.area === "admin" &&
    props.ownSession === true &&
    props.adminClass === "super" &&
    !!props.client.csrfToken,
);
const bounds = ref(cleanupDateBounds229());
const retained = [
  {
    key: "order",
    label: "订单记录",
    reason: "订单为不可变财务记录，不支持删除。",
  },
  {
    key: "refund",
    label: "退款记录",
    reason: "退款为不可变财务记录，不支持删除。",
  },
  {
    key: "settle",
    label: "结算记录",
    reason: "结算为不可变财务记录，不支持删除。",
  },
  {
    key: "ledger",
    label: "资金明细",
    reason: "资金明细为不可变账本，不支持删除。",
  },
  {
    key: "transfer",
    label: "付款记录",
    reason: "付款为不可变财务记录，不支持删除。",
  },
  {
    key: "sharing",
    label: "分账记录",
    reason: "分账为不可变财务记录，不支持删除。",
  },
  {
    key: "complain",
    label: "投诉记录",
    reason: "投诉记录随订单留存，不支持删除。",
  },
  {
    key: "risk",
    label: "风控记录",
    reason: "风控记录随订单留存，不支持删除。",
  },
];
let alive = true,
  epoch = 0,
  controller: AbortController | undefined;
function context() {
  return {
    client: toRaw(props.client),
    csrf: toRaw(props.client).csrfToken,
    own: props.ownSession,
    role: props.adminClass,
  };
}
let captured = context();
function reset() {
  ++epoch;
  controller?.abort();
  controller = undefined;
  preview.value = undefined;
  confirmation.value = false;
  busy.value = false;
  unknown.value = false;
  error.value = "";
  notice.value = "";
}
function ensure() {
  const now = context();
  if (
    canWrite.value &&
    Object.keys(now).every(
      (k) =>
        now[k as keyof typeof now] === captured[k as keyof typeof captured],
    )
  )
    return true;
  reset();
  runtimeOpen.value = false;
  captured = now;
  error.value = "会话或权限已变化，请重新核对清理范围";
  return false;
}
function current(n: number, c: AbortController) {
  return alive && n === epoch && !c.signal.aborted && ensure();
}
async function inspect() {
  if (busy.value || !ensure()) return;
  bounds.value = cleanupDateBounds229();
  let cutoff: string;
  try {
    cutoff = cleanupCutoff229(date.value);
  } catch (e) {
    error.value = e instanceof Error ? e.message : "请选择有效截止日期";
    return;
  }
  controller?.abort();
  const c = new AbortController(),
    n = ++epoch;
  controller = c;
  busy.value = true;
  error.value = "";
  notice.value = "";
  preview.value = undefined;
  try {
    const data = await captured.client.request("/operations/cleanup", {
      method: "POST",
      body: { preview: true, cutoff },
      signal: c.signal,
    });
    if (!current(n, c)) return;
    preview.value = parseCleanupReport229(data, cutoff, true);
    unknown.value = false;
  } catch (e) {
    if (current(n, c))
      error.value = e instanceof Error ? e.message : "清理预览失败，请重新核对";
  } finally {
    if (current(n, c)) busy.value = false;
  }
}
async function quickPreview() {
  if (busy.value || !ensure()) return;
  date.value = cleanupDateDaysAgo229(30);
  await nextTick();
  await inspect();
}
function review() {
  if (!busy.value && !unknown.value && preview.value && ensure())
    confirmation.value = true;
}
async function cleanup() {
  if (
    busy.value ||
    !confirmation.value ||
    unknown.value ||
    !preview.value ||
    !ensure()
  )
    return;
  const original = preview.value,
    c = new AbortController(),
    n = ++epoch;
  controller?.abort();
  controller = c;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const raw = await captured.client.request("/operations/cleanup", {
      method: "POST",
      body: { preview: false, cutoff: original.cutoff },
      signal: c.signal,
    });
    if (!current(n, c)) return;
    const result = parseCleanupReport229(raw, original.cutoff, false);
    notice.value =
      "已清理：" +
      cleanupCounts229
        .map(({ key, label }) => `${label} ${result[key]} 条`)
        .join("，") +
      "。";
    preview.value = undefined;
    confirmation.value = false;
  } catch {
    if (current(n, c)) {
      unknown.value = true;
      confirmation.value = false;
      error.value =
        "清理结果尚未核实，请重新预览同一截止条件并核对；不会自动重复清理。";
    }
  } finally {
    if (current(n, c)) {
      busy.value = false;
      await nextTick();
      previewButton.value?.focus();
    }
  }
}
watch(date, reset, { flush: "sync" });
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
  ],
  () => {
    reset();
    runtimeOpen.value = false;
    captured = context();
  },
  { flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  ++epoch;
  controller?.abort();
});
</script>

<template>
  <section class="panel records cleanup-card">
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <p v-if="!canWrite" role="status">仅本人超级管理员可以预览和清理数据。</p>
    <h3 class="clean-subhead">快捷清理</h3>
    <div class="clean-quick">
      <button v-if="canWrite" :disabled="busy" @click="quickPreview">
        预览30天前登录数据
      </button>
      <button v-if="canWrite" :disabled="busy" @click="runtimeOpen = true">
        清理运行日志
      </button>
    </div>
    <p class="muted">
      设置缓存由服务管理，前端静态资源由发布流程维护。本页不删除运行程序、已发布资源或财务记录。
    </p>
    <h3 class="clean-subhead">自定义清理</h3>
    <div class="clean-grid">
      <article class="clean-item">
        <header class="clean-item-head">
          <strong>登录日志与过期状态</strong
          ><KitStatusTag text="可按时间清理" tone="info" />
        </header>
        <p class="muted">
          同时清理截止时间之前的登录日志、过期会话、内存尝试计数及持久限流记录，最近一天的数据会保留。
        </p>
        <label class="clean-field"
          >清理截止日期（UTC+08:00）<input
            v-model="date"
            type="date"
            class="pa-input"
            :min="bounds.min"
            :max="bounds.max"
            :disabled="busy || !canWrite"
            required
        /></label>
        <small class="muted"
          >可选范围 {{ bounds.min }} 至
          {{ bounds.max }}；截止时间为所选日期零点。</small
        >
        <button
          v-if="canWrite"
          ref="previewButton"
          :disabled="busy"
          @click="inspect"
        >
          {{ unknown ? "重新预览核对" : "预览清理范围" }}
        </button>
        <dl v-if="preview" class="record-detail">
          <dt>截止时间（UTC+08:00）</dt>
          <dd>{{ preview.cutoff.slice(0, 10) }} 00:00:00</dd>
          <template v-for="item in cleanupCounts229" :key="item.key"
            ><dt>{{ item.label }}</dt>
            <dd :data-cleanup-count="item.key">
              {{ preview[item.key] }}
            </dd></template
          >
        </dl>
        <button
          v-if="canWrite && preview && !unknown"
          class="danger-button"
          :disabled="busy"
          @click="review"
        >
          立即删除
        </button>
      </article>
      <article
        v-for="item in retained"
        :key="item.key"
        class="clean-item clean-item--off"
      >
        <header class="clean-item-head">
          <strong>{{ item.label }}</strong
          ><KitStatusTag text="不可删除" tone="neutral" />
        </header>
        <p class="clean-reason muted">{{ item.reason }}</p>
      </article>
    </div>
    <KitModal
      v-if="confirmation && preview"
      title="确认清理登录数据"
      submit-text="确认清理"
      :loading="busy"
      :submit-disabled="!canWrite || unknown"
      danger
      @close="confirmation = false"
      @submit="cleanup"
    >
      <p>
        清理
        {{ preview.cutoff.slice(0, 10) }}
        00:00:00（UTC+08:00）及之前的以下数据。此操作不能撤销，交易、账本和结算记录不受影响。
      </p>
      <dl class="record-detail">
        <template v-for="item in cleanupCounts229" :key="item.key"
          ><dt>{{ item.label }}</dt>
          <dd>{{ preview[item.key] }}</dd></template
        >
      </dl>
      <p class="muted">预览为核对时的数量，实际执行仍按同一截止条件统计。</p>
    </KitModal>
    <RuntimeLogCleanup
      v-if="runtimeOpen"
      :client="client"
      :own-session="ownSession"
      :admin-class="adminClass"
      @close="runtimeOpen = false"
      @updated="notice = '运行日志清理已完成。'"
    />
  </section>
</template>
<style scoped>
.cleanup-card {
  max-width: 1100px;
}
.clean-subhead {
  margin: 18px 0 12px;
  font-size: 14px;
}
.clean-quick {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 12px;
}
.clean-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 300px), 1fr));
  gap: var(--space-4);
}
.clean-item {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  padding: var(--space-4);
}
.clean-item-head {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}
.clean-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.clean-item .muted {
  margin: 0;
  line-height: 1.6;
}
.clean-item--off {
  opacity: 0.8;
}
</style>
