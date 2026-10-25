<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from "vue";
import { ApiClient, displayValue } from "../api";
import {
  buildBatchItems,
  emptyBatchRow,
  type BatchDraftRow,
  type BatchPolicy,
  type TransferBatch,
} from "../transfer-batches";
import BatchRecipientEditor from "./BatchRecipientEditor.vue";
import TransferBatchDetails from "./TransferBatchDetails.vue";
import TransferBatchImport from "./TransferBatchImport.vue";
const props = defineProps<{ client: ApiClient }>();
const merchant = ref(""),
  external = ref(""),
  items = ref<TransferBatch[]>([]),
  before = ref("0"),
  next = ref("0"),
  total = ref(0),
  selected = ref("");
const policy = ref<BatchPolicy | null>(null),
  writes = ref(false),
  loading = ref(false),
  busy = ref(false),
  creating = ref(false),
  confirming = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref("");
const rows = ref<BatchDraftRow[]>([emptyBatchRow()]);
let controller: AbortController | undefined;
let frozen: Record<string, unknown> | undefined;
const ownerQuery = (): Record<string, string> =>
  props.client.area === "admin" ? { merchant_id: merchant.value } : {};
async function load() {
  if (props.client.area === "admin" && !/^[1-9]\d*$/.test(merchant.value)) {
    error.value = "请输入付款商户 ID";
    return;
  }
  controller?.abort();
  const c = new AbortController();
  controller = c;
  loading.value = true;
  error.value = "";
  policy.value = null;
  writes.value = false;
  try {
    const q = new URLSearchParams({
      ...ownerQuery(),
      limit: "20",
      before_id: before.value,
    });
    if (external.value) q.set("external_no", external.value);
    const data = await props.client.request<{
      items: TransferBatch[];
      total: number;
      next_before_id: string;
    }>("/transfer-batches?" + q, { signal: c.signal });
    if (c.signal.aborted) return;
    items.value = data.items;
    total.value = data.total;
    next.value = data.next_before_id;
    const caps = await props.client.request<
      BatchPolicy & { writes_enabled: boolean }
    >("/transfers/capabilities?" + new URLSearchParams(ownerQuery()), {
      signal: c.signal,
    });
    if (!c.signal.aborted) {
      policy.value = caps;
      writes.value = caps.writes_enabled === true;
    }
  } catch (e) {
    if (!c.signal.aborted)
      error.value = e instanceof Error ? e.message : "批次读取失败";
  } finally {
    if (controller === c) loading.value = false;
  }
}
function start() {
  rows.value = [emptyBatchRow()];
  external.value = crypto.randomUUID();
  creating.value = true;
  confirming.value = false;
  uncertain.value = false;
  frozen = undefined;
  error.value = "";
  notice.value = "";
}
function review() {
  try {
    if (!policy.value || !writes.value) throw new Error("付款能力未启用");
    frozen = {
      ...ownerQuery(),
      external_no: external.value,
      items: buildBatchItems(rows.value, policy.value),
    };
    confirming.value = true;
    error.value = "";
  } catch (e) {
    error.value = e instanceof Error ? e.message : "请核对付款项目";
  }
}
async function prepare() {
  if (busy.value || !frozen) return;
  busy.value = true;
  confirming.value = false;
  error.value = "";
  try {
    const data = await props.client.request<TransferBatch>(
      "/transfer-batches",
      { method: "POST", body: frozen },
    );
    selected.value = data.id;
    creating.value = false;
    rows.value = [emptyBatchRow()];
    frozen = undefined;
    notice.value = "整批已预留，尚未发送付款。请核对详情后另行确认执行。";
  } catch (e) {
    uncertain.value = true;
    rows.value = [emptyBatchRow()];
    frozen = undefined;
    notice.value =
      "预留结果未确认。请按原请求号查找，勿创建替代批次；不会自动重新提交。";
    error.value = e instanceof Error ? e.message : "预留结果未知";
  } finally {
    busy.value = false;
  }
}
function discard() {
  rows.value = [emptyBatchRow()];
  frozen = undefined;
  creating.value = false;
  uncertain.value = false;
  confirming.value = false;
}
onMounted(() => {
  if (props.client.area === "merchant") load();
});
onBeforeUnmount(() => {
  controller?.abort();
  rows.value = [];
  frozen = undefined;
});
</script>
<template>
  <TransferBatchDetails
    v-if="selected"
    :key="selected"
    :client="client"
    :batch-id="selected"
    @close="
      selected = '';
      load();
    "
  />
  <section v-else class="panel records" aria-label="持久批量付款">
    <h2>批量付款</h2>
    <p class="muted">
      先整批预留，再单独执行。每批 1–100
      项；未知或已发送项目不重发，取消仅释放可证明尚未发送的项目。此功能不等同自动结算。
    </p>
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <p v-if="notice" role="status">{{ notice }}</p>
    <form
      class="toolbar"
      @submit.prevent="
        before = '0';
        load();
      "
    >
      <label v-if="client.area === 'admin'"
        >付款商户 ID<input
          v-model="merchant"
          :disabled="busy || creating"
          inputmode="numeric"
      /></label>
      <label
        >批次请求号<input
          v-model="external"
          :disabled="busy || creating"
          autocomplete="off"
          maxlength="128"
      /></label>
      <button :disabled="busy || loading">按原请求号查找</button>
      <button
        type="button"
        :disabled="busy || loading || !writes || creating"
        @click="start"
      >
        新建批量付款
      </button>
    </form>
    <section v-if="creating && !uncertain" aria-label="新批次">
      <p>
        本次原请求号：<code>{{ external }}</code
        >。仅保存在当前页面，请记录用于恢复。
      </p>
      <BatchRecipientEditor
        v-if="policy"
        :rows="rows"
        :policy="policy"
        :disabled="busy || confirming"
        @change="rows = $event"
      />
      <TransferBatchImport
        v-if="policy"
        :client="client"
        :policy="policy"
        :merchant-i-d="merchant"
        :disabled="busy || confirming"
        @apply="rows = $event"
      />
      <button :disabled="busy || confirming || !writes" @click="review">
        核对并预留整批资金
      </button>
      <div v-if="confirming" class="alert">
        <p>
          确认商户 {{ client.area === "admin" ? merchant : "当前商户" }}，共
          {{ rows.length }}
          项。将一次性预留所有本金及服务费，任何一项无效则整批回滚；本步骤不发送付款。
        </p>
        <button
          :disabled="busy"
          @click="
            confirming = false;
            frozen = undefined;
          "
        >
          返回编辑</button
        ><button :disabled="busy" @click="prepare">确认预留，不发送付款</button>
      </div>
      <button :disabled="busy" @click="discard">取消编辑并清除收款资料</button>
    </section>
    <p v-if="uncertain" class="alert">
      请先按上方原请求号查找已保存批次，再核对详情；收款资料已从当前表单清除。
    </p>
    <div v-if="items.length" class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>批次 ID</th>
            <th>原请求号</th>
            <th>本金（分）</th>
            <th>预占（分）</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id">
            <td>{{ item.id }}</td>
            <td>{{ item.external_no }}</td>
            <td>{{ item.amount }}</td>
            <td>{{ item.cost_amount }}</td>
            <td>{{ displayValue(item.summary) }}</td>
            <td>
              <button
                :disabled="busy"
                @click="
                  selected = item.id;
                  creating = false;
                  uncertain = false;
                "
              >
                查看批次 {{ item.id }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else-if="!loading">暂无匹配批次</p>
    <p>共 {{ total }} 批</p>
    <button
      v-if="before !== '0'"
      :disabled="busy || loading"
      @click="
        before = '0';
        load();
      "
    >
      返回最新批次</button
    ><button
      v-if="next !== '0'"
      :disabled="busy || loading"
      @click="
        before = next;
        load();
      "
    >
      下一页批次
    </button>
  </section>
</template>
