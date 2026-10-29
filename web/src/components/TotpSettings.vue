<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
import QRCode from "qrcode";
import { ApiClient } from "../api";
import KitModal from "./KitModal.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
const props = defineProps<{ client: ApiClient }>();
const emit = defineEmits<{ reauthenticate: [] }>();
const enabled = ref(false),
  loaded = ref(false),
  busy = ref(false),
  error = ref(""),
  currentPassword = ref(""),
  code = ref(""),
  mode = ref<"enable" | "disable" | null>(null),
  setup = ref<{
    secret: string;
    otpauth_uri: string;
    expires_at: string;
  } | null>(null),
  qr = ref("");
const submitText = computed(() => {
  if (busy.value) return "正在验证…";
  if (mode.value === "enable" && !setup.value) return "验证密码并生成绑定信息";
  return mode.value === "disable" ? "确认关闭" : "确认绑定";
});
function submitModal() {
  if (mode.value === "enable" && !setup.value) void prepare();
  else void update();
}
function reset() {
  mode.value = null;
  currentPassword.value = "";
  code.value = "";
  setup.value = null;
  qr.value = "";
}
async function status() {
  try {
    const data = await props.client.request<{ enabled: boolean }>(
      "/security/totp",
    );
    enabled.value = data.enabled;
    loaded.value = true;
  } catch (e) {
    error.value = e instanceof Error ? e.message : "无法读取动态验证码设置";
  }
}
async function prepare() {
  busy.value = true;
  error.value = "";
  try {
    setup.value = await props.client.request("/security/totp/prepare", {
      method: "POST",
      body: { current_password: currentPassword.value },
    });
    currentPassword.value = "";
    qr.value = await QRCode.toDataURL(setup.value!.otpauth_uri, {
      width: 230,
      margin: 2,
    });
  } catch (e) {
    error.value = e instanceof Error ? e.message : "无法准备身份验证设置";
  } finally {
    busy.value = false;
  }
}
async function update() {
  if (!/^\d{6}$/.test(code.value)) {
    error.value = "请输入 6 位动态验证码";
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    await props.client.request(
      "/security/totp/" + (mode.value === "disable" ? "disable" : "confirm"),
      {
        method: "POST",
        body:
          mode.value === "disable"
            ? { current_password: currentPassword.value, code: code.value }
            : { code: code.value },
      },
    );
    reset();
    emit("reauthenticate");
  } catch (e) {
    error.value = e instanceof Error ? e.message : "验证码验证失败";
  } finally {
    busy.value = false;
  }
}
onMounted(status);
onBeforeUnmount(reset);
</script>
<template>
  <section class="panel account-section">
    <h2>动态验证码保护</h2>
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <template v-if="loaded"
      ><p class="muted">
        {{
          enabled
            ? "已启用。登录时须同时输入密码与验证器中的 6 位验证码。"
            : "尚未启用。绑定身份验证器后，登录需要额外输入动态验证码。"
        }}
      </p>
      <button
        class="pa-btn"
        :class="enabled ? 'pa-btn--danger' : 'pa-btn--primary'"
        :disabled="busy"
        @click="
          mode = enabled ? 'disable' : 'enable';
          error = '';
        "
      >
        {{ enabled ? "关闭动态验证码" : "绑定身份验证器" }}
      </button></template
    >
    <KitModal
      v-if="mode"
      :title="mode === 'enable' ? '绑定身份验证器' : '关闭动态验证码'"
      :loading="busy"
      :danger="mode === 'disable'"
      :submit-text="submitText"
      @close="reset"
      @submit="submitModal"
    >
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <template v-if="mode === 'enable' && !setup">
        <KitField label="当前登录密码" required
          ><KitInput
            v-model="currentPassword"
            type="password"
            required
            :maxlength="1024"
            autocomplete="current-password"
            :disabled="busy"
        /></KitField>
      </template>
      <template v-else>
        <template v-if="setup">
          <p>
            使用身份验证器扫描二维码，然后输入生成的验证码。绑定密钥只在此次设置过程中展示。
          </p>
          <img
            v-if="qr"
            :src="qr"
            width="230"
            height="230"
            alt="身份验证器绑定二维码"
          />
          <KitField label="手动绑定密钥"
            ><KitInput
              :model-value="setup.secret"
              autocomplete="off"
              :disabled="true"
          /></KitField>
          <p class="muted">绑定信息有效期至 {{ setup.expires_at }}</p>
        </template>
        <KitField v-if="mode === 'disable'" label="当前登录密码" required
          ><KitInput
            v-model="currentPassword"
            type="password"
            required
            :maxlength="1024"
            autocomplete="current-password"
            :disabled="busy"
        /></KitField>
        <KitField label="6 位动态验证码" required
          ><KitInput
            v-model="code"
            required
            inputmode="numeric"
            pattern="[0-9]{6}"
            :maxlength="6"
            autocomplete="one-time-code"
            :disabled="busy"
        /></KitField>
        <p class="muted">
          修改后将退出当前账户的全部登录会话。绑定成功后，请等待验证器生成下一组验证码再登录。
        </p>
      </template>
    </KitModal>
  </section>
</template>
