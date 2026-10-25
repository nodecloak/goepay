<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiClient } from "../api";
import type { PlatformPrinting, MerchantPrinting } from "../printing";
const props = defineProps<{
  client: ApiClient;
  ownSession: boolean;
  adminClass?: string;
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
  appID.value = "";
  secret.value = "";
  deviceID.value = "";
  credentialAction.value = "keep";
  deviceAction.value = "keep";
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
  if (!allowed.value) return;
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
async function save() {
  if (!allowed.value || !loaded.value || busy.value || !confirming.value)
    return;
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
      error.value =
        e instanceof Error ? e.message : "设置结果未知，请重新读取版本";
      loaded.value = false;
    }
  } finally {
    clearPrivate();
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
  <section class="panel">
    <h2>{{ admin ? "平台打印配置" : "我的订单打印设置" }}</h2>
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
      <button :disabled="busy" @click="load">重新读取设置</button>
      <form v-if="loaded" @submit.prevent="confirming = true">
        <fieldset :disabled="busy || confirming">
          <template v-if="admin">
            <label
              ><input
                v-model="enabled"
                type="checkbox"
              />允许参与订单打印</label
            >
            <label
              >打印服务<select v-model="provider">
                <option value="local_fake">本地模拟</option>
                <option value="excelsecu">
                  {{
                    platform?.external_execution_available
                      ? "固定HTTPS打印网关"
                      : "原打印网关配置（不执行）"
                  }}
                </option>
              </select></label
            >
            <p>
              现有应用：{{ platform?.app_id_masked || "未配置" }}；密钥：{{
                platform?.secret_present ? "已保存" : "未保存"
              }}
            </p>
            <label
              >凭据操作<select v-model="credentialAction">
                <option value="keep">保留原凭据</option>
                <option value="replace">替换凭据</option>
                <option value="clear">清除凭据（先停用）</option>
              </select></label
            >
            <template v-if="credentialAction === 'replace'"
              ><label
                >应用ID<input
                  v-model="appID"
                  autocomplete="off"
                  maxlength="128" /></label
              ><label
                >应用密钥<input
                  v-model="secret"
                  type="password"
                  autocomplete="new-password"
                  maxlength="512" /></label
            ></template>
          </template>
          <template v-else>
            <label
              >自动打印范围<select v-model.number="mode">
                <option :value="0">不自动打印</option>
                <option :value="1">仅聚合收款订单</option>
                <option :value="2">全部普通支付订单</option>
              </select></label
            >
            <label
              >每单份数<input
                v-model.number="copies"
                type="number"
                min="1"
                max="5"
                required
            /></label>
            <label><input v-model="voice" type="checkbox" />语音播报</label>
            <p>已绑定设备：{{ merchant?.device_masked || "未配置" }}</p>
            <label
              >设备操作<select v-model="deviceAction">
                <option value="keep">保留原设备</option>
                <option value="replace">替换设备</option>
                <option value="clear">清除设备（先停用）</option>
              </select></label
            >
            <label v-if="deviceAction === 'replace'"
              >设备编号<input
                v-model="deviceID"
                autocomplete="off"
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
