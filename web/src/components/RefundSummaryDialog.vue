<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from "vue";
import type { ApiClient } from "../api";
const props = defineProps<{ client: ApiClient; query: string }>(),
  emit = defineEmits<{ close: [] }>();
const fields = [
  { key: "totalCount", label: "退款总笔数" },
  { key: "totalMoney", label: "退款总金额" },
  { key: "successCount", label: "成功笔数" },
  { key: "successMoney", label: "成功金额" },
  { key: "pendingCount", label: "处理中笔数" },
  { key: "pendingMoney", label: "处理中金额" },
] as const;
const data = ref<Record<string, string> | null>(null),
  loading = ref(false),
  error = ref(""),
  closeButton = ref<HTMLButtonElement>(),
  id = useId();
let epoch = 0,
  controller: AbortController | undefined;
function summary(value: unknown): Record<string, string> {
  if (!value || typeof value !== "object") throw Error("退款统计返回格式无效");
  const result: Record<string, string> = {};
  for (const { key } of fields) {
    const raw = (value as Record<string, unknown>)[key];
    if (
      typeof raw !== "string" ||
      !(
        key.endsWith("Money")
          ? /^(0|[1-9]\d{0,35})\.\d{2}$/
          : /^(0|[1-9]\d{0,18})$/
      ).test(raw)
    )
      throw Error("退款统计金额或计数格式无效");
    result[key] = raw;
  }
  for (const suffix of ["Count", "Money"]) {
    const n = (key: string) => BigInt(result[key]!.replace(".", ""));
    if (n("success" + suffix) + n("pending" + suffix) > n("total" + suffix))
      throw Error("退款统计总计与状态明细不一致，请重试读取");
  }
  return result;
}
async function load() {
  controller?.abort();
  const serial = ++epoch,
    c = new AbortController();
  controller = c;
  data.value = null;
  error.value = "";
  loading.value = true;
  const client = props.client,
    query = props.query;
  try {
    const value = await client.request(
      "/refunds/summary" + (query ? "?" + query : ""),
      { signal: c.signal },
    );
    if (serial !== epoch || c.signal.aborted) return;
    data.value = summary(value);
  } catch (e) {
    if (serial === epoch && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "退款统计读取失败，请重试";
  } finally {
    if (serial === epoch) loading.value = false;
  }
}
watch(
  () => [props.client, props.client.area, props.client.csrfToken, props.query],
  () => {
    void load();
  },
  { immediate: true, flush: "sync" },
);
function keydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    emit("close");
    return;
  }
  if (event.key !== "Tab") return;
  const controls = Array.from(
      (event.currentTarget as HTMLElement).querySelectorAll<HTMLButtonElement>(
        "button:not(:disabled)",
      ),
    ),
    first = controls[0],
    last = controls.at(-1);
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}
onMounted(() => void nextTick(() => closeButton.value?.focus()));
onBeforeUnmount(() => {
  epoch++;
  controller?.abort();
  data.value = null;
});
</script>
<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section
      role="dialog"
      aria-modal="true"
      :aria-labelledby="id + '-title'"
      class="modal panel refund-summary"
      tabindex="-1"
      @keydown="keydown"
    >
      <div class="toolbar">
        <h2 :id="id + '-title'">退款统计</h2>
        <button ref="closeButton" type="button" @click="emit('close')">
          关闭
        </button>
      </div>
      <p class="muted">统计当前筛选条件下的全部退款记录，不受列表分页限制。</p>
      <p v-if="loading" role="status">正在读取退款统计…</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <dl v-if="data" class="refund-summary-grid">
        <div v-for="f in fields" :key="f.key">
          <dt>{{ f.label }}</dt>
          <dd>{{ f.key.endsWith("Money") ? "¥" : "" }}{{ data[f.key] }}</dd>
        </div>
      </dl>
      <p class="muted">
        处理中包含退款中及待核实记录；失败记录计入筛选总计，不计入成功金额。
      </p>
      <button v-if="error" type="button" :disabled="loading" @click="load">
        重试读取退款统计
      </button>
    </section>
  </div>
</template>
<style scoped>
.refund-summary {
  width: min(100%, 520px);
}
.refund-summary-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  margin: 20px 0;
  border: 1px solid var(--border, var(--line));
}
.refund-summary-grid > div {
  min-width: 0;
  padding: 12px;
  border-bottom: 1px solid var(--border, var(--line));
}
.refund-summary-grid > div:nth-child(odd) {
  border-right: 1px solid var(--border, var(--line));
}
.refund-summary-grid > div:nth-last-child(-n + 2) {
  border-bottom: 0;
}
.refund-summary-grid dt {
  color: var(--text-secondary, var(--ink-muted));
  font-size: 13px;
}
.refund-summary-grid dd {
  font-weight: 600;
  margin: 7px 0 0;
  overflow-wrap: anywhere;
  font-variant-numeric: tabular-nums;
}
.refund-summary > p {
  line-height: 1.7;
}
</style>
