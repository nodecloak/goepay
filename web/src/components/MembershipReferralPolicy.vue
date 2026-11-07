<script setup lang="ts">
import { computed, ref, watch, onMounted, onBeforeUnmount } from "vue";
import { ApiClient } from "../api";
import {
  readMembershipReferralPolicy,
  membershipReferralPolicyBody,
  type MembershipReferralPolicyView,
} from "../membership-referral";
const props = defineProps<{ client: ApiClient; adminClass?: string }>();
const scope = ref("global"),
  group = ref(""),
  current = ref<MembershipReferralPolicyView | null>(null),
  enabled = ref("disabled"),
  rate = ref("0"),
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
  "/membership-referral-policies/" +
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
    error.value = "仅超级管理员可配置会员奖励政策";
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
    const out = await client.request<MembershipReferralPolicyView>(path, {
      signal: c.signal,
    });
    if (
      !active ||
      c.signal.aborted ||
      currentEpoch !== epoch ||
      !permitted.value
    )
      return;
    current.value = readMembershipReferralPolicy(
      out,
      expectedScope,
      expectedID,
    );
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
        membershipReferralPolicyBody(current.value, enabled.value, rate.value),
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
    notice.value = "会员奖励政策已保存，仅影响之后新的余额会员购买。";
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
  <section class="panel console-policy-card" aria-label="会员推广奖励政策">
    <h2>会员推广奖励政策</h2>
    <p>
      独立配置余额购买会员的推广奖励，默认关闭。预算仅来自该次实际会员购买收入；不使用普通订单手续费或其它购买资金。比例0–10000
      BPS（100 BPS=1%），按分四舍五入。旧购买不追发。
    </p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <form class="console-config-toolbar" @submit.prevent="load">
      <label
        >政策范围<select v-model="scope" :disabled="busy">
          <option value="global">全局</option>
          <option value="group">用户组</option>
        </select></label
      >
      <label v-if="scope === 'group'"
        >用户组 ID<input
          v-model="group"
          inputmode="numeric"
          maxlength="19"
          :disabled="busy"
      /></label>
      <button :disabled="busy || loading || !permitted">
        读取会员奖励政策
      </button>
    </form>
    <section v-if="current" class="console-config-fields">
      <p>
        当前修订 {{ current.revision }}。覆盖顺序：全局 → 购买前付款商户有效组 →
        推广商户有效组；空项继承，关闭及比例0为明确覆盖。
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
      >
      <label
        >会员奖励比例 BPS<input
          v-model="rate"
          inputmode="numeric"
          maxlength="5"
          :placeholder="scope === 'group' ? '留空继承' : '0–10000'"
          :disabled="busy || confirming || uncertain"
      /></label>
      <p class="muted">
        原购买价格、邀请关系、有效用户组和政策版本在购买时固定；余额扣款、会员变更和一次奖励在同一事务提交。外部支付购买暂未开放。
      </p>
      <button
        :disabled="busy || loading || confirming || uncertain"
        @click="review"
      >
        核对会员奖励政策
      </button>
      <div v-if="confirming" class="alert">
        <p>
          确认{{
            scope === "global" ? "全局" : "用户组 " + group
          }}会员奖励政策修订 {{ current.revision }}，启用项{{
            enabled === "inherit"
              ? "继承"
              : enabled === "enabled"
                ? "启用"
                : "关闭"
          }}，比例
          {{
            rate === "" ? "继承" : rate + " BPS"
          }}。仅该次真实余额购买收入支付奖励，新购买生效，旧购买不追发。
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
</template>
