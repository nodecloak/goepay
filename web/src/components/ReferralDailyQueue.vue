<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount } from "vue";
import { ApiClient } from "../api";
import {
  referralID,
  referralAmount,
  readReferralDailyQueue,
  type ReferralDailyQueueItem,
} from "../referral";
import { beijingTimestamp } from "../console-datetime";
const props = defineProps<{
  client: ApiClient;
  available?: boolean;
  promoterId: string;
}>();
const items = ref<ReferralDailyQueueItem[]>([]),
  cursor = ref("0"),
  next = ref(""),
  loading = ref(false),
  error = ref("");
const labels = {
  pending: "等待按日发放",
  awarded: "已发放",
  blocked: "退款结果待核实",
  voided: "已撤销待发奖励",
};
let epoch = 0,
  active = true,
  controller: AbortController | undefined;
function reset() {
  epoch++;
  controller?.abort();
  controller = undefined;
  items.value = [];
  cursor.value = "0";
  next.value = "";
  loading.value = false;
  error.value = "";
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.promoterId,
    props.available,
  ],
  reset,
  { flush: "sync" },
);
async function load(after = "0") {
  if (
    !active ||
    loading.value ||
    props.available !== true ||
    props.client.area !== "merchant" ||
    !referralID(props.promoterId) ||
    !referralID(after, true)
  )
    return;
  controller?.abort();
  const c = new AbortController(),
    version = epoch,
    client = props.client,
    owner = props.promoterId;
  controller = c;
  loading.value = true;
  items.value = [];
  next.value = "";
  error.value = "";
  try {
    const raw = await client.request<unknown>(
      "/referral/daily-queue?" +
        new URLSearchParams({ after_id: after, limit: "20" }),
      { signal: c.signal },
    );
    if (
      !active ||
      c.signal.aborted ||
      version !== epoch ||
      props.available !== true
    )
      return;
    const page = readReferralDailyQueue(raw, owner, after);
    items.value = page.items;
    next.value = page.next_cursor;
    cursor.value = after;
  } catch (e) {
    if (active && !c.signal.aborted && version === epoch)
      error.value = e instanceof Error ? e.message : "待发奖励读取失败";
  } finally {
    if (active && version === epoch && controller === c) loading.value = false;
  }
}
onMounted(() => load());
onBeforeUnmount(() => {
  active = false;
  reset();
});
</script>
<template>
  <section v-if="available === true" aria-label="每日待发奖励">
    <h3>每日待发奖励</h3>
    <p>
      按每笔原订单固定的手续费或订单金额基数计算，奖励不超过原手续费。次日北京时间起等待专用调度器按日发放；这里不表示调度器已启动，也不代表当前可提现余额。不按迟到汇总或当前用户组重算。
    </p>
    <p>
      退款结果未知时暂存；退款失败恢复原待发额；首次成功退款撤销整笔原奖励。下表待发额是当前该原单待发账户金额，退款暂存时可为零，不表示已发放。
    </p>
    <button :disabled="loading" @click="load()">刷新待发奖励首页</button>
    <p v-if="loading" role="status">正在核对原单待发奖励…</p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>原订单号 / ID</th>
            <th>原付款商户</th>
            <th>原订单金额（元）</th>
            <th>原手续费上限（元）</th>
            <th>固定奖励（元）</th>
            <th>当前待发额（元）</th>
            <th>状态</th>
            <th>激活时间（北京时间）</th>
            <th>原业务日</th>
            <th>最早发放日（北京时间）</th>
            <th>来源</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in items" :key="row.order_id">
            <td>
              {{ row.trade_no }}<small>{{ row.order_id }}</small>
            </td>
            <td>{{ row.merchant_id }}</td>
            <td>{{ referralAmount(row.amount) }}</td>
            <td>{{ referralAmount(row.platform_fee) }}</td>
            <td>{{ referralAmount(row.reward_amount) }}</td>
            <td>{{ referralAmount(row.pending_amount) }}</td>
            <td>{{ labels[row.state] }}</td>
            <td>{{ beijingTimestamp(row.activated_at) }}</td>
            <td>{{ row.business_day }}</td>
            <td>{{ row.eligible_day }}</td>
            <td>原条款已验证</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="!loading && !error && !items.length">暂无每日待发奖励记录</p>
    <button :disabled="loading || cursor === '0'" @click="load()">
      待发奖励首页</button
    ><button :disabled="loading || !next" @click="load(next)">
      下一页待发奖励
    </button>
  </section>
</template>
<style scoped>
section {
  min-width: 0;
}
p {
  overflow-wrap: anywhere;
}
.table-scroll {
  max-width: 100%;
  overflow: auto;
}
table {
  min-width: 1100px;
}
small {
  display: block;
}
button {
  min-height: var(--control-h);
  margin: 0.3rem 0.5rem 0.3rem 0;
}
</style>
