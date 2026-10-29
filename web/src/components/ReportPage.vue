<script setup lang="ts">
import {
  ref,
  computed,
  onMounted,
  onBeforeUnmount,
  defineAsyncComponent,
} from "vue";
import { ApiClient } from "../api";
import {
  csvExport,
  validDateRange,
  reportColumns,
  reportRows,
  reportDateRange,
  type ReportColumn,
} from "../reports";
import DataTable from "./DataTable.vue";
import KitPagination from "./KitPagination.vue";
import KitField from "./KitField.vue";
import KitSelect from "./KitSelect.vue";
import KitHelpPopover from "./KitHelpPopover.vue";
import CsvExportButton from "./CsvExportButton.vue";
import ShellIcon from "./ShellIcon.vue";
import MerchantMatrixReport from "./MerchantMatrixReport.vue";
import BuyerStatisticsReport from "./BuyerStatisticsReport.vue";
import OriginalOrderStatistics from "./OriginalOrderStatistics.vue";
const ProvinceStatisticsReport = defineAsyncComponent(
  () => import("./ProvinceStatisticsReport.vue"),
);
const props = defineProps<{
  client: ApiClient;
  report: string;
  ownSession?: boolean;
  adminClass?: string;
}>();
const from = ref(""),
  to = ref(""),
  shortcut = ref(""),
  page = ref(1),
  limit = ref(20),
  columns = ref<ReportColumn[]>([]),
  items = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  loading = ref(false),
  error = ref("");
const summary = ref<Record<string, unknown> | null>(null);
const complaintOverview = computed(() => props.report === "complaints");
const reportNote = computed(() =>
  complaintOverview.value
    ? "本地已登记投诉，包含所有处理状态。按北京时间统计今日、昨日和近7日；近7日比率为投诉条数 ÷ 同期已支付订单数，无支付订单时显示未知。未登记的上游投诉不计入。"
    : "业务日期按 UTC+08:00 统计。未指定日期时查询最近 30 天；未采集的维度会显示为未知。",
);
const originalStatistics = computed(
  () =>
    props.report === "daily" ||
    (props.client.area === "admin" && props.report === "channels"),
);
const merchantMatrix = computed(() =>
  ["merchants", "transfer-merchants"].includes(props.report),
);
let controller: AbortController | undefined;
function applyShortcut() {
  const range = reportDateRange(shortcut.value);
  if (!range) return;
  from.value = range.from;
  to.value = range.to;
  page.value = 1;
  load();
}
function reportQuery() {
  const q = new URLSearchParams({
    page: String(page.value),
    limit: String(limit.value),
  });
  if (complaintOverview.value) {
    q.set("view", "overview");
    return q;
  }
  if (from.value) q.set("from", from.value);
  if (to.value) q.set("to", to.value);
  return q;
}
const csvPath = computed(() => {
  const q = reportQuery();
  q.set("format", "csv");
  return `/reports/${encodeURIComponent(props.report)}?${q}`;
});
async function load() {
  error.value = "";
  if (!validDateRange(from.value, to.value)) {
    error.value = "请选择有效日期，开始日期不得晚于结束日期，日期范围最多367天";
    return;
  }
  controller?.abort();
  controller = new AbortController();
  const c = controller;
  loading.value = true;
  items.value = [];
  try {
    const q = reportQuery();
    const data = await props.client.request<{
      columns: ReportColumn[];
      items: Record<string, unknown>[];
      total: number;
      summary?: Record<string, unknown>;
    }>("/reports/" + encodeURIComponent(props.report) + "?" + q, {
      signal: c.signal,
    });
    if (!c.signal.aborted) {
      columns.value = reportColumns(props.report, data.columns);
      items.value = reportRows(data.items);
      total.value = data.total;
      summary.value = data.summary || null;
    }
  } catch (e) {
    if (!c.signal.aborted)
      error.value = e instanceof Error ? e.message : "统计读取失败，请重试";
  } finally {
    if (!c.signal.aborted) loading.value = false;
  }
}
function download() {
  const blob = new Blob([csvExport(columns.value, items.value)], {
      type: "text/csv;charset=utf-8;",
    }),
    url = URL.createObjectURL(blob),
    a = document.createElement("a");
  a.href = url;
  a.download = `${props.report}-${from.value || "recent"}-${to.value || "today"}-page${page.value}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}
onMounted(() => {
  if (props.report === "provinces") return;
  if (
    !originalStatistics.value &&
    !merchantMatrix.value &&
    props.report !== "buyers"
  )
    void load();
});
onBeforeUnmount(() => controller?.abort());
</script>
<template>
  <ProvinceStatisticsReport v-if="report === 'provinces'" :client="client" />
  <OriginalOrderStatistics
    v-else-if="originalStatistics"
    :client="client"
    :report="report"
  />
  <BuyerStatisticsReport
    v-else-if="report === 'buyers'"
    :client="client"
    :own-session="ownSession"
    :admin-class="adminClass"
  />
  <MerchantMatrixReport
    v-else-if="merchantMatrix"
    :client="client"
    :report="report"
  />
  <div v-else class="report-page">
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div
      v-if="complaintOverview && summary"
      class="stat-grid complaint-stat-grid"
    >
      <section
        v-for="day in [
          { key: 'today', label: '今日投诉' },
          { key: 'yesterday', label: '昨日投诉' },
        ]"
        :key="day.key"
        class="panel stat"
      >
        <span class="stat-icon"><ShellIcon name="shield" /></span>
        <p>{{ day.label }}</p>
        <strong>{{ summary[`${day.key}_count`] ?? "—" }}</strong>
        <p class="card-sub">
          总投诉 <b>{{ summary[`${day.key}_count`] ?? "—" }}</b> · 支付宝
          <b>{{ summary[`${day.key}_alipay`] ?? "—" }}</b> · 微信
          <b>{{ summary[`${day.key}_wxpay`] ?? "—" }}</b>
        </p>
        <span>投诉金额 {{ summary[`${day.key}_money`] ?? "未知" }} 元</span>
      </section>
    </div>
    <form
      class="toolbar list-filter-card panel report-filter-kit"
      @submit.prevent="
        page = 1;
        load();
      "
    >
      <div class="pa-filter-fields list-filters-grid">
        <template v-if="!complaintOverview">
          <KitField label="快捷范围">
            <KitSelect
              v-model="shortcut"
              :options="[
                { value: '', label: '自定义日期' },
                { value: 'today', label: '今天' },
                { value: 'yesterday', label: '昨天' },
                { value: 'last7', label: '最近7天' },
                { value: 'last30', label: '最近30天' },
              ]"
              aria-label="统计日期快捷范围"
              @change="applyShortcut"
            />
          </KitField>
          <KitField label="开始日期">
            <input
              v-model="from"
              type="date"
              aria-label="开始日期"
              @input="shortcut = ''"
            />
          </KitField>
          <KitField label="结束日期">
            <input
              v-model="to"
              type="date"
              aria-label="结束日期"
              @input="shortcut = ''"
            />
          </KitField>
        </template>
      </div>
      <div class="pa-filter-actions">
        <button
          type="submit"
          class="pa-btn pa-btn--primary"
          :disabled="loading"
        >
          {{ complaintOverview ? "刷新统计" : "查询统计" }}
        </button>
      </div>
    </form>
    <section class="panel records">
      <div class="table-tools">
        <button
          type="button"
          class="pa-btn pa-btn--success"
          :disabled="loading || !items.length"
          @click="download"
        >
          <ShellIcon name="export" />导出本页 CSV
        </button>
        <CsvExportButton
          :client="client"
          :path="csvPath"
          :filename="`${report}-filtered.csv`"
          :disabled="loading || !validDateRange(from, to)"
        />
        <span class="table-summary"
          >共 <strong>{{ total }}</strong> 条统计</span
        >
        <KitHelpPopover :text="reportNote" />
      </div>
      <div v-if="loading" class="loading-row" role="status">正在汇总统计…</div>
      <DataTable v-else-if="!error" :columns="columns" :items="items" />
      <KitPagination
        v-model:page="page"
        v-model:page-size="limit"
        :total="total"
        @change="load"
      />
    </section>
  </div>
</template>
<style scoped>
.report-page {
  display: flex;
  flex-direction: column;
}
.complaint-stat-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
  margin-bottom: var(--space-4);
}
.card-sub {
  color: var(--ink-muted);
  font-size: 12px;
  margin-top: 6px;
}
.card-sub b {
  color: var(--ink);
  font-weight: 600;
}
.filter-inline {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  font-size: 13px;
}
.date-range {
  gap: 4px;
}
.date-range input {
  width: auto;
}
.date-range-sep {
  color: var(--ink-muted);
}
.filter-actions {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-left: auto;
}
.table-summary {
  margin-left: auto;
  font-size: 13px;
  color: var(--ink-muted);
}
.table-note {
  margin: 0 var(--space-5) var(--space-3);
  color: var(--ink-muted);
  font-size: 12px;
  line-height: 1.7;
}
@media (max-width: 760px) {
  .complaint-stat-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
