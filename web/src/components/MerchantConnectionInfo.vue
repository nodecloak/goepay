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
    <dl class="connection-fields">
      <div>
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
      </div>
      <div>
        <dt>接口地址</dt>
        <dd class="connection-value">
          <code>{{ rootAddress }}</code>
          <CopyButton :text="rootAddress" label="接口地址" />
        </dd>
      </div>
      <div>
        <dt>支付提交地址</dt>
        <dd class="connection-value">
          <code>{{ submitAddress }}</code>
          <CopyButton :text="submitAddress" label="支付提交地址" />
        </dd>
      </div>
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
.connection-fields {
  display: grid;
  gap: var(--space-3);
  margin: 0;
}
.connection-fields dt {
  margin-bottom: var(--space-1);
  color: var(--muted);
  font-size: 13px;
}
.connection-value {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  min-height: 40px;
  margin: 0;
  padding: 3px var(--space-2) 3px var(--space-3);
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: var(--bg-raised);
}
.connection-value code {
  overflow-wrap: anywhere;
  font-family: var(--font-mono);
  user-select: all;
}
.connection-value :deep(.pa-copy-btn) {
  flex: none;
  width: var(--control-h);
  height: var(--control-h);
  margin: 0;
}
.connection-docs {
  display: flex;
  gap: 12px 24px;
  flex-wrap: wrap;
  margin-top: var(--space-4);
}
</style>
