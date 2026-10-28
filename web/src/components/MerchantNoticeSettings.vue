<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { ApiClient } from "../api";
import MerchantNoticeEvents from "./MerchantNoticeEvents.vue";
import {
  noticeScenes,
  noticeTargets,
  type NoticeConfig,
  type NoticeTarget,
  type MerchantNoticeSettingsView,
} from "../merchant-notice";
const props = defineProps<{
  client: ApiClient;
  ownSession: boolean;
  adminClass?: string;
  merchantID?: string;
  title?: string;
}>();
const admin = computed(() => props.client.area === "admin");
const allowed = computed(
  () =>
    props.ownSession &&
    (!admin.value || ["super", "operator"].includes(props.adminClass ?? "")),
);
const selectedID = ref(props.merchantID ?? ""),
  inputID = ref(props.merchantID ?? "");
const validID = (id: string) =>
  /^[1-9][0-9]{0,18}$/.test(id) && BigInt(id) <= 9223372036854775807n;
const path = computed(() =>
  admin.value
    ? validID(selectedID.value)
      ? `/merchants/${selectedID.value}/notice`
      : ""
    : "/account/notice",
);
const state = ref<MerchantNoticeSettingsView | null>(null),
  config = ref<NoticeConfig | null>(null),
  voice = ref(false);
const busy = ref(false),
  error = ref(""),
  notice = ref("");
const actions = ref<Partial<Record<NoticeTarget, string>>>({}),
  values = ref<Partial<Record<NoticeTarget, string>>>({});
let epoch = 0,
  controller: AbortController | undefined;
function clearPrivate() {
  actions.value = {};
  values.value = {};
}
async function load() {
  const current = ++epoch;
  controller?.abort();
  controller = new AbortController();
  state.value = null;
  config.value = null;
  clearPrivate();
  error.value = "";
  notice.value = "";
  busy.value = false;
  if (!allowed.value || !path.value) return;
  const target = path.value;
  busy.value = true;
  try {
    const v = await props.client.request<MerchantNoticeSettingsView>(target, {
      signal: controller.signal,
    });
    if (current !== epoch) return;
    state.value = v;
    config.value = { ...v.msgconfig };
    voice.value = v.voice_order;
  } catch (e) {
    if (current === epoch)
      error.value = e instanceof Error ? e.message : "读取通知设置失败";
  } finally {
    if (current === epoch) busy.value = false;
  }
}
async function save() {
  if (!allowed.value || busy.value || !state.value || !config.value) return;
  if (
    !/^\d+(?:\.\d{1,2})?$/.test(config.value.order_money) ||
    !/^\d+(?:\.\d{1,2})?$/.test(config.value.balance_money)
  ) {
    error.value = "金额须为最多两位小数的非负数";
    return;
  }
  const targets: Partial<Record<NoticeTarget, string>> = {},
    clear_fields: NoticeTarget[] = [];
  for (const [key] of noticeTargets) {
    if (actions.value[key] === "replace") {
      if (!values.value[key]) {
        error.value = "请填写替换内容";
        return;
      }
      targets[key] = values.value[key];
    } else if (actions.value[key] === "clear") clear_fields.push(key);
  }
  const current = epoch,
    target = path.value;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = await props.client.request<MerchantNoticeSettingsView>(target, {
      method: "PUT",
      body: {
        expected_profile_id: state.value.id,
        msgconfig: { ...config.value },
        voice_order: voice.value,
        targets,
        clear_fields,
      },
      signal: controller?.signal,
    });
    if (current !== epoch) return;
    state.value = v;
    config.value = { ...v.msgconfig };
    voice.value = v.voice_order;
    notice.value = "通知设置已保存";
  } catch (e) {
    if (current === epoch) {
      error.value =
        e instanceof Error ? e.message : "保存结果未知，请重新读取设置";
      state.value = null;
      config.value = null;
    }
  } finally {
    if (current === epoch) {
      clearPrivate();
      busy.value = false;
    }
  }
}
function selectMerchant() {
  if (!validID(inputID.value)) {
    error.value = "请输入有效的商户 ID";
    return;
  }
  if (selectedID.value === inputID.value) void load();
  else selectedID.value = inputID.value;
}
watch(
  () => props.merchantID,
  (v) => {
    selectedID.value = v ?? "";
    inputID.value = v ?? "";
  },
);
watch([path, allowed, () => props.client], load, { immediate: true });
onBeforeUnmount(() => {
  ++epoch;
  controller?.abort();
  clearPrivate();
});
</script>
<template>
  <section class="panel notice-settings">
    <h2>{{ title || (admin ? "商户消息提醒设置" : "消息提醒设置") }}</h2>
    <p v-if="!allowed" class="alert">
      请使用本人会话；管理员需要商户管理权限。
    </p>
    <template v-else>
      <div v-if="admin && !merchantID" class="notice-owner">
        <label
          >商户 ID<input
            v-model="inputID"
            inputmode="numeric"
            autocomplete="off" /></label
        ><button :disabled="busy" @click="selectMerchant">读取商户设置</button>
      </div>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <button v-if="path" :disabled="busy" @click="load">重新读取设置</button>
      <p v-if="busy" role="status">正在处理…</p>
      <form v-if="state && config" @submit.prevent="save">
        <fieldset :disabled="busy">
          <legend>消息类型</legend>
          <div class="notice-grid">
            <label v-for="[key, title] in noticeScenes" :key="key"
              >{{ title
              }}<select v-model.number="config[key]" :name="key">
                <option :value="0">关闭提醒</option>
                <option :value="1">微信模板消息</option>
                <option :value="2">电子邮件</option>
                <option :value="3">短信</option>
                <option :value="4">群机器人</option>
                <option :value="5">Telegram</option>
                <option :value="6">QQ Bot</option>
              </select></label
            >
          </div>
          <div class="notice-grid">
            <label
              >订单金额提醒门槛（元）<input
                v-model="config.order_money"
                name="order_money"
                inputmode="decimal"
                maxlength="20"
                required /></label
            ><label
              >余额不足提醒门槛（元）<input
                v-model="config.balance_money"
                name="balance_money"
                inputmode="decimal"
                maxlength="20"
                required
            /></label>
          </div>
          <label class="notice-checkbox"
            ><input v-model="voice" type="checkbox" />订单语音提醒</label
          >
          <p class="muted">
            订单金额低于门槛时关闭普通订单提醒；语音提醒单独控制。付款码收款不播报语音。
          </p>
        </fieldset>
        <fieldset :disabled="busy">
          <legend>接收设置</legend>
          <p>
            接收邮箱：{{ state.email_masked || "未设置，请先完善账户资料" }}
          </p>
          <div class="notice-grid">
            <div
              v-for="[key, title] in noticeTargets"
              :key="key"
              class="notice-target"
            >
              <label
                >{{ title
                }}<span class="muted">{{
                  state.configured[key] ? "已配置" : "未配置"
                }}</span
                ><select v-model="actions[key]" :data-target-action="key">
                  <option :value="undefined">保留原设置</option>
                  <option value="replace">替换</option>
                  <option value="clear">清除</option>
                </select></label
              >
              <label v-if="actions[key] === 'replace'"
                >新的{{ title
                }}<input
                  v-model="values[key]"
                  :data-target-value="key"
                  type="password"
                  autocomplete="new-password"
                  maxlength="2048"
                  required
              /></label>
            </div>
          </div>
          <p class="muted">
            微信 OpenID 须属于所填
            AppID。保存设置后按平台可用通道发送；保存操作不会立即发送消息。
          </p>
        </fieldset>
        <button type="submit" :disabled="busy">保存设置</button>
      </form>
      <MerchantNoticeEvents v-if="path" :client="client" :path="path" />
    </template>
  </section>
</template>
<style scoped>
.notice-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 17rem), 1fr));
  gap: 1rem;
}
.notice-grid label {
  display: grid;
  gap: 0.4rem;
}
.notice-settings fieldset {
  min-width: 0;
  margin: 1rem 0;
  padding: 1rem;
  border: 1px solid var(--border, var(--line));
  border-radius: 4px;
}
.notice-settings input,
.notice-settings select {
  width: 100%;
  min-width: 0;
}
.notice-checkbox {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-top: 1rem;
}
.notice-checkbox input {
  width: auto;
}
.notice-owner {
  display: flex;
  align-items: end;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.notice-target {
  min-width: 0;
}
.notice-target label + label {
  margin-top: 0.5rem;
}
.notice-target .muted {
  font-size: 0.85rem;
}
</style>
