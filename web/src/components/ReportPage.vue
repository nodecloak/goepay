<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
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
import CsvExportButton from "./CsvExportButton.vue";
const props = defineProps<{ client: ApiClient; report: string }>();
const from = ref(""),
  to = ref(""),
  shortcut = ref(""),
  page = ref(1),
  columns = ref<ReportColumn[]>([]),
  items = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  loading = ref(false),
  error = ref("");
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
  const q = new URLSearchParams({ page: String(page.value), limit: "20" });
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
    }>("/reports/" + encodeURIComponent(props.report) + "?" + q, {
      signal: c.signal,
    });
    if (!c.signal.aborted) {
      columns.value = reportColumns(props.report, data.columns);
      items.value = reportRows(data.items);
      total.value = data.total;
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
onMounted(load);
onBeforeUnmount(() => controller?.abort());
</script>
<template>
  <section class="panel records">
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <form
      class="toolbar report-filter"
      @submit.prevent="
        page = 1;
        load();
      "
    >
      <label
        >快捷范围<select
          v-model="shortcut"
          aria-label="统计日期快捷范围"
          @change="applyShortcut"
        >
          <option value="">自定义日期</option>
          <option value="today">今天</option>
          <option value="yesterday">昨天</option>
          <option value="last7">最近7天</option>
          <option value="last30">最近30天</option>
        </select></label
      >
      <label
        >开始日期<input
          v-model="from"
          type="date"
          @input="shortcut = ''" /></label
      ><label
        >结束日期<input
          v-model="to"
          type="date"
          @input="shortcut = ''" /></label
      ><button :disabled="loading">查询统计</button
      ><button
        type="button"
        :disabled="loading || !items.length"
        @click="download"
      >
        导出本页 CSV
      </button>
      <CsvExportButton
        :client="client"
        :path="csvPath"
        :filename="`${report}-filtered.csv`"
        :disabled="loading || !validDateRange(from, to)"
      />
    </form>
    <p class="resource-description muted">
      业务日期按 UTC+08:00 统计。未指定日期时查询最近 30
      天；未采集的维度会显示为未知。
    </p>
    <div v-if="loading" class="loading-row" role="status">正在汇总统计…</div>
    <DataTable v-else-if="!error" :columns="columns" :items="items" />
    <div class="pagination">
      <span>共 {{ total }} 条统计</span>
      <div>
        <button
          :disabled="page <= 1 || loading"
          @click="
            page--;
            load();
          "
        >
          上一页</button
        ><span>第 {{ page }} 页</span
        ><button
          :disabled="page * 20 >= total || loading"
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
