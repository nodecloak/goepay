<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from "vue";
import { ApiClient, ApiError, displayValue } from "../api";
const props = defineProps<{
  client: ApiClient;
  tradeNo: string;
  orderStatus: unknown;
  allowed: boolean;
  notification?: Record<string, unknown> | null;
  disabled?: boolean;
}>();
const emit = defineEmits<{ updated: [] }>();
const requestNo = ref(""),
  confirming = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  failed = ref(false),
  uncertain = ref(false),
  result = ref<Record<string, unknown> | null>(null),
  retryAt = ref(0),
  now = ref(Date.now());
const timer = setInterval(() => (now.value = Date.now()), 1000);
onBeforeUnmount(() => clearInterval(timer));
const allowed = computed(
  () => props.allowed === true && props.orderStatus === "paid",
);
const cooldown = computed(() =>
  Math.max(0, Math.ceil((retryAt.value - now.value) / 1000)),
);
const blocked = computed(
  () => !!props.disabled || busy.value || cooldown.value > 0,
);
const terminal = computed(
  () =>
    !uncertain.value &&
    ["succeeded", "dead", "failed", "cancelled"].includes(
      String(result.value?.state),
    ),
);
const current = computed(() => result.value || props.notification);
watch(
  () => props.notification,
  (value) => {
    if (
      value &&
      result.value &&
      String(value.job_id) === String(result.value.job_id)
    )
      result.value = { ...result.value, ...value };
  },
);
function open(newIntent: boolean) {
  if (!allowed.value || blocked.value) return;
  if (newIntent) {
    if (requestNo.value && !terminal.value) return;
    requestNo.value = crypto.randomUUID();
    result.value = null;
    failed.value = false;
    uncertain.value = false;
  }
  if (!requestNo.value) return;
  error.value = "";
  notice.value = "";
  confirming.value = true;
}
async function send() {
  if (!allowed.value || blocked.value || !requestNo.value) return;
  busy.value = true;
  confirming.value = false;
  error.value = "";
  notice.value = "";
  uncertain.value = true;
  failed.value = false;
  try {
    const response = await props.client.requestResult<Record<string, unknown>>(
      `/orders/${encodeURIComponent(props.tradeNo)}/resend-notification`,
      { method: "POST", body: { external_no: requestNo.value } },
    );
    result.value = response.data;
    uncertain.value = false;
    notice.value =
      response.status === 202
        ? "通知已排队或正在处理中，尚未确认商户收到。请核对原通知请求，不要创建重复发送。"
        : response.data?.state === "succeeded"
          ? "商户通知已收到成功响应，请核对任务记录。"
          : ["dead", "failed", "cancelled"].includes(
                String(response.data?.state),
              )
            ? `通知任务已终止（${displayValue(response.data?.state)}），尚未确认商户收到。`
            : "通知结果尚未核实，请核对原通知请求。";
  } catch (e) {
    failed.value = true;
    if (e instanceof ApiError && e.status === 429) {
      now.value = Date.now();
      if (e.retryAfterSeconds !== undefined)
        retryAt.value = now.value + e.retryAfterSeconds * 1000;
      error.value = `当前通知重发处于冷却期${e.retryAfterSeconds === undefined ? "" : `，请等待 ${e.retryAfterSeconds} 秒`}。保留原请求号后重试。${e.message}`;
    } else
      error.value = `通知请求结果未知，请保留原请求号并重试原请求，避免创建新的发送。${e instanceof Error ? e.message : "请求失败"}`;
  } finally {
    busy.value = false;
    emit("updated");
  }
}
</script>
<template>
  <section class="notification-panel">
    <h3>商户通知</h3>
    <dl v-if="current" class="record-detail">
      <template
        v-for="(label, key) in {
          job_id: '通知任务编号',
          state: '通知任务状态',
          attempts: '尝试次数',
          run_at: '计划执行时间',
          created_at: '创建时间',
        }"
        :key="key"
        ><dt>{{ label }}</dt>
        <dd>{{ displayValue(current[key]) }}</dd></template
      >
    </dl>
    <p v-else class="muted">尚无通知任务记录。</p>
    <p class="muted">
      仅已支付且服务端允许的订单可请求重发。排队、处理中或请求成功不等于商户已收到，只有任务
      succeeded 表示收到商户成功响应；执行中的通知会合并到原任务。
    </p>
    <div v-if="error" role="alert" class="alert error">{{ error }}</div>
    <div v-if="notice" role="status" class="alert success">{{ notice }}</div>
    <p v-if="cooldown > 0" class="muted">
      冷却剩余 {{ cooldown }} 秒，暂不能提交。
    </p>
    <label v-if="requestNo"
      >本次通知幂等请求号<input :value="requestNo" readonly
    /></label>
    <div v-if="allowed" class="toolbar-actions">
      <button v-if="!requestNo" :disabled="blocked" @click="open(true)">
        重发商户通知</button
      ><button v-else :disabled="blocked" @click="open(false)">
        {{ failed ? "重试原通知请求" : "核对原通知请求" }}</button
      ><button
        v-if="requestNo && terminal"
        :disabled="blocked"
        @click="open(true)"
      >
        发起新的通知重发
      </button>
    </div>
    <div v-if="confirming && allowed" class="alert">
      <h4>核对商户通知重发</h4>
      <p>
        订单 {{ tradeNo }} 将使用服务端保存的商户通知地址。原请求号
        {{
          requestNo
        }}
        会持久去重；网络错误时保留此请求号，只有明确的新操作才创建新号。
      </p>
      <div class="toolbar-actions">
        <button :disabled="busy" @click="confirming = false">
          取消通知请求</button
        ><button :disabled="blocked" @click="send">确认通知请求</button>
      </div>
    </div>
  </section>
</template>
