<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiClient, ApiError } from "../api";
import type { PlatformPrinting, MerchantPrinting } from "../printing";
import KitSelect from "./KitSelect.vue";
import KitSwitch from "./KitSwitch.vue";
import CredentialActionControl from "./CredentialActionControl.vue";
const props = defineProps<{
  client: ApiClient;
  ownSession: boolean;
  adminClass?: string;
  title?: string;
}>();
const admin = computed(() => props.client.area === "admin");
const allowed = computed(
  () => props.ownSession && (!admin.value || props.adminClass === "super"),
);
const loaded = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  confirming = ref(false);
const platform = ref<PlatformPrinting | null>(null),
  merchant = ref<MerchantPrinting | null>(null);
const enabled = ref(false),
  provider = ref("local_fake"),
  credentialAction = ref("keep"),
  appID = ref(""),
  secret = ref("");
const mode = ref(0),
  copies = ref(1),
  voice = ref(false),
  deviceAction = ref("keep"),
  deviceID = ref("");
const controller = new AbortController();
let alive = true;
const path = computed(() =>
  admin.value ? "/printing/settings" : "/account/printing",
);
function clearPrivate() {
  wipePrivateValues();
  credentialAction.value = "keep";
  deviceAction.value = "keep";
}
function wipePrivateValues() {
  appID.value = "";
  secret.value = "";
  deviceID.value = "";
}
function setCredentialAction(next: "keep" | "replace" | "clear") {
  if (busy.value || confirming.value || (next === "clear" && enabled.value))
    return;
  credentialAction.value = next;
  if (next !== "replace") {
    appID.value = "";
    secret.value = "";
  }
}
function setDeviceAction(next: "keep" | "replace" | "clear") {
  if (busy.value || confirming.value || (next === "clear" && mode.value !== 0))
    return;
  deviceAction.value = next;
  if (next !== "replace") deviceID.value = "";
}
function apply(v: PlatformPrinting | MerchantPrinting) {
  if (admin.value) {
    const p = v as PlatformPrinting;
    platform.value = p;
    enabled.value = p.enabled;
    provider.value = p.provider;
  } else {
    const p = v as MerchantPrinting;
    merchant.value = p;
    mode.value = p.mode;
    copies.value = p.copies;
    voice.value = p.voice;
  }
  clearPrivate();
  loaded.value = true;
}
async function load() {
  if (!allowed.value || busy.value) return;
  error.value = "";
  busy.value = true;
  loaded.value = false;
  try {
    const v = await props.client.request<PlatformPrinting | MerchantPrinting>(
      path.value,
      { signal: controller.signal },
    );
    if (alive) apply(v);
  } catch (e) {
    if (alive)
      error.value = e instanceof Error ? e.message : "打印设置读取失败";
  } finally {
    if (alive) busy.value = false;
  }
}
function validationError() {
  const validText = (value: string, max: number) =>
    !!value.trim() &&
    !/[\u0000-\u001f\u007f]/.test(value) &&
    new TextEncoder().encode(value).length <= max;
  if (admin.value) {
    if (!["local_fake", "excelsecu"].includes(provider.value))
      return "请选择有效的打印服务。";
    if (!["keep", "replace", "clear"].includes(credentialAction.value))
      return "请重新选择凭据修改方式。";
    if (credentialAction.value === "clear" && enabled.value)
      return "清除凭据前请先关闭订单打印。";
    if (
      credentialAction.value === "replace" &&
      (!validText(appID.value, 128) || !validText(secret.value, 512))
    )
      return "请填写完整的应用ID和应用密钥，不能包含控制字符。";
    if (
      enabled.value &&
      credentialAction.value !== "replace" &&
      !(credentialAction.value === "keep" && platform.value?.secret_present)
    )
      return "请先设置完整的打印服务凭据，再开启订单打印。";
  } else {
    if (!Number.isInteger(mode.value) || ![0, 1, 2].includes(mode.value))
      return "请选择有效的自动打印范围。";
    if (!Number.isInteger(copies.value) || copies.value < 1 || copies.value > 5)
      return "每单份数须为1到5之间的整数。";
    if (!["keep", "replace", "clear"].includes(deviceAction.value))
      return "请重新选择打印设备修改方式。";
    if (deviceAction.value === "clear" && mode.value !== 0)
      return "清除设备前请先将自动打印范围设为“不自动打印”。";
    if (deviceAction.value === "replace" && !validText(deviceID.value, 128))
      return "请填写有效的设备编号，不能包含控制字符。";
    if (
      mode.value > 0 &&
      deviceAction.value !== "replace" &&
      !(deviceAction.value === "keep" && merchant.value?.device_present)
    )
      return "请先设置打印设备，再开启自动打印。";
  }
  return "";
}
function check() {
  if (!allowed.value || !loaded.value || busy.value || confirming.value) return;
  error.value = validationError();
  notice.value = "";
  if (!error.value) confirming.value = true;
}
async function save() {
  if (!allowed.value || !loaded.value || busy.value || !confirming.value)
    return;
  error.value = validationError();
  if (error.value) {
    confirming.value = false;
    return;
  }
  busy.value = true;
  error.value = "";
  notice.value = "";
  const body = admin.value
    ? {
        expected_revision: platform.value!.revision,
        enabled: enabled.value,
        provider: provider.value,
        credential_action: credentialAction.value,
        app_id: appID.value,
        secret: secret.value,
      }
    : {
        expected_revision: merchant.value!.revision,
        mode: mode.value,
        copies: copies.value,
        voice: voice.value,
        device_action: deviceAction.value,
        device_id: deviceID.value,
      };
  wipePrivateValues();
  let rejected = false;
  try {
    const v = await props.client.request<PlatformPrinting | MerchantPrinting>(
      path.value,
      { method: "PUT", body, signal: controller.signal },
    );
    if (alive) {
      apply(v);
      notice.value = "打印设置已保存；保存设置不会立即打印。";
    }
  } catch (e) {
    if (alive) {
      rejected =
        e instanceof ApiError &&
        e.responseDecoded &&
        [400, 422].includes(e.status);
      if (rejected)
        error.value =
          "打印设置未被接受，请核对后重新保存；私密输入已清空，请重新填写。";
      else {
        error.value =
          e instanceof ApiError && e.status === 409
            ? "打印配置已变化，请刷新页面后核对；不会自动重试。"
            : "保存结果未知，请刷新页面后核对；不会自动重试。";
        loaded.value = false;
      }
    }
  } finally {
    if (rejected) wipePrivateValues();
    else clearPrivate();
    if (alive) {
      busy.value = false;
      confirming.value = false;
    }
  }
}
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  controller.abort();
  clearPrivate();
});
</script>
<template>
  <section
    class="panel"
    :aria-label="title || (admin ? '平台打印配置' : '我的订单打印设置')"
  >
    <h2 v-if="title">{{ title }}</h2>
    <p v-if="!allowed" class="alert">
      请使用本人会话；平台配置仅超级管理员可管理。
    </p>
    <template v-else>
      <p class="muted">
        打印与支付独立。网关受理不表示纸张打印完成，结果未知时不会自动重发。
      </p>
      <p v-if="admin" class="alert">
        {{
          platform?.external_execution_available
            ? "固定网关连接器可用；外部执行默认关闭，需由平台单独启用。本地模拟不会连接实体设备。"
            : "外部打印执行暂未开放；本地模拟受理不会连接实体设备。"
        }}
      </p>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>

      <form class="console-config-form" v-if="loaded" @submit.prevent="check">
        <fieldset :disabled="busy || confirming">
          <template v-if="admin">
            <label class="kit-row"
              ><KitSwitch v-model="enabled" label="允许参与订单打印"
            /></label>
            <label
              >打印服务<KitSelect
                v-model="provider"
                :options="[
                  { value: 'local_fake', label: '本地模拟' },
                  {
                    value: 'excelsecu',
                    label: platform?.external_execution_available
                      ? '固定HTTPS打印网关'
                      : '原打印网关配置（不执行）',
                  },
                ]"
                aria-label="打印服务"
            /></label>
            <p>
              现有应用：{{ platform?.app_id_masked || "未配置" }}；密钥：{{
                platform?.secret_present ? "已保存" : "未保存"
              }}
            </p>
            <CredentialActionControl
              :model-value="credentialAction"
              :configured="!!platform?.secret_present"
              label="打印服务凭据"
              :disabled="busy || confirming"
              :clear-disabled="enabled"
              clear-hint="请先关闭订单打印，再清除凭据。"
              @update:model-value="setCredentialAction"
            />
            <template v-if="credentialAction === 'replace'"
              ><label
                >应用ID<input
                  v-model="appID"
                  autocomplete="off"
                  required
                  maxlength="128" /></label
              ><label
                >应用密钥<input
                  v-model="secret"
                  type="password"
                  autocomplete="new-password"
                  required
                  maxlength="512" /></label
            ></template>
          </template>
          <template v-else>
            <label
              >自动打印范围<KitSelect
                :model-value="String(mode)"
                :options="[
                  { value: '0', label: '不自动打印' },
                  { value: '1', label: '仅聚合收款订单' },
                  { value: '2', label: '全部普通支付订单' },
                ]"
                aria-label="自动打印范围"
                @update:model-value="mode = Number($event)"
            /></label>
            <label
              >每单份数<input
                v-model.number="copies"
                class="pa-input"
                type="number"
                min="1"
                max="5"
                required
            /></label>
            <label class="kit-row"
              ><KitSwitch v-model="voice" label="语音播报"
            /></label>
            <p>已绑定设备：{{ merchant?.device_masked || "未配置" }}</p>
            <CredentialActionControl
              :model-value="deviceAction"
              :configured="!!merchant?.device_present"
              label="打印设备"
              :disabled="busy || confirming"
              :clear-disabled="mode !== 0"
              clear-hint="请先将自动打印范围设为“不自动打印”，再清除设备。"
              @update:model-value="setDeviceAction"
            />
            <label v-if="deviceAction === 'replace'"
              >设备编号<input
                v-model="deviceID"
                autocomplete="off"
                required
                maxlength="128"
            /></label>
          </template>
          <button type="submit">检查并保存打印设置</button>
        </fieldset>
      </form>
      <div v-if="confirming" class="alert">
        <p>
          确认保存当前打印配置？{{
            admin
              ? platform?.external_execution_available
                ? "新订单将按平台运行配置处理；不会立即打印或补打历史。"
                : "这不会启用外部打印执行。"
              : "仅影响配置生效后的新支付；不会补打历史订单。"
          }}
        </p>
        <button
          :disabled="busy"
          @click="
            confirming = false;
            clearPrivate();
          "
        >
          取消</button
        ><button :disabled="busy" @click="save">确认保存</button>
      </div>
    </template>
  </section>
</template>
