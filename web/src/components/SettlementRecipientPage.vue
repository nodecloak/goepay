<script setup lang="ts">
import { ref } from "vue";
import type { Area } from "../menu";
import { ApiClient } from "../api";
import SettlementRecipientPanel from "./SettlementRecipientPanel.vue";
import MerchantPersonalSettings from "./MerchantPersonalSettings.vue";
defineProps<{
  client: ApiClient;
  area: Area;
  merchantId?: string;
  ownSession?: boolean;
}>();
const merchantID = ref("");
</script>
<template>
  <div class="settlement-settings-page">
    <section
      class="panel records recipient-settings-card"
      aria-label="收款账号设置"
    >
      <h2>收款账号设置</h2>
      <p class="resource-description muted">
        收款资料用于后续结算申请的加密快照。修改配置不会更改旧申请，也不会执行外部付款。历史未记录的收款资料不会自动补成快照。
      </p>
      <label v-if="area === 'admin'"
        >商户 ID<input
          v-model="merchantID"
          inputmode="numeric"
          autocomplete="off"
      /></label>
      <SettlementRecipientPanel
        :client="client"
        :admin="area === 'admin'"
        :merchant-i-d="merchantID"
        :disabled="false"
      />
    </section>
    <MerchantPersonalSettings
      v-if="area === 'merchant'"
      class="settlement-reserve-card"
      :client="client"
      :merchant-id="merchantId || ''"
      :own-session="ownSession === true"
      section="settlement"
    />
  </div>
</template>
<style scoped>
.settlement-settings-page {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 18px;
}
.recipient-settings-card > h2 {
  padding: 20px;
  margin: 0;
  border-bottom: 1px solid var(--line);
}
.settlement-settings-page :deep(.settlement-reserve-card) {
  padding: 20px;
  min-width: 0;
}
</style>
