<script lang="ts">
type PendingBusiness185 = {
  body: {
    external_no: string;
    action: string;
    reason: string;
    items: { trade_no: string }[];
  };
  batch?: import("../order-business-batches185").BusinessBatch185;
};
const pendingBusiness185 = new WeakMap<
  object,
  Map<string, PendingBusiness185>
>();
</script>
<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  toRaw,
  watch,
} from "vue";
import { ApiClient, ApiError } from "../api";
import {
  batchBusinessLabels185,
  financialBusinessBatch185,
  parseOrderBusinessBatch185,
  type BusinessBatch185,
  type BusinessBatchAction185,
} from "../order-business-batches185";
const props = defineProps<{
  client: ApiClient;
  action: BusinessBatchAction185;
  rows: Record<string, unknown>[];
  ownSession: boolean;
  adminClass?: string;
}>();
const emit = defineEmits<{ close: []; updated: []; busy: [value: boolean] }>();
const dialog = ref<HTMLElement>(),
  reason = ref(""),
  ack = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  uncertain = ref(false),
  batch = ref<BusinessBatch185 | null>(null);
const originalClient = toRaw(props.client),
  originalCSRF = originalClient.csrfToken,
  originalRole = props.adminClass;
let contexts = pendingBusiness185.get(originalClient);
if (!contexts) {
  contexts = new Map();
  pendingBusiness185.set(originalClient, contexts);
}
let pending: PendingBusiness185 | undefined = contexts.get(originalCSRF),
  alive = true,
  epoch = 0,
  controller: AbortController | undefined;
const selected = ref(
    pending?.body.items.map((x) => x.trade_no) ??
      props.rows.map((r) => String(r.trade_no)),
  ),
  action = ref<BusinessBatchAction185>(
    (pending?.body.action ?? props.action) as BusinessBatchAction185,
  );
if (pending) {
  reason.value = pending.body.reason;
  batch.value = pending.batch ?? null;
  uncertain.value = true;
  notice.value = "请先核对上次原批次结果";
}
const allowed = computed(
  () =>
    props.ownSession &&
    props.adminClass === originalRole &&
    ["super", "operator"].includes(props.adminClass ?? "") &&
    toRaw(props.client) === originalClient &&
    originalClient.area === "admin" &&
    originalClient.csrfToken === originalCSRF,
);
const done = computed(
  () =>
    batch.value?.state === "completed" || batch.value?.state === "cancelled",
);
const validReason = computed(
  () =>
    !!reason.value.trim() &&
    new TextEncoder().encode(reason.value.trim()).length <= 240,
);
function remember() {
  if (pending) contexts!.set(originalCSRF, pending);
  else contexts!.delete(originalCSRF);
}
function current(run: number, c: AbortController) {
  return alive && epoch === run && !c.signal.aborted && allowed.value;
}
function accept(raw: unknown) {
  const v = parseOrderBusinessBatch185(raw);
  if (
    !pending ||
    v.external_no !== pending.body.external_no ||
    v.action !== pending.body.action ||
    v.items.map((x) => x.trade_no).join("\0") !== selected.value.join("\0")
  )
    throw Error("原批次选集或请求号无法确认");
  batch.value = v;
  pending.batch = v;
  remember();
  return v;
}
function begin() {
  controller?.abort();
  const c = new AbortController();
  controller = c;
  const run = ++epoch;
  busy.value = true;
  emit("busy", true);
  error.value = "";
  return { c, run };
}
function end(run: number, c: AbortController) {
  if (current(run, c)) {
    busy.value = false;
    emit("busy", false);
  }
}
async function readOriginal() {
  if (!allowed.value || busy.value || !pending) return;
  const { c, run } = begin();
  try {
    const path = pending.batch
      ? `/orders/business-batches/${pending.batch.id}`
      : `/orders/business-batches?external_no=${encodeURIComponent(pending.body.external_no)}`;
    const v = await originalClient.request(path, { signal: c.signal });
    if (!current(run, c)) return;
    accept(v);
    uncertain.value = false;
    notice.value = "已读取原批次，请核对逐笔结果";
  } catch (e) {
    if (current(run, c)) {
      if (e instanceof ApiError && e.status === 404 && !pending.batch) {
        uncertain.value = false;
        notice.value = "未找到已接受的原批次，可使用同一请求号再次确认";
      } else {
        uncertain.value = true;
        error.value = "原结果仍无法确认，请稍后重新读取";
      }
    }
  } finally {
    end(run, c);
  }
}
async function execute() {
  if (
    !allowed.value ||
    busy.value ||
    done.value ||
    uncertain.value ||
    !ack.value ||
    !validReason.value ||
    selected.value.length < 1 ||
    selected.value.length > 100
  )
    return;
  if (!pending) {
    pending = {
      body: {
        external_no: crypto.randomUUID(),
        action: action.value,
        reason: reason.value.trim(),
        items: selected.value.map((trade_no) => ({ trade_no })),
      },
    };
    remember();
  }
  const { c, run } = begin();
  notice.value = "";
  try {
    if (!pending.batch) {
      const raw = await originalClient.request("/orders/business-batches", {
        method: "POST",
        body: pending.body,
        signal: c.signal,
      });
      if (!current(run, c)) return;
      accept(raw);
    }
    for (let step = 0; step < 100 && current(run, c) && !done.value; step++) {
      const path = `/orders/business-batches/${pending.batch!.id}/execute`,
        body = { limit: 1 };
      let raw: unknown;
      try {
        raw = await originalClient.request(path, {
          method: "POST",
          body,
          signal: c.signal,
          fundsAuthorizationChecked: true,
        });
      } catch (e) {
        if (!current(run, c)) return;
        if (
          e instanceof ApiError &&
          e.status === 428 &&
          financialBusinessBatch185(action.value)
        ) {
          raw = await originalClient.request(path, {
            method: "POST",
            body,
            signal: c.signal,
          });
        } else throw e;
      }
      if (!current(run, c)) return;
      const previous = pending.batch!.counts.processed;
      const next = accept(raw);
      if (
        next.state === "attention" ||
        next.counts.failed > 0 ||
        (next.counts.processed <= previous && !done.value)
      ) {
        notice.value =
          next.state === "attention"
            ? "存在结果未确认的项目，请核对原请求后继续"
            : next.counts.failed > 0
              ? "已保留成功与失败结果，后续项目尚未处理"
              : "当前项目仍在处理，请读取原结果";
        break;
      }
    }
    if (done.value) notice.value = "本批处理结束，请核对逐笔结果";
  } catch (e) {
    if (current(run, c)) {
      uncertain.value = true;
      error.value =
        e instanceof ApiError && [403, 409, 428].includes(e.status)
          ? "权限、授权或原状态已变化，请先读取原结果"
          : "响应未确认，请先读取原批次结果";
    }
  } finally {
    end(run, c);
  }
}
async function cancelUnsent() {
  if (
    !allowed.value ||
    busy.value ||
    uncertain.value ||
    !batch.value ||
    done.value
  )
    return;
  const { c, run } = begin();
  try {
    const raw = await originalClient.request(
      `/orders/business-batches/${batch.value.id}/cancel`,
      {
        method: "POST",
        body: { expected_fingerprint: batch.value.fingerprint },
        signal: c.signal,
      },
    );
    if (!current(run, c)) return;
    accept(raw);
    notice.value = "尚未执行的项目已取消，已领取或未知项目保留原请求";
  } catch {
    if (current(run, c)) {
      uncertain.value = true;
      error.value = "取消结果未确认，请读取原结果";
    }
  } finally {
    end(run, c);
  }
}
function close() {
  if (busy.value) return;
  if (done.value) {
    pending = undefined;
    remember();
    emit("updated");
  }
  emit("close");
}
function trap(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.stopPropagation();
    close();
    return;
  }
  if (e.key !== "Tab" || !dialog.value) return;
  const all = Array.from(
    dialog.value.querySelectorAll<HTMLElement>(
      "button:not([disabled]),textarea:not([disabled]),input:not([disabled])",
    ),
  );
  const first = all[0],
    last = all.at(-1);
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last?.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first?.focus();
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
  ],
  () => {
    if (!allowed.value) {
      epoch++;
      controller?.abort();
      busy.value = false;
      uncertain.value = true;
      contexts!.delete(originalCSRF);
      pending = undefined;
      batch.value = null;
      error.value = "会话已变化，已清除本次提交上下文";
      emit("busy", false);
    }
  },
  { flush: "sync" },
);
onMounted(
  () =>
    void nextTick(() =>
      dialog.value?.querySelector<HTMLElement>("textarea,button")?.focus(),
    ),
);
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  remember();
  emit("busy", false);
});
const stateText = (state: string) =>
  ({
    unsent: "未处理",
    running: "处理中",
    unknown: "结果待核对",
    succeeded: "成功",
    failed: "失败",
    unpaid: "未支付",
    unsupported: "不支持",
    queued: "已入队",
    cancelled: "已取消",
  })[state] ?? "待核对";
</script>
<template>
  <Teleport to="body"
    ><div class="modal-backdrop" @keydown="trap">
      <section
        ref="dialog"
        class="modal panel business-batch185"
        role="dialog"
        aria-modal="true"
        aria-label="批量订单业务确认"
      >
        <h2>{{ batchBusinessLabels185[action] }}确认</h2>
        <p>已选 {{ selected.length }} 条订单</p>
        <p v-if="action === 'refund'" class="muted">
          逐笔按原订单可退金额处理；首次退款前核对本批资金授权。出现失败或待核对结果时停止后续项目。
        </p>
        <p v-if="action === 'notify'" class="muted">
          通知入队后由原通知任务发送，已入队不表示送达成功。
        </p>
        <p v-if="error" role="alert" class="alert error">{{ error }}</p>
        <p v-if="notice" role="status">{{ notice }}</p>
        <template v-if="batch"
          ><p>批次 {{ batch.id }} · 原请求 {{ batch.external_no }}</p>
          <progress
            :max="batch.counts.total"
            :value="batch.counts.processed"
            aria-label="实际已处理订单"
          />
          <p role="status">
            已处理 {{ batch.counts.processed }} / {{ batch.counts.total }}，成功
            {{ batch.counts.succeeded }}，失败 {{ batch.counts.failed }}，待核对
            {{ batch.counts.unknown }}，取消 {{ batch.counts.cancelled }}
          </p>
          <ol>
            <li v-for="item in batch.items" :key="item.trade_no">
              <span>{{ item.trade_no }}</span
              ><strong>{{ stateText(item.state) }}</strong>
            </li>
          </ol></template
        >
        <ol v-else>
          <li v-for="trade in selected" :key="trade">{{ trade }}</li>
        </ol>
        <label
          >操作原因<textarea
            v-model="reason"
            maxlength="240"
            :disabled="busy || !!pending || !allowed"
          /></label
        ><label class="batch185-check"
          ><input
            v-model="ack"
            type="checkbox"
            :disabled="busy || !allowed"
          />我已核对原订单与本批操作</label
        >
        <div class="modal-actions">
          <button :disabled="busy" @click="close">关闭</button
          ><button
            v-if="pending"
            :disabled="busy || !allowed"
            @click="readOriginal"
          >
            读取原批结果</button
          ><button
            v-if="batch && !done"
            :disabled="busy || uncertain || !allowed"
            @click="cancelUnsent"
          >
            取消未处理项目</button
          ><button
            v-if="!done"
            class="primary"
            :disabled="busy || uncertain || !allowed || !ack || !validReason"
            @click="execute"
          >
            {{ busy ? "正在处理…" : batch ? "继续核对原请求" : "确认执行" }}
          </button>
        </div>
      </section>
    </div></Teleport
  >
</template>
<style scoped>
.business-batch185 {
  width: min(660px, calc(100vw - 24px));
  max-height: calc(100dvh - 32px);
  overflow: auto;
}
.business-batch185 p,
.business-batch185 li {
  overflow-wrap: anywhere;
}
.business-batch185 ol {
  max-height: 220px;
  overflow: auto;
  padding-left: 24px;
}
.business-batch185 li {
  padding: 6px 0;
}
.business-batch185 li strong {
  margin-left: 12px;
}
.business-batch185 progress {
  width: 100%;
}
.business-batch185 textarea {
  min-height: 72px;
}
.batch185-check {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-top: 12px;
}
.batch185-check input {
  width: auto;
}
.modal-actions {
  flex-wrap: wrap;
}
.business-batch185 label {
  display: block;
}
.batch185-check {
  display: flex !important;
}
</style>
