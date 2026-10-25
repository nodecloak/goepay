<script setup lang="ts">
import { computed, ref, watch, onMounted, onBeforeUnmount } from "vue";
import { ApiClient } from "../api";
import { referralPolicyBody, type ReferralPolicyView } from "../referral";
import MembershipReferralPolicy from "./MembershipReferralPolicy.vue";
const props = defineProps<{ client: ApiClient; adminClass?: string }>();
const scope = ref("global"),
  group = ref(""),
  current = ref<ReferralPolicyView | null>(null),
  enabled = ref("disabled"),
  rate = ref("0"),
  basis = ref<ReferralPolicyView["basis"]>("fee"),
  mode = ref<ReferralPolicyView["mode"]>("immediate"),
  loading = ref(false),
  busy = ref(false),
  confirming = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref("");
let controller: AbortController | undefined,
  frozen:
    | { target: string; body: Record<string, unknown>; epoch: number }
    | undefined,
  epoch = 0,
  active = true;
const permitted = computed(
  () => props.adminClass === "super" && props.client.area === "admin",
);
const target = () =>
  "/referral-policies/" +
  scope.value +
  "/" +
  (scope.value === "global" ? "0" : group.value);
function reset() {
  epoch++;
  controller?.abort();
  controller = undefined;
  current.value = null;
  frozen = undefined;
  confirming.value = false;
  loading.value = false;
  busy.value = false;
  uncertain.value = false;
  enabled.value = "disabled";
  rate.value = "0";
  basis.value = "fee";
  mode.value = "immediate";
  error.value = "";
  notice.value = "";
}
watch(
  () => [
    scope.value,
    group.value,
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.adminClass,
  ],
  reset,
  { flush: "sync" },
);
async function load() {
  if (!active || busy.value) return;
  if (!permitted.value) {
    error.value = "仅超级管理员可配置邀请政策";
    return;
  }
  if (
    scope.value === "group" &&
    (!/^[1-9]\d{0,18}$/.test(group.value) ||
      BigInt(group.value) > 9223372036854775807n)
  ) {
    error.value = "请输入规范用户组ID";
    return;
  }
  controller?.abort();
  const c = new AbortController(),
    currentEpoch = epoch,
    client = props.client,
    path = target(),
    expectedScope = scope.value,
    expectedID = scope.value === "global" ? "0" : group.value;
  controller = c;
  loading.value = true;
  current.value = null;
  error.value = "";
  confirming.value = false;
  frozen = undefined;
  try {
    const out = await client.request<ReferralPolicyView>(path, {
      signal: c.signal,
    });
    if (
      !active ||
      c.signal.aborted ||
      currentEpoch !== epoch ||
      !permitted.value
    )
      return;
    if (
      !out ||
      typeof out !== "object" ||
      Array.isArray(out) ||
      out.scope !== expectedScope ||
      out.scope_id !== expectedID ||
      typeof out.revision !== "string" ||
      !/^(0|[1-9]\d{0,18})$/.test(out.revision) ||
      BigInt(out.revision) > 9223372036854775807n ||
      typeof out.id !== "string" ||
      !/^(0|[1-9]\d{0,18})$/.test(out.id) ||
      BigInt(out.id) > 9223372036854775807n ||
      (out.enabled !== null && typeof out.enabled !== "boolean") ||
      (out.rate_bps !== null &&
        (!Number.isInteger(out.rate_bps) ||
          out.rate_bps < 0 ||
          out.rate_bps > 10000)) ||
      !["immediate", "daily"].includes(out.mode) ||
      (out.mode === "daily" && out.daily_mode_available !== true) ||
      !["fee", "gross", "profit"].includes(out.basis) ||
      (out.basis === "profit" && out.profit_basis_available !== true) ||
      out.refund_policy !== "any_successful_refund_voids_reward"
    )
      throw new Error("政策响应不匹配，请重新读取");
    current.value = out;
    basis.value = out.basis;
    mode.value = out.mode;
    enabled.value =
      out.enabled === null
        ? scope.value === "group"
          ? "inherit"
          : "disabled"
        : out.enabled
          ? "enabled"
          : "disabled";
    rate.value =
      out.rate_bps === null
        ? scope.value === "group"
          ? ""
          : "0"
        : String(out.rate_bps);
    uncertain.value = false;
  } catch (e) {
    if (active && currentEpoch === epoch && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "政策读取失败";
  } finally {
    if (active && currentEpoch === epoch && controller === c)
      loading.value = false;
  }
}
function review() {
  if (
    !active ||
    !permitted.value ||
    !current.value ||
    busy.value ||
    loading.value ||
    uncertain.value
  )
    return;
  try {
    frozen = {
      target: target(),
      body: Object.freeze(
        referralPolicyBody(
          current.value,
          enabled.value,
          rate.value,
          basis.value,
          mode.value,
        ),
      ),
      epoch,
    };
    confirming.value = true;
    error.value = "";
  } catch (e) {
    error.value = e instanceof Error ? e.message : "请核对政策";
  }
}
async function save() {
  if (
    !active ||
    !permitted.value ||
    !current.value ||
    !frozen ||
    !confirming.value ||
    busy.value ||
    uncertain.value ||
    frozen.epoch !== epoch ||
    frozen.target !== target()
  )
    return;
  const proposal = frozen,
    client = props.client,
    currentEpoch = epoch,
    c = new AbortController();
  controller?.abort();
  controller = c;
  frozen = undefined;
  busy.value = true;
  confirming.value = false;
  error.value = "";
  try {
    await client.request(proposal.target, {
      method: "POST",
      body: proposal.body,
      signal: c.signal,
    });
    if (
      !active ||
      c.signal.aborted ||
      currentEpoch !== epoch ||
      !permitted.value
    )
      return;
    notice.value = "新政策版本已保存，仅影响之后新建订单的条款。";
    busy.value = false;
    await load();
  } catch (e) {
    if (!active || c.signal.aborted || currentEpoch !== epoch) return;
    uncertain.value = true;
    error.value = e instanceof Error ? e.message : "政策保存结果未知";
    notice.value =
      "请重新读取原政策版本，核对后再决定是否修改；不会自动重复提交。";
  } finally {
    if (active && currentEpoch === epoch) busy.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => {
  active = false;
  reset();
});
</script>
<template>
  <section class="panel" aria-label="邀请返现政策">
    <h2>邀请返现政策</h2>
    <p>
      可选择原订单手续费或订单金额作为比例基数；两种奖励均不超过原订单平台手续费。任意成功退款撤销整笔奖励。比例0–10000
      BPS（100
      BPS=1%）；未启用政策不发奖，旧订单不追补。政策变更需本人管理员资金授权。
    </p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <form @submit.prevent="load">
      <label
        >政策范围<select v-model="scope" :disabled="busy">
          <option value="global">全局</option>
          <option value="group">用户组</option>
        </select></label
      ><label v-if="scope === 'group'"
        >用户组 ID<input
          v-model="group"
          inputmode="numeric"
          :disabled="busy"
          maxlength="19" /></label
      ><button :disabled="busy || loading || !permitted">读取政策版本</button>
    </form>
    <section v-if="current">
      <p>
        当前修订 {{ current.revision }}。{{
          current.revision === "0"
            ? "尚未配置本范围政策；全局缺省不发奖，用户组未配置项继承。"
            : "仅新订单使用本版本的有效配置，原奖励及退款来源不改写。"
        }}
      </p>
      <p>
        覆盖顺序：全局 → 付款商户组 →
        推广商户组；启用和比例空项继承，比例0是明确覆盖。存在用户组政策时，其基数明确覆盖，不继承上层基数；发奖时机也明确覆盖。
      </p>
      <label
        >启用状态<select
          v-model="enabled"
          :disabled="busy || confirming || uncertain"
        >
          <option v-if="scope === 'group'" value="inherit">继承</option>
          <option value="disabled">关闭</option>
          <option value="enabled">启用</option>
        </select></label
      ><label
        >奖励基数<select
          v-model="basis"
          :disabled="busy || confirming || uncertain"
        >
          <option value="fee">原订单平台手续费</option>
          <option value="gross">原订单金额（原手续费封顶）</option>
          <option v-if="current.profit_basis_available === true" value="profit">
            配置估计利润（非实际账单）
          </option>
        </select></label
      >
      <label
        >发奖时机<select
          v-model="mode"
          :disabled="busy || confirming || uncertain"
        >
          <option value="immediate">即时发奖</option>
          <option v-if="current.daily_mode_available === true" value="daily">
            次日北京时间待发
          </option>
        </select></label
      >
      <p v-if="mode === 'daily'" class="muted">
        每笔原订单固定奖励和手续费上限，次日北京时间起等待专用调度器按日发放；此设置不代表调度器已启动。
        不按迟到的汇总或当前用户组重新计算。退款结果未知时暂存，退款失败恢复原待发额，首次成功退款撤销整笔原奖励。
      </p>
      <p class="muted">
        {{
          basis === "gross"
            ? "按原订单金额乘以比例，按分四舍五入，奖励不超过原订单平台手续费。"
            : basis === "profit"
              ? "基数按原订单手续费减去通道配置估计成本后按分四舍五入，负利润不发奖；奖励按基数与比例计算，仅使用原订单手续费。配置估计利润非实际账单，启用该奖励时缺少可信成本不能创建相关支付。"
              : "按原订单平台手续费乘以比例，按分四舍五入，奖励不超过原订单平台手续费。"
        }}不使用其它订单资金。
      </p>
      <label
        >返现比例 BPS<input
          v-model="rate"
          inputmode="numeric"
          :disabled="busy || confirming || uncertain"
          maxlength="5"
          :placeholder="scope === 'group' ? '留空继承' : '0–10000'"
      /></label>
      <button
        :disabled="busy || loading || confirming || uncertain"
        @click="review"
      >
        核对政策变更
      </button>
      <div v-if="confirming" class="alert">
        <p>
          确认{{ scope === "global" ? "全局" : "用户组 " + group }}政策修订
          {{ current.revision }}，启用项
          {{
            enabled === "inherit"
              ? "继承"
              : enabled === "enabled"
                ? "启用"
                : "关闭"
          }}，返现比例 {{ rate === "" ? "继承" : rate + " BPS" }}，基数{{
            basis === "gross"
              ? "原订单金额"
              : basis === "profit"
                ? "配置估计利润（非实际账单）"
                : "原订单平台手续费"
          }}，发奖时机{{
            mode === "daily" ? "次日北京时间待专用调度器发放" : "即时发奖"
          }}，按分四舍五入且奖励不超过原订单平台手续费，不使用其它订单资金。任意成功退款会撤销整笔原奖励。
        </p>
        <button
          :disabled="busy"
          @click="
            confirming = false;
            frozen = undefined;
          "
        >
          返回编辑</button
        ><button :disabled="busy" @click="save">确认保存并授权</button>
      </div>
    </section>
  </section>
  <MembershipReferralPolicy
    v-if="current?.membership_referral_policy_available === true"
    :client="client"
    :admin-class="adminClass"
  />
</template>
