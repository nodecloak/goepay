<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { ApiClient, ApiError } from "../api";
import { beijingTimestamp } from "../console-datetime";

type AttestationView = {
  refund_id: string;
  refund_no: string;
  trade_no: string;
  amount: string;
  status: string;
  sent: boolean;
  eligible: boolean;
  reason: string;
  states: string[];
  attestation: {
    state: string;
    provider_reference: string;
    note: string;
    admin_username: string;
    created_at: string;
  } | null;
};

const props = defineProps<{
  client: ApiClient;
  refundId: string;
  ownSession: boolean;
  adminClass?: string;
}>();
const emit = defineEmits<{ close: []; updated: [] }>();

const reasons: Record<string, string> = {
  final: "该退款已有最终结果，无需核实。",
  in_flight: "该退款仍在发送中，请稍后重新读取。",
  automatic_query:
    "该通道支持查询退款结果，系统会自动核对；如长时间未更新，请确认 Worker 已开启 GOEPAY_ENABLE_PROVIDER_QUERIES。",
  combined_order: "合单退款按子订单核对，暂不支持人工结案。",
};
const stateLabels: Record<string, string> = {
  succeeded: "退款成功",
  failed: "退款失败",
};

const view = ref<AttestationView | null>(null),
  state = ref(""),
  reference = ref(""),
  note = ref(""),
  verified = ref(false),
  loading = ref(true),
  busy = ref(false),
  unknown = ref(false),
  done = ref(false),
  error = ref(""),
  notice = ref(""),
  dialog = ref<HTMLElement>();
const initialCSRF = props.client.csrfToken;
const canWrite = computed(
  () =>
    props.client.area === "admin" &&
    props.ownSession &&
    props.adminClass === "super" &&
    props.client.csrfToken === initialCSRF,
);
const ready = computed(
  () =>
    !loading.value &&
    !unknown.value &&
    !done.value &&
    !!view.value?.eligible &&
    view.value.states.includes(state.value) &&
    reference.value.trim() !== "" &&
    note.value.trim() !== "" &&
    verified.value,
);
const controller = new AbortController();
let alive = true;
const previousFocus = document.activeElement as HTMLElement | null;

async function load() {
  if (busy.value || controller.signal.aborted) return;
  loading.value = true;
  verified.value = false;
  error.value = "";
  try {
    const data = await props.client.request<AttestationView>(
      `/refunds/${encodeURIComponent(props.refundId)}/attestation`,
      { signal: controller.signal },
    );
    if (!alive || controller.signal.aborted) return;
    if (data.refund_id !== props.refundId) throw Error("退款记录无法核对");
    view.value = data;
    state.value =
      data.states.length === 1
        ? data.states[0]
        : data.states.includes(state.value)
          ? state.value
          : "";
    unknown.value = false;
  } catch (e) {
    if (alive && !controller.signal.aborted)
      error.value = e instanceof Error ? e.message : "退款核实信息读取失败";
  } finally {
    if (alive && !controller.signal.aborted) {
      loading.value = false;
      await nextTick();
      dialog.value
        ?.querySelector<HTMLElement>(
          "input:not([disabled]),textarea:not([disabled]),button:not([disabled])",
        )
        ?.focus();
    }
  }
}

async function submit() {
  if (!canWrite.value || !ready.value || busy.value || done.value) return;
  busy.value = true;
  error.value = "";
  try {
    const data = await props.client.request<AttestationView>(
      `/refunds/${encodeURIComponent(props.refundId)}/attest-result`,
      {
        method: "POST",
        body: {
          state: state.value,
          provider_reference: reference.value.trim(),
          note: note.value.trim(),
        },
        signal: controller.signal,
      },
    );
    if (!alive || controller.signal.aborted) return;
    view.value = data;
    done.value = true;
    unknown.value = false;
    notice.value = `已记录核实结果：${stateLabels[data.status] || data.status}`;
    emit("updated");
  } catch (e) {
    if (!alive || controller.signal.aborted) return;
    if (
      e instanceof ApiError &&
      e.responseDecoded &&
      e.status >= 400 &&
      e.status < 500 &&
      e.status !== 408 &&
      e.status !== 429
    ) {
      error.value =
        e.status === 409 ? "退款状态已变化，请重新读取后再核实" : e.message;
    } else {
      unknown.value = true;
      error.value = "提交结果未知，请重新读取该退款的状态，不要重复提交";
    }
  } finally {
    if (alive) busy.value = false;
  }
}

function trap(event: KeyboardEvent) {
  if (event.key !== "Tab" || !dialog.value) return;
  const items = Array.from(
    dialog.value.querySelectorAll<HTMLElement>(
      "input:not([disabled]),textarea:not([disabled]),button:not([disabled])",
    ),
  );
  if (event.shiftKey && document.activeElement === items[0]) {
    event.preventDefault();
    items.at(-1)?.focus();
  } else if (!event.shiftKey && document.activeElement === items.at(-1)) {
    event.preventDefault();
    items[0]?.focus();
  }
}

watch(
  () => [props.client, props.client.csrfToken, props.ownSession],
  () => {
    controller.abort();
    busy.value = false;
    loading.value = false;
    error.value = "会话已变化，请关闭后重新进入";
  },
);
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  controller.abort();
  if (previousFocus?.isConnected) previousFocus.focus();
});
</script>
<template>
  <div class="modal-backdrop" @keydown.esc="!busy && emit('close')">
    <section
      ref="dialog"
      class="modal panel refund-attestation"
      role="dialog"
      aria-modal="true"
      aria-label="核实退款结果"
      @keydown="trap"
    >
      <div class="toolbar">
        <h2>核实退款结果</h2>
        <button
          type="button"
          class="pa-icon-btn pa-icon-btn--ghost pa-modal-x"
          aria-label="关闭窗口"
          :disabled="busy"
          @click="emit('close')"
        >
          ×
        </button>
      </div>
      <p class="muted">
        适用于支付机构无法查询结果、长期处于“处理中/未知”的退款。请先在支付机构商家后台确认这笔退款的最终结果，再在此记录；结论提交后不可更改。
      </p>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
      <p v-if="loading" role="status">正在读取退款状态…</p>
      <dl v-if="view" class="refund-facts">
        <dt>退款单号</dt>
        <dd class="pa-order-no">{{ view.refund_no }}</dd>
        <dt>订单号</dt>
        <dd class="pa-order-no">{{ view.trade_no }}</dd>
        <dt>退款金额</dt>
        <dd>{{ view.amount }} 元</dd>
        <dt>发送情况</dt>
        <dd>
          {{ view.sent ? "已发送到支付机构，结果未确定" : "未发送到支付机构" }}
        </dd>
      </dl>
      <div v-if="view?.attestation" class="alert" role="status">
        已于 {{ beijingTimestamp(view.attestation.created_at) }} 由
        {{ view.attestation.admin_username }} 核实为“{{
          stateLabels[view.attestation.state] || view.attestation.state
        }}”，机构凭证：{{ view.attestation.provider_reference }}；说明：{{
          view.attestation.note
        }}
      </div>
      <p v-else-if="view && !view.eligible" class="alert" role="status">
        {{ reasons[view.reason] || "该退款当前不能人工核实。" }}
      </p>
      <fieldset
        v-if="view?.eligible && !done"
        class="form-rows"
        :disabled="busy || unknown || !canWrite"
      >
        <legend>支付机构侧的最终结果</legend>
        <label v-for="option in view.states" :key="option" class="choice"
          ><input
            v-model="state"
            type="radio"
            name="refund-state"
            :value="option"
          />
          {{ stateLabels[option] || option }}</label
        >
        <p v-if="view.states.length === 1" class="muted">
          未发送到支付机构的退款只能核实为失败，预留资金将退回商户可用余额。
        </p>
        <label
          ><span class="field-label">支付机构凭证号</span
          ><input
            v-model="reference"
            maxlength="128"
            autocomplete="off"
            placeholder="机构退款单号或查询记录编号"
        /></label>
        <label
          ><span class="field-label">核实说明</span
          ><textarea
            v-model="note"
            maxlength="500"
            placeholder="在哪里、如何核实，例如商家后台的退款记录状态"
          />
        </label>
        <label class="choice"
          ><input v-model="verified" type="checkbox" />
          我已在支付机构商家后台核实了这笔退款的最终结果</label
        >
      </fieldset>
      <div class="modal-actions">
        <button type="button" :disabled="busy" @click="emit('close')">
          关闭</button
        ><button
          v-if="unknown"
          type="button"
          :disabled="busy || loading"
          @click="load"
        >
          重新读取状态</button
        ><button
          v-if="view?.eligible && !done && !unknown"
          type="button"
          class="primary"
          :disabled="busy || !ready || !canWrite"
          @click="submit"
        >
          {{ busy ? "正在提交…" : "确认并提交" }}
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.refund-attestation {
  max-width: 600px;
  max-height: calc(100dvh - 32px);
  overflow: auto;
  overflow-wrap: anywhere;
}
.refund-facts {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 6px 12px;
  margin: 12px 0;
}
.refund-facts dt {
  color: var(--muted, inherit);
}
.refund-facts dd {
  margin: 0;
}
.refund-attestation textarea {
  min-height: 72px;
}
.choice {
  display: flex;
  align-items: center;
  gap: 8px;
}
.modal-actions {
  flex-wrap: wrap;
}
</style>
