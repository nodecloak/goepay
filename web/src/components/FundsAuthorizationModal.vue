<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount } from "vue";
import { ApiClient, displayValue } from "../api";
import {
  validateAuthorizationTarget,
  readAuthorizationDeadline,
  snapshotFundsBody,
} from "../funds-authorization";
const props = defineProps<{
  client: ApiClient;
  target: string;
  body: Record<string, unknown>;
  beforeDispatch?: () => void;
}>();
const summaryLabels: Record<string, string> = {
  expected_event_id: "原通道停用记录",
  channel_stop_code: "原通道异常代码",
  release_remaining_amount: "本次剩余解除额（元）",
  release_source_revision: "确认来源修订",
  authorization_observation_id: "原冻结观察标识",
  release_policy: "剩余冻结解除规则",
  income_hold_id: "原收入冻结批次",
  income_hold_source_mode: "冻结资金来源",
  held_original: "本批原冻结款（元）",
  held_remaining: "本批剩余冻结款（元）",
  held_pending: "尚未确定的退款预留（元）",
  held_succeeded: "已退款冻结款（元）",
  refund_held_amount: "本次使用原订单冻结款（元）",
  refund_available_amount: "本次使用商户可用余额（元）",
  refund_fee_amount: "本次平台手续费部分（元）",
  refund_funding_mode: "退款资金来源策略",
  income_hold_adoption_policy: "历史冻结款迁移规则",
  plan_id: "计划ID",
  plan_name: "计划名称",
  expected_revision: "核对修订",
  mode: "授权范围",
  merchant_ids: "固定商户名单",
  recipients: "收款对象及原配置",
  maturity: "可结算收入成熟规则",
  retained_money: "固定留存额（元）",
  minimum_money: "政策最低毛额（元）",
  maximum_money: "政策毛额上限（元，0为不额外限制）",
  fee_rate_bps: "手续费率（万分比）",
  group_fee_rules: "各商户原结算费率",
  fee_scope: "手续费规则",
  fee_minimum_money: "最低手续费（元）",
  fee_maximum_money: "手续费封顶（元，0为不额外限制）",
  merchant_daily_limit: "单商户日次数上限（0为不额外限制）",
  valid_from: "授权开始",
  expires_at: "授权截止",
  max_item_gross: "每项毛额上限（元）",
  max_run_gross: "每次毛额上限（元）",
  max_day_gross: "每日毛额上限（元）",
  max_day_items: "每日条数上限",
  previous_authorization_id: "原授权ID",
  revocation: "撤销与到期规则",
  direction: "调整方向",
  adjust_type: "资金明细类型",
  amount: "调整金额（元）",
  external_no: "业务请求号",
};
const dialogElement = ref<HTMLElement | null>(null);
let previousFocus: HTMLElement | null = null;
function trapFocus(event: KeyboardEvent) {
  if (event.key !== "Tab" || !dialogElement.value) return;
  const nodes = [
    ...dialogElement.value.querySelectorAll<HTMLElement>(
      'button:not(:disabled), input:not(:disabled), [href], [tabindex="0"]',
    ),
  ].filter((node) => node.getClientRects().length);
  if (!nodes.length) {
    event.preventDefault();
    dialogElement.value.focus();
    return;
  }
  const first = nodes[0]!,
    last = nodes[nodes.length - 1]!;
  if (
    event.shiftKey &&
    (document.activeElement === first ||
      document.activeElement === dialogElement.value)
  ) {
    event.preventDefault();
    last.focus();
  } else if (
    !event.shiftKey &&
    (document.activeElement === last ||
      document.activeElement === dialogElement.value)
  ) {
    event.preventDefault();
    first.focus();
  }
}
const emit = defineEmits<{
  close: [];
  completed: [result: { status: number; data: unknown }];
  failed: [error: Error];
}>();
const phase = ref<"idle" | "summary" | "password" | "ready" | "sent">("idle"),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  summary = ref<unknown>(null),
  password = ref(""),
  deadline = ref(0),
  now = ref(Date.now());
let challenge = "",
  grant = "",
  binding = "",
  capturedTarget = "",
  capturedBody: Record<string, unknown> = {},
  active = true,
  controller: AbortController | undefined;
const currentBinding = () =>
  JSON.stringify({ target: props.target, body: props.body });
function clear() {
  password.value = "";
  challenge = "";
  grant = "";
  deadline.value = 0;
  summary.value = null;
}
function invalidate() {
  controller?.abort();
  controller = undefined;
  clear();
  phase.value = "idle";
  busy.value = false;
  error.value = "操作参数已变化，请重新预览并核对原业务。";
}
watch(() => [props.target, props.body], invalidate, {
  deep: true,
  flush: "sync",
});
const timer = setInterval(() => {
  now.value = Date.now();
  if (deadline.value && now.value >= deadline.value && phase.value !== "sent") {
    clear();
    phase.value = "idle";
    error.value = "资金授权已过期，请重新预览原业务。";
  }
}, 500);
function valid() {
  if (binding !== currentBinding()) {
    invalidate();
    return false;
  }
  if (deadline.value <= Date.now()) {
    clear();
    phase.value = "idle";
    error.value = "资金授权已过期，请重新预览原业务。";
    return false;
  }
  return true;
}
async function preview() {
  if (busy.value || phase.value === "sent" || props.client.area !== "admin")
    return;
  clear();
  error.value = "";
  notice.value = "";
  phase.value = "idle";
  busy.value = true;
  controller?.abort();
  const c = new AbortController();
  controller = c;
  try {
    validateAuthorizationTarget(props.target);
    capturedTarget = props.target;
    capturedBody = snapshotFundsBody(props.body);
    binding = currentBinding();
    const raw = await props.client.request<{
      challenge_id: string;
      expires_at: string;
      summary: unknown;
    }>("/security/funds-authorizations/challenges", {
      method: "POST",
      body: { target: capturedTarget, body: capturedBody },
      signal: c.signal,
    });
    if (!active || c.signal.aborted || binding !== currentBinding()) return;
    if (
      typeof raw.challenge_id !== "string" ||
      !raw.challenge_id ||
      !raw.summary ||
      typeof raw.summary !== "object"
    )
      throw new Error("资金操作预览响应不完整");
    deadline.value = readAuthorizationDeadline(raw);
    challenge = raw.challenge_id;
    summary.value = raw.summary;
    phase.value = "summary";
  } catch (e) {
    if (active && !c.signal.aborted) {
      clear();
      error.value = e instanceof Error ? e.message : "无法预览资金操作";
    }
  } finally {
    if (controller === c) busy.value = false;
  }
}
function confirmSummary() {
  if (!busy.value && phase.value === "summary" && valid())
    phase.value = "password";
}
async function authorize() {
  if (busy.value || phase.value !== "password" || !valid()) return;
  if (!password.value) {
    error.value = "请输入本人管理员资金密码";
    password.value = "";
    return;
  }
  busy.value = true;
  error.value = "";
  const paymentPassword = password.value;
  password.value = "";
  const c = new AbortController();
  controller = c;
  try {
    const raw = await props.client.request<{
      authorization: string;
      expires_at: string;
    }>("/security/funds-authorizations/confirm", {
      method: "POST",
      body: { challenge_id: challenge, payment_password: paymentPassword },
      signal: c.signal,
    });
    if (!active || c.signal.aborted || binding !== currentBinding()) return;
    if (
      typeof raw.authorization !== "string" ||
      !raw.authorization ||
      /[\r\n]/.test(raw.authorization)
    )
      throw new Error("资金授权响应不完整");
    deadline.value = readAuthorizationDeadline(raw);
    challenge = "";
    grant = raw.authorization;
    phase.value = "ready";
  } catch (e) {
    if (active && !c.signal.aborted) {
      clear();
      phase.value = "idle";
      error.value =
        e instanceof Error ? e.message : "资金密码验证失败，请重新预览";
    }
  } finally {
    password.value = "";
    if (controller === c) busy.value = false;
  }
}
async function execute() {
  if (busy.value || phase.value !== "ready" || !valid() || !grant) return;
  busy.value = true;
  error.value = "";
  phase.value = "sent";
  const authorization = grant;
  grant = "";
  password.value = "";
  try {
    const result = await props.client.requestResult(
      validateAuthorizationTarget(capturedTarget),
      {
        method: "POST",
        body: capturedBody,
        paymentAuthorization: authorization,
        beforeDispatch: props.beforeDispatch,
      },
    );
    if (!active) return;
    notice.value =
      result.status === 202
        ? "业务结果尚未核实，只能查询原业务，不会自动重新提交。"
        : "原业务已返回，请按原记录状态核对，不能只凭 HTTP200 判断资金已到账。";
    emit("completed", result);
  } catch (e) {
    if (active) {
      const failure =
        e instanceof Error ? e : new Error("资金操作结果尚未核实");
      error.value = `原业务结果未确认，请保留原业务请求号，只能查询原业务，不会自动重新提交。${failure.message}`;
      emit("failed", failure);
    }
  } finally {
    clear();
    capturedBody = {};
    if (active) busy.value = false;
  }
}
function close() {
  if (busy.value) return;
  clear();
  capturedBody = {};
  emit("close");
}
onMounted(() => {
  previousFocus =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
  dialogElement.value?.focus();
  void preview();
});
onBeforeUnmount(() => {
  active = false;
  controller?.abort();
  clearInterval(timer);
  clear();
  capturedBody = {};
  if (previousFocus?.isConnected && !previousFocus.closest("[inert]"))
    previousFocus.focus();
});
</script>
<template>
  <div class="modal-backdrop drawer-backdrop" @keydown.esc="close">
    <section
      ref="dialogElement"
      tabindex="-1"
      @keydown="trapFocus"
      class="modal panel modal-drawer funds-auth-drawer"
      role="dialog"
      aria-modal="true"
      aria-label="管理员资金二次授权"
    >
      <div class="toolbar">
        <h2>管理员资金二次授权</h2>
        <button
          type="button"
          class="pa-icon-btn pa-icon-btn--ghost pa-modal-x"
          aria-label="关闭"
          :disabled="busy"
          @click="close"
        >
          ×
        </button>
      </div>
      <div class="funds-auth-body">
        <p>
          仅授权当前核对的原业务；付款金额、收款对象、通道和请求号改变后必须重新预览。授权不表示已付款，也不会开放通道写权限。
        </p>
        <p v-if="busy" role="status">正在核对资金操作…</p>
        <div v-if="error" role="alert" class="alert error">{{ error }}</div>
        <p v-if="notice" role="status" class="alert">{{ notice }}</p>
        <template v-if="summary">
          <h3>服务端操作摘要</h3>
          <dl v-if="!Array.isArray(summary)" class="record-detail">
            <template
              v-for="(value, key) in summary as Record<string, unknown>"
              :key="key"
              ><dt>{{ summaryLabels[key] || key }}</dt>
              <dd>{{ displayValue(value) }}</dd></template
            >
          </dl>
          <p v-else>{{ displayValue(summary) }}</p>
          <p v-if="deadline" class="muted">
            本次确认有效至
            {{ new Date(deadline).toLocaleString() }}，过期后需重新预览。
          </p>
        </template>
        <button
          v-if="phase === 'summary'"
          data-action="confirm-summary"
          :disabled="busy"
          @click="confirmSummary"
        >
          已核对摘要，验证资金密码
        </button>
        <form
          v-if="phase === 'password'"
          class="form-rows"
          @submit.prevent="authorize"
        >
          <label
            ><span class="field-label field-label--req">本人管理员资金密码</span
            ><input
              v-model="password"
              name="payment_password"
              type="password"
              autocomplete="off"
              :disabled="busy"
              required
              autofocus
          /></label>
          <div class="modal-actions">
            <button class="primary" :disabled="busy">确认资金密码</button>
          </div>
        </form>
        <div v-if="phase === 'ready'" class="alert">
          <p>
            资金密码已验证。尚未提交原业务，请再次确认后执行。令牌只用于原请求，不会显示或保存。
          </p>
          <button
            data-action="execute"
            class="danger-button"
            :disabled="busy"
            @click="execute"
          >
            执行已核对的原操作
          </button>
        </div>
      </div>
      <div class="modal-actions">
        <button :disabled="busy" @click="close">关闭资金授权</button
        ><button v-if="phase === 'idle'" :disabled="busy" @click="preview">
          重新预览原业务
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.funds-auth-drawer {
  --drawer-w: 560px;
}
/* The summary table, status blocks and the password step scroll; the head
   and the dialog actions stay in view. */
.funds-auth-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.funds-auth-body > :is(p, h3, dl, form, div) {
  margin: 0;
}
/* The password step's confirm button sits with the step, right-aligned, and
   is not the drawer's sticky bottom bar. */
.funds-auth-body .modal-actions {
  margin: 0;
  padding: 0;
  border: 0;
  position: static;
  background: none;
}
.funds-auth-body .record-detail {
  overflow-wrap: anywhere;
}
</style>
