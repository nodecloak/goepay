<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, computed, watch } from "vue";
import { ApiClient, displayValue } from "../api";
import {
  batchOutcome,
  batchCanExecute,
  batchSummaryLabels,
  type TransferBatch,
} from "../transfer-batches";
import { readFundsAvailability } from "../funds-availability";
import DataTable from "./DataTable.vue";
const props = withDefaults(
  defineProps<{
    client: ApiClient;
    batchId: string;
    newUseAllowed?: boolean;
  }>(),
  { newUseAllowed: true },
);
const emit = defineEmits<{ close: []; updated: [] }>();
const batch = ref<TransferBatch | null>(null),
  loading = ref(false),
  busy = ref(false),
  writesEnabled = ref(false),
  error = ref(""),
  notice = ref(""),
  confirmation = ref<"execute" | "cancel-unsent" | null>(null),
  limit = ref(10);
let controller: AbortController | undefined;
const executeAllowed = computed(
  () =>
    props.newUseAllowed !== false &&
    !!batch.value &&
    batchCanExecute(batch.value, writesEnabled.value),
);
watch(
  () => props.newUseAllowed,
  (allowed) => {
    if (allowed === false && confirmation.value === "execute")
      confirmation.value = null;
  },
  { flush: "sync" },
);
async function load() {
  controller?.abort();
  const c = new AbortController();
  controller = c;
  loading.value = true;
  writesEnabled.value = false;
  error.value = "";
  try {
    const data = await props.client.request<TransferBatch>(
      `/transfer-batches/${encodeURIComponent(props.batchId)}`,
      { signal: c.signal },
    );
    if (c.signal.aborted) return;
    batch.value = data;
    const policy = await props.client.request<Record<string, unknown>>(
      "/transfers/capabilities" +
        (props.client.area === "admin"
          ? `?merchant_id=${encodeURIComponent(data.merchant_id)}`
          : ""),
      { signal: c.signal },
    );
    readFundsAvailability(policy);
    if (!c.signal.aborted) writesEnabled.value = policy.writes_enabled === true;
  } catch (e) {
    if (!c.signal.aborted)
      error.value = e instanceof Error ? e.message : "批次或付款能力读取失败";
  } finally {
    if (controller === c) loading.value = false;
  }
}
async function action(kind: "execute" | "query" | "cancel-unsent") {
  if (busy.value || loading.value || !batch.value) return;
  if (
    kind === "execute" &&
    (!executeAllowed.value ||
      !Number.isInteger(limit.value) ||
      limit.value < 1 ||
      limit.value > 100)
  )
    return;
  if (kind === "cancel-unsent" && !batch.value.summary.unsent) return;
  busy.value = true;
  error.value = "";
  notice.value = "";
  confirmation.value = null;
  try {
    const result = await props.client.requestResult<TransferBatch>(
      `/transfer-batches/${encodeURIComponent(props.batchId)}/${kind}`,
      {
        method: "POST",
        body: kind === "execute" ? { limit: limit.value } : {},
      },
    );
    notice.value =
      (result.status === 202
        ? "本轮处理仍待核实。"
        : "本轮请求已处理，不代表全部付款成功。") + batchOutcome(result.data);
  } catch (e) {
    notice.value =
      "本轮结果未确认，只读取原批次并核对已发送项目，不会自动重发付款。";
    error.value = e instanceof Error ? e.message : "批次操作结果未知";
  } finally {
    await load();
    busy.value = false;
    emit("updated");
  }
}
const columns = [
  { key: "position", label: "行号" },
  { key: "send_state", label: "发送边界" },
  { key: "id", label: "原付款 ID" },
  { key: "channel_id", label: "原通道 ID" },
  { key: "amount", label: "本金（分）" },
  { key: "fee_amount", label: "服务费（分）" },
  { key: "status", label: "有效付款状态" },
  { key: "ledger_status", label: "历史账本状态" },
  { key: "bank_lifecycle", label: "银行生命周期" },
];
const rows = computed(
  () =>
    batch.value?.items.map((item) => ({
      ...item.transfer,
      position: item.position,
      send_state: item.send_state,
    })) || [],
);
onMounted(load);
onBeforeUnmount(() => controller?.abort());
</script>
<template>
  <section class="panel records" aria-label="批量付款详情">
    <h2>批量付款详情</h2>
    <div v-if="error" role="alert" class="alert error">{{ error }}</div>
    <p v-if="notice" role="status" class="alert">{{ notice }}</p>
    <p v-if="loading" role="status">正在核对原批次…</p>
    <template v-if="batch">
      <dl class="record-detail">
        <template
          v-for="(label, key) in {
            id: '批次 ID',
            merchant_id: '付款商户',
            external_no: '批次请求号',
            amount: '总本金（分）',
            cost_amount: '总预占（分）',
            fee_amount: '总服务费（分）',
            state: '批次状态',
          }"
          :key="key"
          ><dt>{{ label }}</dt>
          <dd>{{ displayValue(batch[key]) }}</dd></template
        >
      </dl>
      <p class="alert">{{ batchOutcome(batch) }}</p>
      <dl class="record-detail">
        <template v-for="(label, key) in batchSummaryLabels" :key="key"
          ><dt>{{ label }}</dt>
          <dd>{{ batch.summary[key] ?? "未提供" }}</dd></template
        >
      </dl>
      <p class="muted">
        仅显示服务端保存的脱敏记录，不从详情猜测或还原收款账号。未知、已发送和待核实项目只查询原付款；取消仅释放可证明尚未发送的项目，停写后仍可查询或取消未发送项。
      </p>
      <p v-if="newUseAllowed === false" class="muted" role="note">
        批量代付功能已关闭，仍可查询原批次和取消未发送项目。
      </p>
      <DataTable :columns="columns" :items="rows" />
      <div class="toolbar-actions">
        <button :disabled="loading || busy" @click="load">刷新原批次</button>
        <button :disabled="loading || busy" @click="action('query')">
          查询已发送项目
        </button>
        <button
          v-if="executeAllowed"
          :disabled="loading || busy"
          @click="confirmation = 'execute'"
        >
          执行尚未发送项目
        </button>
        <button
          v-if="batch.summary.unsent"
          :disabled="loading || busy"
          @click="confirmation = 'cancel-unsent'"
        >
          取消尚未发送项目
        </button>
      </div>
      <div v-if="confirmation" class="alert">
        <h3>
          {{
            confirmation === "execute" ? "确认执行付款" : "确认取消未发送项目"
          }}
        </h3>
        <p>
          批次 {{ batch.id }}（{{ batch.external_no }}），未发送项目
          {{ batch.summary.unsent }} 个。{{
            confirmation === "execute"
              ? "此操作会实际向通道发送尚未发送的付款项目；已经发送或结果未知的项目不会重发。"
              : "只处理原批次中仍可证明未发送的项目，已发送或结果未知的项目不会释放预占。"
          }}
        </p>
        <label v-if="confirmation === 'execute'"
          >本轮最多执行项目数<input
            v-model.number="limit"
            type="number"
            min="1"
            max="100"
        /></label>
        <button :disabled="busy" @click="confirmation = null">返回核对</button>
        <button
          class="danger-button"
          :disabled="
            busy ||
            loading ||
            (confirmation === 'execute' &&
              (!executeAllowed ||
                !Number.isInteger(limit) ||
                limit < 1 ||
                limit > 100))
          "
          @click="action(confirmation!)"
        >
          {{
            confirmation === "execute"
              ? "确认仅执行未发送项目"
              : "确认取消未发送项目"
          }}
        </button>
      </div>
    </template>
    <button :disabled="busy" @click="emit('close')">返回批次列表</button>
  </section>
</template>
