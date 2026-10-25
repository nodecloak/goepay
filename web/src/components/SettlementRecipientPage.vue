<script setup lang="ts">
import { ref } from "vue";
import type { Area } from "../menu";
import { ApiClient } from "../api";
import SettlementRecipientPanel from "./SettlementRecipientPanel.vue";
defineProps<{ client: ApiClient; area: Area }>();
const merchantID = ref("");
</script>
<template>
  <section class="panel records">
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
</template>
