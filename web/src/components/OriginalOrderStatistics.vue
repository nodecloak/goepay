<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import { ApiClient } from "../api";
import { reportDateRange } from "../reports";
import {
  originalStatisticsQuery,
  statisticsMoney,
  statisticsTrend,
} from "../original-order-statistics";
import CsvExportButton from "./CsvExportButton.vue";
const props = defineProps<{ client: ApiClient; report: string }>();
const today = () => reportDateRange("today")!;
const filter = reactive({
  type: "",
  channel: "",
  uid: "",
  from: today().from,
  to: today().to,
});
const items = ref<Record<string, unknown>[]>([]),
  summary = ref<Record<string, unknown> | null>(null),
  loading = ref(false),
  error = ref(""),
  missing = ref("0");
const daily = computed(() => props.report === "daily"),
  merchant = computed(() => props.client.area === "merchant"),
  series = computed(() => statisticsTrend(items.value, props.client.area));
const columns = computed(() =>
  merchant.value
    ? [
        { key: "day", label: "日期" },
        { key: "realmoney", label: "订单金额" },
        { key: "getmoney", label: "分成金额" },
        { key: "refundmoney", label: "退款金额" },
        { key: "orderCount", label: "总订单数" },
        { key: "successCount", label: "成功订单数" },
        { key: "successRate", label: "支付成功率" },
      ]
    : [
        {
          key: daily.value ? "day" : "channelName",
          label: daily.value ? "日期" : "支付通道",
        },
        { key: "realmoney", label: "支付金额" },
        { key: "refundmoney", label: "退款金额" },
        { key: "profitmoney", label: "手续费利润" },
        { key: "orderCount", label: "总订单数" },
        { key: "successCount", label: "成功订单数" },
        { key: "successRate", label: "支付成功率" },
      ],
);
function cell(row: Record<string, unknown>, key: string) {
  if (["realmoney", "getmoney", "refundmoney", "profitmoney"].includes(key))
    return statisticsMoney(row[key]);
  if (key === "successRate") return row[key] == null ? "—" : row[key] + "%";
  return row[key];
}
const hiddenSeries = ref<string[]>([]),
  visibleSeries = computed(() =>
    series.value.filter((s) => !hiddenSeries.value.includes(s.key)),
  );
function toggleSeries(key: string) {
  hiddenSeries.value = hiddenSeries.value.includes(key)
    ? hiddenSeries.value.filter((k) => k !== key)
    : [...hiddenSeries.value, key];
}
let controller: AbortController | undefined,
  alive = true;
function clear() {
  controller?.abort();
  items.value = [];
  summary.value = null;
  missing.value = "0";
  loading.value = false;
  error.value = "";
}
watch(() => ({ ...filter }), clear, { flush: "sync" });
const csvPath = computed(() => {
  const parsed = originalStatisticsQuery(
    props.report,
    filter,
    props.client.area,
  );
  if (!parsed.query) return "";
  parsed.query.set("format", "csv");
  return "/reports/" + props.report + "?" + parsed.query;
});
async function load() {
  clear();
  const parsed = originalStatisticsQuery(
    props.report,
    filter,
    props.client.area,
  );
  if (!parsed.query) {
    error.value = parsed.error ?? "统计筛选不正确";
    return;
  }
  controller = new AbortController();
  const c = controller,
    client = props.client,
    csrf = client.csrfToken,
    report = props.report,
    area = client.area;
  loading.value = true;
  try {
    const data = await client.request<{
      items: Record<string, unknown>[];
      summary: Record<string, unknown>;
      missing_costs?: string;
    }>("/reports/" + report + "?" + parsed.query, { signal: c.signal });
    if (
      alive &&
      !c.signal.aborted &&
      props.client === client &&
      client.csrfToken === csrf &&
      client.area === area &&
      props.report === report
    ) {
      items.value = data.items;
      summary.value = !daily.value || merchant.value ? data.summary : null;
      missing.value = data.missing_costs ?? "0";
    }
  } catch (e) {
    if (alive && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "统计读取失败";
  } finally {
    if (alive && !c.signal.aborted) loading.value = false;
  }
}
function reset() {
  clear();
  hiddenSeries.value = [];
  Object.assign(filter, {
    type: "",
    channel: "",
    uid: "",
    from: today().from,
    to: today().to,
  });
  void load();
}
watch(
  () => [props.client, props.client.area, props.client.csrfToken, props.report],
  reset,
);
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  clear();
});
const visibleRows = computed(() =>
  summary.value && !merchant.value
    ? [summary.value, ...items.value]
    : items.value,
);
</script>
<template>
  <section class="panel records original-order-statistics">
    <form class="toolbar" @submit.prevent="load">
      <template v-if="!daily"
        ><label
          >查询日期（开始）<input
            v-model="filter.from"
            type="date"
            aria-label="查询日期（开始）" /></label
        ><label
          >查询日期（结束）<input
            v-model="filter.to"
            type="date"
            aria-label="查询日期（结束）" /></label
      ></template>
      <label
        >支付方式<select v-model="filter.type" aria-label="支付方式">
          <option value="">所有支付方式</option>
          <option value="alipay">支付宝</option>
          <option value="wxpay">微信支付</option>
          <option value="sandbox">本地模拟</option>
        </select></label
      >
      <label v-if="daily && !merchant"
        >通道ID<input
          v-model="filter.channel"
          aria-label="通道ID"
          inputmode="numeric"
          maxlength="19"
          placeholder="请输入通道ID"
      /></label>
      <label v-if="!merchant"
        >商户号<input
          v-model="filter.uid"
          aria-label="商户号"
          inputmode="numeric"
          maxlength="19"
          placeholder="请输入商户号"
      /></label>
      <button :disabled="loading">查询</button
      ><button type="button" :disabled="loading" @click="reset">重置</button
      ><button type="button" :disabled="loading" @click="load">刷新</button>
      <CsvExportButton
        :client="client"
        :path="csvPath"
        :disabled="loading || !!error || !items.length || !csvPath"
        :filename="report + '-original-statistics.csv'"
      />
    </form>
    <p class="muted">
      按北京时间的订单创建日期统计{{
        daily ? "近7天" : ""
      }}，金额单位为元。退款金额为该批订单已成功退款的累计金额；成功率按保留的全部订单计算。
    </p>
    <p v-if="merchant" class="muted">
      分成金额为已支付订单的原商户净额，退款另列；原分成合计不等于当前可用余额。
    </p>
    <p v-else class="muted">
      手续费利润为原订单成本快照的估算值；{{
        missing !== "0"
          ? missing + "笔已支付订单缺少成本，相关利润显示未知。"
          : "无订单时合计为0。"
      }}通道名称以已记录通道号标识。
    </p>
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <p v-if="loading" role="status">正在读取统计…</p>
    <template v-else-if="!error">
      <section v-if="daily && items.length" class="original-statistics-trend">
        <h2>近7天趋势</h2>
        <ul class="trend-legend">
          <li v-for="s in series" :key="s.key">
            <button
              type="button"
              :aria-pressed="!hiddenSeries.includes(s.key)"
              @click="toggleSeries(s.key)"
            >
              <span :style="{ backgroundColor: s.color }" />{{ s.name }}
            </button>
          </li>
        </ul>
        <svg viewBox="0 0 640 260" role="img" aria-label="近7天交易趋势">
          <line
            v-for="y in [20, 120, 220]"
            :key="y"
            x1="40"
            x2="600"
            :y1="y"
            :y2="y"
            stroke="var(--border,#e5e7eb)"
          />
          <g v-for="s in visibleSeries" :key="s.key">
            <polyline
              v-for="(segment, index) in s.segments"
              :key="index"
              :points="segment"
              fill="none"
              :stroke="s.color"
              stroke-width="2"
            />
            <circle
              v-for="(point, index) in s.points"
              :key="index"
              :cx="point.x"
              :cy="point.y"
              r="3.5"
              :fill="s.color"
            >
              <title>{{ point.day }} {{ s.name }}：{{ point.value }} 元</title>
            </circle>
          </g>
          <text
            v-for="(row, index) in items"
            :key="String(row.day)"
            :x="48 + (index * 544) / Math.max(1, items.length - 1)"
            y="248"
            text-anchor="middle"
          >
            {{ String(row.day).slice(5) }}
          </text>
        </svg>
      </section>
      <p v-if="!daily" class="muted">共 {{ items.length }} 条记录</p>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th v-for="column in columns" :key="column.key" scope="col">
                {{ column.label }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="row in visibleRows"
              :key="String(daily ? row.day : row.channel)"
              :class="{ 'statistics-total': row.channelName === '总计' }"
            >
              <td v-for="column in columns" :key="column.key">
                {{ cell(row, column.key) }}
              </td>
            </tr>
            <tr v-if="!visibleRows.length">
              <td colspan="7" class="empty">暂无数据</td>
            </tr>
          </tbody>
          <tfoot v-if="merchant && summary">
            <tr class="statistics-total">
              <td v-for="column in columns" :key="column.key">
                {{ cell(summary, column.key) }}
              </td>
            </tr>
          </tfoot>
        </table>
      </div>
    </template>
  </section>
</template>
<style scoped>
.original-order-statistics {
  min-width: 0;
}
.original-order-statistics th,
.original-order-statistics td {
  white-space: nowrap;
}
.original-order-statistics th:not(:first-child),
.original-order-statistics td:not(:first-child) {
  text-align: right;
}
.statistics-total {
  font-weight: 600;
}
.original-statistics-trend h2 {
  font-size: 16px;
  margin: 18px 0 12px;
}
.original-statistics-trend svg {
  display: block;
  width: 100%;
  max-height: 320px;
}
.original-statistics-trend text {
  fill: var(--text-secondary, #606266);
  font-size: 12px;
}
.trend-legend {
  display: flex;
  gap: 18px;
  flex-wrap: wrap;
  list-style: none;
  padding: 0;
}
.trend-legend li {
  display: flex;
  gap: 6px;
  align-items: center;
  font-size: 13px;
}
.trend-legend button {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px;
  border: 0;
  background: transparent;
  color: inherit;
  box-shadow: none;
}
.trend-legend button[aria-pressed="false"] {
  opacity: 0.45;
}
.trend-legend span {
  width: 12px;
  height: 3px;
  display: inline-block;
}
</style>
