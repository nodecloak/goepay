<script setup lang="ts">
import { ref } from "vue";
import { ApiClient, displayValue } from "../api";
const props = defineProps<{
  client: ApiClient;
  id: string;
  enabled: boolean;
}>();
const tradeNo = ref(""),
  confirm = ref(false),
  busy = ref(false),
  error = ref(""),
  result = ref<Record<string, unknown> | null>(null);
async function execute() {
  busy.value = true;
  error.value = "";
  try {
    result.value = await props.client.request(
      `/workflows/profitsharing-rules/${encodeURIComponent(props.id)}/execute`,
      { method: "POST", body: { trade_no: tradeNo.value } },
    );
    confirm.value = false;
  } catch (e) {
    error.value = e instanceof Error ? e.message : "分账申请失败";
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <section class="case-panel">
    <h3>按已支付订单申请站内分账</h3>
    <p class="muted">
      金额由服务端根据已支付订单和当前规则比例计算。提交后预占余额，须经管理员审批；本流程不调用上游渠道分账。
    </p>
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="result" class="alert success" role="status">
      站内分账申请 {{ displayValue(result.id) }} 已提交，金额
      {{ displayValue(result.amount) }} 元，状态
      {{ displayValue(result.status) }}。请核实审批记录。
    </div>
    <form @submit.prevent="confirm = true">
      <label
        >已支付的平台订单号<input
          v-model="tradeNo"
          required
          maxlength="64"
          :disabled="!enabled || busy" /></label
      ><button :disabled="!enabled || busy">核对并申请站内分账</button>
      <p v-if="!enabled" class="muted">当前规则未启用，请先核实配置。</p>
    </form>
    <div v-if="confirm" class="alert">
      <p>确认对订单 {{ tradeNo }} 按当前启用规则申请站内分账？</p>
      <div class="toolbar-actions">
        <button :disabled="busy" @click="confirm = false">取消</button
        ><button class="primary" :disabled="busy" @click="execute">
          {{ busy ? "正在提交…" : "确认申请站内分账" }}
        </button>
      </div>
    </div>
  </section>
</template>
