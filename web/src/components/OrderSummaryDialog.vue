<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from "vue";
import type { ApiClient } from "../api";
const props = defineProps<{ client: ApiClient; query: string }>(),
  emit = defineEmits<{ close: [] }>();
// Pro's 订单统计概况: 金额统计 then 数量统计 then 成功率, over the current filter.
const moneyFields = [
  { key: "orderMoney", label: "订单总金额" },
  { key: "paidMoney", label: "已支付金额" },
  { key: "merchantMoney", label: "商户分成金额" },
  { key: "refundedMoney", label: "已退款金额" },
] as const;
const countFields = [
  { key: "totalCount", label: "订单总数" },
  { key: "paidCount", label: "已支付订单" },
  { key: "unpaidCount", label: "未支付订单" },
  { key: "refundedCount", label: "已退款订单" },
] as const;
const data = ref<Record<string, string> | null>(null),
  loading = ref(false),
  error = ref(""),
  closeButton = ref<HTMLButtonElement>(),
  id = useId();
let epoch = 0,
  controller: AbortController | undefined;
function overview(value: unknown): Record<string, string> {
  if (!value || typeof value !== "object") throw Error("订单统计返回格式无效");
  const v = value as Record<string, unknown>,
    result: Record<string, string> = {};
  for (const { key } of [...moneyFields, ...countFields]) {
    const raw = v[key];
    if (
      typeof raw !== "string" ||
      !(
        key.endsWith("Money")
          ? /^(0|[1-9]\d{0,35})\.\d{2}$/
          : /^(0|[1-9]\d{0,18})$/
      ).test(raw)
    )
      throw Error("订单统计金额或计数格式无效");
    result[key] = raw;
  }
  const rate = v.successRate;
  if (typeof rate !== "string" || !/^(—|\d{1,3}\.\d{2}%)$/.test(rate))
    throw Error("订单成功率格式无效");
  result.successRate = rate;
  const n = (k: string) => BigInt(result[k]!);
  if (n("paidCount") + n("unpaidCount") < n("totalCount"))
    throw Error("订单统计总计与状态明细不一致，请重试读取");
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
      "/orders/summary" + (query ? "?" + query : ""),
      { signal: c.signal },
    );
    if (serial !== epoch || c.signal.aborted) return;
    data.value = overview(value);
  } catch (e) {
    if (serial === epoch && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "订单统计读取失败，请重试";
  } finally {
    if (serial === epoch) loading.value = false;
  }
}
watch(
  () => [props.client, props.client.area, props.client.csrfToken, props.query],
  () => void load(),
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
  <Teleport to="body">
    <div class="pa-scrim pa-modal-scrim" @pointerdown.self="emit('close')">
      <section
        role="dialog"
        aria-modal="true"
        :aria-labelledby="id + '-title'"
        class="pa-modal order-summary-modal"
        tabindex="-1"
        @keydown="keydown"
      >
        <header class="pa-modal-head">
          <h2 :id="id + '-title'" class="pa-modal-title">订单统计概况</h2>
          <button
            ref="closeButton"
            type="button"
            class="pa-icon-btn pa-icon-btn--ghost pa-modal-x"
            aria-label="关闭"
            @click="emit('close')"
          >
            ×
          </button>
        </header>
        <div class="pa-modal-body">
          <div class="pa-modal-content">
            <p class="pa-muted">
              统计当前筛选条件下的全部订单，不受列表分页限制。
            </p>
            <p v-if="loading" role="status">正在读取订单统计…</p>
            <p v-if="error" role="alert" class="alert error">{{ error }}</p>
            <template v-if="data">
              <section class="order-summary-section">
                <h3 class="order-summary-sub">金额统计</h3>
                <dl class="order-summary-grid">
                  <div v-for="f in moneyFields" :key="f.key">
                    <dt>{{ f.label }}</dt>
                    <dd>¥{{ data[f.key] }}</dd>
                  </div>
                </dl>
              </section>
              <section class="order-summary-section">
                <h3 class="order-summary-sub">数量统计</h3>
                <dl class="order-summary-grid">
                  <div v-for="f in countFields" :key="f.key">
                    <dt>{{ f.label }}</dt>
                    <dd>{{ data[f.key] }}</dd>
                  </div>
                  <div class="order-summary-rate">
                    <dt>订单成功率</dt>
                    <dd>{{ data.successRate }}</dd>
                  </div>
                </dl>
              </section>
            </template>
            <p class="pa-muted">
              成功率为已支付订单占筛选总数的比例；已退款金额为成功退款合计。
            </p>
          </div>
          <footer class="pa-modal-foot">
            <button
              v-if="error"
              type="button"
              class="pa-btn"
              :disabled="loading"
              @click="load"
            >
              重试读取
            </button>
            <button
              type="button"
              class="pa-btn pa-btn--primary"
              @click="emit('close')"
            >
              关闭
            </button>
          </footer>
        </div>
      </section>
    </div>
  </Teleport>
</template>
<style scoped>
.order-summary-modal {
  width: min(100%, 560px);
}
.order-summary-modal p {
  margin: 0;
}
.order-summary-section {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}
.order-summary-sub {
  font-size: 13px;
  margin: 0;
  color: var(--ink-muted);
}
/* Lines come from a 1px gap over the line colour, so every cell keeps its
   edges even when the 成功率 row spans both columns. */
.order-summary-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1px;
  background: var(--line);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm, 6px);
  overflow: hidden;
  margin: 0;
}
.order-summary-grid > div {
  min-width: 0;
  padding: 12px;
  background: var(--bg-card);
}
.order-summary-grid dt {
  color: var(--ink-muted);
  font-size: 13px;
}
.order-summary-grid dd {
  font-weight: 600;
  margin: 7px 0 0;
  overflow-wrap: anywhere;
  font-variant-numeric: tabular-nums;
}
.order-summary-rate {
  grid-column: 1 / -1;
}
</style>
