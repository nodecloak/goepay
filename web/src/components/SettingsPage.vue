<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount } from "vue";
import { ApiClient, ApiError } from "../api";
import type { ResourceSchema } from "../resources";
import BusinessNoticeEvents from "./BusinessNoticeEvents.vue";
import {
  settingsTabs,
  settingDefinition,
  settingDisplay,
  settingStorage,
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
  schema = ref<ResourceSchema | null>(null),
  definitions = ref<SettingDefinition[]>([]),
  records = ref<Record<string, SettingRecord>>({}),
  values = ref<Record<string, string>>({}),
  loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref("");
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
  definitions.value.filter((d) => d.tab === active.value),
);
function original(d: SettingDefinition) {
  const r = records.value[d.key];
  return r ? settingDisplay(d, r.value) : "";
}
function dirty(d: SettingDefinition) {
  return values.value[d.key] !== original(d);
}
const changed = computed(() => fields.value.filter(dirty));
let controller: AbortController | undefined,
  alive = true;
async function load() {
  if (busy.value) return;
  controller?.abort();
  const c = new AbortController();
  controller = c;
  loading.value = true;
  error.value = "";
  uncertain.value = true;
  try {
    const loaded = await props.client.request<ResourceSchema>(
      "/resources/settings/schema",
      { signal: c.signal },
    );
    if (c.signal.aborted || !alive) return;
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
      const data = await props.client.request<{
        items: SettingRecord[];
        total: number;
      }>(`/resources/settings?page=${page}&limit=100`, { signal: c.signal });
      if (c.signal.aborted || !alive) return;
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
        found[d.key] ? settingDisplay(d, found[d.key]!.value) : "",
      ]),
    );
    schema.value = loaded;
    definitions.value = defs;
    records.value = found;
    values.value = draft;
    uncertain.value = false;
  } catch (e) {
    if (!c.signal.aborted && alive)
      error.value = e instanceof Error ? e.message : "设置读取失败";
  } finally {
    if (!c.signal.aborted && alive) loading.value = false;
  }
}
async function save() {
  if (!canWrite.value || busy.value || uncertain.value || !changed.value.length)
    return;
  error.value = "";
  notice.value = "";
  let saved = 0;
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
        const current = await props.client.request<SettingRecord>(
          `/resources/settings/${encodeURIComponent(record.id)}`,
        );
        if (!alive) return;
        if (current.key !== record.key || current.value !== record.value)
          throw new ApiError(`${d.label}已由其它操作修改，请刷新核对`, 409, -1);
      }
      if (
        (record && !schema.value?.permissions.update) ||
        (!record && !schema.value?.permissions.create)
      )
        throw new ApiError(`${d.label}没有保存权限`, 403, -1);
      const row = await props.client.request<SettingRecord>(
        record
          ? `/resources/settings/${encodeURIComponent(record.id)}`
          : "/resources/settings",
        {
          method: record ? "PUT" : "POST",
          body: record ? { value } : { key: d.key, value },
        },
      );
      if (!alive) return;
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
    }
    notice.value = `已保存${saved}项设置`;
  } catch (e) {
    if (!alive) return;
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
    if (alive) busy.value = false;
  }
}
function navigate(event: KeyboardEvent, index: number) {
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
  proxy: "当前没有在线中转代理配置功能。",
  advanced:
    "Go服务的运行参数通过部署配置管理；原PHP/Swoole脚本和在线执行功能不适用。",
};
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  controller?.abort();
});
</script>
<template>
  <section class="panel settings-page">
    <div class="toolbar">
      <span class="muted">按分类维护平台设置</span
      ><button :disabled="loading || busy" @click="load">刷新设置</button>
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
        :disabled="busy"
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
        <p v-if="!canWrite" class="muted">
          当前为只读查看，修改需要本人超级管理员会话。
        </p>
        <p v-if="guidance[active]" class="muted">{{ guidance[active] }}</p>
        <div v-if="active === 'merchant'" class="toolbar">
          <a href="/admin/merchant/group-buy">用户组购买设置</a
          ><a href="/admin/merchant/group-list">用户组设置</a>
        </div>
        <div v-if="active === 'risk'" class="toolbar">
          <a href="/admin/trade/blacklist">黑名单管理</a
          ><a href="/admin/system/order-subject-policies">订单名称限制</a>
        </div>
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
        <p v-if="!fields.length" class="empty">
          该分类当前没有可在线维护的设置项。
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
                :disabled="!writable(d) || busy || uncertain"
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
                :disabled="!writable(d) || busy || uncertain"
                maxlength="1000"
                placeholder="未配置，使用系统默认"
              />
              <small v-if="d.hint" class="muted">{{ d.hint }}</small>
              <button
                v-if="dirty(d)"
                type="button"
                class="link-button"
                :disabled="busy"
                @click="values[d.key] = original(d)"
              >
                撤销此项编辑
              </button>
            </div>
          </div>
          <div class="toolbar">
            <button
              class="primary"
              :disabled="!canWrite || busy || uncertain || !changed.length"
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
