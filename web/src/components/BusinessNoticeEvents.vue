<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type { ApiClient } from "../api";
import {
  businessScenes,
  businessStates,
  businessModes,
  parseBusinessNotices,
  type BusinessNotice,
} from "../business-notice";
const props = defineProps<{
  client: ApiClient;
  ownSession: boolean;
  adminClass?: string;
}>();
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.ownSession &&
    ["super", "operator"].includes(props.adminClass ?? ""),
);
const items = ref<BusinessNotice[]>([]),
  cursor = ref(""),
  busy = ref(false),
  error = ref("");
let epoch = 0,
  controller: AbortController | undefined;
const time = (v: string) =>
  new Date(v).toLocaleString("zh-CN", {
    timeZone: "Asia/Shanghai",
    hour12: false,
  });
const mode = (v: string) =>
  businessModes.find((m) => m.value === v)?.label ?? "—";
async function load(more = false) {
  if (!allowed.value || (more && busy.value)) return;
  const current = ++epoch;
  controller?.abort();
  controller = new AbortController();
  const before = more ? cursor.value : "";
  if (!more) {
    items.value = [];
    cursor.value = "";
  }
  error.value = "";
  busy.value = true;
  try {
    const value = await props.client.request(
      "/business-notices?limit=20" + (before ? "&before_id=" + before : ""),
      { signal: controller.signal },
    );
    if (current !== epoch) return;
    const page = parseBusinessNotices(value);
    items.value = more ? [...items.value, ...page.items] : page.items;
    cursor.value = page.next_cursor;
  } catch {
    if (current === epoch) error.value = "提醒记录读取失败，请刷新后重试";
  } finally {
    if (current === epoch) busy.value = false;
  }
}
watch(
  [() => props.client, () => props.client.csrfToken, allowed],
  () => {
    ++epoch;
    controller?.abort();
    items.value = [];
    cursor.value = "";
    error.value = "";
    busy.value = false;
    if (allowed.value) void load();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  ++epoch;
  controller?.abort();
});
</script>
<template>
  <section class="business-notices" aria-label="管理员业务提醒记录">
    <h3>管理员业务提醒记录</h3>
    <p v-if="!allowed" class="muted">
      请使用具有业务管理权限的管理员本人会话。
    </p>
    <template v-else
      ><p class="muted">
        “已受理”表示消息服务接受请求；“待核实”表示结果尚未确认。
      </p>
      <button :disabled="busy" @click="load()">刷新提醒记录</button>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="busy" role="status">正在读取提醒记录…</p>
      <p v-else-if="!items.length && !error">暂无提醒记录</p>
      <div
        v-if="items.length"
        class="business-notice-table"
        tabindex="0"
        aria-label="管理员提醒表格"
      >
        <table>
          <thead>
            <tr>
              <th>编号</th>
              <th>商户 ID</th>
              <th>业务类型</th>
              <th>来源编号</th>
              <th>提醒方式</th>
              <th>时间（北京时间）</th>
              <th>处理状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in items" :key="item.id">
              <td>{{ item.id }}</td>
              <td>{{ item.merchant_id }}</td>
              <td>{{ businessScenes[item.scene] }}</td>
              <td>{{ item.source_id }}</td>
              <td>{{ mode(item.mode) }}</td>
              <td>{{ time(item.at) }}</td>
              <td>{{ businessStates[item.state] }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <button v-if="cursor" :disabled="busy" @click="load(true)">
        加载更多记录
      </button></template
    >
  </section>
</template>
<style scoped>
.business-notices {
  margin-top: 1.5rem;
  padding-top: 1rem;
  border-top: 1px solid var(--border, var(--line));
  min-width: 0;
}
.business-notice-table {
  overflow: auto;
  max-width: 100%;
  margin: 1rem 0;
}
table {
  width: 100%;
  border-collapse: collapse;
}
td,
th {
  text-align: left;
  padding: 0.65rem;
  border-bottom: 1px solid var(--border, var(--line));
  white-space: nowrap;
}
button {
  margin-bottom: 0.5rem;
}
</style>
