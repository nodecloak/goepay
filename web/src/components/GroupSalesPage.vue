<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount, nextTick } from "vue";
import { ApiClient, ApiError } from "../api";
import { centsMoney } from "../settlement-terms";
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
  <section class="panel group-sales">
    <div class="toolbar">
      <h2>用户组购买设置</h2>
      <button :disabled="busy || loading" @click="load">刷新配置</button>
    </div>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
    <p v-if="!canWrite" class="muted">
      当前为只读查看，保存需要本人超级管理员会话。
    </p>
    <p v-if="loading" role="status">正在读取销售配置…</p>
    <template v-if="configuration">
      <div class="sale-policy">
        <span>用户组购买开关</span
        ><button
          role="switch"
          aria-label="用户组购买开关"
          :aria-checked="configuration.enabled"
          :disabled="!canWrite || busy || uncertain"
          @click="save('global')"
        >
          {{ configuration.enabled ? "已开启" : "已关闭" }}
        </button>
      </div>
      <p class="muted">
        有效期按日历月计算，0或留空表示永久；旧按天配置保留原期限，可明确切换到按月。可见用户组留空表示全部，多个ID以英文逗号分隔，0表示默认用户组。
      </p>
      <h3>已上架用户组</h3>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>用户组</th>
              <th>售价（元）</th>
              <th>有效期</th>
              <th>排序</th>
              <th>可见用户组</th>
              <th>简介</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="g in published" :key="g.id">
              <td>
                {{ g.name }}
                <small class="muted"
                  >{{ g.id }}{{ g.enabled ? "" : " · 用户组已禁用" }}</small
                >
              </td>
              <td>
                <input
                  v-model="drafts[g.id]!.price"
                  :aria-label="`售价（元） · ${g.name}`"
                  inputmode="decimal"
                  :disabled="!canWrite || busy || uncertain"
                />
              </td>
              <td>
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
                /><span v-else>{{ g.duration_days }}天</span>
              </td>
              <td>
                <input
                  v-model="drafts[g.id]!.sort"
                  :aria-label="`排序 · ${g.name}`"
                  inputmode="numeric"
                  :disabled="!canWrite || busy || uncertain"
                />
              </td>
              <td>
                <input
                  v-model="drafts[g.id]!.visible"
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
                  :disabled="!canWrite || busy || uncertain"
                  @click="save('toggle', g)"
                >
                  下架
                </button>
              </td>
            </tr>
            <tr v-if="!published.length">
              <td colspan="7" class="empty">暂无已上架用户组</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="toolbar">
        <button
          class="primary"
          :disabled="!canWrite || busy || uncertain"
          @click="save('all')"
        >
          {{ busy ? "正在保存…" : "保存全部" }}</button
        ><span class="muted"
          >售价、期限、排序和可见范围编辑后需保存；上下架和总开关单独保存。</span
        >
      </div>
      <h3>未上架用户组</h3>
      <p v-if="!unpublished.length" class="muted">暂无未上架用户组</p>
      <div v-for="g in unpublished" :key="g.id" class="toolbar">
        <span>{{ g.name }}（{{ g.id }}）</span
        ><button
          :disabled="!canWrite || busy || uncertain || !g.enabled"
          @click="save('toggle', g)"
        >
          上架
        </button>
      </div>
    </template>
  </section>
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
      <label for="sale-description">用户组简介</label
      ><textarea
        id="sale-description"
        v-model="drafts[editing]!.description"
        maxlength="2000"
        rows="8"
        :disabled="!canWrite || busy || uncertain"
      />
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
.sale-policy {
  display: flex;
  align-items: center;
  gap: 16px;
  margin: 16px 0;
}
.group-sales th {
  white-space: nowrap;
}
.group-sales td {
  min-width: 110px;
  vertical-align: top;
}
.group-sales td:first-child {
  min-width: 150px;
}
.group-sales td:nth-child(3) {
  min-width: 180px;
}
.group-sales td small {
  display: block;
}
.group-sales td input,
.group-sales td select {
  width: 100%;
  min-width: 85px;
  box-sizing: border-box;
}
.group-sales h3 {
  margin-top: 24px;
}
.group-sales table {
  width: 100%;
}
textarea {
  width: 100%;
  box-sizing: border-box;
}
</style>
