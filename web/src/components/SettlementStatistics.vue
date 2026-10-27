<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, nextTick } from "vue";
import { ApiClient } from "../api";
import { readSettlementStatistics } from "../settlement-filters";
const props = defineProps<{ client: ApiClient; query: string }>();
const emit = defineEmits<{ close: [] }>();
const stats = ref<ReturnType<typeof readSettlementStatistics> | null>(null),
  error = ref(""),
  loading = ref(true),
  closeButton = ref<HTMLButtonElement | null>(null),
  controller = new AbortController();
const labels = [
  ["total_count", "总条数"],
  ["waiting_count", "待结算"],
  ["completed_count", "已完成"],
  ["processing_count", "正在结算"],
  ["failed_count", "结算失败"],
  ["cancelled_count", "已取消"],
  ["returning_count", "退回处理中"],
  ["returned_count", "已退回"],
] as const;
onMounted(async () => {
  await nextTick();
  closeButton.value?.focus();
  try {
    const raw = await props.client.request(
      "/workflows/settlements/statistics" +
        (props.query ? "?" + props.query : ""),
      { signal: controller.signal },
    );
    if (!controller.signal.aborted) stats.value = readSettlementStatistics(raw);
  } catch (e) {
    if (!controller.signal.aborted) {
      stats.value = null;
      error.value = e instanceof Error ? e.message : "结算统计读取失败";
    }
  } finally {
    if (!controller.signal.aborted) loading.value = false;
  }
});
onBeforeUnmount(() => {
  controller.abort();
  stats.value = null;
});
</script>
<template>
  <div
    class="modal-backdrop"
    @keydown.esc.stop="emit('close')"
    @keydown.tab.prevent="closeButton?.focus()"
    @click.self="emit('close')"
  >
    <section
      role="dialog"
      aria-modal="true"
      aria-labelledby="settlement-statistics-title"
      class="modal panel settlement-statistics"
    >
      <h2 id="settlement-statistics-title">结算统计</h2>
      <p class="muted">按已查询的筛选范围统计全部记录。</p>
      <p v-if="loading" role="status">正在加载统计…</p>
      <p v-else-if="error" role="alert" class="alert error">{{ error }}</p>
      <div v-else-if="stats" class="stat-grid">
        <div class="settlement-stat-total">
          <span>总结算金额</span><strong>¥{{ stats.total_money }}</strong>
        </div>
        <template v-for="[key, label] in labels" :key="key"
          ><div
            :class="'settlement-stat-' + key"
            v-if="
              ![
                'cancelled_count',
                'returning_count',
                'returned_count',
              ].includes(key) || stats[key] !== '0'
            "
          >
            <span>{{ label }}</span
            ><strong>{{ stats[key] }}</strong>
          </div></template
        >
      </div>
      <div class="modal-actions">
        <button ref="closeButton" type="button" @click="emit('close')">
          关闭
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.settlement-statistics {
  width: min(500px, calc(100vw - 32px));
  max-height: calc(100dvh - 40px);
  overflow: auto;
  box-sizing: border-box;
}
.stat-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.stat-grid > div {
  display: grid;
  gap: 8px;
  padding: 14px;
  border: 1px solid var(--line, #ebeef5);
  border-radius: 6px;
  min-width: 0;
}
.stat-grid strong {
  overflow-wrap: anywhere;
  font-variant-numeric: tabular-nums;
}
.settlement-stat-waiting_count {
  background: #fdf6ec;
}
.settlement-stat-completed_count {
  background: #f0f9eb;
}
.settlement-stat-processing_count {
  background: #ecfafa;
}
.settlement-stat-failed_count {
  background: #fef0f0;
}
.stat-grid .settlement-stat-total {
  grid-column: 1/-1;
  background: var(--surface-soft, #f4f8ff);
}
.modal-actions {
  margin-top: 18px;
}
</style>
