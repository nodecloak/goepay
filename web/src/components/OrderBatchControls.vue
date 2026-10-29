<script lang="ts">
type BatchBody = {
  external_no: string;
  action: string;
  reason: string;
  items: { trade_no: string; expected_revision: string }[];
};
const batchIntents = new WeakMap<object, Map<string, BatchBody>>();
</script>
<script setup lang="ts">
import { computed, ref, nextTick, onBeforeUnmount, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import { adminOrderActions } from "../order-actions";
import OrderBusinessBatchDialog from "./OrderBusinessBatchDialog.vue";
import ActionDropdown from "./ActionDropdown.vue";
import {
  isBusinessBatchAction185,
  businessBatchEligible185,
} from "../order-business-batches185";
const props = defineProps<{
  client: ApiClient;
  items: Record<string, unknown>[];
  selected: string[];
  ownSession: boolean;
  adminClass?: string;
}>();
const emit = defineEmits<{
  updated: [];
  busy: [value: boolean];
  active: [value: boolean];
}>();
const actions = [
  { id: "mark-unpaid", label: "改未完成" },
  { id: "mark-paid", label: "改已完成" },
  { id: "refund", label: "API退款" },
  { id: "freeze-income", label: "冻结订单" },
  { id: "unfreeze-income", label: "解冻订单" },
  { id: "archive", label: "删除订单" },
  { id: "notify", label: "重新通知" },
  { id: "confirm-settlement", label: "确认结算" },
  { id: "query", label: "手动补单" },
  { id: "restore", label: "恢复订单" },
];
const businessOpen = ref(false),
  businessRows = ref<Record<string, unknown>[]>([]);
const businessAction = computed(() =>
  isBusinessBatchAction185(action.value) ? action.value : "query",
);
const initialCSRF = props.client.csrfToken;
let intents = batchIntents.get(props.client);
if (!intents) {
  intents = new Map();
  batchIntents.set(props.client, intents);
}
const pending = ref<BatchBody | null>(intents.get(initialCSRF) || null),
  action = ref("mark-paid"),
  reason = ref(""),
  ack = ref(false),
  open = ref(false),
  busy = ref(false),
  unknown = ref(!!pending.value),
  error = ref(""),
  notice = ref(""),
  done = ref(false),
  failed = ref(false),
  dialog = ref<HTMLElement>();
const canWrite = computed(
  () =>
    props.ownSession &&
    props.client.area === "admin" &&
    ["super", "operator"].includes(props.adminClass || "") &&
    props.client.csrfToken === initialCSRF,
);
watch(
  () => open.value || businessOpen.value,
  (value) => emit("active", value),
);
const selectedRows = computed(() =>
  props.items.filter((r) => props.selected.includes(String(r.trade_no))),
);
const eligible = (id: string) =>
  selectedRows.value.filter((r) =>
    isBusinessBatchAction185(id)
      ? businessBatchEligible185(r, id)
      : adminOrderActions(r).some(
          (a) => a.id === (id === "archive" ? "delete" : id),
        ),
  );
// Pro's 批量操作 dropdown: one menu item per batch action, each showing its
// eligible count; picking one opens that action's confirm dialog for the
// selected rows (same server calls as before).
const batchMenuItems = computed(() =>
  actions.map((a) => ({
    id: a.id,
    label: `${a.label}（可执行 ${eligible(a.id).length} 条）`,
  })),
);
function pick(id: string) {
  if (!canWrite.value || busy.value || pending.value) return;
  action.value = id;
  void show();
}
let alive = true,
  controller: AbortController | undefined,
  previousFocus: HTMLElement | null = null;
function remember() {
  if (pending.value)
    intents!.set(initialCSRF, JSON.parse(JSON.stringify(pending.value)));
  else intents!.delete(initialCSRF);
}
async function show() {
  if (!canWrite.value || busy.value) return;
  if (isBusinessBatchAction185(action.value)) {
    const rows = eligible(action.value);
    if (!rows.length || rows.length > 100) return;
    previousFocus = document.activeElement as HTMLElement;
    businessRows.value = rows.slice();
    businessOpen.value = true;
    return;
  }
  if (!pending.value) {
    const rows = eligible(action.value);
    if (!rows.length) return;
    pending.value = {
      external_no: crypto.randomUUID(),
      action: action.value,
      reason: "",
      items: rows.map((r) => ({
        trade_no: String(r.trade_no),
        expected_revision: String(r.admin_revision ?? "0"),
      })),
    };
  }
  action.value = pending.value.action;
  reason.value = pending.value.reason;
  ack.value = unknown.value;
  done.value = false;
  failed.value = false;
  error.value = "";
  notice.value = "";
  previousFocus = document.activeElement as HTMLElement;
  open.value = true;
  await nextTick();
  dialog.value?.querySelector<HTMLElement>("textarea,button")?.focus();
}
function close() {
  if (busy.value) return;
  open.value = false;
  if (!unknown.value) {
    pending.value = null;
    remember();
  }
  previousFocus?.isConnected && previousFocus.focus();
}
async function execute() {
  if (
    !canWrite.value ||
    busy.value ||
    !pending.value ||
    done.value ||
    failed.value
  )
    return;
  if (!unknown.value) {
    if (
      !reason.value.trim() ||
      reason.value.trim().length > 240 ||
      !ack.value
    ) {
      error.value = "请填写操作原因并核对选中订单";
      return;
    }
    pending.value = { ...pending.value, reason: reason.value.trim() };
    remember();
  }
  busy.value = true;
  emit("busy", true);
  error.value = "";
  const c = new AbortController();
  controller = c;
  try {
    const response = await props.client.request<{
      items: unknown[];
      replayed: boolean;
    }>("/orders/batch-control", {
      method: "POST",
      body: pending.value,
      signal: c.signal,
    });
    if (!alive || c.signal.aborted) return;
    notice.value = `已处理 ${response.items.length} 条订单${response.replayed ? "，已找回原批结果" : ""}`;
    pending.value = null;
    remember();
    unknown.value = false;
    done.value = true;
    emit("updated");
  } catch (e) {
    if (!alive || c.signal.aborted) return;
    if (
      e instanceof ApiError &&
      e.status >= 400 &&
      e.status < 500 &&
      e.status !== 408 &&
      e.status !== 429
    ) {
      pending.value = null;
      remember();
      unknown.value = false;
      failed.value = true;
      error.value =
        e.status === 409
          ? "订单已更新，本批操作未提交；请重新读取并选择订单"
          : e.message;
    } else {
      unknown.value = true;
      error.value = "批量操作结果未知，请核对原批结果，勿更换请求号或所选订单";
    }
  } finally {
    if (alive && controller === c) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
function focusTrap(event: KeyboardEvent) {
  if (event.key !== "Tab" || !dialog.value) return;
  const all = Array.from(
    dialog.value.querySelectorAll<HTMLElement>(
      "button:not([disabled]),textarea:not([disabled]),input:not([disabled])",
    ),
  );
  const first = all[0],
    last = all.at(-1);
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}
watch(
  () => [props.client, props.client.csrfToken, props.ownSession],
  () => {
    controller?.abort();
    open.value = false;
    businessOpen.value = false;
    busy.value = false;
    emit("busy", false);
  },
);
onBeforeUnmount(() => {
  alive = false;
  controller?.abort();
  emit("busy", false);
  emit("active", false);
});
</script>
<template>
  <div class="batch-controls">
    <span>已选 {{ selected.length }} 条</span>
    <ActionDropdown
      v-if="!pending"
      label="批量操作"
      trigger-text="批量操作"
      :items="batchMenuItems"
      :disabled="busy || businessOpen || !canWrite"
      @action="pick"
    />
    <button
      v-else
      class="soft-button"
      :disabled="busy || !canWrite"
      @click="show"
    >
      核对上次批量结果
    </button>
  </div>
  <OrderBusinessBatchDialog
    v-if="businessOpen"
    :client="client"
    :action="businessAction"
    :rows="businessRows"
    :own-session="ownSession"
    :admin-class="adminClass"
    @busy="
      busy = $event;
      emit('busy', $event);
    "
    @updated="emit('updated')"
    @close="
      businessOpen = false;
      previousFocus?.isConnected && previousFocus.focus();
    "
  />
  <Teleport to="body"
    ><div v-if="open" class="modal-backdrop" @keydown.esc="close">
      <section
        ref="dialog"
        class="modal panel batch-dialog"
        role="dialog"
        aria-modal="true"
        aria-label="批量订单操作确认"
        @keydown="focusTrap"
      >
        <h2>批量订单操作确认</h2>
        <p>
          {{ actions.find((a) => a.id === action)?.label }}：{{
            pending?.items.length || 0
          }}
          条订单
        </p>
        <p class="muted">
          本批状态标记或归档保留原支付、退款和账务事实；所有所选订单在同一事务内检查版本。任何订单已变化时，本批全部不提交。
        </p>
        <div v-if="error" class="alert error" role="alert">{{ error }}</div>
        <div v-if="notice" class="alert success" role="status">
          {{ notice }}
        </div>
        <details v-if="pending">
          <summary>核对所选系统订单号</summary>
          <ul>
            <li v-for="item in pending.items" :key="item.trade_no">
              {{ item.trade_no }}
            </li>
          </ul>
        </details>
        <label
          >操作原因<textarea
            v-model="reason"
            maxlength="240"
            :disabled="busy || unknown || done || failed"
          /></label
        ><label class="batch-ack"
          ><input
            v-model="ack"
            type="checkbox"
            :disabled="busy || unknown || done || failed"
          />我已核对选中订单</label
        >
        <div class="modal-actions">
          <button :disabled="busy" @click="close">
            {{ unknown ? "关闭并保留原请求" : "关闭" }}</button
          ><button
            v-if="failed"
            @click="
              close();
              emit('updated');
            "
          >
            重新读取列表</button
          ><button
            v-else-if="!done"
            class="primary"
            :disabled="
              busy || !canWrite || (!unknown && (!reason.trim() || !ack))
            "
            @click="execute"
          >
            {{ busy ? "正在执行…" : unknown ? "核对原批结果" : "确认执行" }}
          </button>
        </div>
      </section>
    </div></Teleport
  >
</template>
<style scoped>
.batch-controls {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  padding: 0;
  font-size: 13px;
  color: var(--ink-muted);
}
.batch-controls select {
  width: auto;
  max-width: 100%;
  margin: 0;
}
.batch-dialog {
  max-width: 640px;
  max-height: calc(100dvh - 32px);
  overflow: auto;
  overflow-wrap: anywhere;
}
.batch-dialog textarea {
  width: 100%;
  min-height: 72px;
}
.batch-ack {
  display: flex;
  gap: 8px;
  align-items: center;
}
.batch-dialog li {
  font-family: monospace;
}
.modal-actions {
  flex-wrap: wrap;
}
</style>
