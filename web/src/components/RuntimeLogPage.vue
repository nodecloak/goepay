<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ApiClient } from "../api";
import { reportDateRange } from "../reports";
import { beijingTimestamp } from "../console-datetime";
import RuntimeLogCleanup from "./RuntimeLogCleanup.vue";
import ShellIcon from "./ShellIcon.vue";
import KitFilterBar from "./KitFilterBar.vue";
import KitField from "./KitField.vue";
import KitPagination from "./KitPagination.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitHelpPopover from "./KitHelpPopover.vue";
const props = defineProps<{
  client: ApiClient;
  ownSession?: boolean;
  adminClass?: string;
}>();
const today = reportDateRange("today")!.from,
  date = ref(today),
  page = ref(1),
  items = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  error = ref(""),
  loading = ref(false),
  cleanup = ref(false),
  auditOpen = ref(false);
const canWrite = computed(
  () =>
    props.client.area === "admin" &&
    props.ownSession &&
    props.adminClass === "super",
);
let controller: AbortController | undefined;
function logTone(
  type: string,
): "success" | "warning" | "danger" | "info" | "neutral" {
  if (["error", "critical", "emergency", "alert"].includes(type))
    return "danger";
  if (["warning", "warn"].includes(type)) return "warning";
  if (["notice", "info", "debug"].includes(type)) return "info";
  return "neutral";
}
function clear() {
  controller?.abort();
  items.value = [];
  total.value = 0;
  loading.value = false;
  error.value = "";
}
watch(date, () => {
  page.value = 1;
  clear();
});
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
  ],
  () => {
    clear();
    cleanup.value = false;
    auditOpen.value = false;
  },
);
async function load() {
  clear();
  const day = new Date(date.value + "T00:00:00Z");
  if (
    !/^\d{4}-\d{2}-\d{2}$/.test(date.value) ||
    Number.isNaN(day.valueOf()) ||
    day.toISOString().slice(0, 10) !== date.value
  ) {
    error.value = "请选择有效日期";
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
      "/operations/runtime-log?" +
        new URLSearchParams({
          date: date.value,
          page: String(page.value),
          limit: "30",
        }),
      { signal: c.signal },
    );
    if (
      !c.signal.aborted &&
      client === props.client &&
      csrf === client.csrfToken
    ) {
      items.value = data.items;
      total.value = data.total;
    }
  } catch (e) {
    if (!c.signal.aborted)
      error.value = e instanceof Error ? e.message : "运行日志读取失败";
  } finally {
    if (!c.signal.aborted) loading.value = false;
  }
}
onMounted(load);
onBeforeUnmount(clear);
</script>
<template>
  <div class="list-page">
    <KitFilterBar
      :loading="loading"
      @query="
        page = 1;
        load();
      "
      @reset="
        date = today;
        page = 1;
        $nextTick(load);
      "
    >
      <KitField label="日期"
        ><input
          v-model="date"
          class="pa-input"
          type="date"
          :max="today"
          aria-label="日期"
          required
      /></KitField>
    </KitFilterBar>
    <section class="panel records runtime-log-page">
      <div class="table-tools">
        <div class="table-tools-end">
          <button type="button" :disabled="loading" @click="load">
            <ShellIcon name="refresh" />刷新</button
          ><button
            v-if="canWrite"
            type="button"
            class="danger-button"
            @click="cleanup = true"
          >
            清除日志
          </button>
          <KitHelpPopover
            text="按北京时间查看本服务已保存的运行事件；部署前或数据库不可用时未保存的终端输出不在列表中。"
          />
        </div>
      </div>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <p v-if="loading" role="status">正在读取运行日志…</p>
      <div v-else-if="!error" class="table-scroll">
        <table>
          <thead>
            <tr>
              <th scope="col">时间</th>
              <th scope="col">日志类型</th>
              <th scope="col">日志内容</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in items" :key="String(row.id)">
              <td>{{ beijingTimestamp(row.time) }}</td>
              <td>
                <KitStatusTag
                  :text="String(row.type)"
                  :tone="logTone(String(row.type))"
                />
              </td>
              <td class="log-content">{{ row.content }}</td>
            </tr>
            <tr v-if="!items.length">
              <td colspan="3" class="empty">暂无记录</td>
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
      <details
        class="list-filters runtime-audit-disclosure"
        @toggle="auditOpen = ($event.target as HTMLDetailsElement).open"
      >
        <summary>
          <ShellIcon name="filter" />查看独立保留的业务审计<ShellIcon
            name="chevronDown"
            class="list-filters-chevron"
          />
        </summary>
        <slot v-if="auditOpen" name="audit" />
      </details>
      <RuntimeLogCleanup
        v-if="cleanup && canWrite"
        :client="client"
        :own-session="ownSession"
        :admin-class="adminClass"
        @close="cleanup = false"
        @updated="load"
      />
    </section>
  </div>
</template>
<style scoped>
.runtime-log-page {
  min-width: 0;
}
.log-content {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-width: 700px;
}
.runtime-audit-disclosure {
  margin: 16px var(--space-5) 0;
}
</style>
