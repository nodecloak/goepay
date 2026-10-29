<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient } from "../api";
import {
  readSmsGateway134,
  smsProviderTitles,
  type SmsGateway134,
  type SmsProviderView,
} from "../sms-gateway134";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitSwitch from "./KitSwitch.vue";
import KitModal from "./KitModal.vue";
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
}>();
const emit = defineEmits<{ busy: [boolean] }>();
const loading = ref(false),
  busy = ref(false),
  testBusy = ref(false),
  error = ref(""),
  notice = ref(""),
  state = ref<SmsGateway134 | null>(null),
  editing = ref<SmsProviderView["provider"] | "">(""),
  testOpen = ref(false),
  testPhone = ref(""),
  testResult = ref("");
const draft = ref({
  access_key_id: "",
  access_key_secret: "",
  appid: "",
  appkey: "",
  user: "",
  pass: "",
  template: "",
  sign: "",
  enabled: false,
});
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.ownSession,
);
let epoch = 0,
  alive = true,
  ac: AbortController | undefined;
function reset() {
  epoch++;
  ac?.abort();
  state.value = null;
  editing.value = "";
  testOpen.value = false;
  testResult.value = "";
  busy.value = false;
  loading.value = false;
}
async function load() {
  if (!allowed.value || busy.value) return;
  reset();
  error.value = "";
  notice.value = "";
  const run = epoch,
    client = toRaw(props.client),
    control = new AbortController();
  ac = control;
  loading.value = true;
  try {
    const v = readSmsGateway134(
      await client.request("/settings/sms-channels", {
        signal: control.signal,
      }),
    );
    if (!alive || run !== epoch) return;
    state.value = v;
  } catch {
    if (alive && run === epoch)
      error.value = "短信网关状态无法确认，请重新读取";
  } finally {
    if (alive && run === epoch) loading.value = false;
  }
}
function title(p: string) {
  return smsProviderTitles[p] ?? p;
}
function openProvider(p: SmsProviderView["provider"]) {
  if (busy.value || !state.value) return;
  const row = state.value.providers.find((x) => x.provider === p);
  draft.value = {
    access_key_id: "",
    access_key_secret: "",
    appid: "",
    appkey: "",
    user: "",
    pass: "",
    template: row?.template ?? "",
    sign: row?.sign ?? "",
    enabled: state.value.status === 1 && state.value.active === p,
  };
  editing.value = p;
  error.value = "";
  notice.value = "";
}
function closeProvider() {
  if (busy.value) return;
  editing.value = "";
  draft.value = {
    access_key_id: "",
    access_key_secret: "",
    appid: "",
    appkey: "",
    user: "",
    pass: "",
    template: "",
    sign: "",
    enabled: false,
  };
}
async function saveProvider() {
  if (!allowed.value || busy.value || !state.value || !editing.value) return;
  const p = editing.value,
    before = state.value;
  const credentials: Record<string, string> = {
    template: draft.value.template,
    sign: draft.value.sign,
  };
  if (p === "aliyun") {
    credentials.access_key_id = draft.value.access_key_id;
    credentials.access_key_secret = draft.value.access_key_secret;
  } else if (p === "qcloud") {
    credentials.appid = draft.value.appid;
    credentials.appkey = draft.value.appkey;
  } else {
    credentials.user = draft.value.user;
    credentials.pass = draft.value.pass;
  }
  const required =
    p === "aliyun"
      ? [credentials.access_key_id, credentials.access_key_secret]
      : p === "qcloud"
        ? [credentials.appid, credentials.appkey]
        : [credentials.user, credentials.pass];
  if (required.some((x) => !x) || !credentials.template || !credentials.sign) {
    error.value = "请填写完整的服务商密钥、模板与签名";
    return;
  }
  const body = {
    provider: p,
    credentials,
    active: draft.value.enabled ? p : before.active,
    status: draft.value.enabled ? 1 : before.active === p ? 0 : before.status,
    expected_fingerprint: before.fingerprint,
  };
  const run = ++epoch,
    client = toRaw(props.client),
    control = new AbortController();
  ac?.abort();
  ac = control;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = readSmsGateway134(
      await client.request("/settings/sms-channels", {
        method: "PATCH",
        body,
        signal: control.signal,
      }),
    );
    if (!alive || run !== epoch) return;
    state.value = v;
    editing.value = "";
    notice.value = "保存成功";
  } catch {
    if (alive && run === epoch) {
      state.value = null;
      editing.value = "";
      error.value = "保存结果无法确认，请重新读取后核对";
    }
  } finally {
    if (alive && run === epoch) busy.value = false;
  }
}
function openTest() {
  if (!state.value || state.value.status !== 1) return;
  testPhone.value = "";
  testResult.value = "";
  testOpen.value = true;
  error.value = "";
}
async function sendTest() {
  if (!allowed.value || testBusy.value || !state.value) return;
  if (!/^\d{5,20}$/.test(testPhone.value)) {
    error.value = "请输入有效的测试手机号";
    return;
  }
  const run = epoch,
    client = toRaw(props.client),
    control = new AbortController();
  testBusy.value = true;
  testResult.value = "";
  try {
    const v = await client.request<{ status: string; reason?: string }>(
      "/settings/sms-channels/test",
      {
        method: "POST",
        body: {
          phone: testPhone.value,
          expected_fingerprint: state.value.fingerprint,
        },
        signal: control.signal,
      },
    );
    if (!alive || run !== epoch) return;
    testResult.value =
      v.status === "accepted"
        ? "服务商已接受测试短信"
        : "发送未成功" + (v.reason ? "：" + v.reason : "");
  } catch {
    if (alive && run === epoch) testResult.value = "发送结果无法确认";
  } finally {
    if (alive && run === epoch) testBusy.value = false;
  }
}
watch(
  () => [props.client, props.adminClass, props.ownSession],
  () => {
    reset();
    void load();
  },
  { immediate: true, flush: "sync" },
);
watch(
  () => busy.value || testBusy.value || editing.value !== "" || testOpen.value,
  (v) => emit("busy", v),
  { flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  reset();
  emit("busy", false);
});
</script>
<template>
  <section aria-label="短信配置">
    <p v-if="!allowed" class="muted">短信网关设置需要超级管理员本人会话。</p>
    <template v-else>
      <button :disabled="loading || busy" @click="load">
        重新读取短信网关
      </button>
      <p v-if="loading" role="status">正在读取短信网关…</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
      <template v-if="state">
        <p class="muted">
          网关默认关闭；开启某服务商后，注册、找回密码与绑定手机的短信验证码才会发送。密钥加密存储，不会回显。
        </p>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>服务商</th>
                <th>状态</th>
                <th>签名</th>
                <th>模板</th>
                <th>账号</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in state.providers" :key="p.provider">
                <td>
                  {{ title(p.provider)
                  }}<span
                    v-if="state.active === p.provider && state.status === 1"
                    class="badge success"
                    >当前</span
                  >
                </td>
                <td>{{ p.configured ? "已配置" : "未配置" }}</td>
                <td>{{ p.sign || "-" }}</td>
                <td>{{ p.template || "-" }}</td>
                <td>{{ p.principal || "-" }}</td>
                <td>
                  <button
                    class="pa-btn"
                    :disabled="busy || editing !== ''"
                    @click="openProvider(p.provider)"
                  >
                    服务商设置
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="sms-gateway-state">
          网关状态：{{ state.status === 1 ? "已开启" : "已关闭"
          }}{{
            state.status === 1 && state.active
              ? "（" + title(state.active) + "）"
              : ""
          }}
          <button
            v-if="state.status === 1"
            class="pa-btn"
            :disabled="testBusy"
            @click="openTest"
          >
            测试发送
          </button>
        </div>
      </template>
    </template>
    <KitModal
      v-if="editing"
      :title="title(editing) + '设置'"
      :loading="busy"
      :submit-text="busy ? '保存中…' : '保存'"
      @close="closeProvider"
      @submit="saveProvider"
    >
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <template v-if="editing === 'aliyun'">
        <KitField label="AccessKeyId" required
          ><KitInput
            v-model="draft.access_key_id"
            autocomplete="off"
            :disabled="busy"
        /></KitField>
        <KitField label="AccessKeySecret" required
          ><KitInput
            v-model="draft.access_key_secret"
            type="password"
            autocomplete="new-password"
            :disabled="busy"
        /></KitField>
      </template>
      <template v-else-if="editing === 'qcloud'">
        <KitField label="AppId" required
          ><KitInput v-model="draft.appid" autocomplete="off" :disabled="busy"
        /></KitField>
        <KitField label="AppKey" required
          ><KitInput
            v-model="draft.appkey"
            type="password"
            autocomplete="new-password"
            :disabled="busy"
        /></KitField>
      </template>
      <template v-else>
        <KitField label="账号" required
          ><KitInput v-model="draft.user" autocomplete="off" :disabled="busy"
        /></KitField>
        <KitField label="密码" required
          ><KitInput
            v-model="draft.pass"
            type="password"
            autocomplete="new-password"
            :disabled="busy"
        /></KitField>
      </template>
      <KitField label="模板" required
        ><KitInput v-model="draft.template" :disabled="busy"
      /></KitField>
      <KitField label="签名" required
        ><KitInput v-model="draft.sign" :disabled="busy"
      /></KitField>
      <KitField label="设为启用网关"
        ><KitSwitch v-model="draft.enabled" :disabled="busy"
      /></KitField>
    </KitModal>
    <KitModal
      v-if="testOpen"
      title="短信网关测试发送"
      :loading="testBusy"
      :submit-text="testBusy ? '发送中…' : '发送'"
      @close="testOpen = false"
      @submit="sendTest"
    >
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <KitField label="测试手机号" required
        ><KitInput
          v-model="testPhone"
          inputmode="numeric"
          :maxlength="20"
          :disabled="testBusy"
      /></KitField>
      <p v-if="testResult" role="status" class="alert">{{ testResult }}</p>
    </KitModal>
  </section>
</template>
<style scoped>
.sms-gateway-state {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 16px;
}
</style>
