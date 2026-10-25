<script setup lang="ts">
import { computed } from "vue";
import { displayValue } from "../api";
import type { ScanRecoveryView } from "../scan-recovery";

const props = defineProps<{ recovery?: ScanRecoveryView | null }>();
const states: Record<string, [string, string]> = {
  submitted: ["已提交，等待确认", "付款码已提交，请等待原订单结果。"],
  waiting: ["等待支付结果", "正在查询原订单，请勿再次提交付款码。"],
  query_unresolved: [
    "查询结果待核实",
    "请查询原订单核对最终结果，勿重复收款。",
  ],
  stopped: ["自动查询已结束", "本次自动查询已结束，可查询原订单核对最终结果。"],
  completed: ["已核实支付", "已核实原订单支付结果。"],
  cancel_pending: [
    "撤销结果待核实",
    "原订单撤销结果待核实，确认前不会入账。请核对原通道记录，勿重复收款。",
  ],
  cancelled: ["已撤销", "原订单已撤销，商户余额未增加。"],
  expired: ["支付结果待核实", "自动处理时间已结束，请查询原订单核对最终结果。"],
};
const status = computed(
  () =>
    states[props.recovery?.state ?? ""] ?? [
      "处理状态待核实",
      "处理状态待核实，请查询原订单。",
    ],
);
</script>

<template>
  <section v-if="recovery" aria-label="付款码支付处理">
    <h3>付款码支付处理</h3>
    <dl class="record-detail">
      <dt>处理状态</dt>
      <dd>{{ status[0] }}</dd>
      <dt>已查询次数</dt>
      <dd>{{ recovery.queries }} / {{ recovery.maximum_queries }}</dd>
      <template v-if="recovery.last_provider_state">
        <dt>最近通道状态</dt>
        <dd>{{ recovery.last_provider_state }}</dd>
      </template>
      <template v-if="recovery.next_query_at">
        <dt>下次查询时间</dt>
        <dd>{{ displayValue(recovery.next_query_at) }}</dd>
      </template>
    </dl>
    <p class="alert" role="status">{{ status[1] }}</p>
  </section>
</template>
