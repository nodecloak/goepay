<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount, nextTick } from "vue";
import { ApiClient, ApiError } from "../api";
import { centsMoney } from "../settlement-terms";
import ShellIcon from "./ShellIcon.vue";
interface SaleGroup {
  id: string;
  name: string;
  enabled: boolean;
  sale_enabled: boolean;
  price_cents: string;
  duration_months: number | null;
  duration_days: number;
  sort: number;
  visible: string;
  description: string;
}
interface Configuration {
  enabled: boolean;
  revision: string;
  groups: SaleGroup[];
}
interface Draft {
  id: string;
  price: string;
  period: "months" | "legacy";
  months: string;
  sort: string;
  visible: string;
  description: string;
}
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
}>();
const configuration = ref<Configuration | null>(null),
  drafts = ref<Record<string, Draft>>({}),
  loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref(""),
  editing = ref("");
const canWrite = computed(
  () => props.adminClass === "super" && props.ownSession,
);
const published = computed(
  () =>
    configuration.value?.groups
      .filter((g) => g.sale_enabled)
      .sort((a, b) => a.sort - b.sort) || [],
);
const unpublished = computed(
  () => configuration.value?.groups.filter((g) => !g.sale_enabled) || [],
);
let controller: AbortController | undefined,
  alive = true;
function draft(g: SaleGroup): Draft {
  return {
    id: g.id,
    price: centsMoney(g.price_cents),
    period: g.duration_months === null ? "legacy" : "months",
    months: String(g.duration_months ?? ""),
    sort: String(g.sort),
    visible: g.visible,
    description: g.description,
  };
}
function accept(data: Configuration, reset: boolean) {
  configuration.value = data;
  if (reset)
    drafts.value = Object.fromEntries(data.groups.map((g) => [g.id, draft(g)]));
}
async function load() {
  controller?.abort();
  const c = new AbortController();
  controller = c;
  loading.value = true;
  error.value = "";
  try {
    const data = await props.client.request<Configuration>(
      "/membership-sales",
      { signal: c.signal },
    );
    if (!c.signal.aborted && alive) {
      accept(data, true);
      uncertain.value = false;
    }
  } catch (e) {
    if (!c.signal.aborted && alive)
      error.value = e instanceof Error ? e.message : "销售配置读取失败";
  } finally {
    if (!c.signal.aborted && alive) loading.value = false;
  }
}
function payload(g: SaleGroup, d: Draft) {
  if (!/^(0|[1-9]\d{0,9})(\.\d{1,2})?$/.test(d.price))
    throw Error(`${g.name}：请输入有效售价，最多两位小数`);
  const [yuan, fraction = ""] = d.price.split("."),
    price = BigInt(yuan!) * 100n + BigInt(fraction.padEnd(2, "0"));
  if (price > 100000000000n) throw Error(`${g.name}：售价超过上限`);
  const months = d.months === "" ? 0 : Number(d.months),
    sort = Number(d.sort);
  if (
    d.period === "months" &&
    (!/^\d*$/.test(d.months) ||
      !Number.isInteger(months) ||
      months < 0 ||
      months > 1200)
  )
    throw Error(`${g.name}：有效期应为0至1200个月，0或留空表示永久`);
  if (
    !/^\d+$/.test(d.sort) ||
    !Number.isInteger(sort) ||
    sort < 0 ||
    sort > 1000000
  )
    throw Error(`${g.name}：排序应为0至1000000的整数`);
  const visible = d.visible
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean)
    .join(",");
  if (visible !== "" && !/^(0|[1-9]\d*)(,(0|[1-9]\d*))*$/.test(visible))
    throw Error(`${g.name}：可见用户组应使用英文逗号分隔ID`);
  return {
    id: g.id,
    sale_enabled: g.sale_enabled,
    price_cents: price.toString(),
    duration_months: d.period === "months" ? months : null,
    sort,
    visible,
    description: d.description,
  };
}
async function save(
  mode: "all" | "toggle" | "global" | "description",
  group?: SaleGroup,
) {
  if (!configuration.value || !canWrite.value || busy.value || uncertain.value)
    return;
  error.value = "";
  notice.value = "";
  try {
    let groups: ReturnType<typeof payload>[] = [];
    if (mode === "all")
      groups = configuration.value.groups.map((g) =>
        payload(g, drafts.value[g.id]!),
      );
    else if (group) {
      const original = draft(group);
      if (mode === "description")
        original.description = drafts.value[group.id]!.description;
      const row = payload(group, original);
      if (mode === "toggle") row.sale_enabled = !group.sale_enabled;
      groups = [row];
    }
    busy.value = true;
    const data = await props.client.request<Configuration>(
      "/membership-sales",
      {
        method: "POST",
        body: {
          expected_revision: configuration.value.revision,
          enabled:
            mode === "global"
              ? !configuration.value.enabled
              : configuration.value.enabled,
          groups,
        },
      },
    );
    if (!alive) return;
    accept(data, mode === "all");
    if (mode === "description") editing.value = "";
    notice.value =
      mode === "toggle"
        ? group?.sale_enabled
          ? "已下架"
          : "已上架"
        : mode === "global"
          ? "用户组购买开关已保存"
          : "保存成功";
  } catch (e) {
    if (!alive) return;
    error.value = e instanceof Error ? e.message : "保存失败";
    if (
      busy.value &&
      (!(e instanceof ApiError) || e.status >= 500 || e.status === 409)
    ) {
      uncertain.value = true;
      error.value += "；请刷新核对当前配置后再保存";
    }
  } finally {
    if (alive) busy.value = false;
  }
}
async function editDescription(id: string) {
  editing.value = id;
  await nextTick();
  document.getElementById("sale-description")?.focus();
}
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  controller?.abort();
});
</script>
<template>
  <div class="group-sales">
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
    <p v-if="!canWrite" class="muted">
      当前为只读查看，保存需要本人超级管理员会话。
    </p>
    <section class="panel sale-policy-card" aria-label="用户组购买设置">
      <div class="sale-policy-head">
        <button
          type="button"
          class="sale-refresh"
          :disabled="busy || loading"
          @click="load"
        >
          <ShellIcon name="refresh" />刷新配置
        </button>
      </div>
      <p
        v-if="loading && !configuration"
        role="status"
        class="muted sale-loading"
      >
        正在读取销售配置…
      </p>
      <template v-if="configuration">
        <div class="sale-policy">
          <span>用户组购买开关</span
          ><button
            type="button"
            role="switch"
            class="sale-switch"
            :class="{ 'sale-switch--on': configuration.enabled }"
            aria-label="用户组购买开关"
            :aria-checked="configuration.enabled"
            :disabled="!canWrite || busy || uncertain"
            @click="save('global')"
          >
            <span class="sale-switch-track" aria-hidden="true"></span
            ><span>{{ configuration.enabled ? "已开启" : "已关闭" }}</span>
          </button>
        </div>
        <p class="muted sale-hint">
          有效期按日历月计算，0或留空表示永久；旧按天配置保留原期限，可明确切换到按月。可见用户组留空表示全部，多个ID以英文逗号分隔，0表示默认用户组。
        </p>
      </template>
    </section>
    <template v-if="configuration">
      <section class="panel records">
        <div class="table-tools">
          <h3 class="sale-table-title">已上架用户组</h3>
          <span class="table-summary"
            >售价、期限、排序和可见范围编辑后需保存；上下架和总开关单独保存。</span
          >
          <div class="table-tools-end">
            <button
              class="primary"
              :disabled="!canWrite || busy || uncertain"
              @click="save('all')"
            >
              {{ busy ? "正在保存…" : "保存全部" }}
            </button>
          </div>
        </div>
        <div class="table-scroll">
          <table class="sale-table">
            <thead>
              <tr>
                <th>GID</th>
                <th>用户组名称</th>
                <th>售价（元）</th>
                <th>有效期（月）</th>
                <th>排序</th>
                <th>可见用户组</th>
                <th>简介</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="g in published" :key="g.id">
                <td class="pa-c-num">{{ g.id }}</td>
                <td class="sale-name">
                  {{ g.name }}
                  <small v-if="!g.enabled" class="muted">用户组已禁用</small>
                </td>
                <td>
                  <input
                    v-model="drafts[g.id]!.price"
                    class="sale-price"
                    :aria-label="`售价（元） · ${g.name}`"
                    inputmode="decimal"
                    :disabled="!canWrite || busy || uncertain"
                  />
                </td>
                <td>
                  <div class="sale-period">
                    <select
                      v-model="drafts[g.id]!.period"
                      :aria-label="`期限类型 · ${g.name}`"
                      :disabled="!canWrite || busy || uncertain"
                    >
                      <option value="months">按月 / 永久</option>
                      <option v-if="g.duration_months === null" value="legacy">
                        旧按天配置
                      </option></select
                    ><input
                      v-if="drafts[g.id]!.period === 'months'"
                      v-model="drafts[g.id]!.months"
                      :aria-label="`有效期（月） · ${g.name}`"
                      inputmode="numeric"
                      placeholder="0或留空为永久"
                      :disabled="!canWrite || busy || uncertain"
                    /><span v-else class="sale-days"
                      >{{ g.duration_days }}天</span
                    >
                  </div>
                </td>
                <td>
                  <input
                    v-model="drafts[g.id]!.sort"
                    class="sale-sort"
                    :aria-label="`排序 · ${g.name}`"
                    inputmode="numeric"
                    :disabled="!canWrite || busy || uncertain"
                  />
                </td>
                <td>
                  <input
                    v-model="drafts[g.id]!.visible"
                    class="sale-visible"
                    :aria-label="`可见用户组 · ${g.name}`"
                    placeholder="全部"
                    :disabled="!canWrite || busy || uncertain"
                  />
                </td>
                <td>
                  <button
                    class="link-button"
                    :disabled="busy || uncertain"
                    @click="editDescription(g.id)"
                  >
                    编辑简介
                  </button>
                </td>
                <td>
                  <button
                    class="link-button danger-text"
                    :disabled="!canWrite || busy || uncertain"
                    @click="save('toggle', g)"
                  >
                    下架
                  </button>
                </td>
              </tr>
              <tr v-if="!published.length">
                <td colspan="8" class="empty">暂无已上架用户组</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
      <section class="panel records">
        <div class="table-tools">
          <h3 class="sale-table-title">未上架用户组</h3>
        </div>
        <div class="table-scroll">
          <table class="sale-table">
            <thead>
              <tr>
                <th>GID</th>
                <th>用户组名称</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="g in unpublished" :key="g.id">
                <td class="pa-c-num">{{ g.id }}</td>
                <td class="sale-name">
                  {{ g.name }}
                  <small v-if="!g.enabled" class="muted">用户组已禁用</small>
                </td>
                <td>
                  <button
                    class="link-button"
                    :disabled="!canWrite || busy || uncertain || !g.enabled"
                    @click="save('toggle', g)"
                  >
                    上架
                  </button>
                </td>
              </tr>
              <tr v-if="!unpublished.length">
                <td colspan="3" class="empty">暂无未上架用户组</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>
  </div>
  <div
    v-if="editing && configuration"
    class="modal-backdrop"
    @keydown.esc="!busy && (editing = '')"
  >
    <section
      class="modal panel"
      role="dialog"
      aria-modal="true"
      aria-label="编辑用户组简介"
    >
      <h2>编辑简介</h2>
      <div class="form-rows">
        <label
          ><span class="field-label">用户组简介</span
          ><textarea
            id="sale-description"
            v-model="drafts[editing]!.description"
            maxlength="2000"
            rows="8"
            :disabled="!canWrite || busy || uncertain"
          />
        </label>
      </div>
      <p class="muted">以纯文本显示，最多2000字。</p>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <div class="modal-actions">
        <button :disabled="busy" @click="editing = ''">取消</button
        ><button
          class="primary"
          :disabled="!canWrite || busy || uncertain"
          @click="
            save(
              'description',
              configuration.groups.find((g) => g.id === editing),
            )
          "
        >
          保存简介
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.group-sales {
  display: grid;
  gap: 16px;
}
.group-sales > p {
  margin: 0;
}
/* Card 1: title, refresh and the global switch. */
.sale-policy-card {
  padding: 20px;
}
.sale-policy-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.sale-refresh {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.sale-refresh :deep(.shell-icon) {
  width: 14px;
  height: 14px;
}
.sale-policy {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 16px 0 8px;
  font-size: 13px;
}
.sale-hint {
  margin: 0;
  font-size: 12px;
  line-height: 20px;
}
.sale-loading {
  margin: 16px 0 0;
  font-size: 13px;
}
/* The switch saves on click, so it stays a button; it is drawn like KitSwitch. */
.sale-switch {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 0;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--ink);
  font-size: 13px;
  cursor: pointer;
}
.sale-switch:hover {
  background: transparent;
}
.sale-switch:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.sale-switch-track {
  position: relative;
  flex: none;
  width: 36px;
  height: 20px;
  border-radius: 999px;
  background: var(--line-control);
  transition: background 0.15s;
}
.sale-switch-track::after {
  content: "";
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #ffffff;
  transition: transform 0.15s;
}
.sale-switch--on .sale-switch-track {
  background: var(--primary);
}
.sale-switch--on .sale-switch-track::after {
  transform: translateX(16px);
}
.sale-switch:focus-visible {
  outline: 2px solid var(--primary-soft);
  outline-offset: 2px;
}
/* Cards 2 and 3: tables. */
.sale-table-title {
  margin: 0;
  font-size: 14px;
  line-height: 32px;
}
.sale-table {
  width: 100%;
}
.sale-table th,
.sale-table td {
  white-space: nowrap;
  vertical-align: middle;
}
.sale-table td.sale-name {
  white-space: normal;
  min-width: 160px;
}
.sale-table td small {
  display: block;
}
.sale-table td input,
.sale-table td select {
  box-sizing: border-box;
  min-height: var(--control-h);
  height: var(--control-h);
  margin: 0;
}
.sale-price {
  width: 110px;
}
.sale-period {
  display: flex;
  align-items: center;
  gap: 8px;
}
.sale-period select {
  width: 136px;
}
.sale-period input {
  width: 120px;
}
.sale-days {
  color: var(--ink-muted);
}
.sale-sort {
  width: 72px;
}
.sale-visible {
  width: 140px;
}
textarea {
  width: 100%;
  box-sizing: border-box;
}
</style>
