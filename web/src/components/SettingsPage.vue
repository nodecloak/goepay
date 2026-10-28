<script setup lang="ts">
import CorePaymentRisk from "./CorePaymentRisk.vue";
import PaymentDefend206 from "./PaymentDefend206.vue";
import { computed, ref, watch, toRaw, onBeforeUnmount } from "vue";
import { ApiClient, ApiError } from "../api";
import type { ResourceSchema } from "../resources";
import BusinessNoticeEvents from "./BusinessNoticeEvents.vue";
import RichTextEditor from "./RichTextEditor.vue";
import PlatformMerchantAccessSettings from "./PlatformMerchantAccessSettings.vue";
import ScanpaySettings192 from "./ScanpaySettings192.vue";
import ScanJSSettings202 from "./ScanJSSettings202.vue";
import SiteBrandSettings193 from "./SiteBrandSettings193.vue";
import MailSettings195 from "./MailSettings195.vue";
import CaptchaKeySettings203 from "./CaptchaKeySettings203.vue";
import ProxyCredentialSettings from "./ProxyCredentialSettings.vue";
import BuyerFeeSettings104 from "./BuyerFeeSettings104.vue";
import { siteKeys191 } from "../site191";
const emit = defineEmits<{ siteChanged: [] }>();
import {
  settingsTabs,
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
function dirty(d: SettingDefinition) {
  return values.value[d.key] !== original(d);
}
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
    mailBusy.value
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
    mailBusy.value
  )
    return;
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  const next =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? settingsTabs.length - 1
        : (index +
            (event.key === "ArrowRight" ? 1 : -1) +
            settingsTabs.length) %
          settingsTabs.length;
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
    <div class="toolbar">
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
        刷新设置
      </button>
    </div>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <div class="settings-tabs" role="tablist" aria-label="系统设置分类">
      <button
        v-for="(tab, index) in settingsTabs"
        :id="`settings-tab-${tab[0]}`"
        :key="tab[0]"
        role="tab"
        :aria-selected="active === tab[0]"
        :aria-controls="`settings-panel-${tab[0]}`"
        :tabindex="active === tab[0] ? 0 : -1"
        :disabled="
          busy || platformBusy || riskBusy || scanBusy || mailBusy || brandBusy
        "
        @click="active = tab[0]"
        @keydown="navigate($event, index)"
      >
        {{ tab[1] }}
      </button>
    </div>
    <div
      :id="`settings-panel-${active}`"
      role="tabpanel"
      :aria-labelledby="`settings-tab-${active}`"
    >
      <p v-if="loading" role="status">正在读取设置…</p>
      <template v-else-if="schema">
        <h2>{{ settingsTabs.find((t) => t[0] === active)?.[1] }}</h2>
        <MailSettings195
          v-if="active === 'mail'"
          :client="client"
          :admin-class="adminClass"
          :own-session="ownSession"
          @busy="mailBusy = $event"
        />
        <SiteBrandSettings193
          v-if="active === 'site'"
          :client="client"
          :admin-class="adminClass"
          :own-session="ownSession"
          @busy="brandBusy = $event"
        />
        <CaptchaKeySettings203
          v-if="active === 'site'"
          :client="client"
          :admin-class="adminClass"
          :own-session="ownSession"
        />
        <ProxyCredentialSettings
          v-if="active === 'proxy'"
          :client="client"
          :admin-class="adminClass"
          :own-session="ownSession"
        />
        <p v-if="!canWrite" class="muted">
          当前为只读查看，修改需要本人超级管理员会话。
        </p>
        <p v-if="guidance[active]" class="muted">{{ guidance[active] }}</p>
        <PlatformMerchantAccessSettings
          v-if="active === 'site' || active === 'merchant'"
          :key="active"
          :client="client"
          :admin-class="adminClass"
          :own-session="ownSession"
          :field="active === 'site' ? 'close_keylogin' : 'user_refund'"
          @busy="platformBusy = $event"
        />
        <div v-if="active === 'merchant'" class="toolbar">
          <a href="/admin/merchant/group-buy">用户组购买设置</a
          ><a href="/admin/merchant/group-list">用户组设置</a>
        </div>
        <ScanpaySettings192
          v-if="active === 'merchant'"
          :client="client"
          :admin-class="adminClass"
          :own-session="ownSession"
          @busy="scanpayBusy = $event"
        />
        <ScanJSSettings202
          v-if="active === 'merchant'"
          :client="client"
          :admin-class="adminClass"
          :own-session="ownSession"
          @busy="scanJSBusy = $event"
        />
        <div v-if="active === 'risk'" class="toolbar">
          <a href="/admin/trade/blacklist">黑名单管理</a
          ><a href="/admin/system/order-subject-policies">订单名称限制</a>
        </div>
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
        <div v-if="active === 'complaint'" class="toolbar">
          <a href="/admin/complain/task">投诉同步任务</a>
        </div>
        <div v-if="active === 'settle'" class="toolbar">
          <a href="/admin/settle/plans">自动结算计划</a>
        </div>
        <div v-if="active === 'notice'" class="toolbar">
          <a href="/admin/merchant/notice">商户消息提醒</a>
        </div>
        <div v-if="active === 'anounce'" class="toolbar">
          <a href="/admin/system/anounce">公告管理</a>
        </div>
        <BuyerFeeSettings104
          v-if="active === 'pay'"
          kind="global"
          :client="client"
          :allowed="ownSession && adminClass === 'super'"
          @busy="platformBusy = $event"
        />
        <p v-if="!fields.length && active !== 'mail'" class="empty">
          {{
            active === "site" || active === "merchant" || active === "risk"
              ? "该分类暂无其它通用设置项。"
              : "该分类当前没有可在线维护的设置项。"
          }}
        </p>
        <form v-else @submit.prevent="save">
          <div class="settings-fields">
            <div
              v-for="d in fields"
              :key="d.key"
              class="setting-field"
              :class="{ 'setting-dirty': dirty(d) }"
            >
              <label :for="`setting-${d.key}`">{{ d.label }}</label>
              <select
                v-if="d.kind === 'select'"
                :id="`setting-${d.key}`"
                v-model="values[d.key]"
                :aria-label="d.label"
                :disabled="
                  !writable(d) ||
                  busy ||
                  platformBusy ||
                  riskBusy ||
                  scanBusy ||
                  mailBusy ||
                  brandBusy ||
                  uncertain
                "
              >
                <option value="" disabled>未配置（使用系统默认）</option>
                <option
                  v-if="
                    values[d.key] &&
                    !d.options?.some((o) => o.value === values[d.key])
                  "
                  :value="values[d.key]"
                >
                  当前值：{{ values[d.key] }}（待核对）
                </option>
                <option v-for="o in d.options" :key="o.value" :value="o.value">
                  {{ o.label }}
                </option>
              </select>
              <RichTextEditor
                v-else-if="d.kind === 'html'"
                v-model="values[d.key]!"
                :label="d.label"
                :disabled="
                  !writable(d) ||
                  busy ||
                  platformBusy ||
                  riskBusy ||
                  scanBusy ||
                  mailBusy ||
                  brandBusy ||
                  uncertain
                "
              />
              <input
                v-else
                :id="`setting-${d.key}`"
                v-model="values[d.key]"
                :aria-label="d.label"
                :type="d.kind === 'email' ? 'email' : 'text'"
                :inputmode="
                  d.kind === 'money'
                    ? 'decimal'
                    : d.kind === 'integer'
                      ? 'numeric'
                      : undefined
                "
                :disabled="
                  !writable(d) ||
                  busy ||
                  platformBusy ||
                  riskBusy ||
                  scanBusy ||
                  mailBusy ||
                  brandBusy ||
                  uncertain
                "
                maxlength="1000"
                placeholder="未配置，使用系统默认"
              />
              <small v-if="d.hint" class="muted">{{ d.hint }}</small>
              <button
                v-if="dirty(d)"
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
                @click="values[d.key] = original(d)"
              >
                撤销此项编辑
              </button>
            </div>
          </div>
          <div class="toolbar">
            <button
              class="primary"
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
        </form>
        <BusinessNoticeEvents
          v-if="active === 'notice'"
          :client="client"
          :own-session="ownSession"
          :admin-class="adminClass"
        />
      </template>
    </div>
  </section>
</template>
<style scoped>
.settings-tabs {
  display: flex;
  overflow-x: auto;
  gap: 8px;
  border-bottom: 1px solid var(--line);
  margin: 12px 0 24px;
  padding: 0 0 8px;
}
.settings-tabs button {
  white-space: nowrap;
  background: none;
  border-color: transparent;
  flex-shrink: 0;
}
.settings-tabs button[aria-selected="true"] {
  color: var(--accent);
  border-bottom-color: var(--accent);
  border-radius: 0;
}
.settings-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 300px), 1fr));
  gap: 22px 28px;
}
.setting-field {
  min-width: 0;
}
.setting-field label {
  display: block;
  margin-bottom: 8px;
}
.setting-field input,
.setting-field select {
  width: 100%;
  min-width: 0;
}
.setting-field small {
  display: block;
  line-height: 1.6;
  margin-top: 6px;
}
.setting-dirty input,
.setting-dirty select {
  border-color: var(--accent);
}
.settings-page form > .toolbar {
  margin-top: 26px;
}
</style>
