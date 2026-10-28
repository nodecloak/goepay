<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";
import { ApiClient } from "../api";
import {
  reportDateRange,
  validDateRange,
  csvExport,
  type ReportColumn,
} from "../reports";
import CsvExportButton from "./CsvExportButton.vue";
const props = defineProps<{ client: ApiClient; report: string }>();
const today = () => reportDateRange("today")!;
const from = ref(today().from),
  to = ref(today().to),
  method = ref("type"),
  metric = ref("0"),
  sort = ref("total"),
  direction = ref("desc"),
  page = ref(1);
const columns = ref<ReportColumn[]>([]),
  items = ref<Record<string, unknown>[]>([]),
  summary = ref<Record<string, unknown> | null>(null),
  total = ref(0),
  missingCosts = ref("0"),
  loading = ref(false),
  error = ref("");
const applied = ref("");
const transfer = computed(() => props.report === "transfer-merchants");
function draft() {
  return new URLSearchParams({
    view: "matrix",
    from: from.value,
    to: to.value,
    method: method.value,
    type: transfer.value ? "0" : metric.value,
    sort: sort.value,
    direction: direction.value,
  }).toString();
}
const dirty = computed(() => draft() !== applied.value);
const exportPath = computed(
  () =>
    `/reports/${encodeURIComponent(props.report)}?${applied.value}&format=csv`,
);
let controller: AbortController | undefined;
function reset() {
  controller?.abort();
  columns.value = [];
  items.value = [];
  summary.value = null;
  total.value = 0;
  missingCosts.value = "0";
  loading.value = false;
  error.value = "";
  applied.value = "";
  page.value = 1;
  from.value = today().from;
  to.value = today().to;
  method.value = "type";
  metric.value = "0";
  resetSort();
}
function resetSort() {
  sort.value = "total";
  direction.value = "desc";
}
function sortBy(key: string) {
  if (loading.value || dirty.value) return;
  if (sort.value === key)
    direction.value = direction.value === "asc" ? "desc" : "asc";
  else {
    sort.value = key;
    direction.value = key === "uid" ? "asc" : "desc";
  }
  page.value = 1;
  void load();
}
watch(
  () => [props.client, props.client.area, props.client.csrfToken, props.report],
  () => {
    reset();
    void load();
  },
  { flush: "sync" },
);
async function load() {
  controller?.abort();
  const c = new AbortController();
  controller = c;
  error.value = "";
  items.value = [];
  summary.value = null;
  total.value = 0;
  if (props.client.area !== "admin") {
    loading.value = false;
    error.value = "仅管理员可查看商户汇总";
    return;
  }
  if (
    !from.value ||
    !to.value ||
    from.value.startsWith("0000-") ||
    to.value.startsWith("0000-") ||
    !validDateRange(from.value, to.value) ||
    !["type", "channel"].includes(method.value) ||
    !["0", "1", "2", "3"].includes(metric.value)
  ) {
    loading.value = false;
    error.value = "请选择有效的统计日期、查看方式和统计类型，日期范围最多367天";
    return;
  }
  const query = draft(),
    client = props.client,
    report = props.report;
  loading.value = true;
  try {
    const result = await client.request<{
      columns: ReportColumn[];
      items: Record<string, unknown>[];
      summary: Record<string, unknown>;
      total: number;
      missing_costs: string;
    }>(
      `/reports/${encodeURIComponent(report)}?${query}&page=${page.value}&limit=20`,
      { signal: c.signal },
    );
    if (c.signal.aborted) return;
    columns.value = result.columns;
    items.value = result.items;
    summary.value = result.summary;
    total.value = result.total;
    missingCosts.value = result.missing_costs;
    applied.value = query;
  } catch (e) {
    if (!c.signal.aborted)
      error.value = e instanceof Error ? e.message : "统计读取失败，请重试";
  } finally {
    if (!c.signal.aborted) loading.value = false;
  }
}
function display(value: unknown) {
  return value === null || value === undefined ? "未知" : String(value);
}
function downloadPage() {
  if (loading.value || dirty.value || !summary.value) return;
  const blob = new Blob(
    [csvExport(columns.value, [summary.value, ...items.value])],
    { type: "text/csv;charset=utf-8;" },
  );
  const url = URL.createObjectURL(blob),
    a = document.createElement("a");
  a.href = url;
  a.download = `${props.report}-${from.value}-${to.value}-page${page.value}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}
onMounted(() => void load());
onBeforeUnmount(() => controller?.abort());
</script>
<template>
  <section class="panel records merchant-matrix">
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <form
      class="matrix-filters"
      @submit.prevent="
        page = 1;
        load();
      "
    >
      <label
        >开始日期<input v-model="from" type="date" :disabled="loading"
      /></label>
      <label
        >结束日期<input v-model="to" type="date" :disabled="loading"
      /></label>
      <label
        >查看方式<select
          v-model="method"
          aria-label="查看方式"
          :disabled="loading"
          @change="resetSort"
        >
          <option value="type">
            {{ transfer ? "以付款方式查看" : "以支付方式查看" }}
          </option>
          <option value="channel">
            {{ transfer ? "以付款通道查看" : "以支付通道查看" }}
          </option>
        </select></label
      >
      <label v-if="!transfer"
        >统计类型<select
          v-model="metric"
          aria-label="统计类型"
          :disabled="loading"
        >
          <option value="0">订单金额统计</option>
          <option value="1">支付金额统计</option>
          <option value="2">分成金额统计</option>
          <option value="3">手续费利润统计</option>
        </select></label
      >
      <div class="matrix-filter-actions">
        <button class="primary" :disabled="loading">查询统计</button
        ><button
          type="button"
          :disabled="loading"
          @click="
            reset();
            load();
          "
        >
          重置
        </button>
      </div>
    </form>
    <p class="muted matrix-description" v-if="transfer">
      按北京时间原完成时间统计成功付款本金；已退回或待核实退回不计入。付款方式来自原加密意图，历史未记录时显示未知。
    </p>
    <p class="muted matrix-description" v-else>
      仅统计已支付订单，按北京时间原支付日期汇总。订单金额与支付金额在当前固定收单金额模式一致；分成为原商户净额。手续费利润使用原成本快照估算，缺成本保留未知。
    </p>
    <p v-if="!loading && dirty && summary" class="muted">
      筛选条件已修改，请查询后查看或导出。
    </p>
    <p
      v-if="!transfer && metric === '3' && summary"
      role="status"
      class="alert"
    >
      {{ missingCosts }} 笔付款缺少成本快照；受影响的单元和总计显示未知。
    </p>
    <div class="matrix-tools">
      <span>共 {{ total }} 个商户</span
      ><button
        type="button"
        :disabled="loading || dirty || !summary"
        @click="downloadPage"
      >
        导出本页 CSV</button
      ><CsvExportButton
        :client="client"
        :path="exportPath"
        :filename="`${report}-${from}-${to}.csv`"
        :disabled="loading || dirty || !summary"
      />
    </div>
    <div v-if="loading" class="loading-row" role="status">正在汇总统计…</div>
    <div v-else-if="!error && summary" class="table-scroll">
      <table>
        <thead>
          <tr>
            <th
              v-for="c in columns"
              :key="c.key"
              scope="col"
              :aria-label="c.label"
              :aria-sort="
                sort === c.key
                  ? direction === 'asc'
                    ? 'ascending'
                    : 'descending'
                  : 'none'
              "
            >
              <button
                class="matrix-sort"
                :aria-label="`按${c.label}排序`"
                :disabled="loading || dirty"
                @click="sortBy(c.key)"
              >
                {{ c.label }}
                <span aria-hidden="true">{{
                  sort === c.key ? (direction === "asc" ? "↑" : "↓") : "↕"
                }}</span>
              </button>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr class="matrix-summary">
            <td v-for="c in columns" :key="c.key">
              {{ c.key === "uid" ? "总计" : display(summary[c.key]) }}
            </td>
          </tr>
          <tr v-for="row in items" :key="String(row.uid)">
            <td v-for="c in columns" :key="c.key">
              <a
                v-if="c.key === 'uid' && /^[1-9]\d*$/.test(String(row.uid))"
                :href="`/admin/merchant/merchant-list?uid=${encodeURIComponent(String(row.uid))}`"
                >{{ row.uid }}</a
              ><template v-else>{{ display(row[c.key]) }}</template>
            </td>
          </tr>
          <tr v-if="!items.length">
            <td :colspan="columns.length" class="muted">
              暂无符合条件的成功记录
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="pagination">
      <span>第 {{ page }} 页</span>
      <div>
        <button
          :disabled="page <= 1 || loading || dirty || !!error"
          @click="
            page--;
            load();
          "
        >
          上一页</button
        ><button
          :disabled="page * 20 >= total || loading || dirty || !!error"
          @click="
            page++;
            load();
          "
        >
          下一页
        </button>
      </div>
    </div>
  </section>
</template>
<style scoped>
.merchant-matrix {
  padding: 20px;
  min-width: 0;
}
.matrix-filters {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 18px;
}
.matrix-filters label {
  display: grid;
  gap: 6px;
  min-width: 0;
  font-size: 13px;
  margin: 0;
}
.matrix-filters input,
.matrix-filters select {
  width: 100%;
  min-width: 0;
  margin: 0;
}
.matrix-filter-actions,
.matrix-tools {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}
.matrix-tools {
  margin: 16px 0;
}
.matrix-tools > span {
  margin-right: auto;
}
.matrix-description {
  font-size: 12px;
  line-height: 1.7;
}
.matrix-summary {
  font-weight: 600;
  background: var(--bg-soft, var(--bg-raised));
}
.matrix-sort {
  border: 0;
  background: transparent;
  padding: 0;
  min-height: 32px;
  color: inherit;
  font: inherit;
  font-weight: 600;
}
table {
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  padding: 13px 16px;
  border-bottom: 1px solid var(--border, var(--line));
  text-align: right;
  white-space: nowrap;
}
th:first-child,
td:first-child {
  text-align: left;
}
@media (max-width: 800px) {
  .matrix-filters {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 420px) {
  .merchant-matrix {
    padding: 16px;
  }
  .matrix-filters {
    grid-template-columns: minmax(0, 1fr);
  }
  .matrix-tools {
    align-items: flex-start;
  }
}
</style>
