<script setup lang="ts">
import { ref, computed, nextTick } from "vue";
import { ApiClient } from "../api";
import KitModal from "./KitModal.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitRadio from "./KitRadio.vue";
import FundsAuthorizationModal from "./FundsAuthorizationModal.vue";

// 后台加款 / 后台扣款 — the admin direct wallet credit/debit (epay_release
// changeUserMoney '后台加款'/'后台扣款'). Collects Pro's fields (direction, amount,
// reason) then hands the signed body to FundsAuthorizationModal, which runs the
// funds-password step and posts /workflows/balance-adjustments.
const props = defineProps<{ client: ApiClient; merchantID: string }>();
const emit = defineEmits<{ close: []; changed: [] }>();

const direction = ref<string>("credit");
const amount = ref("");
const reason = ref("");
const error = ref("");
const authBody = ref<Record<string, unknown> | null>(null);

// A stable idempotency key for this attempt; regenerated when the operator
// edits the fields so a changed amount/direction is a new business number.
const externalNo = ref("");
function freshExternalNo() {
  externalNo.value =
    "adj-" +
    props.merchantID +
    "-" +
    Date.now().toString(36) +
    "-" +
    Math.random().toString(36).slice(2, 8);
}
freshExternalNo();

const amountValid = computed(
  () =>
    /^(0|[1-9]\d{0,9})(\.\d{1,2})?$/.test(amount.value) &&
    Number(amount.value) > 0,
);
const canSubmit = computed(
  () =>
    amountValid.value &&
    reason.value.trim().length > 0 &&
    reason.value.length <= 240,
);

function edited() {
  // Any field change invalidates a prepared authorization and its idempotency key.
  if (authBody.value) {
    authBody.value = null;
    freshExternalNo();
  }
  error.value = "";
}

const target = computed(
  () => "/console/v1/admin/workflows/balance-adjustments",
);

function submit() {
  if (!canSubmit.value) {
    error.value = !amountValid.value
      ? "请输入正数金额，最多两位小数"
      : "请填写调整原因（不超过240字）";
    return;
  }
  authBody.value = {
    merchant_id: props.merchantID,
    direction: direction.value,
    amount: amount.value,
    reason: reason.value.trim(),
    external_no: externalNo.value,
  };
}

function onCompleted() {
  emit("changed");
  emit("close");
}
function onAuthClose() {
  // Return to the field form so the operator can retry/cancel.
  authBody.value = null;
  void nextTick();
}
</script>
<template>
  <KitModal
    v-if="!authBody"
    :title="direction === 'credit' ? '后台加款' : '后台扣款'"
    submit-text="下一步：资金密码"
    :danger="direction === 'debit'"
    @close="emit('close')"
    @submit="submit"
  >
    <p class="pa-help">商户号：{{ merchantID }}</p>
    <KitField label="调整方向" required>
      <div class="adjust-directions">
        <KitRadio
          v-model="direction"
          value="credit"
          label="后台加款"
          @change="edited"
        />
        <KitRadio
          v-model="direction"
          value="debit"
          label="后台扣款"
          @change="edited"
        />
      </div>
    </KitField>
    <KitField label="金额（元）" required>
      <KitInput
        v-model="amount"
        inputmode="decimal"
        placeholder="如 100.00"
        @update:modelValue="edited"
      />
    </KitField>
    <KitField label="原因" required>
      <KitInput
        v-model="reason"
        :maxlength="240"
        placeholder="记录到资金明细与审计"
        @update:modelValue="edited"
      />
    </KitField>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <template #footer-extra>
      <span class="pa-help"
        >加款从平台清算入商户余额，扣款反向且不会透支。</span
      >
    </template>
  </KitModal>
  <FundsAuthorizationModal
    v-else
    :client="client"
    :target="target"
    :body="authBody"
    @completed="onCompleted"
    @failed="onAuthClose"
    @close="onAuthClose"
  />
</template>
<style scoped>
.adjust-directions {
  display: flex;
  gap: var(--space-4);
}
.pa-help {
  font-size: 12px;
  color: var(--ink-muted);
}
</style>
