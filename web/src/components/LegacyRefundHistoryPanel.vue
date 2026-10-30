<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { ApiClient } from "../api";
import { centsMoney } from "../settlement-terms";
const props = defineProps<{ client: ApiClient; ownSession: boolean }>();
interface HistoryRow {
  id: string;
  export_id: string;
  trade_no: string;
  refund_no: string;
  amount: string;
  legacy_status: string;
}
const merchantID = ref(""),
  rows = ref<HistoryRow[]>([]),
  cursor = ref(""),
  error = ref(""),
  busy = ref(false),
  loaded = ref(false);
const admin = computed(() => props.client.area === "admin");
let epoch = 0,
  controller: AbortController | undefined;
function clear() {
  epoch++;
  controller?.abort();
  rows.value = [];
  cursor.value = "";
  error.value = "";
  loaded.value = false;
  busy.value = false;
}
watch([merchantID, () => props.ownSession], clear);
onBeforeUnmount(clear);
function status(value: string) {
  return (
    (
      {
        unknown: "原系统未明确",
        succeeded: "原系统记录成功",
        failed: "原系统记录失败",
      } as Record<string, string>
    )[value] ?? "历史记录"
  );
}
async function load(next = false) {
  if (!props.ownSession || busy.value) return;
  if (admin.value && !/^[1-9][0-9]*$/.test(merchantID.value)) {
    error.value = "请输入明确的商户ID";
    return;
  }
  const current = ++epoch;
  controller?.abort();
  controller = new AbortController();
  busy.value = true;
  error.value = "";
  const query = new URLSearchParams({
    after_id: next ? cursor.value : "0",
    limit: "20",
  });
  if (admin.value) query.set("merchant_id", merchantID.value);
  try {
    const result = await props.client.request<{
      items: HistoryRow[];
      next_cursor: string;
    }>("/legacy-refund-history?" + query, { signal: controller.signal });
    if (current !== epoch) return;
    rows.value = next ? [...rows.value, ...result.items] : result.items;
    cursor.value = result.next_cursor;
    loaded.value = true;
  } catch {
    if (current === epoch) {
      rows.value = [];
      cursor.value = "";
      loaded.value = false;
      error.value = "历史来源读取失败，请核对后重新查询";
    }
  } finally {
    if (current === epoch) busy.value = false;
  }
}
</script>
<template>
  <details class="panel">
    <summary>查看导入退款历史（只读）</summary>
    <p class="muted">
      这些是原系统导出的历史记录，不作为已核验的支付或退款结果。原未结义务继续保留在导入预留余额中；此处不能发送退款或释放资金。
    </p>
    <p v-if="!ownSession" class="alert">请使用本人会话读取历史。</p>
    <form v-else @submit.prevent="load()">
      <label v-if="admin"
        >历史所属商户ID<input
          v-model="merchantID"
          inputmode="numeric"
          maxlength="19"
          required
      /></label>
      <button :disabled="busy">{{ busy ? "读取中…" : "查询历史记录" }}</button>
    </form>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="loaded && !rows.length">没有导入退款历史记录。</p>
    <div v-if="rows.length" class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>历史退款号</th>
            <th>历史订单号</th>
            <th>金额（元）</th>
            <th>原记录状态</th>
            <th>导入批次</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id">
            <td class="pa-order-no">{{ row.refund_no }}</td>
            <td class="pa-order-no">{{ row.trade_no }}</td>
            <td>{{ centsMoney(row.amount) }}</td>
            <td>{{ status(row.legacy_status) }}</td>
            <td>{{ row.export_id }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <button v-if="cursor" :disabled="busy" @click="load(true)">
      读取下一页历史
    </button>
  </details>
</template>
<style scoped>
.table-scroll {
  max-width: 100%;
  overflow-x: auto;
}
</style>
