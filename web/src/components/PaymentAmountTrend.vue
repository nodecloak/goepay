<script setup lang="ts">
import { computed, ref } from "vue";
import type { PaymentStatisticsDay } from "../payment-statistics";
import { paymentTrendSeries } from "../merchant-payment-statistics";
const props = defineProps<{ days: PaymentStatisticsDay[] }>();
const series = computed(() => paymentTrendSeries(props.days)),
  hidden = ref<string[]>([]),
  active = ref("");
const colors = ["var(--chart-1)", "var(--chart-2)", "var(--chart-3)"];
function toggle(type: string) {
  hidden.value = hidden.value.includes(type)
    ? hidden.value.filter((t) => t !== type)
    : [...hidden.value, type];
  active.value = "";
}
</script>
<template>
  <figure class="payment-amount-trend">
    <figcaption>近7日支付金额</figcaption>
    <div class="trend-legend">
      <button
        v-for="(s, index) in series"
        :key="s.type"
        type="button"
        :aria-label="s.name + '趋势'"
        :aria-pressed="!hidden.includes(s.type)"
        @click="toggle(s.type)"
      >
        <span :style="{ backgroundColor: colors[index] }" />{{ s.name }}
      </button>
    </div>
    <svg viewBox="0 0 700 290" role="group" aria-label="近7日支付金额趋势">
      <line
        v-for="y in [30, 140, 250]"
        :key="y"
        x1="50"
        x2="650"
        :y1="y"
        :y2="y"
        style="stroke: var(--chart-grid)"
        stroke-dasharray="3 3"
      />
      <template v-for="(s, index) in series" :key="s.type">
        <g v-if="!hidden.includes(s.type)">
          <polyline
            :points="s.points.map((p) => p.x + ',' + p.y).join(' ')"
            fill="none"
            :style="{ stroke: colors[index] }"
            stroke-width="2"
          />
          <circle
            v-for="(p, pointIndex) in s.points"
            :key="p.date"
            :cx="p.x"
            :cy="p.y"
            :r="pointIndex === s.points.length - 1 ? 4 : 7"
            :class="{
              'trend-last-point': pointIndex === s.points.length - 1,
              'trend-hit-point': pointIndex !== s.points.length - 1,
            }"
            :style="
              pointIndex === s.points.length - 1 ? { fill: colors[index] } : {}
            "
            :tabindex="hidden.includes(s.type) ? -1 : 0"
            :aria-label="p.date + ' ' + s.name + ' ¥' + p.money"
            @focus="active = p.date + ' ' + s.name + ' ¥' + p.money"
            @mouseenter="active = p.date + ' ' + s.name + ' ¥' + p.money"
          >
            <title>{{ p.date }} {{ s.name }} ¥{{ p.money }}</title>
          </circle>
        </g>
      </template>
      <text
        v-for="(d, index) in days"
        :key="d.date"
        :x="50 + index * 100"
        y="280"
        text-anchor="middle"
      >
        {{ d.date.slice(5) }}
      </text>
    </svg>
    <output role="status">{{ active || "选择图上的日期点查看金额" }}</output>
  </figure>
</template>
<style scoped>
.payment-amount-trend {
  margin: 16px 0;
  min-width: 0;
}
.payment-amount-trend svg {
  display: block;
  width: 100%;
  max-height: 320px;
}
.payment-amount-trend text {
  font-size: 12px;
  fill: var(--muted, var(--ink-muted));
}
.payment-amount-trend figcaption {
  font-weight: 600;
  margin-bottom: 8px;
}
.trend-legend {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.trend-legend button {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  padding: 4px 8px;
}
.trend-legend span {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
}
.trend-legend [aria-pressed="false"] {
  opacity: 0.6;
}
.trend-last-point {
  stroke: var(--bg-card);
  stroke-width: 2;
}
.trend-hit-point {
  fill: transparent;
}
.payment-amount-trend circle:focus {
  outline: none;
}
.payment-amount-trend circle:focus-visible {
  outline: 2px solid var(--primary-text);
  outline-offset: 2px;
}
.payment-amount-trend output {
  display: block;
  min-height: 24px;
  font-size: 12px;
  overflow-wrap: anywhere;
}
</style>
