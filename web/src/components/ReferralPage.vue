<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount, computed } from "vue";
import { ApiClient } from "../api";
import { validPromotionToken, referralAmount, referralID } from "../referral";
import ReferralDailyQueue from "./ReferralDailyQueue.vue";
import ReferralCurrentPolicyCard from "./ReferralCurrentPolicyCard.vue";
import MerchantRecords from "./MerchantRecords.vue";
import KitStatusTag from "./KitStatusTag.vue";
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
      throw new Error("邀请资料响应不完整，请刷新页面后核对");
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
      throw Error("本人商户归属未确认，请刷新页面后核对");
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
        throw new Error("完整令牌未确认，请刷新页面后核对");
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
      "请刷新页面后核对；不会自动重试，也无法找回已发行的完整令牌。";
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
  <div class="list-page referral" aria-label="邀请返现">
    <div class="alert info referral-note">
      <span
        >奖励来自原付款订单的已固定条款，奖励不超过原订单手续费；即时与按日奖励分别核对，任意成功退款将撤销整笔原奖励。历史发放、撤销和退款暂存流水不代表当前可提现余额。</span
      >
    </div>
    <p v-if="loading" role="status" class="muted">正在核对邀请资料…</p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
    <dl v-if="summary" class="stat-grid referral-stats">
      <div v-for="(label, key) in summaryLabels" :key="key" class="panel stat">
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
    <div v-if="token" class="referral-settings">
      <section class="panel referral-token" aria-label="推广令牌管理">
        <header>
          <h2>推广链接</h2>
          <KitStatusTag
            :text="token.enabled ? (tokenLive ? '有效' : '已过期') : '未启用'"
            :tone="tokenLive ? 'success' : 'neutral'"
          />
        </header>
        <p>
          推广令牌用于新注册邀请关系，与注册准入邀请码独立。仅本人可发行、轮换或撤销；不能代他人绑定关系。
        </p>
        <dl class="referral-token-facts">
          <div>
            <dt>当前版本</dt>
            <dd>{{ token.version }}</dd>
          </div>
          <div v-if="token.masked">
            <dt>令牌</dt>
            <dd>
              <code>{{ token.masked }}</code>
            </dd>
          </div>
          <div v-if="token.expires_at">
            <dt>有效期至</dt>
            <dd>{{ new Date(token.expires_at).toLocaleString() }}</dd>
          </div>
        </dl>
        <div class="referral-actions">
          <button
            type="button"
            :disabled="busy || loading || uncertain"
            @click="
              action = 'rotate';
              issuedURL = '';
            "
          >
            发行或轮换推广令牌</button
          ><button
            v-if="token.enabled"
            type="button"
            :disabled="busy || loading || uncertain"
            @click="
              action = 'revoke';
              issuedURL = '';
            "
          >
            撤销推广令牌
          </button>
        </div>
        <div v-if="action" class="alert referral-confirm">
          <p>
            {{
              action === "rotate"
                ? "新链接将作废所有旧链接。完整令牌仅本次返回，请确认后自行保管。"
                : "撤销后旧链接立即停止建立新关系，已绑定关系不变。"
            }}
          </p>
          <div class="referral-actions">
            <button type="button" :disabled="busy" @click="action = ''">
              取消</button
            ><button
              type="button"
              class="primary"
              :disabled="busy"
              @click="changeToken"
            >
              {{
                action === "rotate"
                  ? "确认轮换并作废旧链接"
                  : "确认撤销推广令牌"
              }}
            </button>
          </div>
        </div>
        <div v-if="issuedURL" class="referral-secret">
          <label
            >本次推广链接<input
              type="password"
              readonly
              :value="issuedURL"
              autocomplete="off"
          /></label>
          <div class="referral-actions">
            <button type="button" @click="copy">复制本次推广链接</button>
            <button type="button" @click="issuedURL = ''">清除本次链接</button>
          </div>
        </div>
      </section>
      <ReferralCurrentPolicyCard
        v-if="referralID(token.merchant_id)"
        :client="client"
        :merchant-i-d="token.merchant_id"
      />
    </div>
    <ReferralDailyQueue
      v-if="dailyAvailable && token"
      :client="client"
      :available="dailyAvailable"
      :promoter-id="token.merchant_id"
    />
    <section class="panel records referral-sublist" aria-label="已邀请商户">
      <div class="table-tools">
        <h3 class="referral-sublist-title">已邀请商户</h3>
      </div>
      <MerchantRecords v-if="invitees.length" class="table-scroll">
        <table role="table">
          <thead role="rowgroup">
            <tr role="row">
              <th>关系ID</th>
              <th>商户ID</th>
              <th>注册时间</th>
              <th>令牌版本</th>
            </tr>
          </thead>
          <tbody role="rowgroup">
            <tr v-for="row in invitees" :key="row.id" role="row">
              <td data-cell-label="关系 ID" role="cell">{{ row.id }}</td>
              <td data-cell-label="商户 ID" role="cell">
                {{ row.invited_id }}
              </td>
              <td data-cell-label="注册时间" role="cell">
                {{ row.created_at }}
              </td>
              <td data-cell-label="令牌版本" role="cell">
                {{ row.token_version }}
              </td>
            </tr>
          </tbody>
        </table>
      </MerchantRecords>
      <p v-else-if="!loading && !error" class="referral-empty">暂无邀请关系</p>
      <div v-if="invitees.length || inviteCursor !== '0'" class="pa-pager">
        <span class="pa-pager-total">本页 {{ invitees.length }} 条</span>
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
    <section class="panel records referral-sublist" aria-label="邀请奖励流水">
      <div class="table-tools">
        <h3 class="referral-sublist-title">邀请奖励流水</h3>
      </div>
      <MerchantRecords v-if="rewards.length" class="table-scroll">
        <table role="table">
          <thead role="rowgroup">
            <tr role="row">
              <th>奖励ID</th>
              <th>原订单号</th>
              <th>受邀商户</th>
              <th>原发放金额（元）</th>
              <th>状态</th>
              <th>时间</th>
            </tr>
          </thead>
          <tbody role="rowgroup">
            <tr v-for="row in rewards" :key="row.id" role="row">
              <td data-cell-label="奖励 ID" role="cell">{{ row.id }}</td>
              <td
                class="pa-order-no"
                data-cell-label="原订单号"
                data-cell-wide
                role="cell"
              >
                {{ row.trade_no }}
              </td>
              <td data-cell-label="受邀商户" role="cell">
                {{ row.invited_id }}
              </td>
              <td data-cell-label="原发放金额（元）" role="cell">
                {{ referralAmount(row.amount) }}
              </td>
              <td data-cell-label="状态" role="cell">
                <KitStatusTag
                  :text="states[row.state]"
                  :tone="
                    row.state === 'active'
                      ? 'success'
                      : row.state === 'recovery_pending'
                        ? 'warning'
                        : 'neutral'
                  "
                />
              </td>
              <td data-cell-label="时间" role="cell">{{ row.created_at }}</td>
            </tr>
          </tbody>
        </table>
      </MerchantRecords>
      <p v-else-if="!loading && !error" class="referral-empty">暂无奖励流水</p>
      <div v-if="rewards.length || rewardCursor !== '0'" class="pa-pager">
        <span class="pa-pager-total">本页 {{ rewards.length }} 条</span>
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
  </div>
</template>
<style scoped>
.referral {
  min-width: 0;
}
.referral-settings {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 340px), 1fr));
  align-items: start;
  gap: var(--space-4);
}
.referral-settings > .current-policy,
.referral > .referral-stats {
  margin: 0;
}
.referral-token {
  min-width: 0;
}
.referral-actions,
.referral-token > header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
}
.referral-token > header {
  justify-content: space-between;
}
.referral-token h2 {
  margin: 0;
  font-size: 1rem;
}
.referral-token-facts {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 115px), 1fr));
  gap: var(--space-3);
  margin-block: var(--space-4);
  padding: var(--space-3);
  border-radius: var(--radius-md);
  background: var(--bg-raised);
}
.referral-token-facts dt {
  color: var(--ink-muted);
  font-size: 0.8rem;
}
.referral-token-facts dd {
  margin: var(--space-1) 0 0;
  overflow-wrap: anywhere;
}
.referral-secret {
  margin-top: var(--space-4);
  border-top: 1px solid var(--line);
  padding-top: var(--space-3);
}
.referral-secret input {
  display: block;
  width: 100%;
  min-width: 0;
  margin-bottom: var(--space-2);
}
.referral-confirm {
  margin-top: var(--space-3);
}
.referral-confirm p {
  margin-top: 0;
}
.referral-token p {
  overflow-wrap: anywhere;
}
.referral-stats {
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
}
.referral-stats .stat {
  min-height: 0;
}
.referral-stats dt {
  font-size: 13px;
  color: var(--ink-muted);
  padding-inline-end: 0;
}
.referral-stats dd {
  margin: var(--space-2) 0 0;
  font-weight: 700;
  font-size: 24px;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
}
.referral-sublist {
  min-width: 0;
}
.referral-sublist-title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  line-height: 32px;
}
.referral-empty {
  margin: 0;
  padding: var(--space-3) var(--space-5);
  color: var(--ink-muted);
}
@media (max-width: 600px) {
  .referral-actions > button {
    min-height: 44px;
  }
}
.table-scroll {
  overflow: auto;
  max-width: 100%;
}
</style>
