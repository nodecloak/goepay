<script setup lang="ts">
import { nextTick, ref, watch } from "vue";
import { RouterLink } from "vue-router";
import type { ApiClient } from "../api";
import AccountPage from "./AccountPage.vue";
import TotpSettings from "./TotpSettings.vue";
import AccountLoginLogDialog from "./AccountLoginLogDialog.vue";
import MerchantAPISwitches from "./MerchantAPISwitches.vue";
import MerchantPersonalSettings from "./MerchantPersonalSettings.vue";
import BuyerFeeSettings104 from "./BuyerFeeSettings104.vue";
const props = defineProps<{
  client: ApiClient;
  ownSession: boolean;
  merchantId: string;
}>();
const emit = defineEmits<{ reauthenticate: [message: string] }>();
const logsOpen = ref(false),
  epoch = ref(0);
let caller: HTMLElement | undefined;
function openLogs() {
  if (!props.ownSession) return;
  caller =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : undefined;
  logsOpen.value = true;
}
function closeLogs() {
  logsOpen.value = false;
  void nextTick(() => {
    if (caller?.isConnected) caller.focus();
  });
}
watch(
  () => [props.client, props.client.csrfToken, props.ownSession],
  () => {
    epoch.value++;
    logsOpen.value = false;
    caller = undefined;
  },
  { flush: "sync" },
);
</script>
<template>
  <div class="merchant-account-settings">
    <div class="merchant-account-profile">
      <AccountPage
        :key="'account' + epoch"
        :client="client"
        section="profile"
        :show-login-log="ownSession"
        @login-log="openLogs"
        @reauthenticate="
          (message) =>
            emit('reauthenticate', message || '密码已修改，请重新登录')
        "
      />
    </div>
    <section
      class="panel merchant-account-functions"
      aria-label="联系方式与功能设置"
    >
      <h2>联系方式与功能设置</h2>
      <div class="merchant-function-grid">
        <MerchantPersonalSettings
          class="merchant-account-personal"
          section="contact"
          :client="client"
          :own-session="ownSession"
          :merchant-id="merchantId"
        />
        <BuyerFeeSettings104
          class="merchant-account-fees"
          kind="merchant"
          :client="client"
          :allowed="ownSession"
          :merchant-id="merchantId"
        />
        <MerchantAPISwitches
          class="merchant-account-api"
          :client="client"
          :own-session="ownSession"
          :merchant-id="merchantId"
        />
      </div>
    </section>
    <div class="merchant-account-security">
      <TotpSettings
        v-if="ownSession"
        :key="'totp' + epoch"
        :client="client"
        @reauthenticate="
          emit('reauthenticate', '身份验证设置已更新，请重新登录')
        "
      />
      <p v-else class="panel muted">
        安全设置和本人登录日志需要商户本人登录后查看。
      </p>
    </div>
    <nav class="panel merchant-account-shortcuts" aria-label="消息与打印设置">
      <RouterLink to="/merchant/account/notice">消息提醒设置</RouterLink>
      <RouterLink to="/merchant/account/printing">订单打印设置</RouterLink>
    </nav>
    <AccountLoginLogDialog
      v-if="logsOpen"
      :key="'logs' + epoch"
      :client="client"
      :own-session="ownSession"
      @close="closeLogs"
    />
  </div>
</template>
<style scoped>
.merchant-account-settings {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 18px;
  align-items: start;
}
.merchant-account-profile,
.merchant-account-functions,
.merchant-account-security {
  min-width: 0;
}
.merchant-account-profile :deep(.panel),
.merchant-account-security > :deep(.panel),
.merchant-account-functions {
  padding: 20px;
}
.merchant-account-profile :deep(.account-pages),
.merchant-account-profile :deep(.account-section),
.merchant-account-security :deep(.account-section) {
  max-width: none;
  margin-bottom: 0;
}
.merchant-account-profile :deep(.panel > h2),
.merchant-account-security :deep(.panel > h2),
.merchant-account-functions > h2 {
  padding-bottom: 14px;
  margin-bottom: 18px;
  border-bottom: 1px solid var(--line);
}
.merchant-function-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 24px 32px;
  align-items: start;
}
.merchant-function-grid :deep(.panel) {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
  border-radius: 0;
  background: transparent;
}
.merchant-function-grid :deep(.merchant-account-api) {
  grid-column: 2;
  grid-row: 1 / span 2;
}
.merchant-function-grid :deep(.merchant-account-fees) {
  grid-column: 1;
}
.merchant-account-shortcuts {
  display: flex;
  flex-wrap: wrap;
  gap: 16px 24px;
  padding: 20px;
}
.merchant-account-shortcuts a {
  padding-block: 4px;
}
@media (max-width: 850px) {
  .merchant-function-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .merchant-function-grid :deep(.merchant-account-api),
  .merchant-function-grid :deep(.merchant-account-fees) {
    grid-column: auto;
    grid-row: auto;
  }
}
</style>
