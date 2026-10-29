<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";
import {
  publicRequest,
  registrationNotice,
  parseEnrollmentCapabilities,
  utf8Length,
  type EnrollmentCapabilities,
} from "../enrollment";
import { validPromotionToken } from "../referral";
import RichContent from "./RichContent.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
const props = defineProps<{
  purpose: "register" | "recover";
  initialPromotionToken?: string;
  registrationAnnouncement?: string;
}>();
const emit = defineEmits<{ close: []; prefillConsumed: [] }>();
const promotionToken = ref(
  props.purpose === "register" &&
    validPromotionToken(props.initialPromotionToken || "")
    ? props.initialPromotionToken!
    : "",
);
const capabilities = ref<EnrollmentCapabilities | null>(null),
  loading = ref(true),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  completed = ref(false);
const email = ref(""),
  phone = ref(""),
  channel = ref<"email" | "phone">("email"),
  username = ref(""),
  name = ref(""),
  password = ref(""),
  confirmation = ref(""),
  inviteCode = ref(""),
  code = ref(""),
  challenge = ref(""),
  challengeEmail = ref(""),
  expiresAt = ref(0),
  cooldownUntil = ref(0),
  now = ref(Date.now());
const controller = new AbortController();
let active = true;
const canonicalEmail = computed(() => email.value.trim().toLowerCase());
const canonicalPhone = computed(() => phone.value.trim());
const register = computed(() => props.purpose === "register");
const smsEnabled = computed(() => !!capabilities.value?.sms_enabled);
const byPhone = computed(() => channel.value === "phone" && smsEnabled.value);
// A phone send only needs an accepted request (no challenge); an email send
// returns a challenge bound to the address. phoneRequested tracks the former.
const phoneRequested = ref(false);
const enabled = computed(() =>
  byPhone.value
    ? smsEnabled.value &&
      (!register.value || !!capabilities.value?.registration_open)
    : !!capabilities.value?.email_enabled &&
      (!register.value || !!capabilities.value?.registration_open),
);
const cooldown = computed(() =>
  Math.max(0, Math.ceil((cooldownUntil.value - now.value) / 1000)),
);
const validPhone = computed(() => /^[0-9]{5,20}$/.test(canonicalPhone.value));
const validChallenge = computed(
  () =>
    !!challenge.value &&
    challengeEmail.value === canonicalEmail.value &&
    expiresAt.value > now.value,
);
// Ready to submit: phone needs a sent code + a 6-digit entry; email needs a live
// challenge + a 7-digit entry.
const readyToSubmit = computed(() =>
  byPhone.value
    ? phoneRequested.value && validPhone.value && /^\d{6}$/.test(code.value)
    : validChallenge.value,
);
const timer = setInterval(() => {
  now.value = Date.now();
}, 1000);
function clearChallenge() {
  challenge.value = "";
  challengeEmail.value = "";
  code.value = "";
  expiresAt.value = 0;
  phoneRequested.value = false;
}
function clearSecrets() {
  clearChallenge();
  password.value = "";
  confirmation.value = "";
  inviteCode.value = "";
  promotionToken.value = "";
}
watch(canonicalEmail, () => {
  clearChallenge();
  notice.value = "";
});
watch(canonicalPhone, () => {
  clearChallenge();
  notice.value = "";
});
watch(channel, () => {
  clearChallenge();
  error.value = "";
  notice.value = "";
  cooldownUntil.value = 0;
});
async function loadCapabilities() {
  if (busy.value) return;
  loading.value = true;
  error.value = "";
  capabilities.value = null;
  try {
    const data = await publicRequest(
      "capabilities",
      undefined,
      controller.signal,
    );
    if (active) capabilities.value = parseEnrollmentCapabilities(data);
  } catch (e) {
    if (active)
      error.value = e instanceof Error ? e.message : "服务能力读取失败";
  } finally {
    if (active) loading.value = false;
  }
}
function validEmail() {
  return (
    canonicalEmail.value.length <= 254 &&
    /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(canonicalEmail.value)
  );
}
async function send() {
  if (busy.value || !enabled.value || cooldown.value > 0) return;
  if (byPhone.value) {
    await sendPhone();
    return;
  }
  if (!validEmail()) {
    error.value = "请输入有效邮箱";
    return;
  }
  busy.value = true;
  error.value = "";
  notice.value = "";
  clearChallenge();
  const sentEmail = canonicalEmail.value;
  try {
    const result = await publicRequest<{
      challenge_id: string;
      expires_at: string;
    }>(
      "verification-code",
      { purpose: props.purpose, email: sentEmail },
      controller.signal,
    );
    if (!active) return;
    const expiry = Date.parse(result.expires_at);
    if (
      !result.challenge_id ||
      !Number.isFinite(expiry) ||
      expiry <= Date.now()
    )
      throw new Error("验证码凭据响应无效，请稍后重新申请");
    cooldownUntil.value = Date.now() + 60000;
    if (sentEmail !== canonicalEmail.value)
      throw new Error("邮箱已变化，请为当前邮箱重新申请验证码");
    challenge.value = result.challenge_id;
    challengeEmail.value = sentEmail;
    expiresAt.value = expiry;
    now.value = Date.now();
    notice.value =
      "验证码发送请求已受理，请检查邮箱；未收到时不要猜测验证码。新验证码会使旧验证码失效。";
  } catch (e) {
    if (active)
      error.value =
        e instanceof Error ? e.message : "未确认验证码发送，请稍后核对";
  } finally {
    if (active) busy.value = false;
  }
}
async function sendPhone() {
  if (!validPhone.value) {
    error.value = "请输入有效手机号码";
    return;
  }
  busy.value = true;
  error.value = "";
  notice.value = "";
  clearChallenge();
  const sent = canonicalPhone.value;
  try {
    // The public SMS-code endpoint lives at /console/v1/public/sms-code and
    // returns an accepted request (no challenge); the code is bound to the phone
    // in verification_codes134.
    await publicRequest<null>(
      "sms-code",
      { purpose: register.value ? "register" : "recover", phone: sent },
      controller.signal,
    );
    if (!active) return;
    cooldownUntil.value = Date.now() + 60000;
    if (sent !== canonicalPhone.value)
      throw new Error("手机号已变化，请为当前号码重新获取验证码");
    phoneRequested.value = true;
    now.value = Date.now();
    notice.value =
      "短信验证码发送请求已受理，请查收短信；未收到时不要猜测验证码。新验证码会使旧验证码失效。";
  } catch (e) {
    if (active)
      error.value =
        e instanceof Error ? e.message : "未确认短信发送，请稍后核对";
  } finally {
    if (active) busy.value = false;
  }
}
async function submit() {
  if (busy.value || !enabled.value || !readyToSubmit.value) return;
  if (byPhone.value) {
    await submitPhone();
    return;
  }
  error.value = "";
  notice.value = "";
  if (!validEmail() || !/^\d{7}$/.test(code.value)) {
    error.value = "请输入有效邮箱和 7 位邮箱验证码";
    return;
  }
  const bytes = utf8Length(password.value);
  if (bytes < 12 || bytes > 1024 || password.value === canonicalEmail.value) {
    error.value = "密码须为 12–1024 字节且不能与邮箱相同";
    return;
  }
  if (password.value !== confirmation.value) {
    error.value = "两次输入的新密码不一致";
    return;
  }
  if (
    register.value &&
    (!/^[a-zA-Z0-9_.@-]{3,100}$/.test(username.value) ||
      utf8Length(name.value) < 1 ||
      utf8Length(name.value) > 100)
  ) {
    error.value =
      "用户名须为 3–100 位英文字母、数字或 _.@-，商户名称须为 1–100 字节";
    return;
  }
  if (
    register.value &&
    capabilities.value?.invite_required &&
    !inviteCode.value.trim()
  ) {
    error.value = "请输入邀请码";
    return;
  }
  if (
    register.value &&
    promotionToken.value.trim() &&
    !validPromotionToken(promotionToken.value.trim())
  ) {
    error.value = "推广令牌格式无效，请清除或重新粘贴；与注册准入邀请码独立。";
    return;
  }
  busy.value = true;
  const body = {
    challenge_id: challenge.value,
    code: code.value,
    email: canonicalEmail.value,
    ...(register.value
      ? {
          username: username.value,
          name: name.value,
          password: password.value,
          ...(inviteCode.value.trim()
            ? { invite_code: inviteCode.value.trim() }
            : {}),
          ...(promotionToken.value.trim()
            ? { promotion_token: promotionToken.value.trim() }
            : {}),
        }
      : { new_password: password.value }),
  };
  try {
    const result = await publicRequest<{
      merchant_id: string;
      username: string;
      pay?: string;
    } | null>(
      register.value ? "register" : "recover-password",
      body,
      controller.signal,
    );
    if (!active) return;
    const registrationResult = register.value ? registrationNotice(result) : "";
    completed.value = true;
    notice.value = register.value
      ? registrationResult
      : "请求处理完成，请按正常流程登录；已绑定的动态验证码仍需正常验证。此提示不确认某邮箱是否存在。";
    clearSecrets();
  } catch (e) {
    if (active)
      error.value =
        e instanceof Error ? e.message : "请求结果尚未确认，请稍后核对";
  } finally {
    if (active) busy.value = false;
  }
}
async function submitPhone() {
  error.value = "";
  notice.value = "";
  if (!validPhone.value || !/^\d{6}$/.test(code.value)) {
    error.value = "请输入有效手机号码和 6 位短信验证码";
    return;
  }
  const bytes = utf8Length(password.value);
  if (bytes < 12 || bytes > 1024 || password.value === canonicalPhone.value) {
    error.value = "密码须为 12–1024 字节且不能与手机号相同";
    return;
  }
  if (password.value !== confirmation.value) {
    error.value = "两次输入的新密码不一致";
    return;
  }
  if (
    register.value &&
    (!/^[a-zA-Z0-9_.@-]{3,100}$/.test(username.value) ||
      utf8Length(name.value) < 1 ||
      utf8Length(name.value) > 100)
  ) {
    error.value =
      "用户名须为 3–100 位英文字母、数字或 _.@-，商户名称须为 1–100 字节";
    return;
  }
  if (
    register.value &&
    capabilities.value?.invite_required &&
    !inviteCode.value.trim()
  ) {
    error.value = "请输入邀请码";
    return;
  }
  if (
    register.value &&
    promotionToken.value.trim() &&
    !validPromotionToken(promotionToken.value.trim())
  ) {
    error.value = "推广令牌格式无效，请清除或重新粘贴；与注册准入邀请码独立。";
    return;
  }
  busy.value = true;
  const body = {
    code: code.value,
    phone: canonicalPhone.value,
    ...(register.value
      ? {
          username: username.value,
          name: name.value,
          password: password.value,
          ...(inviteCode.value.trim()
            ? { invite_code: inviteCode.value.trim() }
            : {}),
          ...(promotionToken.value.trim()
            ? { promotion_token: promotionToken.value.trim() }
            : {}),
        }
      : { new_password: password.value }),
  };
  try {
    const result = await publicRequest<{
      merchant_id: string;
      username: string;
      pay?: string;
    } | null>(
      register.value ? "register-phone" : "recover-password-phone",
      body,
      controller.signal,
    );
    if (!active) return;
    const registrationResult = register.value ? registrationNotice(result) : "";
    completed.value = true;
    notice.value = register.value
      ? registrationResult
      : "请求处理完成，请按正常流程登录；已绑定的动态验证码仍需正常验证。此提示不确认某手机号是否存在。";
    clearSecrets();
  } catch (e) {
    if (active)
      error.value =
        e instanceof Error ? e.message : "请求结果尚未确认，请稍后核对";
  } finally {
    if (active) busy.value = false;
  }
}
onMounted(() => {
  emit("prefillConsumed");
  loadCapabilities();
});
onBeforeUnmount(() => {
  active = false;
  controller.abort();
  clearInterval(timer);
  clearSecrets();
  email.value = "";
  phone.value = "";
  username.value = "";
  name.value = "";
});
</script>
<template>
  <section aria-label="商户邮箱服务">
    <h2>{{ register ? "注册商户" : "找回商户密码" }}</h2>
    <RichContent
      v-if="register && registrationAnnouncement"
      :content="registrationAnnouncement"
      format="html"
      class="registration-announcement"
    />
    <p class="muted">
      {{
        register
          ? "仅注册商户账户，不提供管理员注册。"
          : "仅支持已可信验证绑定的邮箱；资料页填写的历史邮箱不自动取得恢复权限。"
      }}
    </p>
    <p v-if="loading" role="status">正在读取服务能力…</p>
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert" role="status">{{ notice }}</div>
    <template v-if="!completed">
      <p
        v-if="capabilities && !byPhone && !capabilities.email_enabled"
        class="alert"
      >
        邮件发送尚未配置，当前不能发送验证码。
      </p>
      <p
        v-if="capabilities && register && !capabilities.registration_open"
        class="alert"
      >
        当前未开放商户注册。
      </p>
      <button
        v-if="!loading && !capabilities"
        :disabled="busy"
        @click="loadCapabilities"
      >
        重新读取服务能力
      </button>
      <div
        v-if="capabilities && smsEnabled"
        class="enrollment-channel"
        role="group"
        aria-label="验证方式"
      >
        <button
          type="button"
          class="enrollment-channel-tab"
          :class="{ active: channel === 'email' }"
          :aria-pressed="channel === 'email'"
          :disabled="busy"
          @click="channel = 'email'"
        >
          邮箱验证
        </button>
        <button
          type="button"
          class="enrollment-channel-tab"
          :class="{ active: channel === 'phone' }"
          :aria-pressed="channel === 'phone'"
          :disabled="busy"
          @click="channel = 'phone'"
        >
          手机验证
        </button>
      </div>
      <form @submit.prevent="submit">
        <p v-if="register && capabilities?.review_required" class="alert">
          新注册商户的收款权限须经管理员审核；注册后可登录查看状态。
        </p>
        <KitField v-if="byPhone" label="手机号码" required
          ><KitInput
            v-model="phone"
            name="enrollment-phone"
            inputmode="tel"
            autocomplete="tel"
            :maxlength="20"
            :disabled="busy"
            required
        /></KitField>
        <KitField v-else label="邮箱" required
          ><KitInput
            v-model="email"
            name="enrollment-email"
            type="email"
            autocomplete="email"
            :maxlength="254"
            :disabled="busy"
            required
        /></KitField>
        <button
          type="button"
          :disabled="loading || busy || !enabled || cooldown > 0"
          @click="send"
        >
          {{ cooldown > 0 ? `${cooldown} 秒后可重新发送` : "发送验证码" }}
        </button>
        <p v-if="!byPhone && challenge" class="muted">
          验证码有效至
          {{
            new Date(expiresAt).toLocaleString()
          }}，请在同一网络来源完成。重发会使旧凭据失效。
        </p>
        <p v-if="!byPhone && challenge && !validChallenge" class="alert">
          验证码已过期，请重新申请。
        </p>
        <p v-if="byPhone && phoneRequested" class="muted">
          短信验证码已发送，请在 1 小时内输入；重发会使旧验证码失效。
        </p>
        <template v-if="register">
          <KitField label="用户名" required
            ><KitInput
              v-model="username"
              autocomplete="username"
              :maxlength="100"
              :disabled="busy"
              required
          /></KitField>
          <KitField label="商户名称" required
            ><KitInput
              v-model="name"
              autocomplete="organization"
              :disabled="busy"
              required
          /></KitField>
          <KitField label="邀请码" :required="capabilities?.invite_required"
            ><KitInput
              v-model="inviteCode"
              ariaLabel="邀请码"
              autocomplete="off"
              :disabled="busy"
              :required="capabilities?.invite_required"
            /><small v-if="capabilities?.invite_required"
              >当前注册必须提供邀请码。</small
            ></KitField
          >
          <KitField label="推广令牌"
            ><KitInput
              v-model="promotionToken"
              ariaLabel="推广令牌"
              type="password"
              autocomplete="off"
              :maxlength="46"
              :disabled="busy"
            /><small
              >用于确认邀请关系，与注册准入邀请码独立。仅本次明确提交时使用，可清除或手动粘贴；不会自动注册。</small
            ></KitField
          >
        </template>
        <KitField :label="byPhone ? '短信验证码' : '邮箱验证码'" required
          ><KitInput
            v-model="code"
            inputmode="numeric"
            :pattern="byPhone ? '[0-9]{6}' : '[0-9]{7}'"
            :maxlength="byPhone ? 6 : 7"
            autocomplete="one-time-code"
            :disabled="busy"
            required
        /></KitField>
        <KitField label="新密码" required
          ><KitInput
            v-model="password"
            ariaLabel="新密码"
            type="password"
            autocomplete="new-password"
            :disabled="busy"
            required
          /><small
            >12–1024 字节，不能与{{ byPhone ? "手机号" : "邮箱" }}相同。</small
          ></KitField
        >
        <KitField label="确认新密码" required
          ><KitInput
            v-model="confirmation"
            type="password"
            autocomplete="new-password"
            :disabled="busy"
            required
        /></KitField>
        <button
          class="primary full"
          :disabled="loading || busy || !enabled || !readyToSubmit"
        >
          {{ busy ? "正在处理…" : register ? "完成注册" : "提交密码恢复" }}
        </button>
      </form>
    </template>
    <button class="full" :disabled="busy" @click="emit('close')">
      返回登录
    </button>
  </section>
</template>
<style scoped>
.enrollment-channel {
  display: flex;
  gap: 8px;
  margin: 12px 0;
}
.enrollment-channel-tab {
  flex: 1;
  padding: 8px 12px;
  border: 1px solid var(--line, var(--line-control));
  border-radius: 6px;
  background: var(--bg-raised, var(--surface-alt));
  color: var(--ink, inherit);
  cursor: pointer;
}
.enrollment-channel-tab.active {
  border-color: var(--primary, var(--focus));
  background: var(--primary-soft, var(--bg-raised));
  color: var(--primary-text, var(--ink));
  font-weight: 600;
}
.enrollment-channel-tab:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}
</style>
