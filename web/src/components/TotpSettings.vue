<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from "vue";
import QRCode from "qrcode";
import { ApiClient } from "../api";
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
        :disabled="busy"
        @click="
          mode = enabled ? 'disable' : 'enable';
          error = '';
        "
      >
        {{ enabled ? "关闭动态验证码" : "绑定身份验证器" }}
      </button></template
    >
    <div v-if="mode" class="modal-backdrop">
      <section
        class="modal panel"
        role="dialog"
        aria-modal="true"
        :aria-label="mode === 'enable' ? '绑定身份验证器' : '关闭动态验证码'"
      >
        <h2>{{ mode === "enable" ? "绑定身份验证器" : "关闭动态验证码" }}</h2>
        <div v-if="error" class="alert error" role="alert">{{ error }}</div>
        <form v-if="mode === 'enable' && !setup" @submit.prevent="prepare">
          <label
            >当前登录密码<input
              v-model="currentPassword"
              type="password"
              required
              maxlength="1024"
              autocomplete="current-password"
          /></label>
          <div class="modal-actions">
            <button type="button" :disabled="busy" @click="reset">取消</button
            ><button class="primary" :disabled="busy">
              验证密码并生成绑定信息
            </button>
          </div>
        </form>
        <form v-else @submit.prevent="update">
          <template v-if="setup"
            ><p>
              使用身份验证器扫描二维码，然后输入生成的验证码。绑定密钥只在此次设置过程中展示。
            </p>
            <img
              v-if="qr"
              :src="qr"
              width="230"
              height="230"
              alt="身份验证器绑定二维码"
            /><label
              >手动绑定密钥<input
                :value="setup.secret"
                readonly
                autocomplete="off"
            /></label>
            <p class="muted">
              绑定信息有效期至 {{ setup.expires_at }}
            </p></template
          ><label v-if="mode === 'disable'"
            >当前登录密码<input
              v-model="currentPassword"
              type="password"
              required
              maxlength="1024"
              autocomplete="current-password" /></label
          ><label
            >6 位动态验证码<input
              v-model="code"
              required
              inputmode="numeric"
              pattern="[0-9]{6}"
              maxlength="6"
              autocomplete="one-time-code"
          /></label>
          <p class="muted">
            修改后将退出当前账户的全部登录会话。绑定成功后，请等待验证器生成下一组验证码再登录。
          </p>
          <div class="modal-actions">
            <button type="button" :disabled="busy" @click="reset">取消</button
            ><button class="primary" :disabled="busy">
              {{
                busy ? "正在验证…" : mode === "enable" ? "确认绑定" : "确认关闭"
              }}
            </button>
          </div>
        </form>
      </section>
    </div>
  </section>
</template>
