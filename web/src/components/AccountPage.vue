<script setup lang="ts">
import { computed, nextTick, ref, watch, onBeforeUnmount } from "vue";
import { ApiClient, ApiError } from "../api";
import MerchantRSAKeyRotation from "./MerchantRSAKeyRotation.vue";
import MerchantSignaturePolicy from "./MerchantSignaturePolicy.vue";
import MerchantRefundEvents201 from "./MerchantRefundEvents201.vue";
import MerchantEmailBinding197 from "./MerchantEmailBinding197.vue";
import MerchantBindings134 from "./MerchantBindings134.vue";
import MerchantConnectionInfo from "./MerchantConnectionInfo.vue";
import CopyButton from "./CopyButton.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitModal from "./KitModal.vue";
import { merchantFieldValue } from "../pro-console-fields";
import { isPersonalMerchantID } from "../merchant-personal-settings";
const props = withDefaults(
  defineProps<{
    client: ApiClient;
    section: "profile" | "api-info" | "security";
    showLoginLog?: boolean;
    ownSession?: boolean;
    merchantId?: string;
  }>(),
  { ownSession: undefined },
);
const emit = defineEmits<{
  reauthenticate: [message?: string];
  "login-log": [];
}>();
const loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  info = ref<Record<string, unknown>>({}),
  profile = ref({ name: "", email: "", phone: "" });
const payStatus = ref<unknown>(null);
const hasPassword = ref<boolean | null>(null),
  passwordOpen = ref(false),
  passwordUncertain = ref(false),
  rotationUncertain = ref(false);
const passwordTrigger = ref<HTMLButtonElement>(),
  rotationTrigger = ref<HTMLButtonElement>();
const profileFacts = ref<{
  uid: string;
  addtime: string;
  known: boolean | null;
}>({ uid: "", addtime: "", known: null });
const policyReload = ref(0);
const passwords = ref({
    current_password: "",
    new_password: "",
    confirmation: "",
  }),
  rotationOpen = ref(false),
  rotationPassword = ref(""),
  newKey = ref("");
const canRotateMD5 = computed(
  () =>
    props.client.area === "merchant" &&
    Boolean(props.client.csrfToken) &&
    props.ownSession !== false &&
    info.value.algorithm === "MD5",
);
const publicKeys = computed(() =>
  [
    {
      label: "商户公钥",
      description: "供平台验证你的 RSA 请求签名。",
      value: info.value.merchant_public_key,
    },
    {
      label: "平台公钥",
      description: "用于验证平台响应和回调签名。",
      value: info.value.platform_public_key,
    },
  ].map(({ label, description, value }) => ({
    label,
    description,
    value: typeof value === "string" && value.trim() ? value : "",
  })),
);
let controller: AbortController | undefined;
let loadEpoch = 0;
let contextEpoch = 0,
  actionEpoch = 0,
  writeController: AbortController | undefined,
  activeAction: "profile" | "password" | "rotation" | undefined;
function context() {
  return {
    epoch: contextEpoch,
    client: props.client,
    area: props.client.area,
    csrf: props.client.csrfToken,
    section: props.section,
  };
}
type Context = ReturnType<typeof context>;
function sameContext(source: Context) {
  return (
    source.epoch === contextEpoch &&
    source.client === props.client &&
    source.area === props.client.area &&
    source.csrf === props.client.csrfToken &&
    source.section === props.section
  );
}
function startAction(kind: NonNullable<typeof activeAction>) {
  const source = context(),
    action = ++actionEpoch,
    abort = new AbortController();
  writeController?.abort();
  writeController = abort;
  activeAction = kind;
  busy.value = true;
  return {
    ...source,
    signal: abort.signal,
    current: () =>
      action === actionEpoch && !abort.signal.aborted && sameContext(source),
  };
}
function finishAction(source: ReturnType<typeof startAction>) {
  if (!source.current()) return;
  busy.value = false;
  activeAction = undefined;
  writeController = undefined;
}
function stopAction() {
  ++actionEpoch;
  writeController?.abort();
  writeController = undefined;
  activeAction = undefined;
  busy.value = false;
}
function clearPasswords() {
  passwords.value = {
    current_password: "",
    new_password: "",
    confirmation: "",
  };
}
function knownRejection(e: unknown) {
  return (
    e instanceof ApiError &&
    e.responseDecoded &&
    e.status >= 400 &&
    e.status < 500 &&
    e.status !== 408
  );
}
function showPassword() {
  if (
    busy.value ||
    passwordUncertain.value ||
    hasPassword.value !== true ||
    props.client.area !== "merchant" ||
    !props.client.csrfToken
  )
    return;
  clearPasswords();
  error.value = "";
  notice.value = "";
  passwordOpen.value = true;
}
function closePassword() {
  const pending = activeAction === "password" && busy.value,
    source = context();
  if (pending) stopAction();
  passwordOpen.value = false;
  clearPasswords();
  error.value = pending
    ? "已停止等待密码修改结果。修改可能已生效，请重新登录核对，不会自动重试。"
    : passwordUncertain.value
      ? error.value
      : "";
  if (pending) passwordUncertain.value = true;
  void nextTick(() => {
    if (sameContext(source)) passwordTrigger.value?.focus();
  });
}
function showRotation() {
  if (busy.value || rotationUncertain.value || !canRotateMD5.value) return;
  rotationPassword.value = "";
  newKey.value = "";
  error.value = "";
  notice.value = "";
  rotationOpen.value = true;
}
function closeRotation() {
  const pending = activeAction === "rotation" && busy.value,
    source = context();
  if (pending) stopAction();
  rotationOpen.value = false;
  rotationPassword.value = "";
  error.value = pending
    ? "已停止等待密钥重置结果。请刷新页面后核对。"
    : rotationUncertain.value
      ? error.value
      : "";
  if (pending) rotationUncertain.value = true;
  void nextTick(() => {
    if (sameContext(source)) rotationTrigger.value?.focus();
  });
}
async function load(): Promise<boolean> {
  const epoch = ++loadEpoch,
    client = props.client,
    area = client.area,
    section = props.section,
    csrf = client.csrfToken;
  loading.value = false;
  if (props.section === "security" || area !== "merchant") return false;
  const current = () =>
    epoch === loadEpoch &&
    client === props.client &&
    area === client.area &&
    section === props.section &&
    csrf === client.csrfToken;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const data = await props.client.request<Record<string, unknown>>(
      "/account/" + props.section,
      { signal: controller.signal },
    );
    if (!current()) return false;
    if (props.section === "profile") {
      profile.value = {
        name: String(data.name ?? ""),
        email: String(data.email ?? ""),
        phone: String(data.phone ?? ""),
      };
      payStatus.value = data.pay;
      hasPassword.value =
        typeof data.has_pwd === "boolean" ? data.has_pwd : null;
      profileFacts.value = {
        uid: isPersonalMerchantID(data.uid) ? data.uid : "",
        addtime:
          data.registration_time_known === true &&
          typeof data.addtime === "string"
            ? data.addtime
            : "",
        known:
          typeof data.registration_time_known === "boolean"
            ? data.registration_time_known
            : null,
      };
    } else {
      if (!data || typeof data !== "object" || Array.isArray(data))
        throw Error("接口接入信息响应无效");
      info.value = data;
    }
    return true;
  } catch (e) {
    if (!current()) return false;
    error.value = e instanceof Error ? e.message : "读取账户失败";
    return false;
  } finally {
    if (epoch === loadEpoch) loading.value = false;
  }
}
async function saveProfile() {
  if (busy.value) return;
  const source = startAction("profile");
  error.value = "";
  notice.value = "";
  try {
    await source.client.request("/account/profile", {
      method: "PATCH",
      body: { ...profile.value },
      signal: source.signal,
    });
    if (!source.current()) return;
    notice.value = "账户资料已保存";
  } catch (e) {
    if (!source.current()) return;
    error.value = e instanceof Error ? e.message : "保存失败";
  } finally {
    finishAction(source);
  }
}
async function changePassword() {
  if (
    busy.value ||
    passwordUncertain.value ||
    (props.section === "profile" &&
      (!passwordOpen.value || hasPassword.value !== true))
  )
    return;
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
  const source = startAction("password");
  try {
    await source.client.request("/account/password", {
      method: "POST",
      body: {
        current_password: p.current_password,
        new_password: p.new_password,
      },
      signal: source.signal,
    });
    if (!source.current()) return;
    clearPasswords();
    passwordOpen.value = false;
    notice.value = "密码已修改，请重新登录";
    emit("reauthenticate", notice.value);
  } catch (e) {
    if (!source.current()) return;
    if (knownRejection(e))
      error.value = e instanceof Error ? e.message : "密码修改失败";
    else {
      passwordUncertain.value = true;
      clearPasswords();
      error.value =
        "未能确认密码修改结果。请使用新密码重新登录核对，不会自动重试。";
    }
  } finally {
    if (source.current()) passwords.value.current_password = "";
    finishAction(source);
  }
}
async function rotate() {
  if (
    busy.value ||
    !rotationOpen.value ||
    rotationUncertain.value ||
    !rotationPassword.value ||
    !canRotateMD5.value
  )
    return;
  const source = startAction("rotation");
  error.value = "";
  notice.value = "";
  try {
    const data = await source.client.request<{ api_key: string }>(
      "/account/api-key/rotate",
      {
        method: "POST",
        body: { current_password: rotationPassword.value },
        signal: source.signal,
      },
    );
    if (!source.current()) return;
    if (
      !data ||
      typeof data.api_key !== "string" ||
      !data.api_key ||
      data.api_key.length > 4096
    )
      throw Error("密钥重置响应无效");
    newKey.value = data.api_key;
    rotationPassword.value = "";
    rotationOpen.value = false;
    await load();
  } catch (e) {
    if (!source.current()) return;
    if (knownRejection(e))
      error.value = e instanceof Error ? e.message : "密钥重置失败";
    else {
      rotationUncertain.value = true;
      rotationOpen.value = false;
      error.value = "未能确认密钥重置结果。请刷新页面后核对；不会自动重试。";
    }
  } finally {
    if (source.current()) rotationPassword.value = "";
    finishAction(source);
  }
}
async function copyKey() {
  if (!newKey.value) return;
  const source = context(),
    key = newKey.value;
  try {
    await navigator.clipboard.writeText(key);
    if (!sameContext(source) || newKey.value !== key) return;
    notice.value = "新密钥已复制，请妥善保存";
  } catch {
    if (!sameContext(source) || newKey.value !== key) return;
    error.value = "无法访问剪贴板，请手动复制新密钥";
  }
}
function closeKey() {
  newKey.value = "";
  notice.value = "";
  const source = context();
  void nextTick(() => {
    if (sameContext(source)) rotationTrigger.value?.focus();
  });
}
function refreshRSA() {
  ++policyReload.value;
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
    props.ownSession,
    props.merchantId,
  ],
  () => {
    ++contextEpoch;
    stopAction();
    controller?.abort();
    ++loadEpoch;
    info.value = {};
    profile.value = { name: "", email: "", phone: "" };
    profileFacts.value = { uid: "", addtime: "", known: null };
    payStatus.value = null;
    hasPassword.value = null;
    passwordOpen.value = false;
    passwordUncertain.value = false;
    rotationUncertain.value = false;
    rotationOpen.value = false;
    rotationPassword.value = "";
    newKey.value = "";
    clearPasswords();
    notice.value = "";
    error.value = "";
    void load();
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  ++contextEpoch;
  stopAction();
  ++loadEpoch;
  controller?.abort();
  profile.value = { name: "", email: "", phone: "" };
  profileFacts.value = { uid: "", addtime: "", known: null };
  hasPassword.value = null;
  passwordOpen.value = false;
  rotationOpen.value = false;
  rotationPassword.value = "";
  newKey.value = "";
  clearPasswords();
});
</script>
<template>
  <div
    class="account-pages"
    :class="{ 'api-account': section === 'api-info' }"
    role="region"
    :aria-label="
      section === 'api-info' ? '商户 API 信息' : '账户资料与安全设置'
    "
  >
    <h1 v-if="section === 'api-info'" class="api-account-title">API 接入</h1>
    <div
      v-if="error && !passwordOpen && !rotationOpen"
      class="alert error"
      role="alert"
    >
      {{ error }}
    </div>
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
        <dl class="account-profile-facts" aria-label="商户基本资料">
          <div>
            <dt>商户ID</dt>
            <dd>{{ profileFacts.uid || "—" }}</dd>
          </div>
          <div>
            <dt>联系邮箱</dt>
            <dd>{{ profile.email || "未填写" }}</dd>
          </div>
          <div>
            <dt>注册时间</dt>
            <dd>
              {{
                profileFacts.known === false
                  ? "原注册时间未提供"
                  : profileFacts.addtime || "—"
              }}
            </dd>
          </div>
          <div role="group" aria-label="登录密码">
            <dt>登录密码</dt>
            <dd class="account-password-value">
              <span
                class="badge"
                :class="
                  hasPassword === true
                    ? 'success'
                    : hasPassword === false
                      ? 'danger'
                      : 'info'
                "
                >{{
                  hasPassword === true
                    ? "已设置"
                    : hasPassword === false
                      ? "未设置"
                      : "暂未读取"
                }}</span
              >
              <button
                ref="passwordTrigger"
                class="link-button"
                type="button"
                :disabled="busy || hasPassword !== true || passwordUncertain"
                @click="showPassword"
              >
                修改
              </button>
              <button
                v-if="showLoginLog"
                class="link-button"
                type="button"
                @click="emit('login-log')"
              >
                登录日志
              </button>
            </dd>
          </div>
        </dl>
        <p v-if="hasPassword === false" class="muted">
          此账号尚未设置登录密码，请使用已验证的找回密码流程，或联系管理员通过已有初始化流程处理。
        </p>
        <p>收款状态：{{ merchantFieldValue("pay", payStatus) }}</p>
        <MerchantEmailBinding197
          :client="client"
          @reauthenticate="emit('reauthenticate', '修改绑定成功，请重新登录')"
        />
        <MerchantBindings134 :client="client" />
        <form class="account-kit-form" @submit.prevent="saveProfile">
          <KitField label="商户名称" required
            ><KitInput
              v-model="profile.name"
              required
              :maxlength="100"
              autocomplete="organization"
          /></KitField>
          <KitField label="联系手机"
            ><KitInput
              v-model="profile.phone"
              :maxlength="21"
              inputmode="tel"
              autocomplete="tel"
              placeholder="选填，5 至 20 位数字，支持前缀 +"
          /></KitField>
          <div class="account-kit-actions">
            <button
              type="submit"
              class="pa-btn pa-btn--primary"
              :class="{ 'pa-btn--loading': busy }"
              :disabled="busy"
            >
              {{ busy ? "正在保存…" : "保存账户资料" }}
            </button>
          </div>
        </form>
      </section>
      <section v-if="section === 'security'" class="panel account-section">
        <h2>
          {{ client.area === "admin" ? "修改管理员密码" : "修改登录密码" }}
        </h2>
        <form class="account-kit-form" @submit.prevent="changePassword">
          <KitField label="当前密码" required
            ><KitInput
              v-model="passwords.current_password"
              type="password"
              required
              autocomplete="current-password"
              :disabled="busy || passwordUncertain"
              :maxlength="1024"
          /></KitField>
          <KitField label="新密码" required
            ><KitInput
              v-model="passwords.new_password"
              type="password"
              required
              autocomplete="new-password"
              :disabled="busy || passwordUncertain"
              :maxlength="1024"
          /></KitField>
          <KitField label="确认密码" required
            ><KitInput
              v-model="passwords.confirmation"
              type="password"
              required
              autocomplete="new-password"
              :disabled="busy || passwordUncertain"
              :maxlength="1024"
          /></KitField>
          <p class="muted">新密码须为 12 至 1024 个字符。</p>
          <div class="account-kit-actions">
            <button
              type="submit"
              class="pa-btn pa-btn--primary"
              :class="{ 'pa-btn--loading': busy }"
              :disabled="busy || passwordUncertain"
            >
              {{ busy ? "正在修改…" : "修改密码" }}
            </button>
          </div>
        </form>
      </section></template
    >
    <template v-else>
      <div class="api-account-grid api-account-overview">
        <section
          class="panel account-section api-connection"
          aria-label="接入参数"
        >
          <h2>接入参数</h2>
          <MerchantConnectionInfo :merchant-id="info.pid" />
        </section>
        <MerchantSignaturePolicy :client="client" :reload="policyReload" />
      </div>
      <div class="api-account-grid api-account-credentials">
        <section class="panel account-section" aria-label="MD5 API 密钥">
          <h2>MD5 API 密钥</h2>
          <p class="muted">用于支持 MD5 签名的接入。</p>
          <div class="api-secret-mask">
            <code
              v-if="
                typeof info.api_key_masked === 'string' && info.api_key_masked
              "
            >
              {{ info.api_key_masked }}
            </code>
            <span v-else class="muted">暂未读取</span>
          </div>
          <button
            v-if="canRotateMD5 && !newKey"
            ref="rotationTrigger"
            type="button"
            :disabled="busy || rotationUncertain"
            @click="showRotation"
          >
            重置 API 密钥
          </button>
          <div v-if="newKey" class="api-new-secret">
            <p class="muted">新密钥仅展示一次，请保存到安全位置。</p>
            <label>
              新 API 密钥
              <textarea
                :value="newKey"
                readonly
                rows="2"
                autocomplete="off"
                spellcheck="false"
              />
            </label>
            <div class="toolbar-actions">
              <button type="button" class="primary" @click="copyKey">
                复制新密钥
              </button>
              <button type="button" @click="closeKey">已保存，隐藏密钥</button>
            </div>
          </div>
        </section>
        <section class="panel account-section" aria-label="RSA 公钥">
          <h2>RSA 公钥</h2>
          <dl class="api-public-keys">
            <div v-for="key in publicKeys" :key="key.label">
              <dt>{{ key.label }}</dt>
              <dd>
                <div class="api-public-key-heading">
                  <span class="muted">{{ key.description }}</span>
                  <CopyButton
                    v-if="key.value"
                    :text="key.value"
                    :label="key.label"
                  />
                  <span v-else class="api-key-status">
                    {{
                      Object.hasOwn(info, "algorithm") ? "未配置" : "暂未读取"
                    }}
                  </span>
                </div>
                <details v-if="key.value">
                  <summary>查看{{ key.label }}</summary>
                  <pre>{{ key.value }}</pre>
                </details>
              </dd>
            </div>
          </dl>
          <MerchantRSAKeyRotation
            :client="client"
            :allowed="info.can_rotate_rsa === true && ownSession !== false"
            :algorithm="info.algorithm"
            :fingerprint="info.rsa_fingerprint"
            :has-key="Boolean(publicKeys[0]?.value)"
            @refresh="refreshRSA"
          />
        </section>
      </div>
      <MerchantRefundEvents201
        :client="client"
        :own-session="ownSession === true"
        :merchant-id="merchantId || ''"
      />
    </template>
    <KitModal
      v-if="passwordOpen"
      title="修改登录密码"
      :loading="busy"
      :submit-text="busy ? '正在修改…' : '修改密码'"
      @close="closePassword"
      @submit="changePassword"
    >
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <KitField label="当前密码" required
        ><KitInput
          v-model="passwords.current_password"
          type="password"
          required
          :maxlength="1024"
          autocomplete="current-password"
          :disabled="busy || passwordUncertain"
      /></KitField>
      <KitField label="新密码" required
        ><KitInput
          v-model="passwords.new_password"
          type="password"
          required
          :maxlength="1024"
          autocomplete="new-password"
          :disabled="busy || passwordUncertain"
      /></KitField>
      <KitField label="确认密码" required
        ><KitInput
          v-model="passwords.confirmation"
          type="password"
          required
          :maxlength="1024"
          autocomplete="new-password"
          :disabled="busy || passwordUncertain"
      /></KitField>
      <p class="muted">新密码须为 12 至 1024 个字符。修改后需重新登录。</p>
    </KitModal>
    <KitModal
      v-if="rotationOpen"
      title="重置 API 密钥"
      danger
      :loading="busy"
      :submit-text="busy ? '正在重置…' : '确认重置密钥'"
      @close="closeRotation"
      @submit="rotate"
    >
      <p>旧密钥将立即失效。确认您已准备好更新商户接入配置。</p>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <KitField label="登录密码" required
        ><KitInput
          v-model="rotationPassword"
          type="password"
          required
          :maxlength="1024"
          autocomplete="current-password"
          :disabled="busy"
      /></KitField>
    </KitModal>
  </div>
</template>
<style scoped>
.account-section {
  min-width: 0;
}
.api-account {
  max-width: 1200px;
}
.api-account-title {
  margin: 0 0 var(--space-4);
  font-size: 22px;
  line-height: 1.4;
}
.api-account-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--space-4);
  margin-bottom: var(--space-4);
}
.api-account > :deep(.account-section),
.api-account-grid > :deep(.account-section) {
  max-width: none;
  margin-bottom: 0;
}
.api-account-grid > :deep(.account-section > h2) {
  margin: 0 0 var(--space-3);
  padding: 0;
  border: 0;
  font-size: 16px;
}
.api-account-grid .api-connection {
  border-top: 2px solid var(--primary);
}
.api-secret-mask,
.api-public-keys pre {
  padding: var(--space-3);
  margin: var(--space-3) 0;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: var(--bg-raised);
  overflow-wrap: anywhere;
}
.api-secret-mask code,
.api-public-keys pre {
  font-family: var(--font-mono);
  font-size: 13px;
}
.api-new-secret textarea {
  display: block;
  width: 100%;
  margin: var(--space-2) 0;
  resize: vertical;
}
.api-new-secret .toolbar-actions {
  flex-wrap: wrap;
}
.api-public-keys {
  display: grid;
  gap: var(--space-3);
  margin: 0 0 var(--space-4);
}
.api-public-keys > div + div {
  border-top: 1px solid var(--line);
  padding-top: var(--space-3);
}
.api-public-keys dt {
  font-weight: 600;
}
.api-public-keys dd {
  margin: var(--space-1) 0 0;
  font-family: inherit;
}
.api-public-key-heading {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: var(--space-2);
}
.api-public-key-heading :deep(.pa-copy-btn) {
  width: var(--control-h);
  height: var(--control-h);
  margin: 0;
  background: var(--bg-raised);
}
.api-key-status {
  color: var(--muted);
}
.api-public-keys summary {
  width: fit-content;
  margin-block: var(--space-1);
  color: var(--primary-text);
  cursor: pointer;
}
.api-public-keys pre {
  white-space: pre-wrap;
}
@media (min-width: 1024px) {
  .api-account-overview {
    grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);
  }
  .api-account-credentials {
    grid-template-columns: minmax(0, 2fr) minmax(0, 3fr);
  }
}
.account-kit-form {
  display: grid;
  gap: 16px;
  max-width: 520px;
  margin-top: 16px;
}
.account-kit-actions {
  display: flex;
  justify-content: flex-start;
}
.account-section form label {
  display: block;
  min-width: 0;
}
.account-section form input,
.account-section form textarea {
  width: 100%;
  box-sizing: border-box;
  min-width: 0;
}
.account-profile-facts {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 10px 20px;
  margin: 16px 0;
  font-size: 14px;
}
.account-profile-facts > div {
  display: flex;
  min-width: 0;
  align-items: baseline;
  gap: 12px;
}
.account-profile-facts dt {
  flex: 0 0 120px;
  text-align: right;
  color: var(--muted, var(--ink-muted));
}
.account-profile-facts dd {
  margin: 0;
  min-width: 0;
  overflow-wrap: anywhere;
}
.account-password-value {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
}
.account-password-value .link-button {
  border: 0;
  background: transparent;
  padding: 0;
  min-height: var(--control-h);
  color: var(--primary, var(--primary-text));
  font-size: inherit;
}
.account-password-value .link-button:disabled {
  color: var(--muted, var(--ink-muted));
}
.account-password-value .badge.success {
  color: var(--success-text);
  background: var(--success-soft);
  border-color: var(--success-soft);
}
.account-password-value .badge.danger {
  color: var(--danger-text);
  background: var(--danger-soft);
  border-color: var(--danger-soft);
}
.account-password-value .badge.info {
  color: var(--ink);
  background: var(--bg-raised);
  border-color: var(--line-control);
}
.account-password-value button:focus-visible,
.account-password-dialog button:focus-visible,
.account-password-dialog input:focus-visible {
  outline: 2px solid var(--accent, var(--focus));
  outline-offset: 2px;
}
.account-password-dialog {
  width: min(460px, calc(100vw - 32px));
  max-height: calc(100dvh - 32px);
  overflow-y: auto;
}
.account-password-dialog label {
  display: grid;
  gap: 8px;
  margin: 16px 0;
}
@media (max-width: 767px) {
  .account-profile-facts {
    grid-template-columns: minmax(0, 1fr);
  }
  .account-profile-facts dt {
    flex-basis: 84px;
    text-align: left;
  }
}
@media (max-width: 480px) {
  .account-password-dialog .modal-actions button {
    flex: 1;
  }
}
</style>
