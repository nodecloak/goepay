<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { ApiClient } from "../api";
import {
  manualPrintBody,
  printingMoney,
  printingState,
  recoverPrintBody,
  type PrintReceipt,
  type PrintJob,
} from "../printing";
const props = defineProps<{
  client: ApiClient;
  tradeNo: string;
  ownSession: boolean;
}>();
const emit = defineEmits<{ busy: [value: boolean] }>();
const owner = computed(
  () => props.client.area === "merchant" && props.ownSession,
);
const receipt = ref<PrintReceipt | null>(null),
  jobs = ref<PrintJob[]>([]),
  next = ref("");
const busy = ref(false),
  loaded = ref(false),
  error = ref(""),
  notice = ref(""),
  confirming = ref(false),
  uncertain = ref(false),
  externalNo = ref("");
const pendingRecovery = ref<{ id: string; taskID: string } | null>(null),
  recoveryUncertain = ref(false);
watch(
  () => busy.value || uncertain.value || recoveryUncertain.value,
  (value) => emit("busy", value),
  { flush: "sync" },
);
const controller = new AbortController();
let alive = true;
const blocked = computed(
  () =>
    !loaded.value ||
    !!next.value ||
    jobs.value.some((j) => !["accepted", "rejected"].includes(j.state)),
);
const base = computed(() => `/orders/${encodeURIComponent(props.tradeNo)}`);
async function history(append = false) {
  const data = await props.client.request<{
    items: PrintJob[];
    next_cursor: string;
  }>(
    base.value + `/print-jobs?after_id=${append ? next.value : "0"}&limit=20`,
    { signal: controller.signal },
  );
  if (
    !Array.isArray(data.items) ||
    data.items.some((j) => j.trade_no !== props.tradeNo)
  )
    throw new Error("打印记录原订单不一致");
  if (alive) {
    jobs.value = append ? [...jobs.value, ...data.items] : data.items;
    next.value = data.next_cursor;
  }
}
async function load() {
  if (!owner.value || busy.value) return;
  busy.value = true;
  error.value = "";
  loaded.value = false;
  try {
    const r = await props.client.request<PrintReceipt>(
      base.value + "/receipt",
      { signal: controller.signal },
    );
    printingMoney(r.amount);
    if (r.trade_no !== props.tradeNo) throw new Error("收据原订单不一致");
    if (!alive) return;
    receipt.value = r;
    await history();
    if (alive) loaded.value = true;
  } catch (e) {
    if (alive)
      error.value = e instanceof Error ? e.message : "打印记录读取失败";
  } finally {
    if (alive) busy.value = false;
  }
}
function review() {
  if (!owner.value || blocked.value || busy.value || uncertain.value) return;
  externalNo.value = "print-" + crypto.randomUUID();
  confirming.value = true;
}
function reviewRecovery(j: PrintJob) {
  if (
    !owner.value ||
    busy.value ||
    uncertain.value ||
    pendingRecovery.value ||
    j.can_recover !== true
  )
    return;
  recoverPrintBody(j.task_id);
  pendingRecovery.value = { id: j.id, taskID: j.task_id! };
  recoveryUncertain.value = false;
}
async function recover() {
  const target = pendingRecovery.value;
  if (!target || busy.value || !owner.value) return;
  busy.value = true;
  error.value = "";
  try {
    const { data, status } = await props.client.requestResult<PrintJob>(
      `/print-jobs/${encodeURIComponent(target.id)}/recover`,
      {
        method: "POST",
        body: recoverPrintBody(target.taskID),
        signal: controller.signal,
      },
    );
    if (
      status !== 200 ||
      data.id !== target.id ||
      data.trade_no !== props.tradeNo
    )
      throw new Error("原打印任务恢复响应未核实");
    if (alive) {
      jobs.value = jobs.value.map((j) => (j.id === data.id ? data : j));
      pendingRecovery.value = null;
      recoveryUncertain.value = false;
      notice.value =
        "原打印任务状态已核对：" + printingState(data.state, data.provider);
    }
  } catch (e) {
    if (alive) {
      recoveryUncertain.value = true;
      error.value =
        e instanceof Error ? e.message : "恢复结果未知，保留原任务号核对";
    }
  } finally {
    if (alive) busy.value = false;
  }
}
async function send() {
  if (!owner.value || busy.value || !externalNo.value) return;
  busy.value = true;
  error.value = "";
  try {
    const { data, status } = await props.client.requestResult<PrintJob>(
      base.value + "/print-jobs",
      {
        method: "POST",
        body: manualPrintBody(externalNo.value),
        signal: controller.signal,
      },
    );
    if (
      !["200", "201", "202"].includes(String(status)) ||
      data.trade_no !== props.tradeNo ||
      data.external_no !== externalNo.value
    )
      throw new Error("打印请求响应未核实，请保留原号核对");
    if (alive) {
      uncertain.value = false;
      confirming.value = false;
      notice.value = printingState(data.state, data.provider);
      jobs.value = [data, ...jobs.value.filter((j) => j.id !== data.id)];
      externalNo.value = "";
    }
  } catch (e) {
    if (alive) {
      uncertain.value = true;
      confirming.value = false;
      error.value = e instanceof Error ? e.message : "打印请求结果未知";
    }
  } finally {
    if (alive) busy.value = false;
  }
}
async function more() {
  if (!next.value || busy.value) return;
  busy.value = true;
  try {
    await history(true);
  } catch (e) {
    if (alive) error.value = e instanceof Error ? e.message : "记录读取失败";
  } finally {
    if (alive) busy.value = false;
  }
}
onBeforeUnmount(() => {
  alive = false;
  controller.abort();
  emit("busy", false);
});
</script>
<template>
  <section class="panel">
    <h3>订单打印收据</h3>
    <p v-if="!owner" class="muted">打印由商户本人会话管理。</p>
    <template v-else>
      <button :disabled="busy" @click="load">查看或刷新打印收据</button>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <template v-if="receipt && loaded">
        <dl class="record-detail">
          <dt>原订单</dt>
          <dd class="pa-order-no">{{ receipt.trade_no }}</dd>
          <dt>实付金额</dt>
          <dd>{{ printingMoney(receipt.amount) }} 元</dd>
          <dt>商品</dt>
          <dd>{{ receipt.name }}</dd>
          <dt>商户</dt>
          <dd>{{ receipt.merchant_name }}</dd>
          <dt>支付时间</dt>
          <dd>{{ receipt.paid_at }}</dd>
          <dt>备注</dt>
          <dd>{{ receipt.remark || "无" }}</dd>
        </dl>
        <p>只显示已核实支付收据。打印故障不会更改支付结果。</p>
        <p v-if="next" class="alert">
          请先读取全部原订单打印记录再创建新请求。
        </p>
        <p v-else-if="blocked" class="alert">
          已有未发送、处理中或未知记录；请核对原记录，本页不提供未知结果重发。
        </p>
        <button
          :disabled="busy || blocked || uncertain || !!pendingRecovery"
          @click="review"
        >
          创建手工打印请求
        </button>
        <ul>
          <li v-for="j in jobs" :key="j.id">
            {{ j.request_no }} · {{ printingState(j.state, j.provider) }} ·
            {{ j.copies }} 份 · {{ j.device_masked }}
            <span v-if="j.task_state"> · 处理任务：{{ j.task_state }}</span>
            <button
              v-if="j.can_recover === true"
              :disabled="busy || uncertain || !!pendingRecovery"
              @click="reviewRecovery(j)"
            >
              核对并恢复原未发送任务
            </button>
          </li>
        </ul>
        <button v-if="next" :disabled="busy" @click="more">
          读取更早打印记录
        </button>
      </template>
      <div v-if="confirming" class="alert">
        <p>
          确认打印订单
          <span class="pa-order-no">{{ tradeNo }}</span>
          的收据？已有受理记录时可能再次打印。请求号：{{ externalNo }}
        </p>
        <button
          :disabled="busy"
          @click="
            confirming = false;
            externalNo = '';
          "
        >
          取消</button
        ><button :disabled="busy" @click="send">确认创建打印请求</button>
      </div>
      <div v-if="uncertain" class="alert">
        <p>
          提交结果未知。保留原请求号
          {{ externalNo }}，核对不会换号或重发原未知任务。
        </p>
        <button :disabled="busy" @click="send">按原请求号核对</button>
      </div>
      <div v-if="pendingRecovery" class="alert">
        <p>
          核对原处理任务
          {{
            pendingRecovery.taskID
          }}。仅恢复尚未发送的原打印意图，不新增收据或改变原请求号；发送中或未知结果不允许恢复。
        </p>
        <template v-if="!recoveryUncertain"
          ><button :disabled="busy" @click="pendingRecovery = null">
            取消恢复</button
          ><button :disabled="busy" @click="recover">
            确认恢复原未发送任务
          </button></template
        ><button v-else :disabled="busy" @click="recover">
          按原任务号核对恢复
        </button>
      </div>
    </template>
  </section>
</template>
