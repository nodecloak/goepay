<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from "vue";
import type { ApiClient } from "../api";
import { consoleValue, minorYuan } from "../pro-console-fields";
import { authorizationReleaseNotice } from "../authorization-release";

const props = defineProps<{
  client: ApiClient;
  order: Record<string, unknown>;
  allowed?: boolean;
  ownSession?: boolean;
  disabled?: boolean;
  releaseAllowed?: boolean;
  releaseRevision?: string;
}>();
const emit = defineEmits<{
  released: [message: string];
  busy: [value: boolean];
}>();
type Observation = {
  request_id: string;
  client_request_no: string;
  merchant_id: string;
  order_id: string;
  attempt_id: string;
  channel_id: string;
  trade_no: string;
  app_id: string;
  auth_no: string;
  buyer_id: string;
  amount: string;
  status: "processing" | "verified" | "unknown";
  rest_known: boolean;
  rest_amount?: string;
  provider_status: string;
  order_status: string;
  operation_type: string;
  observed_at: string;
  source_verified: boolean;
};
const hex = (v: unknown): v is string =>
  typeof v === "string" && /^[0-9a-f]{64}$/.test(v);
const id = (v: unknown): v is string =>
  typeof v === "string" &&
  /^[1-9]\d{0,18}$/.test(v) &&
  BigInt(v) <= 9223372036854775807n;
const minor = (v: unknown): v is string =>
  typeof v === "string" &&
  /^(0|[1-9]\d{0,18})$/.test(v) &&
  BigInt(v) <= 9223372036854775807n;
const text = (v: unknown, required = false): v is string =>
  typeof v === "string" &&
  (!required || !!v) &&
  v.length <= 256 &&
  !/[\x00-\x1f\x7f]/.test(v);
const permitted = computed(
  () =>
    props.allowed === true &&
    props.ownSession === true &&
    ["admin", "merchant"].includes(props.client.area) &&
    id(props.order.id) &&
    id(props.order.merchant_id) &&
    text(props.order.trade_no, true),
);
const observation = ref<Observation | null>(null),
  confirmRemaining = ref(false),
  error = ref(""),
  busy = ref(false),
  started = ref(false),
  clientRequest = ref(""),
  requestID = ref("");
let epoch = 0,
  controller: AbortController | undefined;
function reset() {
  epoch++;
  controller?.abort();
  controller = undefined;
  observation.value = null;
  confirmRemaining.value = false;
  error.value = "";
  busy.value = false;
  started.value = false;
  clientRequest.value = "";
  requestID.value = "";
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.order.id,
    props.order.merchant_id,
    props.order.trade_no,
    props.allowed,
    props.ownSession,
    props.releaseRevision,
  ],
  reset,
  { flush: "sync" },
);
onBeforeUnmount(reset);
watch(
  () => props.releaseAllowed,
  () => {
    confirmRemaining.value = false;
  },
);

function decode(
  value: unknown,
  nonce: string,
  expectedRequest: string,
): Observation {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw Error();
  const v = value as Record<string, unknown>;
  if (
    !hex(v.request_id) ||
    !hex(v.client_request_no) ||
    v.client_request_no !== nonce ||
    (expectedRequest && v.request_id !== expectedRequest) ||
    v.merchant_id !== props.order.merchant_id ||
    v.order_id !== props.order.id ||
    v.trade_no !== props.order.trade_no ||
    !id(v.merchant_id) ||
    !id(v.order_id) ||
    !id(v.attempt_id) ||
    !id(v.channel_id) ||
    !minor(v.amount) ||
    BigInt(v.amount) <= 0n ||
    !["processing", "verified", "unknown"].includes(String(v.status)) ||
    typeof v.rest_known !== "boolean" ||
    typeof v.source_verified !== "boolean"
  )
    throw Error();
  for (const key of ["app_id", "auth_no", "buyer_id"])
    if (!text(v[key], key !== "buyer_id")) throw Error();
  for (const key of ["provider_status", "order_status", "operation_type"])
    if (v[key] !== undefined && !text(v[key])) throw Error();
  const observedAt = v.observed_at ?? "";
  if (
    !text(observedAt) ||
    (observedAt &&
      (!/^\d{4}-\d{2}-\d{2}T/.test(observedAt) ||
        !Number.isFinite(Date.parse(observedAt))))
  )
    throw Error();
  if (minor(props.order.amount) && v.amount !== props.order.amount)
    throw Error();
  if (
    v.status === "verified" &&
    (!v.source_verified || !v.rest_known || !observedAt)
  )
    throw Error();
  if (v.status !== "verified" && (v.source_verified || v.rest_known))
    throw Error();
  if (
    v.rest_known &&
    (v.status !== "verified" ||
      !v.source_verified ||
      !minor(v.rest_amount) ||
      BigInt(v.rest_amount) > BigInt(v.amount))
  )
    throw Error();
  if (!v.rest_known && v.rest_amount !== undefined) throw Error();
  return {
    request_id: v.request_id,
    client_request_no: v.client_request_no,
    merchant_id: v.merchant_id,
    order_id: v.order_id,
    attempt_id: v.attempt_id,
    channel_id: v.channel_id,
    trade_no: v.trade_no as string,
    app_id: v.app_id as string,
    auth_no: v.auth_no as string,
    buyer_id: v.buyer_id as string,
    amount: v.amount,
    status: v.status as Observation["status"],
    rest_known: v.rest_known,
    ...(v.rest_known ? { rest_amount: v.rest_amount as string } : {}),
    provider_status: (v.provider_status ?? "") as string,
    order_status: (v.order_status ?? "") as string,
    operation_type: (v.operation_type ?? "") as string,
    observed_at: observedAt,
    source_verified: v.source_verified,
  };
}
async function query(fresh: boolean) {
  if (
    !permitted.value ||
    props.disabled ||
    busy.value ||
    (!fresh && !requestID.value)
  )
    return;
  if (fresh) {
    try {
      const bytes = crypto.getRandomValues(new Uint8Array(32));
      clientRequest.value = Array.from(bytes, (b) =>
        b.toString(16).padStart(2, "0"),
      ).join("");
    } catch {
      error.value = "浏览器无法创建安全查询标识，请重新打开页面。";
      return;
    }
    requestID.value = "";
  }
  started.value = true;
  confirmRemaining.value = false;
  observation.value = null;
  error.value = "";
  busy.value = true;
  controller?.abort();
  const c = new AbortController(),
    current = ++epoch,
    nonce = clientRequest.value,
    originalRequest = requestID.value;
  controller = c;
  const path = `/orders/${encodeURIComponent(String(props.order.trade_no))}/authorization-observations`;
  try {
    const data = await props.client.request<unknown>(
      fresh ? path : `${path}/${originalRequest}`,
      {
        method: fresh ? "POST" : "GET",
        ...(fresh ? { body: { client_request_no: nonce } } : {}),
        signal: c.signal,
      },
    );
    if (current !== epoch || c.signal.aborted || !permitted.value) return;
    const verified = decode(data, nonce, originalRequest);
    observation.value = verified;
    requestID.value = verified.request_id;
  } catch {
    if (current === epoch && !c.signal.aborted)
      error.value =
        "本次查询结果未知或无法核对，未重发原请求。可刷新已有查询状态，或明确发起一次新的只读查询。";
  } finally {
    if (current === epoch && !c.signal.aborted) busy.value = false;
  }
}
const canReleaseRemaining = computed(() => {
  const q = observation.value;
  return (
    permitted.value &&
    props.releaseAllowed === true &&
    !!q &&
    q.status === "verified" &&
    q.source_verified === true &&
    q.rest_known === true &&
    q.provider_status === "SUCCESS" &&
    q.order_status === "AUTHORIZED" &&
    q.operation_type === "FREEZE" &&
    minor(q.rest_amount) &&
    BigInt(q.rest_amount) > 0n &&
    /^(0|[1-9]\d?)$/.test(props.releaseRevision ?? "")
  );
});
async function releaseRemaining() {
  if (
    !canReleaseRemaining.value ||
    !confirmRemaining.value ||
    busy.value ||
    props.disabled ||
    !observation.value
  )
    return;
  const q = observation.value;
  confirmRemaining.value = false;
  busy.value = true;
  emit("busy", true);
  let message = "剩余冻结解除结果尚未核实，请保留原解除请求号并查询原请求。";
  try {
    const result = await props.client.requestResult(
      `/orders/${encodeURIComponent(String(props.order.trade_no))}/release-remaining-authorization`,
      {
        method: "POST",
        body: {
          expected_observation_id: q.request_id,
          expected_remaining_cents: q.rest_amount,
          expected_source_revision: props.releaseRevision,
        },
      },
    );
    message = authorizationReleaseNotice(result.data, result.status);
  } catch (e) {
    message += e instanceof Error ? ` ${e.message}` : "";
  } finally {
    busy.value = false;
    observation.value = null;
    emit("busy", false);
    emit("released", message);
  }
}
</script>
<template>
  <section
    v-if="permitted"
    class="preauthorization-observation"
    aria-label="预授权原冻结与余款"
  >
    <h3>原冻结与余款查询</h3>
    <p class="muted">
      仅查询原预授权，不会扣款或解除冻结，也不会修改订单。结果是观察时间的历史记录，不授予后续资金操作权限。
    </p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="busy" role="status">正在读取查询结果…</p>
    <template v-if="observation">
      <p role="status">
        {{
          observation.status === "verified"
            ? "查询已核实"
            : observation.status === "processing"
              ? "查询处理中，余款未知"
              : "查询结果未知，余款未知"
        }}
      </p>
      <dl class="record-detail">
        <dt>原授权编号</dt>
        <dd>{{ observation.auth_no }}</dd>
        <dt>授权付款人</dt>
        <dd>{{ observation.buyer_id || "未知" }}</dd>
        <dt>原冻结金额（元）</dt>
        <dd>{{ minorYuan(observation.amount) }}</dd>
        <dt>余款（元）</dt>
        <dd>
          {{
            observation.rest_known &&
            observation.status === "verified" &&
            observation.source_verified
              ? minorYuan(observation.rest_amount)
              : "余款未知"
          }}
        </dd>
        <dt>通道授权状态</dt>
        <dd>{{ observation.order_status || "未知" }}</dd>
        <dt>通道结果状态</dt>
        <dd>{{ observation.provider_status || "未知" }}</dd>
        <dt>操作类型</dt>
        <dd>{{ observation.operation_type || "未知" }}</dd>
        <dt>来源验证</dt>
        <dd>
          {{ observation.source_verified ? "来源已验证" : "来源尚未验证" }}
        </dd>
        <dt>观察时间（北京时间）</dt>
        <dd>
          {{
            observation.observed_at
              ? consoleValue("observed_at", observation.observed_at)
              : "尚无已核实观察时间"
          }}
        </dd>
        <dt>原查询标识</dt>
        <dd>{{ observation.request_id }}</dd>
      </dl>
    </template>
    <p v-else-if="started && !busy" role="status">查询结果未知，余款未知。</p>
    <p v-if="started" class="muted">本次查询请求号：{{ clientRequest }}</p>
    <div class="toolbar-actions">
      <button type="button" :disabled="busy || disabled" @click="query(true)">
        {{ started ? "重新查询原冻结与余款" : "查询原冻结与余款" }}
      </button>
      <button
        v-if="requestID"
        type="button"
        :disabled="busy || disabled"
        @click="query(false)"
      >
        刷新原查询状态
      </button>
    </div>
    <div v-if="canReleaseRemaining" class="alert">
      <button
        v-if="!confirmRemaining"
        type="button"
        class="danger-button"
        :disabled="busy || disabled"
        @click="confirmRemaining = true"
      >
        解除剩余冻结
      </button>
      <template v-else>
        <h4>确认解除剩余冻结</h4>
        <dl class="record-detail">
          <dt>原冻结金额（元）</dt>
          <dd>{{ minorYuan(observation?.amount) }}</dd>
          <dt>本次剩余解除额（元）</dt>
          <dd>{{ minorYuan(observation?.rest_amount) }}</dd>
          <dt>确认来源修订</dt>
          <dd>{{ releaseRevision }}</dd>
        </dl>
        <p>
          服务端会重新核对原授权和当前剩余额度。额度变化需重新查询、确认；发送后只查询原解除请求。解除不会增加商户余额。
        </p>
        <div class="toolbar-actions">
          <button
            type="button"
            :disabled="busy"
            @click="confirmRemaining = false"
          >
            取消剩余解除</button
          ><button
            type="button"
            class="danger-button"
            :disabled="busy || disabled"
            @click="releaseRemaining"
          >
            确认解除本次剩余额度
          </button>
        </div>
      </template>
    </div>
  </section>
</template>
<style scoped>
.preauthorization-observation {
  min-width: 0;
  overflow-wrap: anywhere;
  border-top: 1px solid var(--line);
  padding-top: 16px;
  margin: 20px 0;
}
</style>
