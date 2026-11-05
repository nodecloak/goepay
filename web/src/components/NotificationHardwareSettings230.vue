<script setup lang="ts">
import { computed, ref, watch, toRaw, onBeforeUnmount } from "vue";
import { ApiClient, ApiError } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import KitSwitch from "./KitSwitch.vue";
import KitSelect from "./KitSelect.vue";
import KitModal from "./KitModal.vue";
import CredentialActionControl from "./CredentialActionControl.vue";
const props = defineProps<{
  client: ApiClient;
  kind: "voice" | "printing";
  ownSession: boolean;
  adminClass?: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{ busy: [boolean] }>();
interface State {
  revision: string;
  enabled: boolean;
  app_id_masked: string;
  secret_present: boolean;
  provider?: string;
}
const state = ref<State>(),
  enabled = ref(false),
  provider = ref("excelsecu"),
  action = ref("keep"),
  appID = ref(""),
  secret = ref("");
const loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  confirming = ref(false),
  error = ref(""),
  notice = ref("");
const voice = computed(() => props.kind === "voice");
const title = computed(() => (voice.value ? "云喇叭语音播报" : "订单小票打印"));
const path = computed(() =>
  voice.value ? "/settings/voice" : "/printing/settings",
);
const allowed = computed(
  () =>
    props.ownSession &&
    props.adminClass === "super" &&
    props.client.area === "admin" &&
    !!props.client.csrfToken,
);
const dirty = computed(
  () =>
    !!state.value &&
    (enabled.value !== state.value.enabled ||
      (!voice.value && provider.value !== state.value.provider) ||
      action.value !== "keep" ||
      !!appID.value ||
      !!secret.value),
);
const { confirmDiscard } = useSettingsDraft212(
  title.value,
  () => dirty.value || uncertain.value,
);
let alive = true,
  epoch = 0,
  controller: AbortController | undefined;
function context() {
  return [
    toRaw(props.client),
    props.client.area,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
    props.kind,
  ] as const;
}
function current(run: number, c: readonly unknown[]) {
  return (
    alive &&
    epoch === run &&
    c.every((v, i) => v === context()[i]) &&
    allowed.value
  );
}
function clearPrivate() {
  appID.value = "";
  secret.value = "";
  action.value = "keep";
  confirming.value = false;
}
function setCredentialAction(next: "keep" | "replace" | "clear") {
  if (
    busy.value ||
    loading.value ||
    confirming.value ||
    uncertain.value ||
    props.disabled
  )
    return;
  if (next === "clear" && enabled.value) return;
  action.value = next;
  if (next !== "replace") {
    appID.value = "";
    secret.value = "";
  }
}
function apply(v: State) {
  if (
    !v ||
    !/^\d+$/.test(v.revision) ||
    typeof v.enabled !== "boolean" ||
    typeof v.secret_present !== "boolean" ||
    typeof v.app_id_masked !== "string"
  )
    throw Error("配置响应不完整，请刷新页面后核对");
  state.value = v;
  enabled.value = v.enabled;
  provider.value = v.provider || "excelsecu";
  uncertain.value = false;
  clearPrivate();
}
async function load(force = false) {
  if (!allowed.value || busy.value || (!force && !(await confirmDiscard())))
    return;
  const run = ++epoch,
    c = context();
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = await props.client.request<State>(path.value, {
      signal: controller.signal,
    });
    if (current(run, c)) apply(v);
  } catch (e) {
    if (current(run, c)) {
      state.value = undefined;
      clearPrivate();
      error.value = e instanceof Error ? e.message : "读取失败";
    }
  } finally {
    if (current(run, c)) loading.value = false;
  }
}
function check() {
  error.value = "";
  notice.value = "";
  if (
    !allowed.value ||
    !state.value ||
    busy.value ||
    uncertain.value ||
    props.disabled
  )
    return;
  if (enabled.value && action.value === "clear") {
    error.value = "清除凭据前请先关闭开关。";
    return;
  }
  if (
    (action.value === "replace" && (!appID.value || !secret.value)) ||
    (enabled.value &&
      (action.value === "clear" ||
        (action.value === "keep" && !state.value.secret_present)))
  ) {
    error.value = voice.value
      ? "请先填写完整的云喇叭agent_id和agent_secret，再开启收款提醒。"
      : "请先填写完整的APPID和APPSECRET，再开启订单小票打印。";
    return;
  }
  confirming.value = true;
}
async function save() {
  if (
    !allowed.value ||
    !state.value ||
    !confirming.value ||
    busy.value ||
    props.disabled
  )
    return;
  const run = ++epoch,
    c = context();
  controller?.abort();
  controller = new AbortController();
  busy.value = true;
  emit("busy", true);
  error.value = "";
  const body = {
    expected_revision: state.value.revision,
    enabled: enabled.value,
    credential_action: action.value,
    app_id: action.value === "replace" ? appID.value : "",
    secret: action.value === "replace" ? secret.value : "",
    ...(!voice.value ? { provider: provider.value } : {}),
  };
  clearPrivate();
  try {
    const v = await props.client.request<State>(path.value, {
      method: "PUT",
      body,
      signal: controller.signal,
    });
    if (current(run, c)) {
      apply(v);
      notice.value = "配置已保存；不会立即发送、打印或补发历史订单。";
    }
  } catch (e) {
    if (current(run, c)) {
      uncertain.value = !(
        e instanceof ApiError &&
        e.responseDecoded &&
        [400, 422].includes(e.status)
      );
      error.value = uncertain.value
        ? "保存结果尚未确认，请刷新页面后核对；不会自动重试。"
        : e instanceof Error
          ? e.message
          : "配置未被接受";
    }
  } finally {
    if (current(run, c)) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
    props.kind,
  ],
  () => {
    ++epoch;
    controller?.abort();
    state.value = undefined;
    clearPrivate();
    loading.value = false;
    busy.value = false;
    uncertain.value = false;
    error.value = "";
    notice.value = "";
    emit("busy", false);
    if (allowed.value) void load(true);
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  ++epoch;
  controller?.abort();
  clearPrivate();
  emit("busy", false);
});
</script>
<template>
  <section class="notification-hardware230" :aria-label="title">
    <p v-if="!allowed" class="alert">请使用超级管理员本人会话管理配置。</p>
    <template v-else>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>

      <form v-if="state" @submit.prevent="check">
        <fieldset :disabled="busy || loading || confirming || disabled">
          <div class="hardware-row230">
            <span>{{ voice ? "云喇叭收款提醒开关" : "订单小票打印开关" }}</span
            ><KitSwitch
              v-model="enabled"
              :label="enabled ? '开启' : '关闭'"
              :aria-label="voice ? '云喇叭收款提醒开关' : '订单小票打印开关'"
              :disabled="uncertain || disabled"
            />
          </div>
          <label v-if="!voice" class="hardware-row230"
            ><span>打印服务</span
            ><KitSelect
              v-model="provider"
              aria-label="打印服务"
              :options="[
                { value: 'excelsecu', label: '启易付打印机' },
                { value: 'local_fake', label: '本地模拟' },
              ]"
              :disabled="uncertain || disabled"
          /></label>
          <CredentialActionControl
            :model-value="action"
            :configured="state.secret_present"
            :label="`${title}凭据`"
            :disabled="busy || loading || confirming || uncertain || disabled"
            :clear-disabled="enabled"
            clear-hint="请先关闭开关，再清除凭据。"
            @update:model-value="setCredentialAction"
          />
          <label class="hardware-row230"
            ><span>{{ voice ? "云喇叭agent_id" : "APPID" }}</span
            ><input
              v-model="appID"
              type="text"
              :aria-label="voice ? '云喇叭agent_id' : 'APPID'"
              autocomplete="off"
              maxlength="128"
              :placeholder="state.app_id_masked || '未配置'"
              :disabled="action !== 'replace' || uncertain || disabled"
              :required="action === 'replace'"
          /></label>
          <label class="hardware-row230"
            ><span>{{ voice ? "云喇叭agent_secret" : "APPSECRET" }}</span
            ><input
              v-model="secret"
              type="password"
              :aria-label="voice ? '云喇叭agent_secret' : 'APPSECRET'"
              autocomplete="new-password"
              :maxlength="voice ? 1024 : 512"
              :placeholder="
                state.secret_present ? '已保存（不回显）' : '未配置'
              "
              :disabled="action !== 'replace' || uncertain || disabled"
              :required="action === 'replace'"
          /></label>
          <p class="muted">
            {{
              state.secret_present
                ? "凭据已配置，加密保存，不回显原值。"
                : "凭据未配置；请点击“设置”填写。"
            }}
            未修改的凭据无需重填；停用后可清除。
          </p>
          <p v-if="voice" class="muted">
            对接启易付云喇叭。商户需在消息推送中填写设备编号并开启订单语音提醒；付款码收款订单不播报。实际发送还需部署端开启消息任务并配置受支持的HTTPS网关。
          </p>
          <p v-else class="muted">
            对接启易付打印机。商户需在订单打印设置中填写打印机序列号、打印份数及自动打印范围。仅处理生效后的新支付，不补打历史订单；网关受理不表示纸张已打印完成。实际外部打印任务仍需部署端单独启用，本地模拟不连接实体设备。
          </p>
          <button type="submit" :disabled="uncertain || disabled || !dirty">
            保存{{ voice ? "云喇叭" : "小票打印" }}配置
          </button>
        </fieldset>
      </form>
      <KitModal
        v-if="confirming"
        :title="`确认保存${title}`"
        submit-text="确认保存"
        :loading="busy"
        @close="clearPrivate"
        @submit="save"
      >
        <p>
          将{{ enabled ? "开启" : "关闭"
          }}{{ voice ? "云喇叭收款提醒" : "订单小票打印" }}，{{
            action === "replace"
              ? "替换"
              : action === "clear"
                ? "清除"
                : "保留"
          }}应用凭据。保存不会发送测试消息、立即打印或补发历史订单。
        </p>
      </KitModal>
    </template>
  </section>
</template>
<style scoped>
.hardware-row230 {
  display: grid;
  grid-template-columns: 240px minmax(0, 1fr);
  align-items: center;
  gap: 18px;
  margin: 16px 0;
}
.hardware-row230 input {
  width: 100%;
  max-width: 640px;
}
.hardware-row230 > span {
  font-weight: 500;
}
.notification-hardware230 fieldset {
  border: 0;
  padding: 0;
  margin: 0;
}
.notification-hardware230 > button {
  margin: 8px 0;
}
.notification-hardware230 p {
  line-height: 1.7;
}
.notification-hardware230 input:disabled {
  opacity: 0.8;
}
@media (max-width: 760px) {
  .hardware-row230 {
    grid-template-columns: 1fr;
    gap: 8px;
  }
}
</style>
