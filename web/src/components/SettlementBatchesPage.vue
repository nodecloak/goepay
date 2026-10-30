<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from "vue";
import type { ApiClient } from "../api";
import {
  settlementBatchBody,
  settlementBatchStatus,
  type SettlementBatch,
  type SettlementBatchList,
} from "../settlement-batches";
import { centsMoney } from "../settlement-terms";
import { payoutStateLabel, payoutFundingLabel } from "../settlement-payout";
import SettlementPayoutPanel from "./SettlementPayoutPanel.vue";
import { settlementSnapshotLabel } from "../settlement-recipient";
import KitStatusTag from "./KitStatusTag.vue";
type KitTone = "success" | "warning" | "danger" | "info" | "neutral";
function batchTone(state: string): KitTone {
  return (
    (
      {
        pending: "warning",
        active: "warning",
        approved: "success",
        succeeded: "success",
        completed: "success",
        rejected: "danger",
        failed: "danger",
        archived: "neutral",
      } as Record<string, KitTone>
    )[state] || "neutral"
  );
}
const typeLabel = (type: string) =>
  (
    ({ alipay: "支付宝", wxpay: "微信支付", bank: "银行卡" }) as Record<
      string,
      string
    >
  )[type] || "待核实";
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
  initialRequestIds?: string;
}>();
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.ownSession &&
    ["super", "operator"].includes(props.adminClass || ""),
);
const items = ref<SettlementBatch[]>([]),
  selected = ref<SettlementBatch | null>(null),
  total = ref(0),
  next = ref("0"),
  before = ref("0"),
  filter = ref(""),
  batchFilter = ref(""),
  statusFilter = ref(""),
  ids = ref(""),
  external = ref(""),
  loading = ref(false),
  busy = ref(false),
  creating = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref(""),
  payout = ref(""),
  confirmation = ref<"archive" | "complete" | null>(null);
let listController: AbortController | undefined,
  detailController: AbortController | undefined,
  writeController: AbortController | undefined,
  exportController: AbortController | undefined;
let detailEpoch = 0,
  epoch = 0,
  frozen: ReturnType<typeof settlementBatchBody> | undefined;
async function load(reset = false) {
  if (!allowed.value) return;
  listController?.abort();
  const c = new AbortController();
  listController = c;
  loading.value = true;
  error.value = "";
  if (reset) {
    before.value = "0";
    detailEpoch++;
    detailController?.abort();
    selected.value = null;
    payout.value = "";
    confirmation.value = null;
  }
  const q = new URLSearchParams({ limit: "20", before_id: before.value });
  if (filter.value) q.set("external_no", filter.value);
  if (batchFilter.value) q.set("batch", batchFilter.value);
  if (statusFilter.value) q.set("status", statusFilter.value);
  try {
    const data = await props.client.request<SettlementBatchList>(
      "/settlement-batches?" + q,
      { signal: c.signal },
    );
    if (c.signal.aborted) return;
    items.value = data.items;
    total.value = data.total;
    next.value = data.next_before_id;
  } catch (e) {
    if (!c.signal.aborted)
      error.value = e instanceof Error ? e.message : "批次列表读取失败";
  } finally {
    if (!c.signal.aborted) loading.value = false;
  }
}
async function detail(id: string) {
  detailController?.abort();
  const c = new AbortController();
  detailController = c;
  const token = ++detailEpoch;
  selected.value = null;
  payout.value = "";
  confirmation.value = null;
  error.value = "";
  try {
    const data = await props.client.request<SettlementBatch>(
      "/settlement-batches/" + id,
      { signal: c.signal },
    );
    if (!c.signal.aborted && token === detailEpoch) selected.value = data;
  } catch (e) {
    if (!c.signal.aborted && token === detailEpoch)
      error.value = e instanceof Error ? e.message : "批次详情读取失败";
  }
}
function openCreate() {
  creating.value = true;
  ids.value = "";
  external.value = "settle-" + crypto.randomUUID();
  frozen = undefined;
  uncertain.value = false;
}
async function create(retry = false) {
  if (!allowed.value || busy.value) return;
  try {
    if (!retry) frozen = settlementBatchBody(external.value, ids.value);
    if (!frozen) throw new Error("请先选择原申请");
  } catch (e) {
    error.value = e instanceof Error ? e.message : "申请编号无效";
    return;
  }
  const token = epoch;
  busy.value = true;
  error.value = "";
  writeController = new AbortController();
  try {
    const out = await props.client.request<SettlementBatch>(
      "/settlement-batches",
      { method: "POST", body: frozen, signal: writeController.signal },
    );
    if (token !== epoch) return;
    creating.value = false;
    uncertain.value = false;
    notice.value = "批次已生成，请逐笔核对原申请与出款结果";
    filter.value = frozen!.external_no;
    batchFilter.value = "";
    statusFilter.value = "";
    await load(true);
    await detail(out.id);
  } catch (e) {
    if (token === epoch) {
      uncertain.value = true;
      error.value =
        (e instanceof Error ? e.message : "生成结果未确认") +
        "；可按原请求号查询，重试会使用相同申请与请求号。";
    }
  } finally {
    if (token === epoch) busy.value = false;
  }
}
async function action() {
  const b = selected.value,
    op = confirmation.value;
  if (!b || !op || busy.value || !allowed.value) return;
  const token = epoch;
  busy.value = true;
  error.value = "";
  writeController = new AbortController();
  try {
    await props.client.request("/settlement-batches/" + b.id + "/" + op, {
      method: "POST",
      body: {},
      signal: writeController.signal,
    });
    if (token !== epoch) return;
    confirmation.value = null;
    notice.value =
      op === "archive" ? "批次已归档，原申请与出款记录保留" : "批次已完成";
    await load();
    await detail(b.id);
  } catch (e) {
    if (token === epoch) {
      confirmation.value = null;
      error.value =
        (e instanceof Error ? e.message : "操作结果未确认") +
        "，请刷新原批次核实。";
    }
  } finally {
    if (token === epoch) busy.value = false;
  }
}
async function download(format: string) {
  const b = selected.value;
  if (!b || busy.value || !allowed.value || props.adminClass !== "super")
    return;
  const token = epoch;
  busy.value = true;
  error.value = "";
  exportController = new AbortController();
  try {
    await props.client.downloadCSV(
      "/settlement-batches/" + b.id + "/export?format=" + format,
      "pay_" + format + "_" + b.batch + ".csv",
      {
        signal: exportController.signal,
        encoding:
          format === "alipay" || format === "mybank" ? "gbk" : "utf8-bom",
      },
    );
  } catch (e) {
    if (token === epoch)
      error.value = e instanceof Error ? e.message : "导出失败";
  } finally {
    if (token === epoch) busy.value = false;
  }
}
function reset() {
  epoch++;
  detailEpoch++;
  listController?.abort();
  detailController?.abort();
  writeController?.abort();
  exportController?.abort();
  items.value = [];
  selected.value = null;
  payout.value = "";
  busy.value = false;
  creating.value = false;
  confirmation.value = null;
  frozen = undefined;
}
watch(
  () => [
    props.client,
    props.ownSession,
    props.adminClass,
    props.initialRequestIds,
  ],
  () => {
    reset();
    if (allowed.value) {
      void load(true);
      if (props.initialRequestIds) {
        try {
          settlementBatchBody("selected-requests", props.initialRequestIds);
          openCreate();
          ids.value = props.initialRequestIds;
        } catch (e) {
          error.value = e instanceof Error ? e.message : "原申请选择无效";
        }
      }
    }
  },
  { immediate: true },
);
onBeforeUnmount(reset);
</script>
<template>
  <section class="list-page settlement-batches" aria-label="批量结算">
    <div class="batches-head">
      <div v-if="allowed" class="batches-head-actions">
        <button
          type="button"
          class="primary"
          :disabled="busy"
          @click="openCreate"
        >
          生成结算批次</button
        ><a class="text-button" href="/admin/settle/list">结算申请列表</a>
      </div>
    </div>
    <p v-if="!allowed" class="alert error">
      请使用本人管理员会话查看结算批次。
    </p>
    <template v-else>
      <p class="muted batches-help">
        点击 生成结算批次
        之后会将所有“待结算”状态的记录更改为“正在结算”，通过转账接口转账之后会将状态更改为“已完成”，如果是手动转账，需要手动将状态更改为“已完成”。
      </p>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
      <form class="toolbar list-filter-card panel" @submit.prevent="load(true)">
        <label
          >批次号<input
            v-model="batchFilter"
            :disabled="busy"
            placeholder="完整批次号"
        /></label>
        <label
          >状态<select v-model="statusFilter" :disabled="busy">
            <option value="">全部</option>
            <option value="active">待完成</option>
            <option value="completed">已完成</option>
            <option value="archived">已归档</option>
          </select></label
        >
        <label
          >请求号<input
            v-model="filter"
            :disabled="busy"
            placeholder="按原生成请求号查询"
        /></label>
        <div class="toolbar-actions">
          <button :disabled="loading || busy">查询</button
          ><button type="button" :disabled="busy" @click="load()">刷新</button>
        </div>
      </form>
      <section class="panel records">
        <div v-if="loading" role="status">正在读取批次…</div>
        <div class="table-scroll">
          <table>
            <thead>
              <tr>
                <th>批次号</th>
                <th>总金额（元）</th>
                <th>总数量</th>
                <th>生成时间</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in items" :key="row.id">
                <td>{{ row.batch }}</td>
                <td>{{ row.allmoney }}</td>
                <td>{{ row.count }}</td>
                <td>{{ row.time }}</td>
                <td>
                  <KitStatusTag
                    :text="settlementBatchStatus(row.status)"
                    :tone="batchTone(row.status)"
                  />
                </td>
                <td>
                  <button
                    class="link-button"
                    :aria-label="'查看批次 ' + row.id"
                    :disabled="busy"
                    @click="detail(row.id)"
                  >
                    查看
                  </button>
                </td>
              </tr>
              <tr v-if="!items.length && !loading">
                <td colspan="6" class="empty">暂无结算批次</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="pagination">
          <span>共 {{ total }} 条</span>
          <div>
            <button
              :disabled="before === '0' || loading || busy"
              @click="load(true)"
            >
              第一页</button
            ><button
              :disabled="next === '0' || loading || busy"
              @click="
                before = next;
                load();
              "
            >
              下一页
            </button>
          </div>
        </div>
        <section v-if="selected" class="batch-detail" aria-label="批次详情">
          <h3>批次 {{ selected.batch }}</h3>
          <p>
            请求号：{{ selected.external_no }} · 净额合计
            {{ selected.allmoney }} 元 · {{ selected.count }} 笔
          </p>
          <div class="toolbar-actions">
            <button
              :disabled="
                busy || selected.status !== 'active' || !selected.all_paid
              "
              @click="confirmation = 'complete'"
            >
              完成批次</button
            ><button
              :disabled="busy || selected.status === 'archived'"
              @click="confirmation = 'archive'"
            >
              归档批次</button
            ><template v-if="adminClass === 'super'"
              ><button
                v-for="format in [
                  { id: 'common', name: '通用表格' },
                  { id: 'mybank', name: '网商银行' },
                  { id: 'alipay', name: '支付宝' },
                  { id: 'wxpay', name: '微信支付' },
                ]"
                :key="format.id"
                :disabled="busy"
                @click="download(format.id)"
              >
                导出{{ format.name }}
              </button></template
            >
          </div>
          <p class="muted">
            导出包含原申请的收款信息，请妥善保管。批次完成以原出款结果为准。
          </p>
          <div class="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>申请编号</th>
                  <th>商户 ID</th>
                  <th>转账方式</th>
                  <th>原收款快照</th>
                  <th>申请金额（元）</th>
                  <th>手续费（元）</th>
                  <th>结算净额（元）</th>
                  <th>申请状态</th>
                  <th>资金状态</th>
                  <th>出款状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="member in selected.items" :key="member.request_id">
                  <td>{{ member.request_id }}</td>
                  <td>{{ member.uid }}</td>
                  <td>{{ typeLabel(member.type) }}</td>
                  <td>
                    {{
                      member.recipient
                        ? settlementSnapshotLabel(member.recipient)
                        : "缺少原收款快照"
                    }}
                  </td>
                  <td>{{ centsMoney(member.gross_amount_cents) }}</td>
                  <td>{{ centsMoney(member.fee_amount_cents) }}</td>
                  <td>{{ centsMoney(member.net_amount_cents) }}</td>
                  <td>
                    <KitStatusTag
                      :text="String(member.status)"
                      :tone="batchTone(String(member.status))"
                    />
                  </td>
                  <td>{{ payoutFundingLabel(member.funding_state) }}</td>
                  <td>{{ payoutStateLabel(member.payout_state) }}</td>
                  <td>
                    <button
                      class="link-button"
                      :disabled="busy"
                      @click="payout = member.request_id"
                    >
                      核对原出款
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </section>
    </template>
  </section>
  <SettlementPayoutPanel
    v-if="allowed && payout"
    :key="payout"
    :client="client"
    area="admin"
    :request-id="payout"
    @close="payout = ''"
    @changed="
      selected && detail(selected.id);
      load();
    "
  />
  <div v-if="creating && allowed" class="modal-backdrop">
    <section
      class="modal panel"
      role="dialog"
      aria-modal="true"
      aria-label="生成结算批次"
    >
      <h2>生成结算批次</h2>
      <p>输入待结算或已核准申请的编号。生成后可在批次内核对每笔原出款。</p>
      <form @submit.prevent="create()">
        <label
          >生成请求号<input
            v-model="external"
            :disabled="busy || uncertain"
            required
            maxlength="128" /></label
        ><label
          >原结算申请编号<textarea
            v-model="ids"
            :disabled="busy || uncertain"
            required
            placeholder="多个编号以逗号或换行分隔，最多 100 条"
          />
        </label>
        <p v-if="error" role="alert" class="alert error">{{ error }}</p>
        <div class="toolbar-actions">
          <button :disabled="busy || uncertain" class="primary">生成批次</button
          ><button
            v-if="uncertain"
            type="button"
            :disabled="busy"
            @click="create(true)"
          >
            按原请求重试</button
          ><button
            v-if="uncertain"
            type="button"
            :disabled="busy"
            @click="
              filter = external;
              creating = false;
              load(true);
            "
          >
            查询原请求</button
          ><button type="button" :disabled="busy" @click="creating = false">
            关闭
          </button>
        </div>
      </form>
    </section>
  </div>
  <div v-if="confirmation && allowed" class="modal-backdrop">
    <section
      class="modal panel"
      role="dialog"
      aria-modal="true"
      aria-label="批次操作确认"
    >
      <h2>{{ confirmation === "archive" ? "归档批次" : "完成批次" }}</h2>
      <p>
        {{
          confirmation === "archive"
            ? "归档后保留所有原结算申请和出款记录。"
            : "请确认原申请均已实际出款成功。"
        }}
      </p>
      <div class="toolbar-actions">
        <button :disabled="busy" class="primary" @click="action">确认</button
        ><button :disabled="busy" @click="confirmation = null">取消</button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.batches-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.batches-head-actions {
  display: inline-flex;
  align-items: center;
  gap: 12px;
}
.batch-detail {
  border-top: 1px solid var(--line);
  margin-top: 20px;
  padding-top: 16px;
}
.settlement-batches .toolbar,
.batch-detail .toolbar-actions {
  flex-wrap: wrap;
  gap: 8px;
}
.settlement-batches .toolbar {
  justify-content: flex-start;
  align-items: flex-end;
}
.settlement-batches .toolbar > label {
  margin: 0;
  flex: 1 1 160px;
  min-width: 0;
}
.settlement-batches .toolbar > label > select {
  display: block;
  margin-top: 8px;
  width: 100%;
}
.settlement-batches td {
  white-space: nowrap;
  overflow-wrap: normal;
}
.settlement-batches input,
.modal textarea {
  max-width: 100%;
  box-sizing: border-box;
}
.modal textarea {
  min-height: 110px;
  width: 100%;
}
.batch-detail p {
  overflow-wrap: anywhere;
}
</style>
