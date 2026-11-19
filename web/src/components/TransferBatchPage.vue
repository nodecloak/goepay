<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount, watch } from "vue";
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
import KitFilterBar from "./KitFilterBar.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitEmpty from "./KitEmpty.vue";
import CopyButton from "./CopyButton.vue";
import MerchantRecords from "./MerchantRecords.vue";
const props = withDefaults(
  defineProps<{ client: ApiClient; newUseAllowed?: boolean }>(),
  { newUseAllowed: true },
);
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
const canPrepare = computed(
  () => props.newUseAllowed !== false && writes.value,
);
// Why 新建批量付款 is unavailable, shown beside the grey button: the admin has
// not loaded a 付款商户 yet, or the loaded merchant's payout capability is off.
const writesHint = computed(() => {
  if (props.newUseAllowed === false)
    return "批量代付功能已关闭，仍可查询原批次和取消未发送项目";
  if (loading.value || creating.value || writes.value) return "";
  if (!policy.value)
    return props.client.area === "admin" ? "请先输入付款商户并查询" : "";
  return "当前商户未开启付款能力，暂不能新建批量付款";
});
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
  if (!canPrepare.value || busy.value || loading.value) return;
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
    if (!policy.value || !canPrepare.value) throw new Error("付款能力未启用");
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
  if (busy.value || !frozen || !canPrepare.value) return;
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
watch(
  () => props.newUseAllowed,
  (allowed) => {
    if (allowed !== false) return;
    // An already dispatched request retains its outcome/recovery flow. An
    // unsubmitted draft must stop holding recipient data after access closes.
    rows.value = [emptyBatchRow()];
    frozen = undefined;
    confirming.value = false;
    if (!busy.value) creating.value = false;
  },
  { flush: "sync" },
);
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
    :new-use-allowed="newUseAllowed"
    @close="
      selected = '';
      load();
    "
  />
  <div
    v-else
    class="list-page"
    :class="{ 'merchant-batch-list': client.area === 'merchant' }"
    aria-label="持久批量付款"
  >
    <KitFilterBar
      :loading="loading"
      @query="
        before = '0';
        load();
      "
      @reset="
        external = '';
        before = '0';
        load();
      "
    >
      <KitField v-if="client.area === 'admin'" label="付款商户">
        <KitInput
          :model-value="merchant"
          :disabled="busy || creating"
          inputmode="numeric"
          aria-label="付款商户ID"
          @update:model-value="merchant = $event"
        />
      </KitField>
      <KitField label="请求号">
        <KitInput
          :model-value="external"
          :disabled="busy || creating"
          :maxlength="128"
          aria-label="批次请求号"
          @update:model-value="external = $event"
        />
      </KitField>
    </KitFilterBar>
    <section class="panel records batch-records" aria-label="批量付款">
      <p class="table-note">
        先整批预留，再单独执行。每批 1–100
        项；未知或已发送项目不重发，取消仅释放可证明尚未发送的项目。此功能不等同自动结算。
      </p>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <p v-if="notice" class="alert" role="status">{{ notice }}</p>
      <div class="table-tools">
        <button
          type="button"
          class="pa-btn pa-btn--primary"
          :disabled="busy || loading || !canPrepare || creating"
          @click="start"
        >
          新建批量付款
        </button>
        <span v-if="writesHint" class="batch-hint" role="note">{{
          writesHint
        }}</span>
      </div>
      <section
        v-if="creating && !uncertain"
        class="batch-editor"
        aria-label="新批次"
      >
        <div class="batch-request">
          <div>
            <p class="muted">本次原请求号</p>
            <code>{{ external }}</code>
          </div>
          <CopyButton :text="external" label="批次请求号" />
        </div>
        <p class="muted">请记录请求号，用于查找提交结果未确认的批次。</p>
        <BatchRecipientEditor
          v-if="policy"
          :rows="rows"
          :policy="policy"
          :disabled="busy || confirming || !canPrepare"
          @change="rows = $event"
        />
        <TransferBatchImport
          v-if="policy"
          :client="client"
          :policy="policy"
          :merchant-i-d="merchant"
          :disabled="busy || confirming || !canPrepare"
          @apply="rows = $event"
        />
        <button :disabled="busy || confirming || !canPrepare" @click="review">
          核对并预留整批资金
        </button>
        <div v-if="confirming" class="alert batch-confirm">
          <p>
            确认商户 {{ client.area === "admin" ? merchant : "当前商户" }}，共
            {{ rows.length }}
            项。将一次性预留所有本金及服务费，任何一项无效则整批回滚；本步骤不发送付款。
          </p>
          <div class="toolbar-actions">
            <button
              type="button"
              class="pa-btn"
              :disabled="busy"
              @click="
                confirming = false;
                frozen = undefined;
              "
            >
              返回编辑</button
            ><button
              type="button"
              class="pa-btn pa-btn--primary"
              :disabled="busy || !canPrepare"
              @click="prepare"
            >
              确认预留，不发送付款
            </button>
          </div>
        </div>
        <button :disabled="busy" @click="discard">
          取消编辑并清除收款资料
        </button>
      </section>
      <p v-if="uncertain" class="alert">
        原请求号已保留在上方「请求号」中，请先点「查询」查找已保存批次，再核对详情；收款资料已从当前表单清除。
      </p>
      <div v-if="loading" class="loading-row" role="status">正在读取批次…</div>
      <MerchantRecords
        :stacked="client.area === 'merchant'"
        class="table-scroll"
      >
        <table class="pa-table" role="table">
          <thead role="rowgroup">
            <tr role="row">
              <th scope="col">批次 ID</th>
              <th scope="col">请求号</th>
              <th scope="col" class="pa-c-num">本金（分）</th>
              <th scope="col" class="pa-c-num">预占（分）</th>
              <th scope="col">状态</th>
              <th scope="col">操作</th>
            </tr>
          </thead>
          <tbody role="rowgroup">
            <tr v-for="item in items" :key="item.id" role="row">
              <td class="pa-c-mono" data-cell-label="批次 ID" role="cell">
                {{ item.id }}
              </td>
              <td data-cell-label="请求号" data-cell-wide role="cell">
                {{ item.external_no }}
              </td>
              <td class="pa-c-num" data-cell-label="本金（分）" role="cell">
                {{ item.amount }}
              </td>
              <td class="pa-c-num" data-cell-label="预占（分）" role="cell">
                {{ item.cost_amount }}
              </td>
              <td data-cell-label="状态" data-cell-wide role="cell">
                {{ displayValue(item.summary) }}
              </td>
              <td data-cell-actions role="cell">
                <button
                  type="button"
                  class="link-button"
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
            <tr v-if="!items.length && !loading" role="row">
              <td class="empty" :colspan="6" role="cell">
                <KitEmpty text="暂无数据" />
              </td>
            </tr>
          </tbody>
        </table>
      </MerchantRecords>
      <div class="pa-pager pa-pager--keyset">
        <span class="pa-pager-total">共 {{ total }} 批</span>
        <button
          v-if="before !== '0'"
          type="button"
          class="pa-btn"
          :disabled="busy || loading"
          @click="
            before = '0';
            load();
          "
        >
          返回最新批次
        </button>
        <button
          v-if="next !== '0'"
          type="button"
          class="pa-btn"
          :disabled="busy || loading"
          @click="
            before = next;
            load();
          "
        >
          下一页批次
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
/* Full-width records card (as 付款记录): the note, alerts, toolbar, editor
   and the first/last table cells share the card's 20px inset. */
.batch-records > .table-note {
  padding-top: var(--space-4);
}
.batch-records > .alert {
  margin: 0 var(--space-5) var(--space-3);
}
.batch-records > .table-tools {
  padding-top: var(--space-2);
}
.batch-hint {
  color: var(--ink-muted);
  font-size: 12px;
}
.batch-editor {
  padding: 0 var(--space-5) var(--space-4);
}
.merchant-batch-list .batch-editor {
  min-width: 0;
  margin: var(--space-2) var(--space-5) var(--space-4);
  padding: var(--space-4);
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
}
.batch-request {
  display: flex;
  gap: var(--space-3);
  align-items: center;
  width: fit-content;
  max-width: 100%;
  padding: var(--space-2) var(--space-3);
  background: var(--bg-raised);
  border-radius: var(--radius-sm);
  overflow-wrap: anywhere;
}
.batch-request p {
  margin: 0;
}
.batch-confirm {
  margin-block: var(--space-3);
  border-inline-start: 3px solid var(--warning-text);
}
.batch-confirm p {
  margin-top: 0;
}
</style>
