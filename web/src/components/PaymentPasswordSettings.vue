<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from "vue";
import { ApiClient } from "../api";
import {
  readPaymentPasswordStatus,
  type PaymentPasswordStatus,
} from "../funds-authorization";
import { utf8Length } from "../enrollment";
const props = defineProps<{ client: ApiClient }>();
const emit = defineEmits<{ updated: [] }>();
const status = ref<PaymentPasswordStatus | null>(null),
  loading = ref(false),
  busy = ref(false),
  editing = ref(false),
  error = ref(""),
  notice = ref("");
const currentPassword = ref(""),
  currentPaymentPassword = ref(""),
  newPaymentPassword = ref(""),
  confirmation = ref(""),
  code = ref("");
let active = true;
const controller = new AbortController();
function clear() {
  currentPassword.value = "";
  currentPaymentPassword.value = "";
  newPaymentPassword.value = "";
  confirmation.value = "";
  code.value = "";
}
function cancel() {
  clear();
  editing.value = false;
  error.value = "";
}
async function load() {
  if (props.client.area !== "admin") return;
  loading.value = true;
  status.value = null;
  error.value = "";
  try {
    const raw = await props.client.request("/security/payment-password", {
      signal: controller.signal,
    });
    if (active) status.value = readPaymentPasswordStatus(raw);
  } catch (e) {
    if (active)
      error.value = e instanceof Error ? e.message : "资金安全状态读取失败";
  } finally {
    if (active) loading.value = false;
  }
}
async function save() {
  if (busy.value || !status.value || props.client.area !== "admin") return;
  error.value = "";
  notice.value = "";
  const bytes = utf8Length(newPaymentPassword.value);
  if (
    !currentPassword.value ||
    (status.value.configured && !currentPaymentPassword.value) ||
    bytes < 12 ||
    bytes > 1024 ||
    newPaymentPassword.value === currentPassword.value ||
    newPaymentPassword.value !== confirmation.value ||
    (status.value.totp_required && !/^\d{6}$/.test(code.value))
  ) {
    error.value =
      "请核对当前登录密码、已有资金密码、新密码确认及动态验证码；新资金密码须为 12–1024 字节且不同于登录密码。";
    clear();
    return;
  }
  busy.value = true;
  const body = {
    current_password: currentPassword.value,
    current_payment_password: currentPaymentPassword.value,
    new_payment_password: newPaymentPassword.value,
    code: code.value,
  };
  clear();
  try {
    await props.client.request("/security/payment-password", {
      method: "PUT",
      body,
      signal: controller.signal,
    });
    if (active) {
      editing.value = false;
      notice.value = "资金密码已更新，旧的未使用授权将失效。";
      await load();
      emit("updated");
    }
  } catch (e) {
    if (active)
      error.value =
        e instanceof Error
          ? e.message
          : "资金密码设置未确认，请重新读取状态后核对";
  } finally {
    clear();
    if (active) busy.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => {
  active = false;
  controller.abort();
  clear();
});
</script>
<template>
  <section
    v-if="client.area === 'admin'"
    class="panel account-section"
    aria-label="管理员资金密码"
  >
    <h2>管理员资金密码</h2>
    <p class="muted">
      每位管理员单独设置资金密码，没有默认密码。此处核验本人管理员登录密码，不使用商户密码。设置资金密码不会开放支付通道写权限。
    </p>
    <p v-if="loading" role="status">正在读取资金安全状态…</p>
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <p v-if="notice" role="status" class="alert">{{ notice }}</p>
    <template v-if="status">
      <p>
        {{
          status.configured ? "已设置本人资金密码" : "尚未设置本人资金密码"
        }}；{{
          status.enforced
            ? "平台已要求敏感资金操作二次授权"
            : "平台尚未强制资金二次授权"
        }}。
      </p>
      <button
        v-if="!editing"
        :disabled="busy || loading"
        @click="
          editing = true;
          clear();
          notice = '';
        "
      >
        {{ status.configured ? "修改资金密码" : "设置资金密码" }}
      </button>
      <form v-else @submit.prevent="save">
        <label
          >当前管理员登录密码<input
            v-model="currentPassword"
            name="current_password"
            type="password"
            autocomplete="current-password"
            :disabled="busy"
            required
        /></label>
        <label v-if="status.configured"
          >当前资金密码<input
            v-model="currentPaymentPassword"
            name="current_payment_password"
            type="password"
            autocomplete="off"
            :disabled="busy"
            required
        /></label>
        <label
          >新资金密码<input
            v-model="newPaymentPassword"
            name="new_payment_password"
            type="password"
            autocomplete="new-password"
            :disabled="busy"
            required
        /></label>
        <label
          >确认新资金密码<input
            v-model="confirmation"
            name="confirmation"
            type="password"
            autocomplete="new-password"
            :disabled="busy"
            required
        /></label>
        <label v-if="status.totp_required"
          >当前动态验证码<input
            v-model="code"
            name="code"
            inputmode="numeric"
            pattern="[0-9]{6}"
            maxlength="6"
            autocomplete="one-time-code"
            :disabled="busy"
            required
        /></label>
        <div class="toolbar-actions">
          <button type="button" :disabled="busy" @click="cancel">
            取消资金密码修改</button
          ><button class="primary" :disabled="busy">
            {{ busy ? "正在核验…" : "确认更新资金密码" }}
          </button>
        </div>
      </form>
    </template>
    <button v-if="!editing" :disabled="loading || busy" @click="load">
      刷新资金安全状态
    </button>
  </section>
</template>
