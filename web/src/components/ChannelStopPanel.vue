<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, computed } from "vue";
import { ApiClient } from "../api";
import { consoleValue } from "../pro-console-fields";
import KitStatusTag from "./KitStatusTag.vue";
import KitHelpPopover from "./KitHelpPopover.vue";
const props = defineProps<{ client: ApiClient }>();
interface Status {
  channel_id: string;
  event_id: string;
  revision: string;
  blocked: boolean;
  code?: string;
  changed_at?: string | null;
  provider: string;
  notice_state?: "pending" | "attempted" | "accepted" | "unknown";
  notice_transport?: "smtp" | "robot" | "telegram" | "qqbot";
}
const items = ref<Status[]>([]),
  selected = ref<Status | null>(null),
  confirmed = ref(false),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
let controller: AbortController | undefined,
  epoch = 0;
const expanded = ref(false);
const blockedCount = computed(
  () => items.value.filter((x) => x.blocked).length,
);
// The runtime card stays a compact status chip until something needs the full
// recovery form: a channel is stopped, a row is chosen to restore, or the user
// expands it. Nothing is removed — the whole recovery flow is behind 展开.
const open = computed(
  () => expanded.value || blockedCount.value > 0 || !!selected.value,
);
function valid(raw: unknown): raw is Status {
  if (!raw || typeof raw !== "object") return false;
  const x = raw as Status;
  if (
    typeof x.channel_id !== "string" ||
    typeof x.event_id !== "string" ||
    typeof x.revision !== "string" ||
    !/^[1-9]\d{0,18}$/.test(x.channel_id) ||
    !/^(0|[1-9]\d{0,18})$/.test(x.event_id) ||
    !/^(0|[1-9]\d{0,3})$/.test(x.revision)
  )
    return false;
  if (
    BigInt(x.channel_id) > 9223372036854775807n ||
    BigInt(x.event_id) > 9223372036854775807n ||
    BigInt(x.revision) > 4096n ||
    (x.notice_state !== undefined &&
      !["pending", "attempted", "accepted", "unknown"].includes(
        x.notice_state,
      )) ||
    (x.notice_transport !== undefined &&
      (!["smtp", "robot", "telegram", "qqbot"].includes(x.notice_transport) ||
        !x.notice_state ||
        x.notice_state === "pending"))
  )
    return false;
  return (
    typeof x.blocked === "boolean" &&
    ["wxpay", "alipay", "qqpay"].includes(x.provider) &&
    (!x.blocked ||
      (x.event_id !== "0" &&
        x.revision !== "0" &&
        typeof x.code === "string" &&
        x.code.length > 0))
  );
}
async function load() {
  if (busy.value) return;
  const own = ++epoch;
  controller?.abort();
  controller = new AbortController();
  selected.value = null;
  confirmed.value = false;
  items.value = [];
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const data = await props.client.request<{ items: Status[] }>(
      "/channel-stops",
      { signal: controller.signal },
    );
    if (own !== epoch) return;
    if (
      !Array.isArray(data?.items) ||
      data.items.length > 1000 ||
      !data.items.every(valid) ||
      new Set(data.items.map((x) => x.channel_id)).size !== data.items.length
    )
      throw new Error("通道运行状态响应不完整，请重新查询");
    items.value = data.items;
  } catch (e) {
    if (own === epoch)
      error.value = e instanceof Error ? e.message : "通道状态查询失败";
  } finally {
    if (own === epoch) loading.value = false;
  }
}
function choose(row: Status) {
  if (busy.value) return;
  selected.value = { ...row };
  confirmed.value = false;
  notice.value = "";
  error.value = "";
}
async function restore() {
  if (!selected.value || !confirmed.value || busy.value) return;
  const row = { ...selected.value },
    own = epoch;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const next = await props.client.request<Status>(
      `/channel-stops/${row.channel_id}/resume`,
      {
        method: "POST",
        body: {
          expected_event_id: row.event_id,
          expected_revision: row.revision,
        },
        signal: controller?.signal,
      },
    );
    if (own !== epoch) return;
    const updated = { ...next, provider: row.provider };
    if (
      !valid(updated) ||
      updated.channel_id !== row.channel_id ||
      updated.blocked ||
      BigInt(updated.revision) !== BigInt(row.revision) + 1n
    )
      throw new Error("恢复结果待核对，请刷新状态");
    items.value = items.value.map((x) =>
      x.channel_id === row.channel_id ? updated : x,
    );
    notice.value = "通道已恢复，原转账不会重发。";
  } catch (e) {
    if (own === epoch) {
      error.value =
        e instanceof Error ? e.message : "恢复结果待核对，请刷新状态";
      items.value = [];
    }
  } finally {
    if (own === epoch) {
      selected.value = null;
      confirmed.value = false;
      busy.value = false;
    }
  }
}
onMounted(load);
watch(
  () => props.client,
  () => {
    epoch++;
    controller?.abort();
    busy.value = false;
    void load();
  },
);
onBeforeUnmount(() => {
  epoch++;
  controller?.abort();
});
</script>
<template>
  <section class="channel-stop-panel" aria-labelledby="channel-stop-title">
    <div class="toolbar">
      <h3 id="channel-stop-title">通道运行状态</h3>
      <KitStatusTag
        v-if="!loading"
        :tone="blockedCount ? 'danger' : 'success'"
        :text="blockedCount ? blockedCount + ' 个已停用' : '运行正常'"
      />
      <KitHelpPopover
        label="说明"
        text="付款异常自动停用后，请先核对原转账和通道配置，再恢复新请求。此处显示实际运行通道，修改下方通道资料不会解除停用。"
      />
      <span class="channel-stop-tools">
        <button type="button" :disabled="loading || busy" @click="load">
          刷新状态
        </button>
        <button
          type="button"
          :aria-expanded="open"
          :disabled="blockedCount > 0 || !!selected"
          @click="expanded = !expanded"
        >
          {{ open ? "收起" : "展开" }}
        </button>
      </span>
    </div>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <template v-if="open">
      <p v-if="loading" role="status">正在读取通道状态…</p>
      <p v-else-if="!items.length && !error" class="muted">
        暂无已配置的运行通道。
      </p>
      <div v-for="row in items" :key="row.channel_id" class="channel-stop-row">
        <div>
          <strong>通道 {{ row.channel_id }}</strong>
          <span>{{
            row.provider === "alipay"
              ? "支付宝"
              : row.provider === "wxpay"
                ? "微信支付"
                : "QQ钱包"
          }}</span>
          <KitStatusTag
            :tone="
              row.blocked ? 'danger' : row.revision === '0' ? 'info' : 'success'
            "
            :text="
              row.blocked
                ? '已停用'
                : row.revision === '0'
                  ? '允许新请求'
                  : '已恢复'
            "
          />
          <p v-if="row.code" class="muted">原异常代码：{{ row.code }}</p>
          <p v-if="row.notice_state" class="muted">
            管理员通知：{{
              {
                pending: "待发送",
                attempted: "发送待核对",
                accepted: "通知服务已受理",
                unknown: "发送结果待核对",
              }[row.notice_state]
            }}
          </p>
          <p v-if="row.notice_transport" class="muted">
            通知方式：{{
              {
                smtp: "邮件",
                robot: "群机器人",
                telegram: "Telegram",
                qqbot: "QQ机器人",
              }[row.notice_transport]
            }}
          </p>
          <small v-if="row.changed_at" class="muted"
            >最近变更（北京时间）：{{
              consoleValue("created_at", row.changed_at)
            }}</small
          >
        </div>
        <button
          v-if="row.blocked"
          type="button"
          :disabled="busy || loading"
          @click="choose(row)"
        >
          恢复通道
        </button>
      </div>
      <form
        v-if="selected"
        class="channel-stop-confirm"
        @submit.prevent="restore"
      >
        <h4>恢复通道 {{ selected.channel_id }}</h4>
        <p>
          核对停用记录 {{ selected.event_id }}（修订
          {{ selected.revision }}）及错误
          {{ selected.code }}。恢复只允许后续新请求，未知转账仍须按原单号查询。
        </p>
        <label
          ><input
            v-model="confirmed"
            type="checkbox"
            :disabled="busy"
          />我已核对原转账和通道配置，确认恢复</label
        >
        <div class="toolbar">
          <button type="submit" :disabled="!confirmed || busy">
            {{ busy ? "正在确认…" : "确认恢复" }}</button
          ><button type="button" :disabled="busy" @click="selected = null">
            取消
          </button>
        </div>
      </form>
    </template>
  </section>
</template>
<style scoped>
.channel-stop-panel {
  margin-bottom: 20px;
  padding: 16px;
  border: 1px solid var(--border, var(--line));
  border-radius: 3px;
  background: var(--surface, var(--bg-card));
}
.channel-stop-panel h3 {
  margin: 0;
  font-size: 16px;
}
.channel-stop-tools {
  display: inline-flex;
  gap: 8px;
  margin-left: auto;
}
.channel-stop-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border, var(--line));
}
.channel-stop-row > div {
  min-width: 0;
  overflow-wrap: anywhere;
}
.channel-stop-row p {
  margin: 6px 0;
}
.channel-stop-row button {
  flex-shrink: 0;
}
.channel-stop-confirm {
  margin-top: 16px;
  padding: 16px;
  background: var(--bg, var(--bg-raised));
  overflow-wrap: anywhere;
}
.channel-stop-confirm label {
  display: flex;
  gap: 8px;
  align-items: flex-start;
}
.channel-stop-confirm .toolbar {
  margin-top: 12px;
}
.channel-stop-panel .danger {
  color: var(--danger-text);
  background: var(--danger-soft);
}
@media (max-width: 480px) {
  .channel-stop-row {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
