<script setup lang="ts">
import { nextTick, ref, watch } from "vue";
import type { ApiClient } from "../api";
import AccountPage from "./AccountPage.vue";
import TotpSettings from "./TotpSettings.vue";
import MerchantNoticeSettings from "./MerchantNoticeSettings.vue";
import PrintingSettingsPage from "./PrintingSettingsPage.vue";
import AccountLoginLogDialog from "./AccountLoginLogDialog.vue";
const props = defineProps<{ client: ApiClient; ownSession: boolean }>();
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
    <div class="merchant-account-column">
      <AccountPage
        :key="'account' + epoch"
        :client="client"
        section="profile"
        :show-login-log="ownSession"
        @login-log="openLogs"
        @reauthenticate="emit('reauthenticate', '密码已修改，请重新登录')"
      />
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
    <div class="merchant-account-column">
      <MerchantNoticeSettings
        :key="'notice' + epoch"
        :client="client"
        :own-session="ownSession"
        title="消息推送"
      />
      <PrintingSettingsPage
        :key="'printing' + epoch"
        :client="client"
        :own-session="ownSession"
        title="小票打印设置"
      />
    </div>
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
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 18px;
  align-items: start;
}
.merchant-account-column {
  min-width: 0;
}
.merchant-account-column :deep(.panel) {
  margin-bottom: 18px;
}
.merchant-account-column > :deep(.panel) {
  padding: 20px;
}
.merchant-account-column :deep(.panel > h2) {
  padding-bottom: 14px;
  margin-bottom: 18px;
  border-bottom: 1px solid var(--line);
}
@media (max-width: 1050px) {
  .merchant-account-settings {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
