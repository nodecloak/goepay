<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from "vue";
import { ApiClient, ApiError } from "../api";
import {
  autoAuthorizationBody,
  readAutoAuthorization,
  type AutoAuthorization,
  type AutoPlan,
} from "../settlement-auto";
import type { AuthorizationDraft } from "../settlement-plans";
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
  plan: AutoPlan;
}>();
const current = ref<AutoAuthorization | null>(null),
  channels = ref<Record<string, string>>({}),
  ack = ref(false),
  loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref(""),
  reviewing = ref(false);
const empty = (): AuthorizationDraft => ({
  valid_from: "",
  expires_at: "",
  max_item_gross: "",
  max_run_gross: "",
  max_day_gross: "",
  max_day_items: "",
});
const draft = ref(empty());
const permitted = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.ownSession,
);
let active = true,
  epoch = 0,
  controller: AbortController | undefined,
  frozen:
    | {
        body: Record<string, unknown>;
        target: string;
        epoch: number;
        revoke: boolean;
      }
    | undefined;
const target = () => `/settlement-plans/${props.plan.id}`;
function reset() {
  epoch++;
  controller?.abort();
  controller = undefined;
  current.value = null;
  channels.value = {};
  ack.value = false;
  draft.value = empty();
  loading.value = false;
  busy.value = false;
  uncertain.value = false;
  error.value = "";
  notice.value = "";
  reviewing.value = false;
  frozen = undefined;
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.adminClass,
    props.ownSession,
    props.plan.id,
    props.plan.revision,
    props.plan.last_authorization_id,
    props.plan.merchant_ids.join(","),
  ],
  reset,
  { flush: "sync" },
);
async function load() {
  if (!active || !permitted.value || busy.value) return;
  controller?.abort();
  const c = new AbortController(),
    stamp = epoch,
    client = props.client,
    path = target(),
    plan: AutoPlan = {
      id: props.plan.id,
      revision: props.plan.revision,
      last_authorization_id: props.plan.last_authorization_id,
      merchant_ids: [...props.plan.merchant_ids],
    };
  controller = c;
  loading.value = true;
  reviewing.value = false;
  frozen = undefined;
  error.value = "";
  try {
    const raw = await client.request(path + "/auto-authorization", {
      signal: c.signal,
    });
    if (!active || c.signal.aborted || stamp !== epoch || !permitted.value)
      return;
    const value = readAutoAuthorization(raw, plan);
    current.value = value;
    channels.value = {};
    ack.value = false;
    for (const mid of plan.merchant_ids) {
      const allowed = value.wallet_channels.filter((v) =>
        v.merchant_ids.includes(mid),
      );
      if (allowed.length === 1) channels.value[mid] = allowed[0]!.id;
    }
    draft.value =
      value.id === "0"
        ? empty()
        : {
            valid_from: value.valid_from!,
            expires_at: value.expires_at!,
            max_item_gross: value.max_item_gross!,
            max_run_gross: value.max_run_gross!,
            max_day_gross: value.max_day_gross!,
            max_day_items: value.max_day_items!,
          };
    uncertain.value = false;
  } catch (e) {
    if (active && stamp === epoch && !c.signal.aborted) {
      current.value = null;
      error.value = e instanceof Error ? e.message : "自动出款授权读取失败";
    }
  } finally {
    if (active && stamp === epoch && controller === c) loading.value = false;
  }
}
function review(revoke = false) {
  if (
    !active ||
    !permitted.value ||
    busy.value ||
    loading.value ||
    !current.value ||
    (!revoke && uncertain.value)
  )
    return;
  try {
    if (revoke && current.value.id === "0")
      throw new Error("没有可撤销的原自动授权");
    const body = revoke
      ? {}
      : autoAuthorizationBody(
          props.plan,
          current.value,
          draft.value,
          channels.value,
          ack.value,
        );
    const copy = JSON.parse(JSON.stringify(body)) as Record<string, unknown>;
    if (Array.isArray(copy.channels)) {
      for (const c of copy.channels) Object.freeze(c);
      Object.freeze(copy.channels);
    }
    frozen = {
      body: Object.freeze(copy),
      target:
        target() +
        "/auto-authorizations" +
        (revoke ? `/${current.value.id}/revoke` : ""),
      epoch,
      revoke,
    };
    reviewing.value = true;
    error.value = "";
  } catch (e) {
    error.value = e instanceof Error ? e.message : "请核对自动出款授权";
  }
}
async function submit() {
  if (
    !active ||
    !permitted.value ||
    busy.value ||
    !frozen ||
    !reviewing.value ||
    frozen.epoch !== epoch ||
    (!frozen.revoke && uncertain.value)
  )
    return;
  const proposal = frozen,
    stamp = epoch,
    client = props.client,
    c = new AbortController();
  controller?.abort();
  controller = c;
  frozen = undefined;
  reviewing.value = false;
  busy.value = true;
  error.value = "";
  try {
    await client.request(proposal.target, {
      method: "POST",
      body: proposal.body,
      signal: c.signal,
    });
    if (!active || c.signal.aborted || stamp !== epoch || !permitted.value)
      return;
    notice.value = proposal.revoke
      ? "已撤销原自动出款授权；已有未知交易继续按原号查询。"
      : "已提交独立自动出款授权；执行器是否运行由独立部署配置决定。";
    uncertain.value = true;
    busy.value = false;
    await load();
  } catch (e) {
    if (active && stamp === epoch && !c.signal.aborted) {
      error.value = e instanceof Error ? e.message : "提交结果不确定";
      uncertain.value =
        !(e instanceof ApiError) ||
        e.status >= 500 ||
        e.status === 408 ||
        e.status === 409;
    }
  } finally {
    if (active && stamp === epoch && controller === c) busy.value = false;
  }
}
onBeforeUnmount(() => {
  active = false;
  reset();
});
</script>
<template>
  <section v-if="permitted" class="case-panel" aria-label="独立自动出款授权">
    <h3>自动付款 · 独立授权</h3>
    <p>
      原计划授权仅生成申请。自动付款另行绑定原生成授权、收款配置与每商户通道；本页面不会启动执行器或发送付款。
    </p>
    <button :disabled="loading || busy" @click="load">读取自动付款授权</button>
    <p v-if="loading" role="status">正在读取原自动出款授权…</p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <p v-if="uncertain" role="status">
      请先重新读取授权状态再进行新的授权；请勿重复提交。
    </p>
    <template v-if="current">
      <p>
        原自动授权 {{ current.id }} ·
        {{
          current.id === "0"
            ? "尚未签发"
            : current.revoked
              ? "已撤销"
              : "已签发（执行前仍须核验）"
        }}；原生成授权 {{ plan.last_authorization_id }}。上限单位为分。
      </p>
      <p v-if="!current.wallet_channels.length">
        没有同时具备明确产品、付款账户和商户准入的钱包通道。
      </p>
      <fieldset
        :disabled="
          busy ||
          loading ||
          reviewing ||
          uncertain ||
          !current.available ||
          !current.wallet_channels.length ||
          plan.last_authorization_id === '0'
        "
      >
        <label v-for="mid in plan.merchant_ids" :key="mid"
          >自动出款商户 {{ mid }} 通道<select v-model="channels[mid]">
            <option value="">请选择已准入通道</option>
            <option
              v-for="c in current.wallet_channels.filter((c) =>
                c.merchant_ids.includes(mid),
              )"
              :key="c.id"
              :value="c.id"
            >
              通道{{ c.id }} · {{ c.provider }} · {{ c.product }} ·
              {{ c.payer_masked }} · {{ c.app_id }}
            </option>
          </select></label
        >
        <label
          >自动出款开始 RFC3339<input
            v-model="draft.valid_from"
            placeholder="2026-10-24T09:00:00+08:00"
        /></label>
        <label>自动出款截止 RFC3339<input v-model="draft.expires_at" /></label>
        <label
          >自动出款单项毛额上限（分）<input
            v-model="draft.max_item_gross"
            inputmode="numeric"
        /></label>
        <label
          >自动出款每次毛额上限（分）<input
            v-model="draft.max_run_gross"
            inputmode="numeric"
        /></label>
        <label
          >自动出款每日毛额上限（分）<input
            v-model="draft.max_day_gross"
            inputmode="numeric"
        /></label>
        <label
          >自动出款每日条数上限<input
            v-model="draft.max_day_items"
            inputmode="numeric"
        /></label>
        <p>
          毛额＝原净额＋平台手续费。重新授权不重置本计划已经消费的当日、原运行额度；最长31天，每日最多100条。
        </p>
        <label
          ><input
            v-model="ack"
            type="checkbox"
          />我已确认：支付机构账户另收费不包含在本授权限额内，不保证机构成本封顶。</label
        >
        <button @click="review()">核对独立自动出款授权</button>
      </fieldset>
      <button
        v-if="current.id !== '0' && !current.revoked"
        :disabled="busy || loading"
        @click="review(true)"
      >
        撤销原自动出款授权
      </button>
    </template>
    <section v-if="reviewing" aria-label="独立自动出款操作确认" role="region">
      <h4>
        {{ frozen?.revoke ? "确认撤销原自动出款授权" : "确认有限自动付款范围" }}
      </h4>
      <pre>{{ JSON.stringify(frozen?.body, null, 2) }}</pre>
      <p>
        仅限原生成授权产生的原申请；首次发送前停止，未知结果只查询原交易号。签发必须再次验证本人资金密码。
      </p>
      <button :disabled="busy" @click="submit">
        {{ frozen?.revoke ? "确认撤销自动付款" : "确认授权并验证本人资金密码" }}
      </button>
      <button
        :disabled="busy"
        @click="
          reviewing = false;
          frozen = undefined;
        "
      >
        取消自动出款核对
      </button>
    </section>
  </section>
</template>
