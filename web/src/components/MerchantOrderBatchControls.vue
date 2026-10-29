<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { ApiClient } from "../api";
import { merchantOrderActions } from "../order-actions";
import ActionDropdown from "./ActionDropdown.vue";
const props = defineProps<{
  client: ApiClient;
  items: Record<string, unknown>[];
  selected: string[];
  ownSession: boolean;
}>();
const emit = defineEmits<{
  updated: [];
  busy: [value: boolean];
  active: [value: boolean];
  // Hand eligible refundable trades to the parent to step through the existing
  // per-order refund dialog (funds-authorised; never auto-fired).
  "refund-step": [trades: string[]];
}>();
// Only the merchant-scoped operations the server already allows per row. Refund
// is stepped (funds-auth), notify/query are idempotent and run in a client loop.
const actions = [
  { id: "notify", label: "重新通知" },
  { id: "query", label: "手动补单" },
  { id: "refund", label: "批量退款" },
] as const;
type Action = (typeof actions)[number]["id"];
const action = ref<Action>("notify"),
  open = ref(false),
  busy = ref(false),
  done = ref(false),
  error = ref(""),
  notice = ref(""),
  progress = ref(""),
  dialog = ref<HTMLElement>();
const initialCSRF = props.client.csrfToken;
const canWrite = computed(
  () =>
    props.ownSession &&
    props.client.area === "merchant" &&
    props.client.csrfToken === initialCSRF,
);
// A row is eligible for an action only when the server marked it so (the same
// per-row capability the row menu uses); nothing the merchant may not do runs.
function eligible(id: Action): Record<string, unknown>[] {
  const chosen = new Set(props.selected);
  return props.items.filter((r) => {
    if (!chosen.has(String(r.trade_no))) return false;
    return merchantOrderActions(r).some((a) => a.id === id);
  });
}
// One 批量操作 menu: each merchant-scoped action with its eligible count; picking
// one opens that action's confirm (notify/query) or hands the refund step list.
const batchMenuItems = computed(() =>
  actions.map((a) => ({
    id: a.id,
    label: `${a.label}（可执行 ${eligible(a.id).length} 条）`,
  })),
);
function pick(id: string) {
  if (!canWrite.value || busy.value) return;
  action.value = id as Action;
  void show();
}
let alive = true,
  controller: AbortController | undefined,
  previousFocus: HTMLElement | null = null;
watch(open, (value) => emit("active", value));
async function show() {
  if (!canWrite.value || busy.value) return;
  if (!eligible(action.value).length) return;
  error.value = "";
  notice.value = "";
  progress.value = "";
  done.value = false;
  previousFocus = document.activeElement as HTMLElement;
  open.value = true;
  await nextTick();
  dialog.value?.querySelector<HTMLElement>("button")?.focus();
}
function close() {
  if (busy.value) return;
  open.value = false;
  previousFocus?.isConnected && previousFocus.focus();
}
async function run() {
  if (!canWrite.value || busy.value || done.value) return;
  const rows = eligible(action.value);
  if (!rows.length) return;
  if (action.value === "refund") {
    // Stepped, not auto-fired: the parent opens the existing per-order refund
    // dialog (merchant refund permission + rules + funds authorisation) for each.
    emit(
      "refund-step",
      rows.map((r) => String(r.trade_no)),
    );
    open.value = false;
    previousFocus?.isConnected && previousFocus.focus();
    return;
  }
  busy.value = true;
  emit("busy", true);
  error.value = "";
  notice.value = "";
  const c = new AbortController();
  controller = c;
  let ok = 0;
  const failures: string[] = [];
  for (let i = 0; i < rows.length; i++) {
    if (!alive || c.signal.aborted) return;
    const trade = String(rows[i]!.trade_no);
    progress.value = `正在处理第 ${i + 1}/${rows.length} 条：${trade}`;
    try {
      // query = query-then-repair (only repairs when the channel verifies paid);
      // notify = resend the stored notification. Both idempotent per order.
      const r = await props.client.requestResult<Record<string, unknown>>(
        `/orders/${encodeURIComponent(trade)}/${action.value === "query" ? "query" : "notify"}`,
        { method: "POST", body: {}, signal: c.signal },
      );
      if (r.status >= 200 && r.status < 300) ok++;
      else failures.push(trade);
    } catch {
      failures.push(trade);
    }
  }
  if (!alive || c.signal.aborted) return;
  progress.value = "";
  const verb = action.value === "query" ? "补单查询" : "重新通知";
  notice.value =
    failures.length === 0
      ? `已对 ${ok} 条订单执行${verb}；补单仅在通道核实支付成功时生效，未发起新的支付。`
      : `已处理 ${ok} 条，${failures.length} 条未成功（请重新读取后核对，未发起新的支付）。`;
  done.value = true;
  busy.value = false;
  emit("busy", false);
  emit("updated");
}
watch(
  () => [props.client, props.client.csrfToken, props.ownSession],
  () => {
    controller?.abort();
    open.value = false;
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
  <div v-if="canWrite" class="batch-controls">
    <span>已选 {{ selected.length }} 条</span>
    <ActionDropdown
      label="批量操作"
      trigger-text="批量操作"
      :items="batchMenuItems"
      :disabled="busy"
      @action="pick"
    />
  </div>
  <Teleport to="body">
    <div v-if="open" class="modal-backdrop" @keydown.esc="close">
      <section
        ref="dialog"
        class="modal panel batch-dialog"
        role="dialog"
        aria-modal="true"
        aria-label="批量订单操作确认"
      >
        <h2>批量订单操作确认</h2>
        <p>
          {{ actions.find((a) => a.id === action)?.label }}：{{
            eligible(action).length
          }}
          条订单
        </p>
        <p v-if="action === 'refund'" class="muted">
          批量退款将逐单打开退款核对；每单在你的退款权限与退款规则内单独确认，不会一次性自动退款。
        </p>
        <p v-else class="muted">
          补单仅查询原通道，核实支付成功时才补单；重新通知重发已存储的通知。两者都不会发起新的支付。
        </p>
        <div v-if="error" class="alert error" role="alert">{{ error }}</div>
        <div v-if="notice" class="alert success" role="status">
          {{ notice }}
        </div>
        <p v-if="progress" role="status">{{ progress }}</p>
        <div class="modal-actions">
          <button :disabled="busy" @click="close">关闭</button>
          <button
            v-if="!done"
            class="primary"
            :disabled="busy || !eligible(action).length"
            @click="run"
          >
            {{
              busy
                ? "正在执行…"
                : action === "refund"
                  ? "逐单退款核对"
                  : "确认执行"
            }}
          </button>
        </div>
      </section>
    </div>
  </Teleport>
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
  max-width: 560px;
  max-height: calc(100dvh - 32px);
  overflow: auto;
  overflow-wrap: anywhere;
}
.modal-actions {
  flex-wrap: wrap;
}
</style>
