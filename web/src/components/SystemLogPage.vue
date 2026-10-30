<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ApiClient } from "../api";
import { beijingTimestamp } from "../console-datetime";
import {
  systemLogRouteQuery,
  systemLogMerchantHref,
  systemLogIPHref,
} from "../system-log";
import ShellIcon from "./ShellIcon.vue";
import KitFilterBar from "./KitFilterBar.vue";
import KitField from "./KitField.vue";
import KitSelect from "./KitSelect.vue";
import KitInput from "./KitInput.vue";
import KitPagination from "./KitPagination.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitHelpPopover from "./KitHelpPopover.vue";
const props = defineProps<{ client: ApiClient }>();
const route = useRoute(),
  router = useRouter(),
  column = ref("uid"),
  value = ref(""),
  page = ref(1),
  total = ref(0),
  items = ref<Record<string, unknown>[]>([]),
  loading = ref(false),
  error = ref(""),
  rawOpen = ref(false);
let controller: AbortController | undefined,
  alive = true;
function clear() {
  controller?.abort();
  items.value = [];
  total.value = 0;
  loading.value = false;
  error.value = "";
}
watch(
  [column, value],
  () => {
    page.value = 1;
    clear();
  },
  { flush: "sync" },
);
watch(
  () => [props.client, props.client.csrfToken],
  () => {
    clear();
    rawOpen.value = false;
  },
);
async function load() {
  clear();
  const filter = systemLogRouteQuery({
    column: column.value,
    value: value.value,
  });
  if (filter.error) {
    error.value = filter.error;
    return;
  }
  controller = new AbortController();
  const c = controller,
    client = props.client,
    csrf = client.csrfToken;
  loading.value = true;
  try {
    const data = await client.request<{
      items: Record<string, unknown>[];
      total: number;
    }>(
      "/operations/system-log?" +
        new URLSearchParams({
          column: filter.column,
          value: filter.value,
          page: String(page.value),
          limit: "30",
        }),
      { signal: c.signal },
    );
    if (
      alive &&
      !c.signal.aborted &&
      client === props.client &&
      csrf === client.csrfToken
    ) {
      items.value = data.items;
      total.value = data.total;
    }
  } catch (e) {
    if (alive && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "系统日志读取失败";
  } finally {
    if (alive && !c.signal.aborted) loading.value = false;
  }
}
function applyRoute() {
  clear();
  const filter = systemLogRouteQuery(route.query);
  if (filter.error) {
    error.value = filter.error;
    return;
  }
  column.value = filter.column;
  value.value = filter.value;
  page.value = 1;
  void load();
}
async function search() {
  const local = systemLogRouteQuery({
    column: column.value,
    value: value.value,
  });
  if (local.error) {
    clear();
    error.value = local.error;
    return;
  }
  const current = systemLogRouteQuery(route.query);
  page.value = 1;
  if (
    !current.error &&
    current.column === local.column &&
    current.value === local.value
  ) {
    await load();
    return;
  }
  await router.replace({
    path: route.path,
    query: { column: local.column, value: local.value },
  });
}
async function reset() {
  clear();
  if (!Object.keys(route.query).length) {
    column.value = "uid";
    value.value = "";
    page.value = 1;
    await nextTick();
    await load();
  } else await router.replace({ path: route.path, query: {} });
}
watch(
  () => route.query,
  () => {
    if (route.path === "/admin/system/log") applyRoute();
    else clear();
  },
);
onMounted(applyRoute);
onBeforeUnmount(() => {
  alive = false;
  clear();
});
</script>
<template>
  <div class="list-page">
    <KitFilterBar :loading="loading" @query="search" @reset="reset">
      <KitField label="搜索字段"
        ><KitSelect
          v-model="column"
          :options="[
            { value: 'uid', label: '用户ID' },
            { value: 'ip', label: '操作IP' },
          ]"
          aria-label="搜索字段"
      /></KitField>
      <KitField label="搜索内容"
        ><KitInput v-model="value" placeholder="搜索内容" :maxlength="253"
      /></KitField>
    </KitFilterBar>
    <section class="panel records system-log-page">
      <div class="table-tools">
        <div class="table-tools-end">
          <button type="button" :disabled="loading" @click="load">
            <ShellIcon name="refresh" />刷新
          </button>
          <KitHelpPopover
            text="本地已记录的登录和商户操作，时间为北京时间。失败登录不按尝试账号推断商户；未记录的操作IP与无法核实的归属保留未知。"
          />
        </div>
      </div>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <p v-if="loading" role="status">正在读取系统日志…</p>
      <div v-else-if="!error" class="table-scroll">
        <table>
          <thead>
            <tr>
              <th scope="col">ID</th>
              <th scope="col">商户号</th>
              <th scope="col">操作类型</th>
              <th scope="col">操作IP</th>
              <th scope="col">时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in items" :key="String(row.source) + ':' + row.id">
              <td class="col-nowrap">
                <span class="id-cell"
                  >{{ row.id
                  }}<KitStatusTag
                    tone="neutral"
                    :text="
                      row.source === 'login'
                        ? '登录'
                        : row.source === 'business'
                          ? '业务'
                          : '来源未记录'
                    "
                /></span>
              </td>
              <td>
                <RouterLink
                  v-if="systemLogMerchantHref(row)"
                  :to="systemLogMerchantHref(row)!"
                  >{{ row.uid }}</RouterLink
                ><span v-else>{{
                  row.subject_kind === "admin" ? "管理员" : "未核实"
                }}</span>
              </td>
              <td :class="{ 'log-failed': row.type === '登录失败' }">
                {{ row.type }}
              </td>
              <td class="col-nowrap">
                <a
                  v-if="systemLogIPHref(row.ip)"
                  :href="systemLogIPHref(row.ip)"
                  target="_blank"
                  rel="noopener noreferrer"
                  >{{ row.ip }}</a
                ><span v-else class="ip-plain">{{ row.ip || "未记录" }}</span>
              </td>
              <td class="col-nowrap">{{ beijingTimestamp(row.date) }}</td>
            </tr>
            <tr v-if="!items.length">
              <td colspan="5" class="empty">暂无记录</td>
            </tr>
          </tbody>
        </table>
      </div>
      <KitPagination
        v-model:page="page"
        :page-size="30"
        :total="total"
        :page-sizes="[30]"
        @change="load"
      />
      <details @toggle="rawOpen = ($event.target as HTMLDetailsElement).open">
        <summary>查看原始登录审计</summary>
        <slot v-if="rawOpen" name="login-audit" />
      </details>
    </section>
  </div>
</template>
<style scoped>
.system-log-page {
  min-width: 0;
}
.system-log-page td {
  overflow-wrap: anywhere;
}
.system-log-page td.col-nowrap {
  white-space: nowrap;
}
.id-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.ip-plain {
  color: var(--ink);
}
.system-log-page details {
  margin: 16px var(--space-5) var(--space-5);
}
.log-failed {
  color: var(--danger-text);
}
</style>
