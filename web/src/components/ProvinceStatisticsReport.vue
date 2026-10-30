<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ApiClient } from "../api";
import {
  provinceStatisticsQuery,
  provinceStatisticsData,
  provincePaths,
  provinceMoney,
  provinceCount,
  provinceMapPalette,
  provinceEmptyColour,
  type ProvinceRegion,
} from "../province-statistics";
import CsvExportButton from "./CsvExportButton.vue";
import ShellIcon from "./ShellIcon.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
const props = defineProps<{ client: ApiClient }>(),
  preset = ref("today"),
  uid = ref(""),
  channel = ref("");
const ranges = [
  { key: "today", label: "今天" },
  { key: "yesterday", label: "昨天" },
  { key: "3days", label: "近3天" },
  { key: "7days", label: "近7天" },
  { key: "15days", label: "近15天" },
  { key: "30days", label: "近30天" },
];
const mapPalette = provinceMapPalette;
const data = ref<ReturnType<typeof provinceStatisticsData> | null>(null),
  totalMoney = ref(""),
  totalCount = ref(""),
  error = ref(""),
  loading = ref(false),
  selected = ref<ProvinceRegion | null>(null),
  zoom = ref(1),
  pan = ref({ x: 0, y: 0 });
const paths = provincePaths(),
  regions = computed(
    () => new Map(data.value?.regions.map((r) => [r.name, r]) ?? []),
  ),
  admin = computed(() => props.client.area === "admin");
// Declutter the dark map: label only provinces that carry data (or the top
// ranks) plus whichever region is currently selected, so labels stop
// overlapping empty provinces.
const labelledProvinces = computed(() => {
  const names = new Set<string>();
  for (const region of data.value?.rank ?? []) names.add(region.name);
  for (const region of data.value?.regions ?? [])
    if (region.count !== "0") names.add(region.name);
  if (selected.value) names.add(selected.value.name);
  return names;
});
let controller: AbortController | undefined,
  alive = true,
  drag: {
    id: number;
    x: number;
    y: number;
    sx: number;
    sy: number;
    scale: number;
  } | null = null,
  dragged = false;
function clear() {
  controller?.abort();
  data.value = null;
  totalMoney.value = "";
  totalCount.value = "";
  error.value = "";
  loading.value = false;
  selected.value = null;
  zoom.value = 1;
  pan.value = { x: 0, y: 0 };
  drag = null;
}
watch([preset, uid, channel], clear, { flush: "sync" });
const csvPath = computed(() => {
  const parsed = provinceStatisticsQuery(
    props.client.area,
    preset.value,
    uid.value,
    channel.value,
  );
  if (!parsed.query) return "";
  parsed.query.set("format", "csv");
  return "/reports/provinces?" + parsed.query;
});
async function load() {
  clear();
  const parsed = provinceStatisticsQuery(
    props.client.area,
    preset.value,
    uid.value,
    channel.value,
  );
  if (!parsed.query) {
    error.value = parsed.error ?? "筛选不正确";
    return;
  }
  controller = new AbortController();
  const c = controller,
    client = props.client,
    csrf = client.csrfToken;
  loading.value = true;
  try {
    const response = await client.request<{
      items: Record<string, unknown>[];
      total_money: string;
      total_count: string;
    }>("/reports/provinces?" + parsed.query, { signal: c.signal });
    const parsedData = provinceStatisticsData(response.items),
      money = provinceMoney(response.total_money),
      count = provinceCount(response.total_count);
    if (
      alive &&
      !c.signal.aborted &&
      props.client === client &&
      client.csrfToken === csrf
    ) {
      data.value = parsedData;
      totalMoney.value = money;
      totalCount.value = count;
    }
  } catch (e) {
    if (alive && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "地区统计读取失败";
  } finally {
    if (alive && !c.signal.aborted) loading.value = false;
  }
}
function changeRange(value: string) {
  preset.value = value;
  void load();
}
function reset() {
  uid.value = "";
  channel.value = "";
  preset.value = "today";
  void load();
}
watch(() => [props.client, props.client.csrfToken], reset);
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  clear();
});
const transform = computed(
  () =>
    `translate(${360 * (1 - zoom.value) + pan.value.x} ${270 * (1 - zoom.value) + pan.value.y}) scale(${zoom.value})`,
);
function zoomBy(delta: number) {
  zoom.value = Math.max(1, Math.min(3, zoom.value + delta));
  if (zoom.value === 1) pan.value = { x: 0, y: 0 };
}
function startDrag(e: PointerEvent) {
  dragged = false;
  if (zoom.value === 1 || e.button !== 0) return;
  const svg = e.currentTarget as SVGSVGElement;
  drag = {
    id: e.pointerId,
    x: e.clientX,
    y: e.clientY,
    sx: pan.value.x,
    sy: pan.value.y,
    scale: 720 / svg.getBoundingClientRect().width,
  };
  svg.setPointerCapture(e.pointerId);
}
function moveDrag(e: PointerEvent) {
  if (!drag || drag.id !== e.pointerId) return;
  const dx = e.clientX - drag.x,
    dy = e.clientY - drag.y;
  dragged = dragged || Math.abs(dx) + Math.abs(dy) > 4;
  pan.value = {
    x: Math.max(-720, Math.min(720, drag.sx + dx * drag.scale)),
    y: Math.max(-540, Math.min(540, drag.sy + dy * drag.scale)),
  };
}
function stopDrag(e: PointerEvent) {
  if (drag?.id === e.pointerId) {
    const svg = e.currentTarget as SVGSVGElement;
    if (svg.hasPointerCapture(e.pointerId))
      svg.releasePointerCapture(e.pointerId);
    drag = null;
  }
}
function choose(name: string, click = false) {
  if (click && dragged) {
    dragged = false;
    return;
  }
  selected.value = regions.value.get(name) ?? null;
}
</script>
<template>
  <div class="list-page province-statistics">
    <form
      class="toolbar list-filter-card panel province-filter"
      @submit.prevent="load"
    >
      <div class="pa-filter-fields list-filters-grid">
        <div class="pa-field province-range-field">
          <span class="pa-label">统计日期范围</span>
          <div class="province-ranges" role="group" aria-label="统计日期范围">
            <button
              v-for="range in ranges"
              :key="range.key"
              type="button"
              :aria-pressed="preset === range.key"
              :disabled="loading"
              @click="changeRange(range.key)"
            >
              {{ range.label }}
            </button>
          </div>
        </div>
        <KitField v-if="admin" label="商户号">
          <KitInput
            v-model="uid"
            aria-label="商户号"
            placeholder="商户号"
            inputmode="numeric"
            :maxlength="19"
          />
        </KitField>
        <KitField v-if="admin" label="通道ID">
          <KitInput
            v-model="channel"
            aria-label="通道ID"
            placeholder="通道ID"
            inputmode="numeric"
            :maxlength="19"
          />
        </KitField>
      </div>
      <div class="pa-filter-actions">
        <button type="button" class="pa-btn" :disabled="loading" @click="reset">
          重置
        </button>
        <button
          v-if="admin"
          type="submit"
          class="pa-btn pa-btn--primary"
          :class="{ 'pa-btn--loading': loading }"
          :disabled="loading"
        >
          查询
        </button>
      </div>
    </form>
    <section class="panel records">
      <div class="table-tools">
        <CsvExportButton
          :client="client"
          :path="csvPath"
          filename="province-statistics.csv"
          :disabled="loading || !data || !!error"
        />
        <div v-if="data && !error" class="province-total filter-total">
          <span>累计交易金额</span
          ><strong
            >¥<span>{{ totalMoney }}</span></strong
          ><span
            >（<span>{{ totalCount }}</span> 笔）</span
          >
        </div>
        <div class="table-tools-end">
          <button
            type="button"
            class="icon-button table-refresh"
            :aria-label="loading ? '正在更新…' : '刷新数据'"
            :title="loading ? '正在更新…' : '刷新数据'"
            :disabled="loading"
            @click="load"
          >
            <ShellIcon name="refresh" />
          </button>
        </div>
      </div>
      <p class="muted province-note">
        按北京时间订单创建日期统计已支付金额与笔数。地区来自已记录资料，未记录或未匹配的地区单独保留，不根据支付IP猜测。
      </p>
      <div v-if="error" class="alert error province-line" role="alert">
        {{ error }}
      </div>
      <p v-if="loading" role="status" class="province-line">
        正在读取地区统计…
      </p>
      <template v-else-if="data && !error">
        <p class="muted province-line">
          未匹配地区（含未知）：{{ data.unmappedMoney }} 元，{{
            data.unmappedCount
          }}
          笔
        </p>
        <div class="province-layout">
          <section class="province-map">
            <div class="map-tools">
              <button
                type="button"
                aria-label="缩小地图"
                :disabled="zoom <= 1"
                @click="zoomBy(-0.25)"
              >
                −</button
              ><span aria-label="地图缩放比例"
                >{{ Math.round(zoom * 100) }}%</span
              ><button
                type="button"
                aria-label="放大地图"
                :disabled="zoom >= 3"
                @click="zoomBy(0.25)"
              >
                ＋</button
              ><button
                type="button"
                aria-label="复位地图"
                @click="
                  zoom = 1;
                  pan = { x: 0, y: 0 };
                "
              >
                复位
              </button>
            </div>
            <svg
              viewBox="0 0 720 540"
              role="group"
              aria-label="地区交易分布地图"
              :style="{ touchAction: zoom > 1 ? 'none' : 'pan-y' }"
              @pointerdown="startDrag"
              @pointermove="moveDrag"
              @pointerup="stopDrag"
              @pointercancel="stopDrag"
            >
              <g :transform="transform">
                <path
                  v-for="boundary in paths"
                  :key="boundary.name"
                  :d="boundary.path"
                  :style="{
                    fill:
                      regions.get(boundary.name)?.colour || provinceEmptyColour,
                  }"
                  fill-rule="evenodd"
                  role="button"
                  tabindex="0"
                  :aria-label="
                    boundary.name +
                    '：交易金额' +
                    (regions.get(boundary.name)?.money ?? '0.00') +
                    '元，交易笔数' +
                    (regions.get(boundary.name)?.count ?? '0')
                  "
                  :aria-pressed="selected?.name === boundary.name"
                  @click="choose(boundary.name, true)"
                  @pointerenter="choose(boundary.name)"
                  @focus="choose(boundary.name)"
                  @keydown.enter.prevent="choose(boundary.name)"
                  @keydown.space.prevent="choose(boundary.name)"
                >
                  <title>
                    {{ boundary.name }}：{{
                      regions.get(boundary.name)?.money
                    }}
                    元，{{ regions.get(boundary.name)?.count }} 笔
                  </title>
                </path>
                <template
                  v-for="boundary in paths"
                  :key="'label-' + boundary.name"
                >
                  <text
                    v-if="
                      boundary.label && labelledProvinces.has(boundary.name)
                    "
                    :x="boundary.label.x"
                    :y="boundary.label.y"
                    class="province-label"
                    aria-hidden="true"
                  >
                    {{ boundary.name }}
                  </text>
                </template>
              </g>
            </svg>
            <p
              v-if="selected"
              role="status"
              aria-label="选中地区"
              class="selected-province"
            >
              {{ selected.name }}：¥{{ selected.money }}，{{ selected.count }}
              笔
            </p>
            <p v-else class="muted">
              悬停或使用键盘选择地区查看明细；放大后可拖动地图。
            </p>
            <div class="map-scale" aria-label="交易金额色阶">
              <span>低</span
              ><i
                v-for="colour in mapPalette"
                :key="colour"
                :style="{ background: colour }"
              /><span>高</span>
            </div>
          </section>
          <section class="province-ranking panel">
            <h2>Top 10 省份</h2>
            <div v-if="!data.rank.length" class="province-ranking-empty">
              <ShellIcon name="chart" />
              <p class="empty">暂无已记录地区的数据</p>
            </div>
            <ol v-else>
              <li v-for="(region, index) in data.rank" :key="region.name">
                <span
                  class="province-rank"
                  :class="{ 'rank-leading': index < 3 }"
                  >{{ index + 1 }}</span
                ><button
                  type="button"
                  class="link-button"
                  @click="selected = region"
                >
                  {{ region.name }}</button
                ><strong>¥{{ region.money }}</strong
                ><span>{{ region.count }}笔</span>
              </li>
            </ol>
          </section>
        </div>
        <details>
          <summary>地区数据明细</summary>
          <div class="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>地区</th>
                  <th>交易金额（元）</th>
                  <th>交易笔数</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="region in [
                    ...data.regions.filter((r) => r.count !== '0'),
                    ...data.unmapped,
                  ]"
                  :key="region.name"
                >
                  <td>{{ region.name }}</td>
                  <td>{{ region.money }}</td>
                  <td>{{ region.count }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </details>
      </template>
    </section>
  </div>
</template>
<style scoped>
.province-statistics {
  min-width: 0;
}
.province-filter.list-filter-card {
  padding: var(--space-4) var(--space-5);
}
/* The date-range chips are one field; let it take the room several chips need
   instead of the default 220px field width. */
.province-range-field {
  flex: 1 1 340px;
}
.province-note {
  margin: 0 var(--space-5) var(--space-3);
  font-size: 12px;
  line-height: 1.7;
}
/* Body lines, the map/Top 10 row and the 明细 fold keep the same side
   padding as the toolbar above (the card itself has none). */
.province-line {
  margin-inline: var(--space-5);
}
.province-ranges {
  display: flex;
  flex-wrap: wrap;
  gap: 3px;
}
.province-ranges button {
  padding: 6px 10px;
}
.province-ranges button[aria-pressed="true"] {
  color: var(--on-primary);
  background: var(--primary);
  border-color: var(--primary-text);
}
.province-total {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  align-items: baseline;
  margin: 18px 0;
}
.province-total.filter-total {
  margin: 0;
}
.province-total strong {
  color: var(--primary-text);
  font-size: 24px;
  overflow-wrap: anywhere;
}
.province-total.filter-total strong {
  font-size: 20px;
}
.province-layout {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(240px, 1fr);
  gap: 20px;
  padding-inline: var(--space-5);
}
.province-map {
  min-width: 0;
}
.province-map svg {
  display: block;
  width: 100%;
  max-height: 600px;
  overflow: hidden;
  background: color-mix(in srgb, var(--bg-raised) 60%, var(--bg-card));
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
}
.province-map path {
  stroke: var(--ink-muted);
  stroke-width: 0.9;
  cursor: pointer;
  vector-effect: non-scaling-stroke;
  /* Lift every province above the near-black background so empty regions are
     still distinguishable on the dark theme; data fills still show through. */
  fill-opacity: 0.92;
}
.province-label {
  pointer-events: none;
  text-anchor: middle;
  dominant-baseline: middle;
  font-size: 10px;
  fill: var(--ink);
  paint-order: stroke;
  stroke: var(--bg-card);
  stroke-width: 2px;
  stroke-linejoin: round;
}
.province-map path:hover,
.province-map path:focus,
.province-map path[aria-pressed="true"] {
  fill: var(--warning-text);
  stroke: var(--warning-text);
  stroke-width: 2;
  outline: none;
}
.map-tools {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}
.map-tools button {
  padding: 5px 10px;
}
.map-scale {
  display: flex;
  gap: 4px;
  align-items: center;
  justify-content: center;
  margin: 10px 0;
}
.map-scale i {
  display: block;
  width: 25px;
  height: 12px;
}
.selected-province {
  overflow-wrap: anywhere;
}
.province-ranking {
  padding: var(--space-4) var(--space-5);
  align-self: start;
}
.province-ranking h2 {
  font-size: 16px;
  margin: 0 0 var(--space-3);
}
.province-ranking-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  min-height: 180px;
  text-align: center;
  color: var(--ink-muted);
}
.province-ranking-empty .shell-icon {
  width: 32px;
  height: 32px;
  opacity: 0.5;
}
.province-ranking-empty .empty {
  margin: 0;
}
.province-ranking ol {
  list-style: none;
  margin: 0;
  padding: 0;
}
.province-ranking li {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr);
  gap: 6px 10px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border, var(--line));
  align-items: center;
}
.province-ranking li strong,
.province-ranking li > span:last-child {
  grid-column: 2;
  overflow-wrap: anywhere;
}
.province-rank {
  grid-row: span 3;
  background: var(--bg-raised);
  border-radius: 50%;
  text-align: center;
  line-height: 24px;
}
.rank-leading {
  background: var(--primary-soft);
  color: var(--primary-text);
}
.province-ranking .link-button {
  justify-self: start;
  text-align: left;
}
.province-statistics details {
  margin: 20px var(--space-5) var(--space-5);
}
/* The table inside spans the card, like every other records table; its
   first and last cells already carry the side padding. */
.province-statistics details .table-scroll {
  margin-inline: calc(-1 * var(--space-5));
}
@media (max-width: 760px) {
  .province-layout {
    grid-template-columns: minmax(0, 1fr);
  }
  .province-total strong {
    font-size: 20px;
  }
}
</style>
