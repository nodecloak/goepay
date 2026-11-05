<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { ApiClient } from "../api";
import MerchantNoticeEvents from "./MerchantNoticeEvents.vue";
import CredentialActionControl from "./CredentialActionControl.vue";
import {
  noticeScenes,
  noticeTargets,
  noticeChannels,
  channelCell,
  recipientConfigured,
  type NoticeConfig,
  type NoticeScene,
  type NoticeTarget,
  type NoticeChannelMode,
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
// cellsFor mirrors the backend SelectMerchant rules for a *selected* mode so
// every event×channel cell shows whether GOEPAY can deliver it now, and why
// not. Driven entirely by the loaded platform facts; no fabricated transport.
function cellsFor(scene: NoticeScene) {
  const v = state.value;
  const configured = v?.configured ?? {};
  const smsActive = v?.sms_active === true;
  return noticeChannels.map(([mode, label]) =>
    channelCell(
      scene,
      mode,
      label,
      v?.platform,
      smsActive,
      recipientConfigured(mode, configured),
    ),
  );
}
function pickChannel(scene: NoticeScene, mode: 0 | NoticeChannelMode) {
  if (!config.value) return;
  config.value[scene] = mode;
}
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
function setTargetAction(
  key: NoticeTarget,
  action: "keep" | "replace" | "clear",
) {
  if (busy.value) return;
  if (action === "keep") delete actions.value[key];
  else actions.value[key] = action;
  if (action !== "replace") delete values.value[key];
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
        error.value = "请填写新的通知接收配置";
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
        e instanceof Error ? e.message : "保存结果未知，请刷新页面后核对";
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
  <section
    class="panel notice-settings"
    :aria-label="title || (admin ? '商户消息提醒设置' : '消息提醒设置')"
  >
    <h2 v-if="title">{{ title }}</h2>
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

      <p v-if="busy" role="status">正在处理…</p>
      <form v-if="state && config" @submit.prevent="save">
        <fieldset :disabled="busy">
          <legend>消息推送</legend>
          <p class="muted notice-matrix-help">
            每个事件按平台可投递的方式发送一种提醒。未配置发送方或平台未启用的方式显示为不可选，并标注原因。
          </p>
          <div class="notice-matrix-scroll">
            <table class="notice-matrix" aria-label="消息推送事件与方式">
              <thead>
                <tr>
                  <th scope="col">事件</th>
                  <th scope="col">关闭</th>
                  <th
                    v-for="[, label] in noticeChannels"
                    :key="label"
                    scope="col"
                  >
                    {{ label }}
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="[key, title] in noticeScenes" :key="key">
                  <th scope="row">{{ title }}</th>
                  <td class="notice-matrix-cell">
                    <label :aria-label="title + ' 关闭提醒'"
                      ><input
                        type="radio"
                        :name="'scene-' + key"
                        :checked="config[key] === 0"
                        :data-cell="key + ':0'"
                        @change="pickChannel(key, 0)"
                    /></label>
                  </td>
                  <td
                    v-for="cell in cellsFor(key)"
                    :key="cell.mode"
                    class="notice-matrix-cell"
                    :class="{ 'notice-matrix-cell--off': !cell.ready }"
                  >
                    <label
                      :title="cell.ready ? '' : cell.reason"
                      :aria-label="
                        title +
                        ' ' +
                        cell.label +
                        (cell.ready ? '' : '（不可用：' + cell.reason + '）')
                      "
                      ><input
                        type="radio"
                        :name="'scene-' + key"
                        :checked="config[key] === cell.mode"
                        :disabled="!cell.ready && config[key] !== cell.mode"
                        :data-cell="key + ':' + cell.mode"
                        @change="pickChannel(key, cell.mode)"
                      /><span
                        v-if="!cell.ready"
                        class="notice-matrix-reason"
                        :data-reason="key + ':' + cell.mode"
                        >{{ cell.reason }}</span
                      ></label
                    >
                  </td>
                </tr>
              </tbody>
            </table>
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
          <p class="muted">
            业务短信手机：{{
              state.target_masks.bind_mobile || "未验证绑定"
            }}。<a v-if="!admin" href="/merchant/account/settings"
              >验证绑定手机</a
            >
          </p>
          <div class="notice-grid">
            <div
              v-for="[key, title] in noticeTargets"
              :key="key"
              class="notice-target"
            >
              <CredentialActionControl
                :model-value="actions[key] || 'keep'"
                :configured="!!state.configured[key]"
                :label="title"
                :disabled="busy"
                :data-target-action="key"
                @update:model-value="setTargetAction(key, $event)"
              />
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
.notice-matrix-help {
  margin: 0.5rem 0 1rem;
}
.notice-matrix-scroll {
  overflow-x: auto;
}
.notice-matrix {
  border-collapse: collapse;
  width: 100%;
  min-width: 520px;
  font-size: 13px;
}
.notice-matrix th,
.notice-matrix td {
  border: 1px solid var(--line, var(--line-control));
  padding: 8px 6px;
  text-align: center;
  vertical-align: middle;
}
.notice-matrix thead th {
  background: var(--bg-raised, var(--surface-alt));
  white-space: nowrap;
}
.notice-matrix th[scope="row"] {
  text-align: left;
  white-space: nowrap;
}
.notice-matrix-cell label {
  display: inline-flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  margin: 0;
  cursor: pointer;
}
.notice-matrix-cell input {
  width: 16px;
  height: 16px;
  margin: 0;
}
.notice-matrix-cell--off {
  background: var(--bg-muted, var(--surface-alt));
  color: var(--muted, var(--ink-muted));
}
.notice-matrix-cell--off label {
  cursor: not-allowed;
}
.notice-matrix-reason {
  font-size: 11px;
  line-height: 1.3;
  color: var(--muted, var(--ink-muted));
  max-width: 88px;
}
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
.notice-owner label {
  margin: 0;
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
