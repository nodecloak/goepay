<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch, toRaw } from "vue";
import { useRoute, useRouter } from "vue-router";
import type { ApiClient } from "../api";
import {
  adminSharingRule,
  sharingID,
  sharingPageTotal,
  type AdminSharingRule,
} from "../sharing-overview";
import { minorYuan } from "../pro-console-fields";
import { fundsDefinitiveRefusal212 } from "../funds-intent212";
import {
  sharingRequestContext,
  type SharingRequestContext,
} from "../sharing-request-context";
import ChannelSharingRuleList from "./ChannelSharingRuleList.vue";
import KitFilterBar from "./KitFilterBar.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitSelect from "./KitSelect.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitPayMethod from "./KitPayMethod.vue";
import KitPagination from "./KitPagination.vue";
import KitModal from "./KitModal.vue";
import KitEmpty from "./KitEmpty.vue";
import ShellIcon from "./ShellIcon.vue";

const props = defineProps<{ client: ApiClient }>();
const router = useRouter(),
  route = useRoute();
const fresh = () => ({
  merchant: "",
  channel: "",
  field: "rule_id",
  keyword: "",
  enabled: "",
});
const draft = ref(fresh()),
  applied = ref(fresh()),
  rows = ref<AdminSharingRule[]>([]);
const page = ref(1),
  pageSize = ref(20),
  total = ref(0),
  loading = ref(false),
  error = ref(""),
  notice = ref("");
const routeValid = ref(true);
const managing = ref(""),
  focusChannel = ref(""),
  focusRule = ref(""),
  createOnLoad = ref(false),
  editorBusy = ref(false);
const choosing = ref(false),
  chosenOwner = ref(""),
  choosingError = ref("");
const change = ref<{
    row: AdminSharingRule;
    enabled: boolean;
    uncertain: boolean;
  }>(),
  changeBusy = ref(false),
  changeError = ref("");
const fields = [
  { value: "rule_id", label: "规则ID" },
  { value: "receiver_id", label: "接收方ID" },
];
const statuses = [
  { value: "", label: "全部状态" },
  { value: "true", label: "已开启" },
  { value: "false", label: "已关闭" },
];
let alive = true,
  version = 0,
  controller: AbortController | undefined;
const sessionScope = sharingRequestContext(
  () => ({
    client: toRaw(props.client),
    csrf: props.client.csrfToken,
    area: "admin",
  }),
  () => {
    ++version;
    controller?.abort();
    rows.value = [];
    total.value = 0;
    loading.value = false;
    managing.value = "";
    editorBusy.value = false;
    choosing.value = false;
    change.value = undefined;
    changeBusy.value = false;
    loadedContext = undefined;
    error.value = "会话已变化，请重新查询分账规则";
    notice.value = "";
  },
);
const context = sessionScope.capture,
  current = sessionScope.current;
let loadedContext: SharingRequestContext | undefined;
function params() {
  const f = applied.value,
    q = new URLSearchParams({
      limit: String(pageSize.value),
      offset: String((page.value - 1) * pageSize.value),
    });
  if (f.merchant) q.set("merchant_id", f.merchant);
  if (f.channel) q.set("channel_id", f.channel);
  if (f.keyword) q.set(f.field, f.keyword);
  if (f.enabled) q.set("enabled", f.enabled);
  return q;
}
async function load() {
  if (!alive || !routeValid.value) return;
  if (managing.value || changeBusy.value) return;
  const c = context();
  controller?.abort();
  const ac = new AbortController();
  controller = ac;
  const token = ++version;
  loading.value = true;
  error.value = "";
  try {
    const result = await c.client.request<{
      items: unknown[];
      total: unknown;
      limit: number;
    }>(`/channel-sharing/rule-overview?${params()}`, { signal: ac.signal });
    if (!current(c) || token !== version || ac.signal.aborted) return;
    if (
      !Array.isArray(result.items) ||
      result.items.length > pageSize.value ||
      result.limit !== pageSize.value
    )
      throw Error("分账规则总览分页无效");
    const found = result.items.map(adminSharingRule);
    if (
      new Set(found.map((r) => r.rule.id)).size !== found.length ||
      found.some(
        (r) =>
          (applied.value.merchant &&
            r.rule.merchant_id !== applied.value.merchant) ||
          (applied.value.channel &&
            r.rule.channel_id !== applied.value.channel),
      )
    )
      throw Error("分账规则总览归属不匹配");
    rows.value = found;
    loadedContext = c;
    total.value = sharingPageTotal(result.total);
    const last = Math.max(1, Math.ceil(total.value / pageSize.value));
    if (page.value > last) {
      page.value = last;
      void load();
      return;
    }
  } catch (e) {
    if (current(c) && token === version && !ac.signal.aborted) {
      rows.value = [];
      total.value = 0;
      error.value = e instanceof Error ? e.message : "读取分账规则总览失败";
    }
  } finally {
    if (current(c) && token === version) loading.value = false;
  }
}
function query() {
  if (!routeValid.value) {
    error.value = "链接筛选无效，请先重置后查询";
    return;
  }
  if (changeBusy.value) return;
  for (const [label, value] of [
    ["商户ID", draft.value.merchant],
    ["通道ID", draft.value.channel],
    ["搜索内容", draft.value.keyword],
  ]) {
    if (value!.trim() && !sharingID(value!.trim())) {
      error.value = label + "请输入有效正整数ID";
      return;
    }
  }
  applied.value = {
    ...draft.value,
    merchant: draft.value.merchant.trim(),
    channel: draft.value.channel.trim(),
    keyword: draft.value.keyword.trim(),
  };
  page.value = 1;
  void load();
}
function reset() {
  if (changeBusy.value) return;
  const clean = { ...route.query };
  let changed = false;
  for (const key of ["merchant_id", "channel_id", "rule_id"]) {
    if (clean[key] !== undefined) changed = true;
    delete clean[key];
  }
  if (changed) {
    loading.value = true;
    void router.replace({ path: route.path, query: clean }).catch(() => {
      loading.value = false;
      error.value = "重置链接失败，请重新打开规则总览";
    });
    return;
  }
  routeValid.value = true;
  draft.value = fresh();
  applied.value = fresh();
  page.value = 1;
  void load();
}
function manage(owner: string, row?: AdminSharingRule) {
  if (!loadedContext || !current(loadedContext)) return;
  if (!sharingID(owner) || loading.value || changeBusy.value) return;
  ++version;
  controller?.abort();
  managing.value = owner;
  focusChannel.value = row?.rule.channel_id || "";
  focusRule.value = row?.rule.id || "";
  createOnLoad.value = !row;
  choosing.value = false;
  editorBusy.value = false;
}
function add() {
  chosenOwner.value = applied.value.merchant;
  choosingError.value = "";
  choosing.value = true;
}
function chooseOwner() {
  if (!sharingID(chosenOwner.value.trim())) {
    choosingError.value = "请输入有效商户ID";
    return;
  }
  manage(chosenOwner.value.trim());
}
function back() {
  if (editorBusy.value) return;
  managing.value = "";
  void load();
}
function record(row: AdminSharingRule) {
  void router.push({
    path: "/admin/profitsharing/order",
    query: { merchant_id: row.rule.merchant_id, rule_id: row.rule.id },
  });
}
function beginChange(row: AdminSharingRule) {
  if (!loadedContext || !current(loadedContext)) return;
  if (loading.value) return;
  change.value = { row, enabled: !row.rule.enabled, uncertain: false };
  changeError.value = "";
}
function matchingChangedRule(
  raw: any,
  original: AdminSharingRule,
  enabled: boolean,
) {
  const checked = adminSharingRule({
      rule: raw,
      receivers: original.receivers,
    }).rule,
    prior = original.rule;
  if (
    checked.merchant_id !== prior.merchant_id ||
    checked.channel_id !== prior.channel_id ||
    checked.enabled !== enabled ||
    checked.id === prior.id ||
    BigInt(checked.revision) !== BigInt(prior.revision) + 1n ||
    checked.minimum_amount !== prior.minimum_amount ||
    JSON.stringify(checked.items) !== JSON.stringify(prior.items) ||
    JSON.stringify(checked.methods) !== JSON.stringify(prior.methods)
  )
    throw Error("原规则变更结果尚未核实，请保留原版本并继续核对");
}
async function saveChange(checkOnly = false) {
  if (!loadedContext || !current(loadedContext)) return;
  const action = change.value;
  if (!action || changeBusy.value || (action.uncertain && !checkOnly)) return;
  const c = context(),
    prior = action.row.rule;
  let dispatched = false;
  changeBusy.value = true;
  changeError.value = "";
  try {
    const result = checkOnly
      ? await c.client.request(
          `/channel-sharing/rules/${prior.channel_id}?merchant_id=${prior.merchant_id}`,
        )
      : await c.client.request(
          `/channel-sharing/rules/${prior.channel_id}${action.enabled ? "" : "/disable"}`,
          {
            method: "POST",
            beforeDispatch: () => {
              dispatched = true;
            },
            body: {
              merchant_id: prior.merchant_id,
              expected: prior.id,
              ...(action.enabled
                ? {
                    enabled: true,
                    minimum_amount: prior.minimum_amount,
                    methods: prior.methods.slice(),
                    items: prior.items.map((i) => ({ ...i })),
                  }
                : {}),
            },
          },
        );
    if (!current(c) || change.value !== action) return;
    matchingChangedRule(result, action.row, action.enabled);
    change.value = undefined;
    notice.value = action.enabled
      ? "已开启该商户的分账规则"
      : "已关闭该商户的分账规则";
    changeBusy.value = false;
    await load();
  } catch (e) {
    if (!current(c) || change.value !== action) return;
    if (checkOnly || (dispatched && !fundsDefinitiveRefusal212(e)))
      action.uncertain = true;
    changeError.value = e instanceof Error ? e.message : "规则变更结果未核实";
  } finally {
    if (current(c)) changeBusy.value = false;
  }
}
function readRoute() {
  routeValid.value = true;
  managing.value = "";
  editorBusy.value = false;
  ++version;
  controller?.abort();
  rows.value = [];
  total.value = 0;
  loading.value = false;
  error.value = "";
  draft.value = fresh();
  applied.value = fresh();
  page.value = 1;
  for (const [key, field] of [
    ["merchant_id", "merchant"],
    ["channel_id", "channel"],
    ["rule_id", "keyword"],
  ] as const) {
    const value = route.query[key];
    if (value !== undefined && !sharingID(value)) {
      routeValid.value = false;
      error.value = "分账规则链接中的ID无效";
      return;
    }
    if (value) draft.value[field] = value;
  }
  applied.value = { ...draft.value };
  void load();
}
onMounted(readRoute);
watch(
  () => [route.query.merchant_id, route.query.channel_id, route.query.rule_id],
  readRoute,
);
watch(
  () => [props.client, props.client.csrfToken],
  () => {
    managing.value = "";
    choosing.value = false;
    change.value = undefined;
    changeBusy.value = false;
    editorBusy.value = false;
    notice.value = "";
    readRoute();
  },
);
onBeforeUnmount(() => {
  alive = false;
  sessionScope.dispose();
  ++version;
  controller?.abort();
});
</script>

<template>
  <div v-if="managing" class="list-page">
    <div class="panel management-context">
      <span
        >当前管理商户 <strong class="pa-c-num">{{ managing }}</strong></span
      >
      <button type="button" class="pa-btn" :disabled="editorBusy" @click="back">
        返回规则总览
      </button>
    </div>
    <ChannelSharingRuleList
      :key="managing"
      :client="client"
      area="admin"
      :initial-merchant-id="managing"
      :initial-channel-id="focusChannel"
      :initial-rule-id="focusRule"
      :auto-create="createOnLoad"
      @busy="editorBusy = $event"
    />
  </div>
  <div v-else class="list-page">
    <KitFilterBar :loading="loading" @query="query" @reset="reset">
      <KitField label="商户ID"
        ><KitInput
          v-model="draft.merchant"
          aria-label="商户ID"
          inputmode="numeric"
          placeholder="全部商户"
      /></KitField>
      <KitField label="搜索字段"
        ><KitSelect
          v-model="draft.field"
          :options="fields"
          aria-label="搜索字段"
      /></KitField>
      <KitField label="搜索内容"
        ><KitInput
          v-model="draft.keyword"
          aria-label="搜索内容"
          placeholder="规则ID或接收方ID"
          inputmode="numeric"
      /></KitField>
      <KitField label="状态"
        ><KitSelect
          v-model="draft.enabled"
          :options="statuses"
          aria-label="状态"
      /></KitField>
      <template #extra
        ><KitField label="通道ID"
          ><KitInput
            v-model="draft.channel"
            aria-label="通道ID"
            inputmode="numeric" /></KitField
      ></template>
    </KitFilterBar>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <section class="panel records" aria-label="分账规则总览">
      <div class="table-tools">
        <button
          type="button"
          class="pa-btn pa-btn--primary"
          :disabled="loading"
          @click="add"
        >
          <ShellIcon name="plus" />新增
        </button>
        <div class="table-tools-end">
          <button
            type="button"
            class="pa-btn"
            :disabled="loading"
            @click="load"
          >
            <ShellIcon name="refresh" />刷新
          </button>
        </div>
      </div>
      <p v-if="loading" role="status" class="table-note">正在读取分账规则…</p>
      <div class="table-scroll">
        <table class="pa-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>支付通道</th>
              <th>商户ID</th>
              <th>商户子通道</th>
              <th>分账接收方+分账比例</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rows" :key="row.rule.id">
              <td class="pa-c-num">{{ row.rule.id }}</td>
              <td>
                <KitPayMethod :type="row.rule.provider" /><small class="muted"
                  >({{ row.rule.channel_id }})</small
                >
              </td>
              <td class="pa-c-num">{{ row.rule.merchant_id }}</td>
              <td title="此规则按商户和支付通道配置，未单独绑定子通道">—</td>
              <td>
                <div
                  v-for="item in row.rule.items"
                  :key="item.receiver_id"
                  class="receiver-line"
                >
                  <span>{{
                    row.receivers.find((r) => r.id === item.receiver_id)
                      ?.account_masked
                  }}</span
                  ><span class="pa-c-num">{{
                    item.mode === "rate"
                      ? (Number(item.rate_bps) / 100).toFixed(2) + "%"
                      : "¥" + minorYuan(item.fixed_amount)
                  }}</span>
                </div>
              </td>
              <td>
                <KitStatusTag
                  :text="row.rule.enabled ? '已开启' : '已关闭'"
                  :tone="row.rule.enabled ? 'success' : 'neutral'"
                />
              </td>
              <td>
                <div class="pa-row-actions">
                  <button
                    type="button"
                    class="pa-btn"
                    :disabled="loading"
                    @click="manage(row.rule.merchant_id, row)"
                  >
                    编辑
                  </button>
                  <button
                    type="button"
                    class="pa-btn"
                    :disabled="loading"
                    @click="beginChange(row)"
                  >
                    {{ row.rule.enabled ? "关闭" : "开启" }}
                  </button>
                  <button
                    type="button"
                    class="pa-btn"
                    :disabled="loading"
                    @click="record(row)"
                  >
                    记录
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!rows.length">
              <td colspan="7">
                <KitEmpty
                  :text="
                    loading
                      ? '正在读取…'
                      : error
                        ? '读取失败，请刷新重试'
                        : '暂无数据'
                  "
                />
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <KitPagination
        v-if="total"
        v-model:page="page"
        v-model:pageSize="pageSize"
        :total="total"
        @change="load"
      />
    </section>
    <KitModal
      v-if="choosing"
      title="选择配置商户"
      submit-text="继续配置"
      @close="choosing = false"
      @submit="chooseOwner"
    >
      <p v-if="choosingError" role="alert" class="error">{{ choosingError }}</p>
      <KitField label="商户ID" required
        ><KitInput
          v-model="chosenOwner"
          aria-label="配置商户ID"
          inputmode="numeric"
      /></KitField>
    </KitModal>
    <KitModal
      v-if="change"
      :title="change.enabled ? '开启分账规则' : '关闭分账规则'"
      :loading="changeBusy"
      :submit-disabled="change.uncertain"
      submit-text="确认变更"
      @close="change = undefined"
      @submit="saveChange()"
    >
      <p>
        商户 {{ change.row.rule.merchant_id }} · 通道
        {{ change.row.rule.channel_id }} · 规则 {{ change.row.rule.id }}（版本
        {{ change.row.rule.revision }}）
      </p>
      <p>
        确认{{
          change.enabled ? "开启" : "关闭"
        }}该商户的分账规则？已有支付订单的规则快照保持不变。
      </p>
      <p v-if="changeError" role="alert" class="error">{{ changeError }}</p>
      <div v-if="change.uncertain">
        <p>结果未核实，不会重复提交。请核对原商户、通道和规则版本。</p>
        <button
          type="button"
          class="pa-btn"
          :disabled="changeBusy"
          @click="saveChange(true)"
        >
          核对原规则
        </button>
      </div>
    </KitModal>
  </div>
</template>

<style scoped>
.management-context {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  padding: 16px;
}
.receiver-line {
  display: flex;
  gap: 12px;
  justify-content: space-between;
}
td small {
  display: block;
}
.pa-row-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.pa-c-num {
  white-space: nowrap;
}
</style>
