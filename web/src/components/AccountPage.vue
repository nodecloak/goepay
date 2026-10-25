<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from "vue";
import { ApiClient, displayValue } from "../api";
import MerchantRSAKeyRotation from "./MerchantRSAKeyRotation.vue";
import { merchantFieldValue } from "../pro-console-fields";
const props = defineProps<{
  client: ApiClient;
  section: "profile" | "api-info" | "security";
  showLoginLog?: boolean;
}>();
const emit = defineEmits<{ reauthenticate: []; "login-log": [] }>();
const loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  info = ref<Record<string, unknown>>({}),
  profile = ref({ name: "", email: "" });
const payStatus = ref<unknown>(null);
const passwords = ref({
    current_password: "",
    new_password: "",
    confirmation: "",
  }),
  rotationOpen = ref(false),
  rotationPassword = ref(""),
  newKey = ref("");
let controller: AbortController | undefined;
let loadEpoch = 0;
async function load() {
  const epoch = ++loadEpoch,
    client = props.client,
    area = client.area,
    section = props.section,
    csrf = client.csrfToken;
  loading.value = false;
  if (props.section === "security" || area !== "merchant") return;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const data = await props.client.request<Record<string, unknown>>(
      "/account/" + props.section,
      { signal: controller.signal },
    );
    if (
      epoch !== loadEpoch ||
      client !== props.client ||
      area !== client.area ||
      section !== props.section ||
      csrf !== client.csrfToken
    )
      return;
    if (props.section === "profile") {
      profile.value = {
        name: String(data.name ?? ""),
        email: String(data.email ?? ""),
      };
      payStatus.value = data.pay;
    } else info.value = data;
  } catch (e) {
    if (epoch !== loadEpoch) return;
    error.value = e instanceof Error ? e.message : "读取账户失败";
  } finally {
    if (epoch === loadEpoch) loading.value = false;
  }
}
async function saveProfile() {
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    await props.client.request("/account/profile", {
      method: "PATCH",
      body: profile.value,
    });
    notice.value = "账户资料已保存";
  } catch (e) {
    error.value = e instanceof Error ? e.message : "保存失败";
  } finally {
    busy.value = false;
  }
}
async function changePassword() {
  error.value = "";
  notice.value = "";
  const p = passwords.value;
  if (p.new_password !== p.confirmation) {
    error.value = "两次新密码输入不一致";
    return;
  }
  if (p.new_password.length < 12 || p.new_password.length > 1024) {
    error.value = "新密码须为 12 至 1024 个字符";
    return;
  }
  busy.value = true;
  try {
    await props.client.request("/account/password", {
      method: "POST",
      body: {
        current_password: p.current_password,
        new_password: p.new_password,
      },
    });
    passwords.value = {
      current_password: "",
      new_password: "",
      confirmation: "",
    };
    notice.value = "密码已修改，请使用新密码登录";
    emit("reauthenticate");
  } catch (e) {
    error.value = e instanceof Error ? e.message : "密码修改失败";
  } finally {
    busy.value = false;
  }
}
async function rotate() {
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const data = await props.client.request<{ api_key: string }>(
      "/account/api-key/rotate",
      { method: "POST", body: { current_password: rotationPassword.value } },
    );
    newKey.value = data.api_key;
    rotationPassword.value = "";
    rotationOpen.value = false;
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "密钥重置失败";
  } finally {
    busy.value = false;
  }
}
async function copyKey() {
  try {
    await navigator.clipboard.writeText(newKey.value);
    notice.value = "新密钥已复制，请妥善保存";
  } catch {
    error.value = "无法访问剪贴板，请手动复制新密钥";
  }
}
function closeKey() {
  newKey.value = "";
  notice.value = "";
}
function refreshRSA() {
  notice.value =
    "请核对当前商户公钥。RSA 私钥仅在成功响应展示，关闭后无法再次查询。";
  void load();
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.section,
  ],
  () => {
    controller?.abort();
    ++loadEpoch;
    info.value = {};
    payStatus.value = null;
    rotationOpen.value = false;
    rotationPassword.value = "";
    newKey.value = "";
    passwords.value = {
      current_password: "",
      new_password: "",
      confirmation: "",
    };
    notice.value = "";
    error.value = "";
    void load();
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  ++loadEpoch;
  controller?.abort();
  rotationPassword.value = "";
  newKey.value = "";
  passwords.value = {
    current_password: "",
    new_password: "",
    confirmation: "",
  };
});
</script>
<template>
  <div
    class="account-pages"
    role="region"
    :aria-label="
      section === 'api-info' ? '商户 API 信息' : '账户资料与安全设置'
    "
  >
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
    <div v-if="loading" class="panel loading-row" role="status">
      正在加载账户…
    </div>
    <template v-else-if="section === 'profile' || section === 'security'"
      ><section
        v-if="section === 'profile'"
        class="panel account-section"
        aria-label="账户资料"
      >
        <h2>账户资料</h2>
        <button v-if="showLoginLog" type="button" @click="emit('login-log')">
          登录日志
        </button>
        <p>收款状态：{{ merchantFieldValue("pay", payStatus) }}</p>
        <form @submit.prevent="saveProfile">
          <label
            >商户名称<input
              v-model="profile.name"
              required
              maxlength="100"
              autocomplete="organization" /></label
          ><label
            >电子邮箱<input
              v-model="profile.email"
              type="email"
              maxlength="254"
              autocomplete="email" /></label
          ><button class="primary" :disabled="busy">
            {{ busy ? "正在保存…" : "保存账户资料" }}
          </button>
        </form>
      </section>
      <section class="panel account-section">
        <h2>修改登录密码</h2>
        <form @submit.prevent="changePassword">
          <label
            >当前密码<input
              v-model="passwords.current_password"
              type="password"
              required
              autocomplete="current-password"
              maxlength="1024" /></label
          ><label
            >新密码<input
              v-model="passwords.new_password"
              type="password"
              required
              autocomplete="new-password"
              maxlength="1024" /></label
          ><label
            >再次输入新密码<input
              v-model="passwords.confirmation"
              type="password"
              required
              autocomplete="new-password"
              maxlength="1024"
          /></label>
          <p class="muted">新密码须为 12 至 1024 个字符。</p>
          <button class="primary" :disabled="busy">修改密码</button>
        </form>
      </section></template
    ><template v-else
      ><section class="panel account-section">
        <h2>商户接入信息</h2>
        <dl class="record-detail">
          <template
            v-for="(label, key) in {
              pid: '商户 ID',
              algorithm: '签名算法',
              api_key_masked: 'API 密钥（已隐藏）',
              merchant_public_key: '商户公钥',
              platform_public_key: '平台公钥',
            }"
            :key="key"
            ><dt>{{ label }}</dt>
            <dd>{{ displayValue(info[key]) }}</dd></template
          >
        </dl>
        <p class="muted">
          已生成的签名私钥无法再次读取。重置密钥后，请同步更新您的接入系统。
        </p>
        <button
          v-if="String(info.algorithm).toUpperCase() === 'MD5'"
          :disabled="busy"
          @click="
            rotationOpen = true;
            error = '';
            rotationPassword = '';
          "
        >
          重置 API 密钥
        </button>
      </section>
      <MerchantRSAKeyRotation
        :client="client"
        :allowed="info.can_rotate_rsa === true"
        :algorithm="info.algorithm"
        :fingerprint="info.rsa_fingerprint"
        :has-key="
          typeof info.merchant_public_key === 'string' &&
          info.merchant_public_key.length > 0
        "
        @refresh="refreshRSA"
      />
      <section v-if="newKey" class="panel account-section">
        <h2>请保存新密钥</h2>
        <p class="muted">
          新密钥只在此次重置后展示。离开页面或关闭后将隐藏，请在安全位置保存。
        </p>
        <label
          >新 API 密钥<textarea
            :value="newKey"
            readonly
            rows="2"
            autocomplete="off"
          />
        </label>
        <div class="toolbar-actions">
          <button class="primary" @click="copyKey">复制新密钥</button
          ><button @click="closeKey">已保存，隐藏密钥</button>
        </div>
      </section></template
    >
    <div v-if="rotationOpen" class="modal-backdrop">
      <section
        class="modal panel"
        role="dialog"
        aria-modal="true"
        aria-label="重置 API 密钥"
      >
        <h2>重置 API 密钥</h2>
        <p>旧密钥将立即失效。确认您已准备好更新商户接入配置。</p>
        <div v-if="error" class="alert error" role="alert">{{ error }}</div>
        <form @submit.prevent="rotate">
          <label
            >当前登录密码<input
              v-model="rotationPassword"
              type="password"
              required
              maxlength="1024"
              autocomplete="current-password"
          /></label>
          <div class="modal-actions">
            <button
              type="button"
              :disabled="busy"
              @click="
                rotationOpen = false;
                rotationPassword = '';
              "
            >
              取消</button
            ><button class="danger-button" :disabled="busy">
              {{ busy ? "正在重置…" : "确认重置密钥" }}
            </button>
          </div>
        </form>
      </section>
    </div>
  </div>
</template>
