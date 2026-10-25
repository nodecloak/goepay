<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { ApiClient } from "../api";
import {
  noticeScenes,
  type MerchantNoticeEvent,
  type MerchantNoticeEventsPage,
} from "../merchant-notice";
const props = defineProps<{ client: ApiClient; path: string }>();
const items = ref<MerchantNoticeEvent[]>([]),
  cursor = ref(""),
  busy = ref(false),
  error = ref("");
const scenes: Record<string, string> = Object.fromEntries(noticeScenes);
const states: Record<string, string> = {
  not_scheduled: "未安排发送",
  queued: "等待处理",
  blocked: "处理已停止",
  accepted: "已受理",
  unknown: "待核实",
  not_sent: "未发送",
};
const transports: Record<string, string> = {
  wechat_template: "微信模板消息",
  smtp: "电子邮件",
  sms: "短信",
  robot: "群机器人",
  telegram: "Telegram",
  qqbot: "QQ Bot",
  voice: "语音",
};
let epoch = 0,
  controller: AbortController | undefined;
const time = (value: string) =>
  Number.isFinite(Date.parse(value))
    ? new Date(value).toLocaleString("zh-CN", {
        timeZone: "Asia/Shanghai",
        hour12: false,
      })
    : "—";
async function load(more = false) {
  if (busy.value && more) return;
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
    const page = await props.client.request<MerchantNoticeEventsPage>(
      `${props.path}/events?limit=20${before ? `&before_id=${before}` : ""}`,
      { signal: controller.signal },
    );
    if (current !== epoch) return;
    if (
      !Array.isArray(page.items) ||
      typeof page.next_cursor !== "string" ||
      (page.next_cursor !== "" && !/^[1-9][0-9]*$/.test(page.next_cursor))
    )
      throw Error("invalid page");
    items.value = more ? [...items.value, ...page.items] : page.items;
    cursor.value = page.next_cursor;
  } catch {
    if (current === epoch) error.value = "通知记录读取失败，请刷新后重试";
  } finally {
    if (current === epoch) busy.value = false;
  }
}
watch([() => props.path, () => props.client], () => void load(), {
  immediate: true,
});
onBeforeUnmount(() => {
  ++epoch;
  controller?.abort();
});
</script>
<template>
  <section class="notice-events" aria-label="通知记录">
    <h3>通知记录</h3>
    <p class="muted">
      “已受理”表示发送服务已接受请求；“待核实”表示尚未确认结果。
    </p>
    <button :disabled="busy" @click="load()">刷新通知记录</button>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="busy" role="status">正在读取通知记录…</p>
    <p v-else-if="!error && !items.length">暂无通知记录</p>
    <div
      v-if="items.length"
      class="notice-event-table"
      tabindex="0"
      aria-label="通知记录表格"
    >
      <table>
        <thead>
          <tr>
            <th scope="col">通知编号</th>
            <th scope="col">消息类型</th>
            <th scope="col">订单号</th>
            <th scope="col">时间（北京时间）</th>
            <th scope="col">处理状态</th>
            <th scope="col">通知方式</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id">
            <td>{{ item.id }}</td>
            <td>{{ scenes[item.scene] ?? "其他通知" }}</td>
            <td>{{ item.trade_no || "—" }}</td>
            <td>{{ time(item.at) }}</td>
            <td>{{ states[item.state] ?? "待核实" }}</td>
            <td>
              <div
                v-for="delivery in item.deliveries"
                :key="delivery.transport"
              >
                {{ transports[delivery.transport] ?? "其他方式" }}：{{
                  states[delivery.state] ?? "待核实"
                }}
              </div>
              <span v-if="!item.deliveries.length">—</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <button v-if="cursor" :disabled="busy" @click="load(true)">
      加载更多记录
    </button>
  </section>
</template>
<style scoped>
.notice-events {
  margin-top: 1.5rem;
  border-top: 1px solid var(--border, #e5e7eb);
  padding-top: 1rem;
}
.notice-event-table {
  overflow-x: auto;
  margin: 1rem 0;
}
table {
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  text-align: left;
  padding: 0.65rem;
  border-bottom: 1px solid var(--border, #e5e7eb);
  white-space: nowrap;
}
</style>
