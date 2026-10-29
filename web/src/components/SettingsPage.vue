<script setup lang="ts">
import CorePaymentRisk from "./CorePaymentRisk.vue";
import PaymentDefend206 from "./PaymentDefend206.vue";
import { computed, ref, watch, toRaw, onBeforeUnmount, useId } from "vue";
import { ApiClient, ApiError } from "../api";
import type { ResourceSchema } from "../resources";
import ShellIcon from "./ShellIcon.vue";
import KitSwitch from "./KitSwitch.vue";
import KitSelect from "./KitSelect.vue";
import KitInput from "./KitInput.vue";
import BusinessNoticeEvents from "./BusinessNoticeEvents.vue";
import RichTextEditor from "./RichTextEditor.vue";
import PlatformMerchantAccessSettings from "./PlatformMerchantAccessSettings.vue";
import ScanpaySettings192 from "./ScanpaySettings192.vue";
import ScanJSSettings202 from "./ScanJSSettings202.vue";
import SiteBrandSettings193 from "./SiteBrandSettings193.vue";
import MailSettings195 from "./MailSettings195.vue";
import SmsSettings134 from "./SmsSettings134.vue";
import CaptchaKeySettings203 from "./CaptchaKeySettings203.vue";
import ProxyCredentialSettings from "./ProxyCredentialSettings.vue";
import BuyerFeeSettings104 from "./BuyerFeeSettings104.vue";
import { siteKeys191 } from "../site191";
const emit = defineEmits<{ siteChanged: [] }>();
import {
  settingsTabs,
  settingsTitles,
  settingsBlocks,
  requiredSettings,
  settingDefinition,
  settingDisplay,
  settingStorage,
  settingInitialValue,
  type SettingsTab,
  type SettingDefinition,
} from "../settings";
interface SettingRecord {
  id: string;
  key: string;
  value: string;
}
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
}>();
const active = ref<SettingsTab>("site"),
  coreRiskBusy = ref(false),
  paymentDefendBusy = ref(false),
  scanpayBusy = ref(false),
  scanJSBusy = ref(false),
  brandBusy = ref(false),
  mailBusy = ref(false),
  smsBusy = ref(false),
  schema = ref<ResourceSchema | null>(null),
  definitions = ref<SettingDefinition[]>([]),
  records = ref<Record<string, SettingRecord>>({}),
  values = ref<Record<string, string>>({}),
  loading = ref(false),
  busy = ref(false),
  platformBusy = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref("");
const scanBusy = computed(() => scanpayBusy.value || scanJSBusy.value);
const riskBusy = computed(() => coreRiskBusy.value || paymentDefendBusy.value);
const canWrite = computed(
  () =>
    props.adminClass === "super" &&
    props.ownSession &&
    !!(schema.value?.permissions.create || schema.value?.permissions.update),
);
function writable(d: SettingDefinition) {
  return (
    canWrite.value &&
    !!(records.value[d.key]
      ? schema.value?.permissions.update
      : schema.value?.permissions.create)
  );
}
const fields = computed(() =>
  definitions.value
    .filter((d) => d.tab === active.value)
    .filter(
      (d) =>
        !["test_pay_uid", "captcha_open_test"].includes(d.key) ||
        values.value.test_open === "1",
    )
    .filter(
      (d) =>
        !(
          d.key === "site_name" &&
          definitions.value.some((x) => x.key === "sitename")
        ) &&
        !(
          d.key === "support_email" &&
          definitions.value.some((x) => x.key === "email")
        ) &&
        !(
          d.key === "mail_name" &&
          definitions.value.some((x) => x.key === "mail_recv")
        ),
    ),
);
function original(d: SettingDefinition) {
  const r = records.value[d.key];
  return r ? settingDisplay(d, r.value) : settingInitialValue(d);
}
// Fields and the separately saved blocks, in the original console's order.
const blocks = computed(() => settingsBlocks(active.value, fields.value));
// Inputs are tied to this form by id, so the ordered rows can sit between
// blocks that carry their own forms and buttons.
const formId = `settings-form-${useId()}`;
const tabIcons: Record<SettingsTab, string> = {
  site: "st-globe",
  merchant: "st-store",
  pay: "st-card",
  complaint: "st-alert",
  risk: "shield",
  settle: "st-coin",
  transfer: "st-transfer",
  oauth: "st-login",
  notice: "st-bell",
  certificate: "st-badge",
  ocr: "st-scan",
  anounce: "st-doc",
  mail: "st-mail",
  sms: "st-message",
  iptype: "st-pin",
  proxy: "st-server",
  advanced: "settings",
};
const links: Partial<Record<SettingsTab, [string, string][]>> = {
  merchant: [
    ["/admin/merchant/group-buy", "用户组购买设置"],
    ["/admin/merchant/group-list", "用户组设置"],
  ],
  risk: [
    ["/admin/trade/blacklist", "黑名单管理"],
    ["/admin/system/order-subject-policies", "订单名称限制"],
  ],
  complaint: [["/admin/complain/task", "投诉同步任务"]],
  settle: [["/admin/settle/plans", "自动结算计划"]],
  notice: [["/admin/merchant/notice", "商户消息提醒"]],
  anounce: [["/admin/system/anounce", "公告管理"]],
};
function dirty(d: SettingDefinition) {
  return values.value[d.key] !== original(d);
}

// A two-choice on/off select renders as a kit switch: the original console
// shows 开启/关闭 toggles, which the design kit draws as KitSwitch rather than a
// dropdown. Detection is by LABELS (开启 / 关闭), not merely a 0/1 value pair —
// genuine two-way choices like 极验版本 V3.0/V4.0, 结算周期 T+0/T+1 or
// 新商户审核 无需审核/需管理员审核 keep their KitSelect dropdown.
function switchPair(d: SettingDefinition): readonly [string, string] | null {
  if (d.kind !== "select" || !d.options || d.options.length !== 2) return null;
  const on = d.options.find((o) => o.label === "开启");
  const off = d.options.find((o) => o.label === "关闭");
  return on && off ? [on.value, off.value] : null;
}
function isSwitch(d: SettingDefinition): boolean {
  return switchPair(d) !== null;
}
function switchOn(d: SettingDefinition): boolean {
  const pair = switchPair(d);
  return !!pair && values.value[d.key] === pair[0];
}
function setSwitch(d: SettingDefinition, on: boolean) {
  const pair = switchPair(d);
  if (pair) values.value[d.key] = on ? pair[0] : pair[1];
}
// Controls are disabled together while any save/load/sibling-block is busy.
const controlsBusy = computed(
  () =>
    busy.value ||
    platformBusy.value ||
    riskBusy.value ||
    scanBusy.value ||
    mailBusy.value ||
    smsBusy.value ||
    brandBusy.value ||
    uncertain.value,
);
const changed = computed(() => fields.value.filter(dirty));
let controller: AbortController | undefined,
  alive = true,
  epoch = 0,
  readyClient: ApiClient | undefined,
  readyCSRF = "";
function resetContext() {
  epoch++;
  controller?.abort();
  paymentDefendBusy.value = false;
  schema.value = null;
  definitions.value = [];
  records.value = {};
  values.value = {};
  busy.value = false;
  loading.value = false;
  uncertain.value = true;
  notice.value = "";
  readyClient = undefined;
  readyCSRF = "";
}
function context() {
  return {
    epoch,
    client: toRaw(props.client),
    csrf: toRaw(props.client).csrfToken,
  };
}
function currentContext(stamp: ReturnType<typeof context>) {
  if (!alive || stamp.epoch !== epoch) return false;
  if (
    stamp.client !== toRaw(props.client) ||
    stamp.csrf !== toRaw(props.client).csrfToken
  ) {
    resetContext();
    error.value = "会话已变化，请刷新核对设置";
    return false;
  }
  return true;
}
async function load() {
  if (
    busy.value ||
    platformBusy.value ||
    riskBusy.value ||
    scanBusy.value ||
    brandBusy.value ||
    mailBusy.value ||
    smsBusy.value
  )
    return;
  controller?.abort();
  const c = new AbortController();
  controller = c;
  const stamp = context();
  loading.value = true;
  error.value = "";
  uncertain.value = true;
  try {
    const loaded = await stamp.client.request<ResourceSchema>(
      "/resources/settings/schema",
      { signal: c.signal },
    );
    if (c.signal.aborted || !currentContext(stamp)) return;
    const options = loaded.fields.find((f) => f.name === "key")?.options || [];
    const defs = options.map((o) => {
      const key = typeof o === "string" ? o : String(o.value),
        label = typeof o === "string" ? o : o.label;
      if (!/^[a-z][a-z0-9_]*$/.test(key)) throw Error("设置字段格式不正确");
      return settingDefinition(key, label);
    });
    const allowed = new Set(defs.map((d) => d.key));
    if (allowed.size !== defs.length)
      throw Error("设置字段重复，请核对服务端定义");
    const found: Record<string, SettingRecord> = {};
    let offset = 0,
      total = 1,
      page = 1;
    while (offset < total) {
      const data = await stamp.client.request<{
        items: SettingRecord[];
        total: number;
      }>(`/resources/settings?page=${page}&limit=100`, { signal: c.signal });
      if (c.signal.aborted || !currentContext(stamp)) return;
      if (
        !Number.isSafeInteger(data.total) ||
        data.total < 0 ||
        data.total > 10000 ||
        !Array.isArray(data.items) ||
        data.items.length > 100
      )
        throw Error("设置分页数据不完整");
      total = data.total;
      if (!data.items.length && offset < total)
        throw Error("设置分页数据不完整，请刷新核对");
      for (const row of data.items) {
        if (!allowed.has(row.key)) continue;
        if (
          !/^[1-9]\d*$/.test(row.id) ||
          typeof row.value !== "string" ||
          found[row.key]
        )
          throw Error("设置记录格式或唯一性错误，请核对数据");
        found[row.key] = row;
      }
      offset += data.items.length;
      page++;
    }
    const draft = Object.fromEntries(
      defs.map((d) => [
        d.key,
        found[d.key]
          ? settingDisplay(d, found[d.key]!.value)
          : settingInitialValue(d),
      ]),
    );
    schema.value = loaded;
    definitions.value = defs;
    records.value = found;
    values.value = draft;
    readyClient = stamp.client;
    readyCSRF = stamp.csrf;
    uncertain.value = false;
  } catch (e) {
    if (!c.signal.aborted && currentContext(stamp))
      error.value = e instanceof Error ? e.message : "设置读取失败";
  } finally {
    if (!c.signal.aborted && currentContext(stamp)) loading.value = false;
  }
}
async function save() {
  if (
    !canWrite.value ||
    busy.value ||
    platformBusy.value ||
    riskBusy.value ||
    scanBusy.value ||
    brandBusy.value ||
    mailBusy.value ||
    smsBusy.value ||
    uncertain.value ||
    !changed.value.length
  )
    return;
  const stamp = context();
  if (!readyCSRF || readyClient !== stamp.client || readyCSRF !== stamp.csrf) {
    resetContext();
    error.value = "会话已变化，请刷新核对设置";
    return;
  }
  controller?.abort();
  const c = new AbortController();
  controller = c;
  error.value = "";
  notice.value = "";
  let saved = 0;
  let siteSaved = false;
  try {
    const pending = changed.value.map((d) => ({
      definition: d,
      value: settingStorage(d, values.value[d.key] || ""),
      record: records.value[d.key],
    }));
    busy.value = true;
    for (const entry of pending) {
      const { definition: d, value, record } = entry;
      if (record) {
        const current = await stamp.client.request<SettingRecord>(
          `/resources/settings/${encodeURIComponent(record.id)}`,
          { signal: c.signal },
        );
        if (c.signal.aborted || !currentContext(stamp)) return;
        if (current.key !== record.key || current.value !== record.value)
          throw new ApiError(`${d.label}已由其它操作修改，请刷新核对`, 409, -1);
      }
      if (
        (record && !schema.value?.permissions.update) ||
        (!record && !schema.value?.permissions.create)
      )
        throw new ApiError(`${d.label}没有保存权限`, 403, -1);
      if (!currentContext(stamp) || c.signal.aborted) return;
      const row = await stamp.client.request<SettingRecord>(
        record
          ? `/resources/settings/${encodeURIComponent(record.id)}`
          : "/resources/settings",
        {
          method: record ? "PUT" : "POST",
          body: record ? { value } : { key: d.key, value },
          signal: c.signal,
        },
      );
      if (c.signal.aborted || !currentContext(stamp)) return;
      if (
        !row ||
        !/^[1-9]\d*$/.test(row.id) ||
        row.key !== d.key ||
        row.value !== value
      )
        throw Error(`${d.label}的保存结果未确认`);
      records.value[d.key] = row;
      values.value[d.key] = settingDisplay(d, row.value);
      saved++;
      if ((siteKeys191 as readonly string[]).includes(d.key)) siteSaved = true;
    }
    notice.value = `已保存${saved}项设置`;
  } catch (e) {
    if (c.signal.aborted || !currentContext(stamp)) return;
    error.value = e instanceof Error ? e.message : "保存失败";
    if (saved) notice.value = `已保存${saved}项，其余设置未完成`;
    if (
      busy.value &&
      (!(e instanceof ApiError) || e.status >= 500 || e.status === 409)
    ) {
      uncertain.value = true;
      error.value += "；请刷新核对后再保存";
    }
  } finally {
    if (!c.signal.aborted && currentContext(stamp) && siteSaved)
      emit("siteChanged");
    if (!c.signal.aborted && currentContext(stamp)) busy.value = false;
  }
}
function navigate(event: KeyboardEvent, index: number) {
  if (
    busy.value ||
    platformBusy.value ||
    riskBusy.value ||
    scanBusy.value ||
    brandBusy.value ||
    mailBusy.value ||
    smsBusy.value
  )
    return;
  if (
    ![
      "ArrowLeft",
      "ArrowRight",
      "ArrowUp",
      "ArrowDown",
      "Home",
      "End",
    ].includes(event.key)
  )
    return;
  event.preventDefault();
  const step = event.key === "ArrowRight" || event.key === "ArrowDown" ? 1 : -1;
  const next =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? settingsTabs.length - 1
        : (index + step + settingsTabs.length) % settingsTabs.length;
  active.value = settingsTabs[next]![0];
  document.getElementById(`settings-tab-${active.value}`)?.focus();
}
const guidance: Partial<Record<SettingsTab, string>> = {
  oauth: "快捷登录的凭据不在通用设置中维护，当前页面没有在线修改入口。",
  certificate: "实名认证业务插件按本轮范围暂缓。",
  ocr: "OCR业务插件按本轮范围暂缓。",
  sms: "当前尚未接入短信业务插件。",
  iptype: "当前不提供在线IP归属服务配置，未采集的地域统计显示未知。",
  advanced:
    "Go服务的运行参数通过部署配置管理；原PHP/Swoole脚本和在线执行功能不适用。",
};
watch(
  () => [props.client, props.adminClass, props.ownSession] as const,
  () => {
    resetContext();
    error.value = "";
    void load();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
});
</script>
<template>
  <section class="panel settings-page">
    <div class="settings-head">
      <span class="muted">按分类维护平台设置</span
      ><button
        :disabled="
          loading ||
          busy ||
          platformBusy ||
          riskBusy ||
          scanBusy ||
          mailBusy ||
          brandBusy
        "
        @click="load"
      >
        <ShellIcon name="refresh" />刷新设置
      </button>
    </div>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <div class="settings-layout">
      <div
        class="settings-tabs"
        role="tablist"
        aria-label="系统设置分类"
        aria-orientation="vertical"
      >
        <button
          v-for="(tab, index) in settingsTabs"
          :id="`settings-tab-${tab[0]}`"
          :key="tab[0]"
          role="tab"
          :aria-selected="active === tab[0]"
          :aria-controls="`settings-panel-${tab[0]}`"
          :tabindex="active === tab[0] ? 0 : -1"
          :disabled="
            busy ||
            platformBusy ||
            riskBusy ||
            scanBusy ||
            mailBusy ||
            brandBusy
          "
          @click="active = tab[0]"
          @keydown="navigate($event, index)"
        >
          <ShellIcon :name="tabIcons[tab[0]]" />{{ tab[1] }}
        </button>
      </div>
      <div
        :id="`settings-panel-${active}`"
        class="settings-panel"
        role="tabpanel"
        :aria-labelledby="`settings-tab-${active}`"
      >
        <p v-if="loading" role="status">正在读取设置…</p>
        <template v-else-if="schema">
          <h2>{{ settingsTitles[active] }}</h2>
          <form
            v-if="fields.length"
            :id="formId"
            class="settings-form"
            aria-label="系统设置表单"
            @submit.prevent="save"
          ></form>
          <p v-if="!canWrite" class="muted">
            当前为只读查看，修改需要本人超级管理员会话。
          </p>
          <p v-if="guidance[active]" class="muted">{{ guidance[active] }}</p>
          <nav
            v-if="links[active]"
            class="settings-links"
            aria-label="相关设置"
          >
            <a v-for="link in links[active]" :key="link[0]" :href="link[0]">{{
              link[1]
            }}</a>
          </nav>
          <MailSettings195
            v-if="active === 'mail'"
            :client="client"
            :admin-class="adminClass"
            :own-session="ownSession"
            @busy="mailBusy = $event"
          />
          <SmsSettings134
            v-if="active === 'sms'"
            :client="client"
            :admin-class="adminClass"
            :own-session="ownSession"
            @busy="smsBusy = $event"
          />
          <template v-if="active === 'risk'">
            <div :inert="paymentDefendBusy">
              <CorePaymentRisk
                :client="client"
                :allowed="adminClass === 'super' && ownSession"
                @busy="coreRiskBusy = $event"
              />
            </div>
            <PaymentDefend206
              :client="client"
              :admin-class="adminClass"
              :own-session="ownSession"
              :disabled="
                busy ||
                coreRiskBusy ||
                platformBusy ||
                scanBusy ||
                brandBusy ||
                mailBusy
              "
              @busy="paymentDefendBusy = $event"
            />
          </template>
          <template v-for="block in blocks" :key="block.key">
            <h3 v-if="block.type === 'group'" class="settings-group">
              {{ block.title }}
            </h3>
            <div v-else-if="block.type === 'embed'" class="settings-embed">
              <SiteBrandSettings193
                v-if="block.embed === 'brand'"
                :client="client"
                :admin-class="adminClass"
                :own-session="ownSession"
                @busy="brandBusy = $event"
              />
              <CaptchaKeySettings203
                v-else-if="block.embed === 'captcha-key'"
                :client="client"
                :admin-class="adminClass"
                :own-session="ownSession"
              />
              <PlatformMerchantAccessSettings
                v-else-if="
                  block.embed === 'keylogin' || block.embed === 'user-refund'
                "
                :key="block.embed"
                :client="client"
                :admin-class="adminClass"
                :own-session="ownSession"
                :field="
                  block.embed === 'keylogin' ? 'close_keylogin' : 'user_refund'
                "
                @busy="platformBusy = $event"
              />
              <ScanpaySettings192
                v-else-if="block.embed === 'scanpay'"
                :client="client"
                :admin-class="adminClass"
                :own-session="ownSession"
                @busy="scanpayBusy = $event"
              />
              <ScanJSSettings202
                v-else-if="block.embed === 'scanjs'"
                :client="client"
                :admin-class="adminClass"
                :own-session="ownSession"
                @busy="scanJSBusy = $event"
              />
              <BuyerFeeSettings104
                v-else-if="block.embed === 'buyer-fee'"
                kind="global"
                :client="client"
                :allowed="ownSession && adminClass === 'super'"
                @busy="platformBusy = $event"
              />
              <ProxyCredentialSettings
                v-else-if="block.embed === 'proxy-credentials'"
                :client="client"
                :admin-class="adminClass"
                :own-session="ownSession"
              />
            </div>
            <div
              v-else
              class="setting-field"
              :class="{
                'setting-dirty': dirty(block.definition),
                'setting-required': requiredSettings.has(block.key),
              }"
            >
              <label :id="`setting-label-${block.key}`">{{
                block.definition.label
              }}</label>
              <KitSwitch
                v-if="isSwitch(block.definition)"
                :model-value="switchOn(block.definition)"
                :label="switchOn(block.definition) ? '开启' : '关闭'"
                :disabled="!writable(block.definition) || controlsBusy"
                @update:model-value="setSwitch(block.definition, $event)"
              />
              <KitSelect
                v-else-if="block.definition.kind === 'select'"
                v-model="values[block.key]!"
                :options="block.definition.options || []"
                placeholder="未配置（使用系统默认）"
                :aria-label="block.definition.label"
                :disabled="!writable(block.definition) || controlsBusy"
              />
              <RichTextEditor
                v-else-if="block.definition.kind === 'html'"
                v-model="values[block.key]!"
                :label="block.definition.label"
                :disabled="!writable(block.definition) || controlsBusy"
              />
              <KitInput
                v-else
                v-model="values[block.key]!"
                :aria-label="block.definition.label"
                :type="block.definition.kind === 'email' ? 'email' : 'text'"
                :inputmode="
                  block.definition.kind === 'money'
                    ? 'decimal'
                    : block.definition.kind === 'integer'
                      ? 'numeric'
                      : undefined
                "
                :disabled="!writable(block.definition) || controlsBusy"
                :maxlength="1000"
                placeholder="未配置，使用系统默认"
              />
              <small v-if="block.definition.hint" class="muted">{{
                block.definition.hint
              }}</small>
              <button
                v-if="dirty(block.definition)"
                type="button"
                class="link-button"
                :disabled="
                  busy ||
                  platformBusy ||
                  riskBusy ||
                  scanBusy ||
                  mailBusy ||
                  brandBusy
                "
                @click="values[block.key] = original(block.definition)"
              >
                撤销此项编辑
              </button>
            </div>
          </template>
          <p v-if="!fields.length && active !== 'mail'" class="empty">
            {{
              active === "site" || active === "merchant" || active === "risk"
                ? "该分类暂无其它通用设置项。"
                : "该分类当前没有可在线维护的设置项。"
            }}
          </p>
          <div v-if="fields.length" class="settings-actions">
            <button
              type="submit"
              class="primary"
              :form="formId"
              :disabled="
                !canWrite ||
                busy ||
                platformBusy ||
                scanBusy ||
                mailBusy ||
                brandBusy ||
                uncertain ||
                !changed.length
              "
            >
              {{ busy ? "正在保存…" : "保存本页设置" }}</button
            ><span class="muted"
              >{{
                changed.length
                  ? `本页有${changed.length}项未保存`
                  : "本页没有未保存修改"
              }}。未编辑的默认项不会写入。</span
            >
          </div>
          <BusinessNoticeEvents
            v-if="active === 'notice'"
            :client="client"
            :own-session="ownSession"
            :admin-class="adminClass"
          />
        </template>
      </div>
    </div>
  </section>
</template>
<style scoped>
.settings-page {
  overflow: hidden;
}
.settings-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 20px;
  border-bottom: 1px solid var(--line);
}
.settings-head button {
  min-height: var(--control-h-sm);
  padding: 0 10px;
  font-size: 12px;
}
.settings-page > .alert {
  margin: 12px 20px 0;
}
/* Left menu, right form: the original console's settings layout. */
.settings-layout {
  display: grid;
  grid-template-columns: 200px minmax(0, 1fr);
  min-height: 560px;
}
.settings-tabs {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 12px 8px;
  border-right: 1px solid var(--line);
}
.settings-tabs button {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  height: 40px;
  min-height: 40px;
  padding: 0 14px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--ink);
  font-size: 13px;
  text-align: left;
  white-space: nowrap;
}
.settings-tabs button:hover:not(:disabled) {
  background: var(--bg-hover);
  color: var(--ink);
}
.settings-tabs button[aria-selected="true"],
.settings-tabs button[aria-selected="true"]:hover {
  background: var(--primary-soft);
  color: var(--primary-text);
}
.settings-tabs button .shell-icon {
  width: 16px;
  height: 16px;
  margin: 0;
  flex: none;
}
.settings-panel {
  min-width: 0;
  padding: 20px 24px 28px;
}
.settings-panel > h2 {
  margin: 0 0 20px;
}
.settings-panel > p.muted {
  margin: -8px 0 16px;
  font-size: 12px;
}
.settings-links {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
  margin: 0 0 20px;
  font-size: 13px;
}
.settings-group {
  max-width: 960px;
  margin: 28px 0 16px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--line);
}
.settings-panel > h3.settings-group:first-of-type {
  margin-top: 4px;
}
/* One field per row: label on the left, control on the right. */
.setting-field {
  display: grid;
  grid-template-columns: 160px minmax(0, 1fr);
  column-gap: 16px;
  row-gap: 4px;
  align-items: center;
  max-width: 960px;
  margin: 0 0 16px;
}
.setting-field > label:not(.pa-switch) {
  grid-column: 1;
  margin: 0;
  text-align: right;
  font-size: 13px;
  line-height: 1.4;
  color: var(--ink);
}
.setting-required > label:not(.pa-switch)::before {
  content: "* ";
  color: var(--danger-text);
}
.setting-field > :not(label),
.setting-field > label.pa-switch {
  grid-column: 2;
}
.setting-field > label.pa-switch {
  justify-self: start;
}
.setting-field > input,
.setting-field > select {
  width: 100%;
  min-width: 0;
}
.setting-field:has(> .rich-editor) {
  align-items: start;
}
.setting-field:has(> .rich-editor) > label {
  padding-top: 6px;
}
.setting-field > small {
  display: block;
  font-size: 12px;
  line-height: 1.6;
}
.setting-field > .link-button {
  justify-self: start;
  min-height: 0;
  padding: 0;
  font-size: 12px;
}
.setting-dirty > input,
.setting-dirty > select {
  border-color: var(--primary-text);
}
.settings-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  max-width: 960px;
  margin-top: 24px;
  padding: 16px 0 0 176px;
  border-top: 1px solid var(--line);
}
.settings-actions .muted {
  font-size: 12px;
}
/* Separately saved blocks read as form rows too: their heading sits in the
   label column and their controls in the value column. */
.settings-embed {
  max-width: 960px;
  margin: 0 0 16px;
}
.settings-embed :deep(.platform-access),
.settings-embed :deep(.scanpay-settings192),
.settings-embed :deep(.scan-js-settings202),
.settings-embed :deep(.captcha-key203),
.settings-embed :deep(.buyer-fee104),
.settings-embed :deep(.proxy-credentials) {
  display: grid;
  grid-template-columns: 160px minmax(0, 1fr);
  column-gap: 16px;
  align-items: start;
  margin: 0;
  padding: 0;
  border: 0;
  border-radius: 0;
  background: transparent;
}
.settings-embed :deep(.captcha-heading),
.settings-embed :deep(.buyer-fee104 > .toolbar),
.settings-embed :deep(.proxy-heading) {
  display: contents;
}
.settings-embed :deep(.platform-access > h3),
.settings-embed :deep(.scanpay-settings192 > h3),
.settings-embed :deep(.scan-js-settings202 > h3),
.settings-embed :deep(.captcha-heading > h2),
.settings-embed :deep(.buyer-fee104 > .toolbar > h2),
.settings-embed :deep(.proxy-heading > h2) {
  grid-column: 1;
  margin: 0;
  padding-top: 7px;
  text-align: right;
  font-size: 13px;
  font-weight: 400;
  line-height: 1.4;
  color: var(--ink);
}
.settings-embed :deep(.platform-access > :not(h3)),
.settings-embed :deep(.scanpay-settings192 > :not(h3)),
.settings-embed :deep(.scan-js-settings202 > :not(h3)),
.settings-embed :deep(.captcha-key203 > :not(.captcha-heading)),
.settings-embed :deep(.captcha-heading > button),
.settings-embed :deep(.buyer-fee104 > :not(.toolbar)),
.settings-embed :deep(.buyer-fee104 > .toolbar > button),
.settings-embed :deep(.proxy-credentials > :not(.proxy-heading)),
.settings-embed :deep(.proxy-heading > button) {
  grid-column: 2;
  min-width: 0;
}
.settings-embed :deep(.captcha-heading > button),
.settings-embed :deep(.buyer-fee104 > .toolbar > button),
.settings-embed :deep(.proxy-heading > button),
.settings-embed :deep(button) {
  justify-self: start;
}
.settings-embed :deep(.platform-access p),
.settings-embed :deep(.scanpay-settings192 p),
.settings-embed :deep(.scan-js-settings202 p),
.settings-embed :deep(.captcha-key203 p),
.settings-embed :deep(.proxy-credentials p) {
  margin: 6px 0;
  font-size: 12px;
}
.settings-embed :deep(.platform-switch),
.settings-embed :deep(.scanpay-switch192),
.settings-embed :deep(.scan-js-switch202) {
  margin: 8px 0;
}
/* Logos: one row per picture, label left, preview box and picker right. */
.settings-embed :deep(.site-brand-settings193) {
  margin: 0;
  padding: 0;
  border: 0;
}
.settings-embed :deep(.site-brand-settings193 > button),
.settings-embed :deep(.site-brand-settings193 > p) {
  margin-left: 176px;
}
.settings-embed :deep(.brand-slots193) {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
  margin: 12px 0;
}
.settings-embed :deep(.brand-slot193) {
  display: grid;
  grid-template-columns: 160px minmax(0, 1fr);
  column-gap: 16px;
  row-gap: 8px;
  align-items: start;
}
.settings-embed :deep(.brand-slot193 > label) {
  grid-column: 1;
  grid-row: 1 / span 2;
  margin: 0;
  padding-top: 6px;
  text-align: right;
  font-size: 13px;
}
.settings-embed :deep(.brand-slot193 > :not(label)) {
  grid-column: 2;
}
.settings-embed :deep(.brand-slot193 > img) {
  justify-self: start;
  width: auto;
  height: 96px;
  max-width: 240px;
  margin: 0;
  padding: 12px;
  border: 1px dashed var(--line-control);
  border-radius: var(--radius-md);
  background: var(--bg-raised);
  object-fit: contain;
}
.settings-embed :deep(.brand-slot193 > input) {
  max-width: 360px;
}
@media (max-width: 768px) {
  .settings-layout {
    grid-template-columns: minmax(0, 1fr);
    min-height: 0;
  }
  .settings-tabs {
    flex-direction: row;
    overflow-x: auto;
    padding: 8px;
    border-right: 0;
    border-bottom: 1px solid var(--line);
  }
  .settings-tabs button {
    flex: none;
    width: auto;
    height: 34px;
    min-height: 34px;
    padding: 0 10px;
  }
  .settings-panel {
    padding: 16px;
  }
  .setting-field,
  .settings-embed :deep(.platform-access),
  .settings-embed :deep(.scanpay-settings192),
  .settings-embed :deep(.scan-js-settings202),
  .settings-embed :deep(.captcha-key203),
  .settings-embed :deep(.buyer-fee104),
  .settings-embed :deep(.proxy-credentials),
  .settings-embed :deep(.brand-slot193) {
    grid-template-columns: minmax(0, 1fr);
  }
  .setting-field > label,
  .settings-embed :deep(.brand-slot193 > label) {
    text-align: left;
    grid-row: auto;
  }
  .setting-field > :not(label),
  .settings-embed :deep(.brand-slot193 > :not(label)) {
    grid-column: 1;
  }
  .settings-embed :deep(h2),
  .settings-embed :deep(h3) {
    text-align: left;
  }
  .settings-embed :deep(*) {
    grid-column: 1 !important;
  }
  .settings-embed :deep(.site-brand-settings193 > button),
  .settings-embed :deep(.site-brand-settings193 > p) {
    margin-left: 0;
  }
  .settings-actions {
    padding-left: 0;
  }
}
</style>
