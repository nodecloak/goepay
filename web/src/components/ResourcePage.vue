<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";
import { ApiClient, displayValue } from "../api";
import {
  resourceListFields,
  isCredentialField,
  consoleValue,
  consoleColumnLabel,
  merchantGroupLabel,
  merchantFieldValue,
  approvedMerchantDomains,
} from "../pro-console-fields";
import {
  validateFields,
  buildPayload,
  formValues,
  type ResourceSchema,
} from "../resources";
import ResourceFields from "./ResourceFields.vue";
import CasePanel from "./CasePanel.vue";
import RuleExecutionPanel from "./RuleExecutionPanel.vue";
import CollectionCodePanel from "./CollectionCodePanel.vue";
import ChannelStopPanel from "./ChannelStopPanel.vue";
import MerchantDelegationPanel from "./MerchantDelegationPanel.vue";
import BusinessNoticeEvents from "./BusinessNoticeEvents.vue";
import { businessNoticeSettingsFields } from "../business-notice";
const props = defineProps<{
  client: ApiClient;
  resource: string;
  adminClass?: string;
  ownSession?: boolean;
}>();
const delegation = ref<Record<string, unknown> | null>(null);
const canDelegate = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.resource === "merchants" &&
    schema.value?.actions?.some(
      (a) => (typeof a === "string" ? a : a.name) === "sso",
    ),
);
const entityLabel = computed(
  () =>
    (
      ({
        merchants: "商户",
        "order-subject-policies": "订单名称限制策略",
        "admin-users": "管理员",
        groups: "用户组",
        domains: "授权域名",
        "invitation-codes": "邀请码",
        announcements: "公告",
        settings: "系统设置项",
        channels: "支付通道",
        blacklist: "黑名单项",
        "collection-styles": "收款码样式",
        "pay-types": "支付方式",
        "pay-rolls": "轮询配置",
        "weixin-apps": "公众号小程序",
        "onboarding-channels": "进件渠道",
        "onboarding-applications": "本地进件资料",
        subchannels: "子通道资料",
        complaints: "本地投诉工单",
        "complaint-tasks": "同步任务配置",
        "cron-tasks": "计划任务",
        "settlement-settings": "结算收款资料",
        "collection-codes": "收款码资料",
        "plugin-configs": "本地扩展资料",
        "template-configs": "模板资料",
        "addon-configs": "扩展参数",
        "profitsharing-rules": "站内分账规则",
      }) as Record<string, string>
    )[props.resource] ||
    schema.value?.title ||
    "记录",
);
const schema = ref<ResourceSchema | null>(null),
  items = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  page = ref(1),
  query = ref(""),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
const mode = ref<"create" | "edit" | "detail" | "delete" | null>(null),
  selected = ref<Record<string, unknown>>({}),
  values = ref<Record<string, unknown>>({}),
  fieldErrors = ref<Record<string, string>>({});
let controller: AbortController | undefined,
  detailController: AbortController | undefined,
  contextEpoch = 0;
function resetContext() {
  contextEpoch++;
  controller?.abort();
  detailController?.abort();
  schema.value = null;
  items.value = [];
  total.value = 0;
  page.value = 1;
  query.value = "";
  loading.value = false;
  busy.value = false;
  mode.value = null;
  selected.value = {};
  values.value = {};
  fieldErrors.value = {};
  delegation.value = null;
  error.value = "";
  notice.value = "";
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.resource,
    props.adminClass,
  ],
  () => {
    resetContext();
    void load();
  },
  { flush: "sync" },
);
const endpoint = computed(
  () => "/resources/" + encodeURIComponent(props.resource),
);
const fields = computed(
  () =>
    schema.value?.fields.filter(
      (f) =>
        !f.write_only && f.type !== "password" && !isCredentialField(f.name),
    ) || [],
);
const listFields = computed(() =>
  resourceListFields(props.resource, schema.value?.fields || []),
);
const hasGroupProjection = computed(
  () =>
    props.resource === "merchants" &&
    schema.value?.fields.some((f) =>
      ["effective_group_id", "effective_group_name"].includes(f.name),
    ),
);
const hasDomainProjection = computed(
  () =>
    props.resource === "merchants" &&
    schema.value?.fields.some((f) => f.name === "approved_domains"),
);
const formSchema = computed<ResourceSchema | null>(() =>
  schema.value
    ? {
        ...schema.value,
        fields: businessNoticeSettingsFields(
          schema.value.fields.map((f) =>
            mode.value === "edit" &&
            ((props.resource === "merchants" &&
              selected.value.login_username &&
              f.name === "login_username") ||
              (props.resource === "admin-users" && f.name === "username"))
              ? { ...f, read_only: true }
              : f,
          ),
          props.resource === "settings" ? values.value.key : null,
        ),
      }
    : null,
);
const message = (e: unknown) =>
  e instanceof Error ? e.message : "操作失败，请重试";
async function refreshCase() {
  const current = contextEpoch,
    client = props.client,
    path = endpoint.value + "/" + encodeURIComponent(String(selected.value.id));
  await load();
  if (current !== contextEpoch) return;
  detailController?.abort();
  const c = new AbortController();
  detailController = c;
  try {
    const record = await client.request<Record<string, unknown>>(path, {
      signal: c.signal,
    });
    if (current === contextEpoch && !c.signal.aborted) selected.value = record;
  } catch (e) {
    if (current === contextEpoch && !c.signal.aborted) error.value = message(e);
  }
}
async function load() {
  controller?.abort();
  controller = new AbortController();
  const c = controller,
    current = contextEpoch,
    client = props.client,
    path = endpoint.value;
  loading.value = true;
  error.value = "";
  try {
    if (!schema.value) {
      const loadedSchema = await client.request<ResourceSchema>(
        path + "/schema",
        { signal: c.signal },
      );
      if (c.signal.aborted || current !== contextEpoch) return;
      schema.value = loadedSchema;
    }
    const q = new URLSearchParams({
      page: String(page.value),
      limit: "20",
      q: query.value,
    });
    const data = await client.request<{
      items: Record<string, unknown>[];
      total: number;
    }>(path + "?" + q, { signal: c.signal });
    if (!c.signal.aborted && current === contextEpoch) {
      items.value = data.items || [];
      total.value = data.total || 0;
    }
  } catch (e) {
    if (!c.signal.aborted && current === contextEpoch) error.value = message(e);
  } finally {
    if (!c.signal.aborted && current === contextEpoch) loading.value = false;
  }
}
async function open(
  action: "create" | "edit" | "detail" | "delete",
  row: Record<string, unknown> = {},
) {
  detailController?.abort();
  const c = new AbortController(),
    current = contextEpoch;
  detailController = c;
  error.value = "";
  notice.value = "";
  fieldErrors.value = {};
  selected.value = row;
  if (action === "create") {
    values.value = formValues(schema.value!);
    mode.value = action;
    return;
  }
  busy.value = true;
  try {
    const record = await props.client.request<Record<string, unknown>>(
      endpoint.value + "/" + encodeURIComponent(String(row.id)),
      { signal: c.signal },
    );
    if (c.signal.aborted || current !== contextEpoch) return;
    selected.value = record;
    values.value = formValues(schema.value!, selected.value);
    mode.value = action;
  } catch (e) {
    if (!c.signal.aborted && current === contextEpoch) error.value = message(e);
  } finally {
    if (!c.signal.aborted && current === contextEpoch) busy.value = false;
  }
}
async function save() {
  const current = contextEpoch;
  if (!schema.value) return;
  if (props.resource === "merchants") {
    const username = String(
      values.value.login_username || selected.value.login_username || "",
    );
    const password = String(values.value.login_password || "");
    if (username && !selected.value.login_username && !password) {
      error.value = "设置后台登录账号时请同时填写登录密码";
      return;
    }
    if (password && !username) {
      error.value = "请先填写后台登录账号";
      return;
    }
  }
  fieldErrors.value = validateFields(
    formSchema.value!,
    values.value,
    mode.value === "edit",
  );
  if (Object.keys(fieldErrors.value).length) return;
  busy.value = true;
  error.value = "";
  try {
    const payload = buildPayload(
      formSchema.value!,
      values.value,
      mode.value === "edit",
    );
    if (props.resource === "merchants") {
      if (!values.value.login_username) delete payload.login_username;
      if (!values.value.login_password) delete payload.login_password;
    }
    await props.client.request(
      endpoint.value +
        (mode.value === "edit"
          ? "/" + encodeURIComponent(String(selected.value.id))
          : ""),
      {
        method: mode.value === "edit" ? "PATCH" : "POST",
        body: payload,
      },
    );
    if (current !== contextEpoch) return;
    notice.value = mode.value === "edit" ? "修改已保存" : "新增已保存";
    mode.value = null;
    await load();
  } catch (e) {
    if (current === contextEpoch) error.value = message(e);
  } finally {
    if (current === contextEpoch) busy.value = false;
  }
}
async function remove() {
  const current = contextEpoch;
  busy.value = true;
  error.value = "";
  try {
    await props.client.request(
      endpoint.value + "/" + encodeURIComponent(String(selected.value.id)),
      { method: "DELETE" },
    );
    if (current !== contextEpoch) return;
    mode.value = null;
    notice.value = "记录已删除";
    await load();
  } catch (e) {
    if (current === contextEpoch) error.value = message(e);
  } finally {
    if (current === contextEpoch) busy.value = false;
  }
}
onMounted(load);
onBeforeUnmount(resetContext);
</script>
<template>
  <section class="panel records">
    <ChannelStopPanel
      v-if="
        resource === 'channels' &&
        client.area === 'admin' &&
        adminClass === 'super'
      "
      :client="client"
    />
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
    <p v-if="schema?.description" class="resource-description muted">
      {{ schema.description }}
    </p>
    <form
      class="toolbar"
      @submit.prevent="
        page = 1;
        load();
      "
    >
      <div class="search-group">
        <label class="sr-only" :for="'search-' + resource"
          >搜索{{ schema?.title }}</label
        ><input
          :id="'search-' + resource"
          v-model="query"
          placeholder="搜索记录"
        /><button :disabled="loading">查询</button>
      </div>
      <div class="toolbar-actions">
        <button type="button" :disabled="loading || busy" @click="load">
          刷新</button
        ><button
          v-if="schema?.permissions.create"
          type="button"
          class="primary"
          :disabled="busy"
          @click="open('create')"
        >
          新增{{ entityLabel }}
        </button>
      </div>
    </form>
    <div v-if="loading" class="loading-row" role="status">正在加载记录…</div>
    <div v-else-if="schema" class="table-scroll">
      <table>
        <thead>
          <tr>
            <th scope="col">
              {{
                resource === "merchants"
                  ? hasGroupProjection
                    ? "商户号 / 用户组"
                    : "商户号"
                  : "编号"
              }}
            </th>
            <th v-for="field in listFields" :key="field.name" scope="col">
              {{
                resource === "merchants"
                  ? consoleColumnLabel(field.name, field.label)
                  : field.label
              }}
            </th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in items" :key="String(row.id)">
            <td>
              {{ row.id }}
              <template v-if="hasGroupProjection">
                <small class="merchant-balance-detail muted">{{
                  merchantGroupLabel(row)
                }}</small>
                <small
                  v-if="
                    typeof row.membership_expires_at === 'string' &&
                    row.membership_expires_at
                  "
                  class="merchant-balance-detail muted"
                  >{{
                    row.group_membership_active === true
                      ? "成员有效期至"
                      : "原成员到期时间"
                  }}（北京时间）：{{
                    consoleValue(
                      "membership_expires_at",
                      row.membership_expires_at,
                    )
                  }}</small
                >
              </template>
            </td>
            <td v-for="field in listFields" :key="field.name">
              <span class="cell">{{
                resource === "merchants"
                  ? merchantFieldValue(field.name, row[field.name])
                  : field.name === "enabled"
                    ? consoleValue(field.name, row[field.name])
                    : field.type === "boolean"
                      ? row[field.name]
                        ? "是"
                        : "否"
                      : displayValue(row[field.name])
              }}</span>
              <template
                v-if="
                  resource === 'merchants' && field.name === 'available_minor'
                "
              >
                <small
                  v-if="row.frozen_minor !== undefined"
                  class="merchant-balance-detail muted"
                  >冻结预留：{{
                    consoleValue("frozen_minor", row.frozen_minor)
                  }}</small
                >
                <small
                  v-if="row.income_held_minor !== undefined"
                  class="merchant-balance-detail muted"
                  >待解冻收入：{{
                    consoleValue("income_held_minor", row.income_held_minor)
                  }}</small
                >
              </template>
              <template
                v-if="hasDomainProjection && field.name === 'created_at'"
              >
                <small
                  v-for="domain in approvedMerchantDomains(
                    row.approved_domains,
                  )"
                  :key="domain"
                  class="merchant-balance-detail muted"
                  >{{ domain }}</small
                >
              </template>
            </td>
            <td>
              <div class="row-actions">
                <button
                  v-if="canDelegate"
                  class="link-button"
                  :disabled="busy"
                  @click="delegation = row"
                >
                  代登录商户
                </button>
                <button
                  class="link-button"
                  :disabled="busy"
                  @click="open('detail', row)"
                >
                  详情</button
                ><button
                  v-if="schema.permissions.update"
                  class="link-button"
                  :disabled="busy"
                  @click="open('edit', row)"
                >
                  编辑</button
                ><button
                  v-if="schema.permissions.delete"
                  class="link-button danger-text"
                  :disabled="busy"
                  @click="open('delete', row)"
                >
                  删除
                </button>
              </div>
            </td>
          </tr>
          <tr v-if="!items.length">
            <td :colspan="listFields.length + 2" class="empty">
              暂无记录，可通过上方操作添加
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="pagination">
      <span>共 {{ total }} 条记录</span>
      <div>
        <button
          :disabled="page <= 1 || loading"
          @click="
            page--;
            load();
          "
        >
          上一页</button
        ><span>第 {{ page }} 页</span
        ><button
          :disabled="page * 20 >= total || loading"
          @click="
            page++;
            load();
          "
        >
          下一页
        </button>
      </div>
    </div>
    <BusinessNoticeEvents
      v-if="
        resource === 'settings' &&
        client.area === 'admin' &&
        ownSession &&
        ['super', 'operator'].includes(adminClass ?? '')
      "
      :client="client"
      :own-session="ownSession"
      :admin-class="adminClass"
    />
  </section>
  <MerchantDelegationPanel
    v-if="delegation && canDelegate"
    :key="String(delegation.id)"
    :client="client"
    :merchant-id="String(delegation.id)"
    :name="String(delegation.name || delegation.id)"
    @close="delegation = null"
  />
  <div
    v-if="mode && schema"
    class="modal-backdrop"
    @keydown.esc="!busy && (mode = null)"
  >
    <section
      class="modal panel resource-modal"
      role="dialog"
      aria-modal="true"
      :aria-label="
        mode === 'create'
          ? '新增' + schema.title
          : mode === 'edit'
            ? '编辑' + schema.title
            : mode === 'delete'
              ? '删除确认'
              : schema.title + '详情'
      "
    >
      <h2>
        {{
          mode === "create"
            ? "新增"
            : mode === "edit"
              ? "编辑"
              : mode === "delete"
                ? "删除确认"
                : ""
        }}{{ schema.title }}{{ mode === "detail" ? "详情" : "" }}
      </h2>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <template v-if="mode === 'detail'"
        ><dl class="record-detail">
          <template v-for="field in fields" :key="field.name"
            ><dt>
              {{
                resource === "merchants"
                  ? consoleColumnLabel(field.name, field.label)
                  : field.label
              }}
            </dt>
            <dd>
              {{
                resource === "merchants"
                  ? merchantFieldValue(field.name, selected[field.name])
                  : displayValue(selected[field.name])
              }}
            </dd></template
          >
        </dl>
        <CasePanel
          v-if="['complaints', 'onboarding-applications'].includes(resource)"
          :client="client"
          :resource="resource"
          :id="String(selected.id)"
          :initial-status="String(selected.status || '')"
          @updated="refreshCase"
        />
        <RuleExecutionPanel
          v-if="resource === 'profitsharing-rules'"
          :id="String(selected.id)"
          :client="client"
          :enabled="!!selected.enabled"
        />
        <CollectionCodePanel
          v-if="resource === 'collection-codes'"
          :client="client"
          :id="String(selected.id)"
        />
        <button @click="mode = null">关闭</button></template
      ><template v-else-if="mode === 'delete'"
        ><p>确认删除编号 {{ selected.id }} 的记录？此操作会立即生效。</p>
        <div class="modal-actions">
          <button :disabled="busy" @click="mode = null">取消</button
          ><button class="danger-button" :disabled="busy" @click="remove">
            {{ busy ? "正在删除…" : "确认删除" }}
          </button>
        </div></template
      >
      <form v-else @submit.prevent="save">
        <template v-if="resource === 'merchants'"
          ><p class="muted">
            后台登录账号和密码可同时留空，仅使用
            API。填写登录密码会设置或重置后台登录凭据；已有账号不支持改名。
          </p>
          <p v-if="mode === 'edit' && selected.login_username" class="muted">
            已绑定登录账号：{{
              selected.login_username
            }}。禁用商户会使其登录会话失效。
          </p></template
        >
        <p v-if="resource === 'admin-users' && mode === 'edit'" class="muted">
          管理员账号：{{ selected.username }}。账号创建后不可更名。
        </p>
        <ResourceFields
          :fields="formSchema!.fields"
          :values="values"
          :errors="fieldErrors"
          :editing="mode === 'edit'"
          @change="(name, value) => (values[name] = value)"
        />
        <div class="modal-actions">
          <button type="button" :disabled="busy" @click="mode = null">
            取消</button
          ><button class="primary" :disabled="busy">
            {{ busy ? "正在保存…" : "保存" }}
          </button>
        </div>
      </form>
    </section>
  </div>
</template>
<style scoped>
.merchant-balance-detail {
  display: block;
  margin-top: 4px;
  white-space: nowrap;
}
</style>
