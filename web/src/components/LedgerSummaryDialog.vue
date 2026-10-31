<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from "vue";
import type { ApiClient } from "../api";
const props = defineProps<{ client: ApiClient; query: string }>(),
  emit = defineEmits<{ close: [] }>();
const fields = [
  { key: "totalCount", label: "记录总笔数" },
  { key: "increaseMoney", label: "增加金额" },
  { key: "decreaseMoney", label: "减少金额" },
  { key: "netMoney", label: "总计金额" },
] as const;
const data = ref<Record<string, string> | null>(null),
  loading = ref(false),
  error = ref(""),
  closeButton = ref<HTMLButtonElement>(),
  id = useId();
let epoch = 0,
  controller: AbortController | undefined;
function summary(value: unknown): Record<string, string> {
  if (!value || typeof value !== "object")
    throw Error("资金明细统计返回格式无效");
  const result: Record<string, string> = {};
  for (const { key } of fields) {
    const raw = (value as Record<string, unknown>)[key];
    if (
      typeof raw !== "string" ||
      !(
        key === "totalCount"
          ? /^(0|[1-9]\d{0,18})$/
          : key === "netMoney"
            ? /^-?(0|[1-9]\d{0,35})\.\d{2}$/
            : /^(0|[1-9]\d{0,35})\.\d{2}$/
      ).test(raw)
    )
      throw Error("资金明细统计金额或计数格式无效");
    result[key] = raw;
  }
  // 总计 must equal 增加 − 减少 to the cent; a mismatch means a partial read.
  const cents = (key: string) => BigInt(result[key]!.replace(".", ""));
  if (cents("increaseMoney") - cents("decreaseMoney") !== cents("netMoney"))
    throw Error("资金明细统计总计与增减明细不一致，请重试读取");
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
      "/ledger/summary" + (query ? "?" + query : ""),
      { signal: c.signal },
    );
    if (serial !== epoch || c.signal.aborted) return;
    data.value = summary(value);
  } catch (e) {
    if (serial === epoch && !c.signal.aborted)
      error.value =
        e instanceof Error ? e.message : "资金明细统计读取失败，请重试";
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
      class="modal panel ledger-summary"
      tabindex="-1"
      @keydown="keydown"
    >
      <div class="toolbar">
        <h2 :id="id + '-title'">资金明细统计</h2>
        <button
          ref="closeButton"
          type="button"
          class="pa-icon-btn pa-icon-btn--ghost pa-modal-x"
          aria-label="关闭"
          @click="emit('close')"
        >
          ×
        </button>
      </div>
      <p class="muted">统计当前筛选条件下的全部资金明细，不受列表分页限制。</p>
      <p v-if="loading" role="status">正在读取资金明细统计…</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <dl v-if="data" class="ledger-summary-grid">
        <div v-for="f in fields" :key="f.key">
          <dt>{{ f.label }}</dt>
          <dd>{{ f.key === "totalCount" ? "" : "¥" }}{{ data[f.key] }}</dd>
        </div>
      </dl>
      <p class="muted">
        增加金额为全部入账金额合计，减少金额为全部出账金额合计，总计金额为净变动（增加减去减少）。
      </p>
      <div class="modal-actions">
        <button v-if="error" type="button" :disabled="loading" @click="load">
          重试读取资金明细统计</button
        ><button type="button" class="primary" @click="emit('close')">
          关闭
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.ledger-summary {
  width: min(100%, 520px);
}
.ledger-summary-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  margin: 20px 0;
  border: 1px solid var(--border, var(--line));
}
.ledger-summary-grid > div {
  min-width: 0;
  padding: 12px;
  border-bottom: 1px solid var(--border, var(--line));
}
.ledger-summary-grid > div:nth-child(odd) {
  border-right: 1px solid var(--border, var(--line));
}
.ledger-summary-grid > div:nth-last-child(-n + 2) {
  border-bottom: 0;
}
.ledger-summary-grid dt {
  color: var(--text-secondary, var(--ink-muted));
  font-size: 13px;
}
.ledger-summary-grid dd {
  font-weight: 600;
  margin: 7px 0 0;
  overflow-wrap: anywhere;
  font-variant-numeric: tabular-nums;
}
.ledger-summary > p {
  line-height: 1.7;
}
</style>
