<script setup lang="ts">
import type { ListFilters } from "../list-filters";
import type { Area } from "../menu";
defineProps<{ kind: string; area: Area }>();
const filters = defineModel<ListFilters>({ required: true });
defineEmits<{ reset: [] }>();
</script>
<template>
  <details class="list-filters">
    <summary>更多筛选</summary>
    <div class="list-filter-fields">
      <label v-if="kind === 'orders'"
        >通知状态筛选<select
          v-model="filters.notification_state"
          aria-label="通知状态筛选"
        >
          <option value="">全部通知状态</option>
          <option value="none">尚无通知任务</option>
          <option value="pending">等待通知</option>
          <option value="running">正在通知</option>
          <option value="succeeded">通知成功</option>
          <option value="dead">通知失败</option>
        </select></label
      >
      <label v-if="kind === 'orders'"
        >订单收入状态<select
          v-model="filters.income_status"
          aria-label="订单收入状态"
        >
          <option value="">全部收入状态</option>
          <option value="none">未冻结</option>
          <option value="held">收入冻结中</option>
          <option value="released">收入已解冻</option>
        </select></label
      >
      <label v-if="kind === 'orders' || kind === 'refunds'"
        >状态筛选
        <select v-model="filters.status" aria-label="状态筛选">
          <option value="">全部状态</option>
          <template v-if="kind === 'orders'">
            <option value="created">已创建</option>
            <option value="submitting">提交中</option>
            <option value="pending">待支付</option>
            <option value="paid">已支付</option>
            <option value="closed">已关闭</option>
          </template>
          <template v-else
            ><option value="pending">待处理</option>
            <option value="succeeded">退款成功</option></template
          >
          <option value="unknown">结果待核对</option>
          <option value="failed">明确失败</option>
        </select>
      </label>
      <label v-if="kind === 'orders' || kind === 'refunds'"
        >支付方式筛选
        <select v-model="filters.type" aria-label="支付方式筛选">
          <option value="">全部方式</option>
          <option value="alipay">支付宝</option>
          <option value="wxpay">微信支付</option>
          <option value="sandbox">本地模拟</option>
        </select>
      </label>
      <label v-if="area === 'admin'"
        >商户 ID 筛选<input
          v-model="filters.merchant_id"
          inputmode="numeric"
          pattern="[1-9][0-9]*"
      /></label>
      <label v-if="kind === 'orders' || kind === 'refunds'"
        >通道 ID 筛选<input
          v-model="filters.channel_id"
          inputmode="numeric"
          pattern="[1-9][0-9]*"
      /></label>
      <label>创建开始日期<input v-model="filters.from" type="date" /></label>
      <label>创建结束日期<input v-model="filters.to" type="date" /></label>
      <label v-if="kind === 'orders'"
        >买家标识筛选<input v-model="filters.buyer" maxlength="256"
      /></label>
      <label v-if="kind === 'orders'"
        >客户 IP 筛选<input v-model="filters.client_ip" maxlength="45"
      /></label>
    </div>
    <p class="muted">
      日期按北京时间（UTC+8），包含开始和结束当天；状态以服务端已验证结果为准。
    </p>
    <button type="button" @click="$emit('reset')">清空筛选</button>
  </details>
</template>
