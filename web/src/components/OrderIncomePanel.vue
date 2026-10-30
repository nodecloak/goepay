<script setup lang="ts">
import { ref, computed, watch, onMounted } from "vue";
import { ApiClient, displayValue } from "../api";
const props = defineProps<{
  client: ApiClient;
  tradeNo: string;
  hold?: Record<string, unknown> | null;
  freezeAmount?: unknown;
  canFreeze: boolean;
  canUnfreeze: boolean;
  canAdopt?: boolean;
  hasPendingRefund?: boolean;
  disabled?: boolean;
  initialAction?: "freeze" | "unfreeze";
}>();
const emit = defineEmits<{ updated: []; busy: [value: boolean] }>();
const sourceLabels: Record<string, string> = {
  fresh: "新冻结专用款",
  legacy_adopt: "已核对迁移的历史冻结款",
  legacy_unprepared: "历史冻结款待核对",
};
const sourceLabel = (value: unknown) =>
  sourceLabels[String(value)] || "来源尚未核实";
const confirmation = ref<"freeze" | "unfreeze" | "adopt" | null>(null),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  freezeID = ref(""),
  freezeDone = ref(false),
  unfreezeID = ref(""),
  unfreezeDone = ref(false),
  adoptID = ref(""),
  adoptHoldID = ref(""),
  adoptDone = ref(false),
  unfreezeSnapshot = ref<Record<string, unknown>>({});
const admin = computed(() => props.client.area === "admin"),
  freezeAllowed = computed(() => admin.value && props.canFreeze === true),
  refundPending = computed(
    () =>
      props.hasPendingRefund === true ||
      String(props.hold?.refund_pending_count ?? "0") !== "0" ||
      String(props.hold?.refund_reserved_amount ?? "0") !== "0",
  ),
  adoptAllowed = computed(
    () =>
      admin.value &&
      props.canAdopt === true &&
      props.hold?.state === "held" &&
      props.hold?.source_mode === "legacy_unprepared",
  ),
  unfreezeAllowed = computed(
    () =>
      admin.value &&
      props.canUnfreeze === true &&
      props.hold?.state === "held" &&
      !refundPending.value,
  );
watch(
  () => props.hold,
  (hold) => {
    if (hold?.external_no === freezeID.value) freezeDone.value = true;
    if (String(hold?.id) === unfreezeID.value && hold?.state === "released")
      unfreezeDone.value = true;
    if (
      String(hold?.id) === adoptHoldID.value &&
      hold?.source_mode === "legacy_adopt"
    )
      adoptDone.value = true;
  },
);
function prepare(action: "freeze" | "unfreeze" | "adopt") {
  if (busy.value || props.disabled) return;
  if (action === "freeze") {
    if (!freezeAllowed.value) return;
    if (!freezeID.value || freezeDone.value) {
      freezeID.value = crypto.randomUUID();
      freezeDone.value = false;
    }
  } else if (action === "adopt") {
    if (!adoptAllowed.value) return;
    if (!adoptID.value || adoptDone.value) {
      adoptID.value = crypto.randomUUID();
      adoptHoldID.value = String(props.hold!.id);
      adoptDone.value = false;
    }
  } else {
    if (!unfreezeAllowed.value) return;
    if (!unfreezeID.value || unfreezeDone.value) {
      unfreezeID.value = String(props.hold!.id);
      unfreezeSnapshot.value = { ...props.hold };
      unfreezeDone.value = false;
    }
  }
  error.value = "";
  notice.value = "";
  confirmation.value = action;
}
onMounted(() => {
  if (props.initialAction) prepare(props.initialAction);
});
async function execute() {
  const action = confirmation.value;
  if (!action || busy.value || props.disabled || !admin.value) return;
  if (
    (action === "freeze" && !freezeAllowed.value) ||
    (action === "unfreeze" && !unfreezeAllowed.value) ||
    (action === "adopt" && !adoptAllowed.value)
  )
    return;
  busy.value = true;
  emit("busy", true);
  confirmation.value = null;
  error.value = "";
  notice.value = "";
  try {
    const hold = await props.client.request<Record<string, unknown>>(
      `/orders/${encodeURIComponent(props.tradeNo)}/${action === "freeze" ? "freeze-income" : action === "adopt" ? "adopt-income-hold" : "unfreeze-income"}`,
      {
        method: "POST",
        body:
          action === "freeze"
            ? { external_no: freezeID.value }
            : action === "adopt"
              ? { hold_id: adoptHoldID.value, external_no: adoptID.value }
              : { hold_id: unfreezeID.value },
      },
    );
    if (action === "freeze") {
      freezeDone.value = true;
      notice.value =
        hold.state === "held"
          ? "平台订单收入已冻结。退款将按该订单的专用资金来源预留，订单仍保持已支付。"
          : "原冻结请求已核对，请查看其批次状态；不会重新冻结已释放的原请求。";
    } else if (action === "adopt") {
      adoptDone.value = true;
      notice.value =
        hold.source_mode === "legacy_adopt"
          ? "历史冻结款已核对并迁入该订单专用来源，请重新读取订单后退款。"
          : "请重新读取原冻结批次，核对迁移结果。";
    } else {
      unfreezeDone.value = true;
      notice.value =
        hold.state === "released"
          ? "指定平台收入冻结批次已解冻，请核对当前批次和账本。"
          : "解冻结果尚未核实，请核对原冻结批次。";
    }
  } catch (e) {
    error.value =
      action === "freeze"
        ? `冻结结果尚未核实，请保留原冻结请求号 ${freezeID.value} 后重试原请求。${e instanceof Error ? e.message : "请求失败"}`
        : action === "adopt"
          ? `迁移结果尚未核实，请保留原请求号 ${adoptID.value} 和冻结批次 ${adoptHoldID.value}，重试原请求。${e instanceof Error ? e.message : "请求失败"}`
          : `解冻结果尚未核实，请保留原冻结批次 ${unfreezeID.value}，不能自动切换到新批次。${e instanceof Error ? e.message : "请求失败"}`;
  } finally {
    busy.value = false;
    emit("updated");
    emit("busy", false);
  }
}
</script>
<template>
  <section>
    <h3>平台订单收入冻结</h3>
    <p class="muted">
      冻结商户在本平台账本内的订单剩余净收入，不是买家预授权，不调用支付机构，也不改变已支付状态。
    </p>
    <dl v-if="hold" class="record-detail">
      <template
        v-for="(label, key) in {
          id: '冻结批次',
          state: '收入冻结状态',
          external_no: '冻结请求号',
          amount: '本批冻结金额（分）',
          merchant_net: '原商户净收入（分）',
          refund_merchant_amount: '已退商户净额（分）',
          remaining_amount: '本批剩余冻结款（分）',
          refund_reserved_amount: '退款预留冻结款（分）',
          refund_succeeded_amount: '已退款冻结款（分）',
          source_mode: '冻结款来源类型',
        }"
        :key="key"
        ><dt>{{ label }}</dt>
        <dd>
          {{
            key === "source_mode"
              ? sourceLabel(hold[key])
              : displayValue(hold[key])
          }}
        </dd></template
      >
    </dl>
    <p v-if="hold?.state === 'held' && refundPending" class="alert">
      退款结果尚未确定，相关冻结款继续预留；请查询原退款，不能解冻或重复退还。
    </p>
    <p
      v-else-if="
        hold?.state === 'held' && hold?.source_mode === 'legacy_unprepared'
      "
      class="alert"
    >
      历史冻结款尚未建立专用退款来源。超级管理员核对迁移后可使用原订单冻结款退款，也可按原规则解冻。
    </p>
    <p v-else-if="hold?.state === 'held'" class="muted">
      退款优先使用该订单剩余冻结款；不足部分按退款规则从商户可用余额预留。解冻仅释放本批剩余款。
    </p>
    <div v-if="error" role="alert" class="alert error">{{ error }}</div>
    <div v-if="notice" role="status" class="alert success">{{ notice }}</div>
    <div class="toolbar-actions">
      <button
        v-if="adoptAllowed"
        :disabled="busy || disabled"
        @click="prepare('adopt')"
      >
        {{
          adoptID && !adoptDone ? "重试原迁移请求" : "准备历史冻结款退款来源"
        }}
      </button>
      <button
        v-if="freezeAllowed"
        :disabled="busy || disabled"
        @click="prepare('freeze')"
      >
        {{
          freezeID && !freezeDone ? "重试原冻结请求" : "冻结订单收入"
        }}</button
      ><button
        v-if="unfreezeAllowed"
        :disabled="busy || disabled"
        @click="prepare('unfreeze')"
      >
        {{ unfreezeID && !unfreezeDone ? "重试原解冻请求" : "解冻订单收入" }}
      </button>
    </div>
    <div v-if="confirmation" class="alert">
      <h4>
        {{
          confirmation === "freeze"
            ? "确认冻结平台收入"
            : confirmation === "adopt"
              ? "核对并迁移历史冻结款"
              : "确认解冻指定批次"
        }}
      </h4>
      <dl class="record-detail">
        <dt>订单号</dt>
        <dd class="pa-order-no">{{ tradeNo }}</dd>
        <dt>金额（分）</dt>
        <dd>
          {{
            displayValue(
              confirmation === "freeze"
                ? freezeAmount
                : confirmation === "adopt"
                  ? hold?.amount
                  : (unfreezeSnapshot.remaining_amount ??
                    unfreezeSnapshot.amount),
            )
          }}
        </dd>
        <dt>{{ confirmation === "freeze" ? "冻结请求号" : "原冻结批次" }}</dt>
        <dd>
          {{
            confirmation === "freeze"
              ? freezeID
              : confirmation === "adopt"
                ? adoptHoldID
                : unfreezeID
          }}
        </dd>
        <template v-if="confirmation === 'adopt'"
          ><dt>原迁移请求号</dt>
          <dd>{{ adoptID }}</dd></template
        >
      </dl>
      <p>
        本次操作只调整指定订单收入冻结批次。网络结果未知时保留原请求号或批次，不创建新循环。
      </p>
      <p v-if="confirmation === 'adopt'">
        服务端将核对历史冻结凭证与账本总额。此操作要求本人超级管理员资金密码授权，不会增加余额或发起外部退款。
      </p>
      <div class="toolbar-actions">
        <button :disabled="busy" @click="confirmation = null">
          取消收入操作</button
        ><button
          class="danger-button"
          :disabled="
            busy ||
            disabled ||
            (confirmation === 'unfreeze' && !unfreezeAllowed) ||
            (confirmation === 'adopt' && !adoptAllowed)
          "
          @click="execute"
        >
          {{
            confirmation === "freeze"
              ? "确认冻结收入"
              : confirmation === "adopt"
                ? "确认迁移指定冻结款"
                : "确认解冻收入"
          }}
        </button>
      </div>
    </div>
  </section>
</template>
