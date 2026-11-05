<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from "vue";
import { ApiClient } from "../api";
import {
  referralID,
  referralAmount,
  readReferralCurrentPolicy,
  type ReferralCurrentPolicy,
} from "../referral";
import { beijingTimestamp } from "../console-datetime";
const props = defineProps<{ client: ApiClient; merchantID: string }>();
const policy = ref<ReferralCurrentPolicy | null>(null),
  loading = ref(false),
  error = ref("");
let active = true,
  epoch = 0,
  controller: AbortController | undefined;
function reset() {
  epoch++;
  controller?.abort();
  policy.value = null;
  error.value = "";
  loading.value = false;
}
async function load() {
  reset();
  if (
    !active ||
    props.client.area !== "merchant" ||
    !referralID(props.merchantID)
  ) {
    error.value = "仅商户本人可查看当前邀请政策";
    return;
  }
  const client = props.client,
    csrf = client.csrfToken,
    owner = props.merchantID,
    current = epoch,
    c = new AbortController();
  controller = c;
  loading.value = true;
  const valid = () =>
    active &&
    current === epoch &&
    !c.signal.aborted &&
    props.client === client &&
    client.csrfToken === csrf &&
    props.merchantID === owner;
  try {
    const data = await client.request<unknown>("/referral/current-policy", {
      signal: c.signal,
    });
    if (valid()) policy.value = readReferralCurrentPolicy(data, owner);
  } catch (e) {
    if (valid())
      error.value = e instanceof Error ? e.message : "当前政策读取失败";
  } finally {
    if (valid()) loading.value = false;
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.client.area,
    props.merchantID,
  ],
  () => {
    reset();
    void load();
  },
  { flush: "sync" },
);
onMounted(load);
onBeforeUnmount(() => {
  active = false;
  reset();
});
const basisLabel = computed(
  () =>
    ({
      fee: "手续费",
      gross: "订单金额",
      profit: "手续费利润",
      unknown: "待确认",
    })[policy.value?.basis || "unknown"],
);
const percentage = computed(() => {
  const n = policy.value?.rate_bps;
  return n === null || n === undefined
    ? "待确认"
    : `${Math.floor(n / 100)}.${String(n % 100).padStart(2, "0")}%`;
});
const statusLabel = computed(
  () =>
    ({
      active: "当前通用政策已开启",
      conditional: "取决于付款商户条件",
      disabled: "未开启邀请返现",
      budget_disabled: "当前未开启手续费预算",
      merchant_disabled: "当前商户未启用",
      zero_rate: "当前返现比例为零",
      unknown: "当前政策无法确认",
    })[policy.value?.status || "unknown"],
);
const sourceLabel = (source: string) =>
  ({
    default: "默认关闭状态",
    global: "全局政策",
    promoter_group: "本人当前用户组政策",
  })[source] || "待确认";
</script>
<template>
  <section class="panel current-policy" aria-label="当前邀请返现政策">
    <header>
      <h3>邀请返现介绍</h3>
    </header>
    <p v-if="loading" role="status">正在读取当前邀请政策…</p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <template v-if="policy">
      <p class="policy-status" role="status">{{ statusLabel }}</p>
      <p v-if="policy.status === 'unknown'">
        本人基础用户组仍待核对，暂不能确认有效政策或计算示例。
      </p>
      <template v-else>
        <p v-if="!policy.payment_fee_enabled" class="alert">
          当前没有新订单的手续费奖励预算，新订单不产生奖励；历史条款另行核对。下方仅说明比例算法。
        </p>
        <p v-if="policy.payer_group_dependent" class="alert">
          下列为全局与本人当前用户组的通用条件。实际比例、开关或计算基数还可能随付款商户用户组变化，以新订单固定的条款为准。
        </p>
        <div class="policy-grid">
          <div class="policy-facts">
            <p class="rate">
              当前通用返现比例：<strong>{{ percentage }}</strong>
            </p>
            <dl>
              <dt>计算基数</dt>
              <dd>{{ basisLabel }}</dd>
              <dt>发放方式</dt>
              <dd>
                {{
                  policy.mode === "daily"
                    ? "按日核对后发放"
                    : "核实付款后即时发放"
                }}
              </dd>
              <dt>本人有效用户组</dt>
              <dd>
                {{
                  policy.current_group.source === "membership"
                    ? "当前有效会员组"
                    : "基础用户组"
                }}
                #{{ policy.current_group.id
                }}<span v-if="policy.current_group.expires_at"
                  >，有效至
                  {{
                    beijingTimestamp(policy.current_group.expires_at)
                  }}（北京时间）</span
                ><span v-if="policy.current_group.source === 'membership'"
                  >；到期回到基础用户组 #{{
                    policy.current_group.base_group_id
                  }}</span
                >
              </dd>
              <dt>开关 / 比例来源</dt>
              <dd>
                {{ sourceLabel(policy.inheritance.enabled) }} /
                {{ sourceLabel(policy.inheritance.rate_bps) }}
              </dd>
              <dt>基数 / 发放来源</dt>
              <dd>
                {{ sourceLabel(policy.inheritance.basis) }} /
                {{ sourceLabel(policy.inheritance.mode) }}
              </dd>
            </dl>
          </div>
          <div v-if="policy.example" class="example">
            <h4>每100元基数的比例示例</h4>
            <p>
              假设所选基数为
              {{ referralAmount(policy.example.basis_amount) }} 元{{
                basisLabel
              }}，按 {{ percentage }} 计算，比例金额为
              <strong
                >{{
                  referralAmount(policy.example.percentage_amount)
                }}
                元</strong
              >。
            </p>
            <p>
              此示例只演示比例算法；实际奖励最多不超过原订单手续费，不承诺发放。
            </p>
            <p class="estimate">
              <strong>真实订单预估：无法估算</strong
              ><br />实际订单手续费、成本或利润尚未确定，不按零成本推算收益。
            </p>
          </div>
        </div>
      </template>
      <p class="conditions">
        通过本人有效推广链接完成新注册邀请关系后，普通付款订单核实成功且有原手续费预算时，按下单时条款计算奖励；充值不参与。{{
          policy.refund_policy === "any_successful_refund_voids_reward"
            ? "任意成功退款会撤销整笔原奖励。"
            : ""
        }}
        按日奖励先记待发，核对后发放。历史流水与当前政策分别核对，均不代表可提现余额。
      </p>
    </template>
  </section>
</template>
<style scoped>
.current-policy {
  margin: 1rem 0;
  min-width: 0;
  padding: 1rem;
}
.current-policy header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.current-policy h3 {
  margin: 0;
}
.current-policy button {
  min-height: var(--control-h);
}
.policy-status {
  font-weight: 600;
}
.policy-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1.25rem;
}
.policy-facts dl {
  display: grid;
  grid-template-columns: 7.5rem minmax(0, 1fr);
  gap: 0.65rem 0.75rem;
}
.policy-facts dt {
  color: var(--muted, var(--ink-muted));
}
.policy-facts dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.rate strong {
  color: var(--primary, var(--primary-text));
  font-size: 1.35rem;
}
.example {
  border: 1px solid var(--border, var(--line));
  border-radius: 8px;
  padding: 1rem;
  background: var(--surface-muted, var(--bg-raised));
}
.example h4 {
  margin: 0 0 0.75rem;
}
.example strong {
  font-variant-numeric: tabular-nums;
}
.estimate {
  margin-bottom: 0;
}
.conditions {
  color: var(--muted, var(--ink-muted));
  margin-bottom: 0;
}
.current-policy p {
  overflow-wrap: anywhere;
  line-height: 1.65;
}
@media (max-width: 700px) {
  .policy-grid {
    grid-template-columns: 1fr;
  }
  .policy-facts dl {
    grid-template-columns: 1fr;
    gap: 0.3rem;
  }
  .policy-facts dd {
    margin-bottom: 0.5rem;
  }
}
</style>
