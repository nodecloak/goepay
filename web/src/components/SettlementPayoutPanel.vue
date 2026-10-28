<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from "vue";
import type { ApiClient } from "../api";
import type { Area } from "../menu";
import {
  readPayoutDetail,
  readPayoutCapabilities,
  readSettlementPayout,
  payoutActions,
  payoutStateLabel,
  payoutFundingLabel,
  type PayoutDetail,
  type PayoutCapabilities,
} from "../settlement-payout";
import { centsMoney } from "../settlement-terms";
import { settlementSnapshotLabel } from "../settlement-recipient";
const props = defineProps<{
  client: ApiClient;
  area: Area;
  requestId: string;
}>();
const emit = defineEmits<{ close: []; changed: [] }>();
type Action = "prepare" | "execute" | "cancel-unsent";
const detail = ref<PayoutDetail | null>(null),
  caps = ref<PayoutCapabilities | null>(null),
  channel = ref("");
const loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  uncertain = ref(false),
  confirmation = ref<Action | null>(null);
let controller: AbortController | undefined,
  epoch = 0;
const endpoint = computed(
  () => "/settlement-payouts/" + encodeURIComponent(props.requestId),
);
const actions = computed(() =>
  detail.value
    ? payoutActions(detail.value, caps.value, props.area === "admin")
    : { prepare: false, execute: false, cancel: false, query: false },
);
async function load() {
  const current = ++epoch,
    requestId = props.requestId,
    base = endpoint.value;
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  confirmation.value = null;
  error.value = "";
  channel.value = "";
  detail.value = null;
  caps.value = null;
  try {
    const d = readPayoutDetail(
      await props.client.request(base, { signal }),
      requestId,
    );
    if (current !== epoch || signal.aborted) return;
    detail.value = d;
    if (props.area === "admin") {
      const c = readPayoutCapabilities(
        await props.client.request(base + "/capabilities", { signal }),
        requestId,
      );
      if (current !== epoch || signal.aborted) return;
      caps.value = c;
    }
    uncertain.value = false;
  } catch (e) {
    if (current === epoch && !signal.aborted) {
      error.value = e instanceof Error ? e.message : "结算出款读取失败";
      uncertain.value = true;
    }
  } finally {
    if (current === epoch) loading.value = false;
  }
}
function confirm(action: Action) {
  if (busy.value || uncertain.value) return;
  if (
    action === "prepare" &&
    (!actions.value.prepare ||
      !caps.value?.channels.some((c) => c.id === channel.value))
  )
    return;
  if (action === "execute" && !actions.value.execute) return;
  if (action === "cancel-unsent" && !actions.value.cancel) return;
  confirmation.value = action;
  error.value = "";
}
async function act(action: Action | "query") {
  if (
    !detail.value ||
    busy.value ||
    loading.value ||
    (action !== "query" && (uncertain.value || confirmation.value !== action))
  )
    return;
  const current = epoch,
    base = endpoint.value,
    requestId = props.requestId,
    merchant = detail.value.request.merchant_id;
  const body = action === "prepare" ? { channel_id: channel.value } : {};
  confirmation.value = null;
  busy.value = true;
  error.value = "";
  notice.value = "";
  const signal = controller?.signal;
  try {
    const result = await props.client.requestResult(base + "/" + action, {
      method: "POST",
      body,
      signal,
    });
    if (current !== epoch || signal?.aborted) return;
    const payout = readSettlementPayout(result.data, requestId, merchant);
    detail.value = { ...detail.value!, payout };
    notice.value =
      result.status === 202
        ? "请求结果待核实，只查询原申请和原 TF；不得重新准备或发送。"
        : payoutStateLabel(payout.state);
    uncertain.value = result.status === 202;
    emit("changed");
    // Re-read authoritative request funding. A read failure cannot enable another send.
    await load();
  } catch (e) {
    if (current === epoch && !signal?.aborted) {
      uncertain.value = true;
      error.value = `${e instanceof Error ? e.message : "处理结果未知"}。请刷新原申请核对；不会自动重发。`;
    }
  } finally {
    if (current === epoch || current + 1 === epoch) busy.value = false;
  }
}
watch(
  () => [props.requestId, props.client, props.area],
  () => {
    busy.value = false;
    notice.value = "";
    uncertain.value = false;
    void load();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  epoch++;
  controller?.abort();
  confirmation.value = null;
});
</script>
<template>
  <section class="panel" aria-label="结算出款">
    <div class="toolbar">
      <h3>结算出款 · 申请 {{ requestId }}</h3>
      <div class="toolbar-actions">
        <button :disabled="loading || busy" @click="load">刷新原申请</button
        ><button :disabled="busy" @click="emit('close')">关闭出款详情</button>
      </div>
    </div>
    <p class="muted">
      核准不代表已出款。准备仅绑定原申请与唯一
      TF；发送使用原收款快照和独立冻结资金，不会再次扣减可用余额。银行卡成功后保留本申请手续费，等待退票核对；原出款可持续查询。
    </p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert" role="status">{{ notice }}</p>
    <p v-if="loading" role="status">正在读取原申请与出款绑定…</p>
    <template v-if="detail">
      <dl class="detail-grid">
        <dt>商户</dt>
        <dd>{{ detail.request.merchant_id }}</dd>
        <dt>申请审批状态</dt>
        <dd>{{ detail.request.status }}</dd>
        <dt>申请总额</dt>
        <dd>{{ centsMoney(detail.request.amount_cents) }} 元</dd>
        <dt>申请手续费</dt>
        <dd>
          {{
            detail.request.settlement
              ? centsMoney(detail.request.settlement.fee_amount) + " 元"
              : "历史未记录"
          }}
        </dd>
        <dt>出款净额</dt>
        <dd>
          {{
            detail.request.settlement
              ? centsMoney(detail.request.settlement.net_amount) + " 元"
              : "历史未记录"
          }}
        </dd>
        <dt>原收款快照</dt>
        <dd>{{ settlementSnapshotLabel(detail.request.recipient) }}</dd>
        <dt>资金归属</dt>
        <dd>
          {{
            payoutFundingLabel(detail.payout?.funding || detail.request.funding)
          }}
        </dd>
      </dl>
      <template v-if="detail.payout"
        ><dl class="detail-grid">
          <dt>原 TF</dt>
          <dd>{{ detail.payout.transfer.transfer_no }}</dd>
          <dt>固定通道</dt>
          <dd>{{ detail.payout.channel_id }}</dd>
          <dt>发送证明</dt>
          <dd>{{ detail.payout.send_state }}</dd>
          <dt>出款结果</dt>
          <dd>{{ payoutStateLabel(detail.payout.state) }}</dd>
          <template v-if="detail.payout.transfer.type === 'bank'">
            <dt>银行卡产品</dt>
            <dd>
              {{
                detail.payout.transfer.bank_product === "alipay_bankcard"
                  ? "支付宝银行卡转账"
                  : "微信银行卡转账"
              }}
            </dd>
            <dt>本申请仍保留费用</dt>
            <dd>{{ centsMoney(detail.payout.funding.reserved_amount) }} 元</dd>
          </template>
          <dt>TF 金额</dt>
          <dd>{{ centsMoney(detail.payout.transfer.amount) }} 元</dd>
        </dl>
        <p v-if="detail.payout.send_state === 'claimed'" class="muted">
          已领取发送的原意图不能恢复为未发送；处理中、未知或失败均不能生成新 TF
          重发。
        </p></template
      >
      <p v-else class="muted">
        尚未准备出款。缺失申请费用快照、独立资金或收款快照时不能出款；银行卡须匹配明确可用的银行产品。
      </p>
      <p v-if="caps?.reason" class="alert">{{ caps.reason }}</p>
      <hr class="payout-divider" aria-hidden="true" />
      <template v-if="actions.prepare && !uncertain"
        ><label
          >出款通道<select
            v-model="channel"
            :disabled="busy || loading || !!confirmation"
          >
            <option value="">请选择并核对</option>
            <option v-for="c in caps?.channels" :key="c.id" :value="c.id">
              {{
                c.type === "bank"
                  ? c.bank_product === "alipay_bankcard"
                    ? "支付宝银行卡"
                    : "微信银行卡"
                  : c.type === "alipay"
                    ? "支付宝"
                    : "微信"
              }}
              · {{ c.id }}{{ c.app_id ? " · AppID " + c.app_id : "" }}
            </option>
          </select></label
        ><button
          data-action="prepare"
          :disabled="!channel || busy || loading || !!confirmation"
          @click="confirm('prepare')"
        >
          准备唯一出款绑定
        </button></template
      >
      <div class="row-actions">
        <button
          v-if="actions.execute && !uncertain"
          data-action="execute"
          :disabled="busy || loading || !!confirmation"
          class="primary"
          @click="confirm('execute')"
        >
          发送原结算出款</button
        ><button
          v-if="actions.cancel && !uncertain"
          data-action="cancel"
          :disabled="busy || loading || !!confirmation"
          @click="confirm('cancel-unsent')"
        >
          取消已证明未发送出款</button
        ><button
          v-if="actions.query"
          data-action="query"
          :disabled="busy || loading"
          @click="act('query')"
        >
          只查询原出款
        </button>
      </div>
      <div
        v-if="confirmation"
        class="alert"
        role="group"
        aria-label="结算出款确认"
      >
        <p>
          {{
            confirmation === "prepare"
              ? "确认使用当前所选通道绑定原收款快照？此步骤不向支付机构付款。"
              : confirmation === "execute"
                ? "确认向原脱敏收款账户发送本申请净额？扣款将使用本申请原独立冻结资金；结果未知时只能查询原 TF。"
                : "确认仅取消已证明未发送的原 TF，并释放本申请总额？已领取发送的意图不能取消。"
          }}
        </p>
        <button :disabled="busy" @click="confirmation = null">返回核对</button
        ><button
          data-action="confirm"
          :disabled="busy"
          @click="act(confirmation!)"
        >
          {{
            confirmation === "execute" ? "确认发送并核对资金授权" : "确认此操作"
          }}
        </button>
      </div>
    </template>
  </section>
</template>
<style scoped>
.detail-grid {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: var(--space-2) var(--space-4);
  margin: var(--space-3) 0;
  font-size: 14px;
}
.detail-grid dt {
  color: var(--ink-muted);
  text-align: right;
  min-width: 120px;
}
.detail-grid dd {
  margin: 0;
  min-width: 0;
  overflow-wrap: anywhere;
}
.payout-divider {
  border: 0;
  border-top: 1px solid var(--line);
  margin: var(--space-4) 0;
}
</style>
