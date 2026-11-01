<script setup lang="ts">
import { computed } from "vue";
import CopyButton from "./CopyButton.vue";
import { isPersonalMerchantID } from "../merchant-personal-settings";

const props = defineProps<{ merchantId: unknown }>();
const rootAddress = location.origin + "/";
const submitAddress = location.origin + "/api/pay/submit";
const merchantID = computed(() => {
  const value =
    typeof props.merchantId === "number" &&
    Number.isSafeInteger(props.merchantId)
      ? String(props.merchantId)
      : props.merchantId;
  return isPersonalMerchantID(value) ? value : "";
});
</script>
<template>
  <div class="merchant-connection-info">
    <dl class="record-detail">
      <dt>接口地址</dt>
      <dd class="connection-value">
        <code>{{ rootAddress }}</code>
        <CopyButton :text="rootAddress" label="接口地址" />
      </dd>
      <dt>支付提交地址</dt>
      <dd class="connection-value">
        <code>{{ submitAddress }}</code>
        <CopyButton :text="submitAddress" label="支付提交地址" />
      </dd>
      <dt>商户 ID</dt>
      <dd class="connection-value">
        <code v-if="merchantID">{{ merchantID }}</code>
        <span v-else>暂未读取</span>
        <CopyButton
          v-if="merchantID"
          :key="merchantID"
          :text="merchantID"
          label="商户ID"
        />
      </dd>
    </dl>
    <nav class="connection-docs" aria-label="接口接入文档">
      <a href="/doc/" target="_blank" rel="noopener noreferrer">开发文档</a>
      <a href="/doc/pay_submit.html" target="_blank" rel="noopener noreferrer"
        >支付提交文档</a
      >
    </nav>
  </div>
</template>
<style scoped>
.merchant-connection-info {
  min-width: 0;
}
.connection-value {
  display: flex;
  align-items: center;
  min-width: 0;
}
.connection-value code {
  overflow-wrap: anywhere;
  user-select: all;
}
.connection-value :deep(.pa-copy-btn) {
  flex: none;
}
.connection-docs {
  display: flex;
  gap: 12px 24px;
  flex-wrap: wrap;
  margin: 12px 0 20px;
}
@media (max-width: 600px) {
  .merchant-connection-info .record-detail {
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
  }
  .merchant-connection-info dt:not(:first-child) {
    margin-top: 8px;
  }
}
</style>
