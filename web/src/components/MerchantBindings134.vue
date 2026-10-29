<script setup lang="ts">
import { onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient } from "../api";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitModal from "./KitModal.vue";
const props = defineProps<{ client: ApiClient }>();
const providerTitles: Record<string, string> = {
  qq: "QQ",
  wx: "微信",
  alipay: "支付宝",
  telegram: "Telegram",
};
interface ThirdParty {
  provider: string;
  bound: boolean;
}
const loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  mobile = ref(""),
  mobileBound = ref(false),
  smsReady = ref(false),
  thirdParty = ref<ThirdParty[]>([]),
  bindOpen = ref(false),
  codeBusy = ref(false),
  form = ref({ phone: "", code: "" });
let alive = true,
  ac: AbortController | undefined;
function title(p: string) {
  return providerTitles[p] ?? p;
}
async function load() {
  if (busy.value) return;
  loading.value = true;
  error.value = "";
  const client = toRaw(props.client),
    control = new AbortController();
  ac = control;
  try {
    const v = await client.request<{
      mobile: string;
      mobile_bound: boolean;
      sms_ready: boolean;
      third_party: ThirdParty[];
    }>("/account/bindings", { signal: control.signal });
    if (!alive) return;
    mobile.value = String(v.mobile ?? "");
    mobileBound.value = v.mobile_bound === true;
    smsReady.value = v.sms_ready === true;
    thirdParty.value = Array.isArray(v.third_party) ? v.third_party : [];
  } catch {
    if (alive) error.value = "无法读取绑定状态";
  } finally {
    if (alive) loading.value = false;
  }
}
function openBind() {
  if (!smsReady.value) return;
  form.value = { phone: "", code: "" };
  error.value = "";
  notice.value = "";
  bindOpen.value = true;
}
async function sendCode() {
  if (codeBusy.value || !/^\d{5,20}$/.test(form.value.phone)) {
    error.value = "请输入有效的手机号";
    return;
  }
  codeBusy.value = true;
  error.value = "";
  try {
    await toRaw(props.client).request("/account/bindings/mobile/send-code", {
      method: "POST",
      body: { phone: form.value.phone },
    });
    if (alive) notice.value = "验证码已发送，请查收";
  } catch {
    if (alive) error.value = "验证码发送未确认，请稍后重试";
  } finally {
    if (alive) codeBusy.value = false;
  }
}
async function submitBind() {
  if (busy.value) return;
  if (!/^\d{5,20}$/.test(form.value.phone) || !form.value.code) {
    error.value = "请输入手机号与验证码";
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    await toRaw(props.client).request("/account/bindings/mobile", {
      method: "POST",
      body: { phone: form.value.phone, code: form.value.code },
    });
    if (!alive) return;
    bindOpen.value = false;
    notice.value = "修改绑定成功";
    await load();
  } catch {
    if (alive) error.value = "绑定未成功，请核对验证码后重试";
  } finally {
    if (alive) busy.value = false;
  }
}
async function unbind(provider: string) {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    await toRaw(props.client).request("/account/bindings/connect/" + provider, {
      method: "DELETE",
    });
    if (!alive) return;
    notice.value = "已解绑";
    await load();
  } catch {
    if (alive) error.value = "解绑未成功，请重试";
  } finally {
    if (alive) busy.value = false;
  }
}
async function bindConnect(provider: string) {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = await toRaw(props.client).request<{
      authorize_url?: string;
      telegram?: boolean;
    }>("/account/bindings/connect/" + provider + "/start", { method: "POST" });
    if (!alive) return;
    if (v.authorize_url) {
      window.location.href = v.authorize_url;
      return;
    }
    if (v.telegram) {
      // Telegram uses the login widget; rendering it is handled on the login
      // page. Point the user there to complete the bind.
      notice.value = "请在登录页使用 Telegram 登录组件完成绑定";
    } else {
      error.value = "无法发起绑定";
    }
  } catch {
    if (alive) error.value = "无法发起绑定，请稍后重试";
  } finally {
    if (alive) busy.value = false;
  }
}
watch(
  () => [props.client, props.client.area, props.client.csrfToken],
  () => {
    void load();
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  ac?.abort();
});
</script>
<template>
  <section class="panel account-section" aria-label="账号绑定">
    <h2>账号绑定</h2>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <p v-if="loading" role="status">正在读取绑定状态…</p>
    <dl class="account-profile-facts" aria-label="密保手机">
      <div>
        <dt>密保手机</dt>
        <dd>
          {{ mobileBound ? mobile : "未绑定" }}
          <button
            v-if="smsReady"
            type="button"
            class="pa-btn"
            :disabled="busy"
            @click="openBind"
          >
            修改绑定
          </button>
          <span v-else class="muted">短信网关未开启，暂不可修改绑定</span>
        </dd>
      </div>
    </dl>
    <h3>第三方账号</h3>
    <ul class="bindings-list">
      <li v-for="p in thirdParty" :key="p.provider">
        <span>{{ title(p.provider) }}快捷登录</span>
        <span v-if="p.bound" class="bindings-actions">
          <span class="badge success">已绑定</span>
          <button
            type="button"
            class="pa-btn pa-btn--danger"
            :disabled="busy"
            @click="unbind(p.provider)"
          >
            解绑
          </button>
        </span>
        <button
          v-else
          type="button"
          class="pa-btn"
          :disabled="busy"
          @click="bindConnect(p.provider)"
        >
          立即绑定
        </button>
      </li>
    </ul>
    <KitModal
      v-if="bindOpen"
      title="修改密保手机"
      :loading="busy"
      :submit-text="busy ? '绑定中…' : '确定'"
      @close="bindOpen = false"
      @submit="submitBind"
    >
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
      <KitField label="手机号码" required
        ><KitInput
          v-model="form.phone"
          inputmode="numeric"
          :maxlength="20"
          :disabled="busy"
      /></KitField>
      <KitField label="短信验证码" required>
        <span class="bindings-code">
          <KitInput
            v-model="form.code"
            inputmode="numeric"
            :maxlength="8"
            :disabled="busy"
          />
          <button
            type="button"
            class="pa-btn"
            :disabled="codeBusy || busy"
            @click="sendCode"
          >
            {{ codeBusy ? "发送中…" : "获取验证码" }}
          </button>
        </span>
      </KitField>
    </KitModal>
  </section>
</template>
<style scoped>
.bindings-list {
  list-style: none;
  padding: 0;
  margin: 12px 0;
  display: grid;
  gap: 10px;
}
.bindings-list li {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}
.bindings-actions {
  display: inline-flex;
  gap: 10px;
  align-items: center;
}
.bindings-code {
  display: flex;
  gap: 8px;
  align-items: center;
}
</style>
