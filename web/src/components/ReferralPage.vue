<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount, computed } from "vue";
import { ApiClient } from "../api";
import { validPromotionToken, referralAmount, referralID } from "../referral";
import ReferralDailyQueue from "./ReferralDailyQueue.vue";
import ReferralCurrentPolicyCard from "./ReferralCurrentPolicyCard.vue";
const props = defineProps<{ client: ApiClient }>();
interface Token {
  id: string;
  merchant_id: string;
  version: string;
  enabled: boolean;
  masked: string;
  expires_at?: string;
  token?: string;
}
interface Invitee {
  id: string;
  invited_id: string;
  token_version: string;
  created_at: string;
}
interface Reward {
  id: string;
  trade_no: string;
  invited_id: string;
  amount: string;
  state: string;
  created_at: string;
}
const summary = ref<Record<string, string> | null>(null),
  token = ref<Token | null>(null),
  invitees = ref<Invitee[]>([]),
  rewards = ref<Reward[]>([]);
const loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  action = ref(""),
  uncertain = ref(false),
  issuedURL = ref(""),
  inviteCursor = ref("0"),
  rewardCursor = ref("0"),
  inviteNext = ref(""),
  rewardNext = ref("");
const dailyAvailable = ref(false);
let controller: AbortController | undefined;
let active = true;
let epoch = 0;
function reset() {
  epoch++;
  controller?.abort();
  controller = undefined;
  dailyAvailable.value = false;
  summary.value = null;
  token.value = null;
  invitees.value = [];
  rewards.value = [];
  loading.value = false;
  busy.value = false;
  error.value = "";
  notice.value = "";
  action.value = "";
  uncertain.value = false;
  issuedURL.value = "";
  inviteCursor.value = "0";
  rewardCursor.value = "0";
  inviteNext.value = "";
  rewardNext.value = "";
}
watch(() => [props.client, props.client.area, props.client.csrfToken], reset, {
  flush: "sync",
});
const tokenLive = computed(
  () =>
    !!token.value?.enabled &&
    !!token.value.expires_at &&
    Date.parse(token.value.expires_at) > Date.now(),
);
const summaryLabels: Record<string, string> = {
  invited_count: "已邀请商户",
  issued_amount: "累计已发",
  voided_amount: "累计已撤",
  recovery_pending_amount: "退款暂存",
  today_issued: "今日已发",
  yesterday_issued: "昨日已发",
};
const states: Record<string, string> = {
  active: "已发未撤",
  recovery_pending: "退款暂存",
  voided: "已撤销",
};
async function load() {
  if (!active || busy.value) return;
  controller?.abort();
  const c = new AbortController();
  const currentEpoch = epoch,
    client = props.client;
  controller = c;
  loading.value = true;
  issuedURL.value = "";
  action.value = "";
  error.value = "";
  summary.value = null;
  token.value = null;
  invitees.value = [];
  rewards.value = [];
  dailyAvailable.value = false;
  if (props.client.area !== "merchant") {
    error.value = "仅商户本人可查看邀请资料";
    loading.value = false;
    return;
  }
  try {
    const [overview, current, relations, awards] = await Promise.all([
      client.request<Record<string, unknown>>("/referral/overview", {
        signal: c.signal,
      }),
      client.request<Token>("/referral/token", { signal: c.signal }),
      client.request<{ items: Invitee[]; next_cursor: string }>(
        "/referral/invitees?" +
          new URLSearchParams({ after_id: inviteCursor.value, limit: "20" }),
        { signal: c.signal },
      ),
      client.request<{ items: Reward[]; next_cursor: string }>(
        "/referral/rewards?" +
          new URLSearchParams({ after_id: rewardCursor.value, limit: "20" }),
        { signal: c.signal },
      ),
    ]);
    if (!active || c.signal.aborted || currentEpoch !== epoch) return;
    if (
      Object.keys(summaryLabels).some(
        (key) =>
          typeof overview[key] !== "string" ||
          !/^(0|[1-9]\d*)$/.test(overview[key] as string),
      ) ||
      !current ||
      !/^(0|[1-9]\d*)$/.test(current.version) ||
      typeof current.enabled !== "boolean" ||
      !Array.isArray(relations.items) ||
      !Array.isArray(awards.items) ||
      awards.items.some(
        (row) => !states[row.state] || !/^[1-9]\d*$/.test(row.amount),
      )
    )
      throw new Error("邀请资料响应不完整，请重新读取");
    summary.value = Object.fromEntries(
      Object.keys(summaryLabels).map((key) => [key, overview[key] as string]),
    );
    token.value = current;
    invitees.value = relations.items;
    rewards.value = awards.items;
    inviteNext.value = relations.next_cursor;
    rewardNext.value = awards.next_cursor;
    if (
      overview.daily_queue_available === true &&
      !referralID(current.merchant_id)
    )
      throw Error("本人商户归属未确认，请重新读取");
    dailyAvailable.value = overview.daily_queue_available === true;
    uncertain.value = false;
  } catch (e) {
    if (active && currentEpoch === epoch && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "邀请资料读取失败";
  } finally {
    if (active && currentEpoch === epoch && controller === c)
      loading.value = false;
  }
}
async function changeToken() {
  if (
    busy.value ||
    uncertain.value ||
    !token.value ||
    !["rotate", "revoke"].includes(action.value)
  )
    return;
  const operation = action.value;
  const currentEpoch = epoch,
    client = props.client,
    c = controller;
  action.value = "";
  busy.value = true;
  issuedURL.value = "";
  error.value = "";
  notice.value = "";
  try {
    const out = await client.request<Token>("/referral/token/" + operation, {
      method: "POST",
      body: { expected: token.value.version },
      signal: c?.signal,
    });
    if (!active || c?.signal.aborted || currentEpoch !== epoch) return;
    if (
      !out ||
      !/^[1-9]\d*$/.test(out.version) ||
      typeof out.enabled !== "boolean"
    )
      throw new Error("令牌变更结果未确认");
    token.value = { ...out, token: undefined };
    if (operation === "rotate") {
      if (!out.token || !validPromotionToken(out.token))
        throw new Error("完整令牌未确认，请重新读取原版本");
      const url = new URL("/merchant/dashboard", window.location.origin);
      url.hash = new URLSearchParams({ promotion_token: out.token }).toString();
      issuedURL.value = url.toString();
      notice.value =
        "推广链接仅本次显示；默认90天有效。旧链接已失效，已注册关系保留。";
    } else
      notice.value = "推广令牌已撤销；旧链接不能再建立新关系，已有关系仍保留。";
  } catch (e) {
    if (!active || c?.signal.aborted || currentEpoch !== epoch) return;
    uncertain.value = true;
    error.value = e instanceof Error ? e.message : "令牌变更结果未知";
    notice.value =
      "请先重新读取当前版本；不会自动重试，也无法找回已发行的完整令牌。";
  } finally {
    if (active && currentEpoch === epoch) busy.value = false;
  }
}
async function copy() {
  try {
    await navigator.clipboard.writeText(issuedURL.value);
    notice.value = "推广链接已复制。仅向您希望邀请的注册人分享。";
  } catch {
    error.value = "剪贴板不可用，可选择本次链接并手动复制。";
  }
}
onMounted(load);
onBeforeUnmount(() => {
  active = false;
  reset();
});
</script>
<template>
  <section class="panel referral" aria-label="邀请返现">
    <p>
      奖励来自原付款订单的已固定条款，奖励不超过原订单手续费；即时与按日奖励分别核对，任意成功退款将撤销整笔原奖励。历史发放、撤销和退款暂存流水不代表当前可提现余额。
    </p>
    <button :disabled="loading || busy" @click="load">重新读取邀请资料</button>
    <p v-if="loading" role="status">正在核对邀请资料…</p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <ReferralCurrentPolicyCard
      v-if="token && referralID(token.merchant_id)"
      :client="client"
      :merchant-i-d="token.merchant_id"
    />
    <dl v-if="summary" class="referral-stats">
      <div v-for="(label, key) in summaryLabels" :key="key">
        <dt>{{ label }}</dt>
        <dd>
          {{
            key === "invited_count"
              ? summary[key]
              : referralAmount(summary[key]!)
          }}{{ key === "invited_count" ? " 位" : " 元" }}
        </dd>
      </div>
    </dl>
    <section v-if="token" aria-label="推广令牌管理">
      <h3>推广链接</h3>
      <p>
        推广令牌用于新注册邀请关系，与注册准入邀请码独立。仅本人可发行、轮换或撤销；不能代他人绑定关系。
      </p>
      <p>
        当前版本 {{ token.version }}；{{
          token.enabled ? (tokenLive ? "有效" : "已过期") : "未启用"
        }}
        <code>{{ token.masked }}</code
        ><span v-if="token.expires_at"
          >，有效至 {{ new Date(token.expires_at).toLocaleString() }}</span
        >。
      </p>
      <button
        :disabled="busy || loading || uncertain"
        @click="
          action = 'rotate';
          issuedURL = '';
        "
      >
        发行或轮换推广令牌</button
      ><button
        v-if="token.enabled"
        :disabled="busy || loading || uncertain"
        @click="
          action = 'revoke';
          issuedURL = '';
        "
      >
        撤销推广令牌
      </button>
      <div v-if="action" class="alert">
        <p>
          {{
            action === "rotate"
              ? "新链接将作废所有旧链接。完整令牌仅本次返回，请确认后自行保管。"
              : "撤销后旧链接立即停止建立新关系，已绑定关系不变。"
          }}
        </p>
        <button :disabled="busy" @click="action = ''">取消</button
        ><button :disabled="busy" @click="changeToken">
          {{
            action === "rotate" ? "确认轮换并作废旧链接" : "确认撤销推广令牌"
          }}
        </button>
      </div>
      <div v-if="issuedURL">
        <label
          >本次推广链接<input
            type="password"
            readonly
            :value="issuedURL"
            autocomplete="off" /></label
        ><button @click="copy">复制本次推广链接</button
        ><button @click="issuedURL = ''">清除本次链接</button>
      </div>
    </section>
    <ReferralDailyQueue
      v-if="dailyAvailable && token"
      :client="client"
      :available="dailyAvailable"
      :promoter-id="token.merchant_id"
    />
    <section class="referral-sublist" aria-label="已邀请商户">
      <h3>已邀请商户</h3>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>关系ID</th>
              <th>商户ID</th>
              <th>注册时间</th>
              <th>令牌版本</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in invitees" :key="row.id">
              <td>{{ row.id }}</td>
              <td>{{ row.invited_id }}</td>
              <td>{{ row.created_at }}</td>
              <td>{{ row.token_version }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="!loading && !invitees.length">暂无邀请关系</p>
      <div class="pagination">
        <span>共 {{ invitees.length }} 条</span>
        <div>
          <button
            :disabled="loading || busy || inviteCursor === '0'"
            @click="
              inviteCursor = '0';
              load();
            "
          >
            关系首页</button
          ><button
            :disabled="loading || busy || !inviteNext"
            @click="
              inviteCursor = inviteNext;
              load();
            "
          >
            下一页关系
          </button>
        </div>
      </div>
    </section>
    <section class="referral-sublist" aria-label="邀请奖励流水">
      <h3>邀请奖励流水</h3>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>奖励ID</th>
              <th>原订单号</th>
              <th>受邀商户</th>
              <th>原发放金额（元）</th>
              <th>状态</th>
              <th>时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rewards" :key="row.id">
              <td>{{ row.id }}</td>
              <td>{{ row.trade_no }}</td>
              <td>{{ row.invited_id }}</td>
              <td>{{ referralAmount(row.amount) }}</td>
              <td>{{ states[row.state] }}</td>
              <td>{{ row.created_at }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="!loading && !rewards.length">暂无奖励流水</p>
      <div class="pagination">
        <span>共 {{ rewards.length }} 条</span>
        <div>
          <button
            :disabled="loading || busy || rewardCursor === '0'"
            @click="
              rewardCursor = '0';
              load();
            "
          >
            奖励首页</button
          ><button
            :disabled="loading || busy || !rewardNext"
            @click="
              rewardCursor = rewardNext;
              load();
            "
          >
            下一页奖励
          </button>
        </div>
      </div>
    </section>
  </section>
</template>
<style scoped>
.referral {
  min-width: 0;
}
.referral button {
  min-height: var(--control-h);
  margin: 0.3rem 0.5rem 0.3rem 0;
}
.referral input {
  display: block;
  max-width: 100%;
  width: 100%;
  min-height: var(--control-h);
}
.referral p {
  overflow-wrap: anywhere;
}
.referral-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: var(--space-4);
}
.referral-stats div {
  padding: var(--space-4);
  background: var(--bg-card);
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
}
.referral-stats dt {
  font-size: 13px;
  color: var(--ink-muted);
}
.referral-stats dd {
  margin: var(--space-2) 0 0;
  font-weight: 700;
  font-size: 20px;
  color: var(--ink);
}
.referral-sublist {
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  padding: var(--space-4);
  margin-top: var(--space-4);
  min-width: 0;
}
.referral-sublist > h3 {
  margin: 0 0 var(--space-3);
}
.referral table {
  min-width: 580px;
}
.table-scroll {
  overflow: auto;
  max-width: 100%;
}
</style>
