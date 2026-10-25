<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
import { ApiClient, validAmount, displayValue } from "../api";
import type { Area } from "../menu";
import {
  safePaymentUrl,
  browserRechargeMethods,
  type CommerceKind,
} from "../commerce";
import DataTable from "./DataTable.vue";
const props = defineProps<{
  client: ApiClient;
  kind: CommerceKind;
  area: Area;
}>();
const items = ref<Record<string, unknown>[]>([]),
  groups = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  page = ref(1),
  loading = ref(false),
  busy = ref(false),
  dialog = ref(false),
  error = ref(""),
  notice = ref(""),
  receipt = ref<Record<string, unknown> | null>(null),
  balances = ref<Record<string, unknown>>({});
const review = ref<{
  row: Record<string, unknown>;
  action: "approve" | "reject";
} | null>(null);
async function decide() {
  if (!review.value) return;
  busy.value = true;
  error.value = "";
  try {
    await props.client.request(
      `/workflows/deposits/${encodeURIComponent(String(review.value.row.id))}/${review.value.action}`,
      { method: "POST", body: {} },
    );
    notice.value = "保证金申请已处理，请核对资金明细";
    review.value = null;
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "审批失败";
  } finally {
    busy.value = false;
  }
}
let controller: AbortController | undefined;
const form = ref({
  merchant_id: "",
  group_id: "",
  amount: "",
  external_no: "",
  action: "hold",
  type: "alipay",
  method: "page",
});
const title = computed(
  () =>
    ({
      memberships: "购买会员",
      deposits: "保证金操作",
      recharges: "余额充值",
    })[props.kind],
);
const columns = computed(() => {
  const known: Record<string, string> = {
    id: "编号",
    merchant_id: "商户 ID",
    external_no: "业务请求号",
    status: "状态",
    created_at: "创建时间",
    amount: "金额（元）",
    amount_cents: "金额（分）",
    price: "会员价格（元）",
    price_cents: "会员价格（分）",
    group_name: "用户组",
    expires_at: "有效期至",
    group_id: "用户组 ID",
    action: "操作类型",
    trade_no: "充值订单号",
    type: "支付类型",
    paid_at: "支付时间",
  };
  return [...new Set(items.value.flatMap(Object.keys))].map((key) => ({
    key,
    label: known[key] || key,
  }));
});
async function load() {
  controller?.abort();
  controller = new AbortController();
  const c = controller;
  loading.value = true;
  error.value = "";
  try {
    const data = await props.client.request<{
      items: Record<string, unknown>[];
      total: number;
      deposit_money?: string;
      pending_release_money?: string;
    }>(`/workflows/${props.kind}?page=${page.value}&limit=20`, {
      signal: c.signal,
    });
    if (!c.signal.aborted) {
      items.value = data.items;
      total.value = data.total;
      balances.value = data;
    }
    if (props.kind === "memberships") {
      const data = await props.client.request<{
        items: Record<string, unknown>[];
      }>("/resources/groups?limit=100", { signal: c.signal });
      groups.value = data.items.filter((x) => x.enabled !== false);
    }
  } catch (e) {
    if (!c.signal.aborted)
      error.value = e instanceof Error ? e.message : "业务记录读取失败";
  } finally {
    if (!c.signal.aborted) loading.value = false;
  }
}
function open() {
  form.value = {
    merchant_id: "",
    group_id: "",
    amount: "",
    external_no: crypto.randomUUID(),
    action: "hold",
    type: "alipay",
    method: "page",
  };
  error.value = "";
  dialog.value = true;
  receipt.value = null;
}
async function submit() {
  if (
    props.kind === "recharges" &&
    !browserRechargeMethods(form.value.type).includes(form.value.method)
  ) {
    error.value = "当前浏览器充值不支持该支付场景，请重新选择";
    return;
  }
  if (props.kind !== "memberships" && !validAmount(form.value.amount)) {
    error.value = "请输入大于 0 且最多两位小数的金额";
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    const body: Record<string, string> = {
      external_no: form.value.external_no,
    };
    if (props.area === "admin") body.merchant_id = form.value.merchant_id;
    if (props.kind === "memberships") body.group_id = form.value.group_id;
    else body.amount = form.value.amount;
    if (props.kind === "deposits") body.action = form.value.action;
    if (props.kind === "recharges") {
      body.type = form.value.type;
      body.method = form.value.method;
    }
    const data = await props.client.request<Record<string, unknown>>(
      `/workflows/${props.kind}`,
      { method: "POST", body },
    );
    receipt.value = data;
    dialog.value = false;
    notice.value =
      props.kind === "memberships"
        ? "会员购买已完成，请核对购买记录与账户资料"
        : props.kind === "deposits"
          ? form.value.action === "release"
            ? "保证金释放申请已提交，须经管理员审批"
            : "保证金已从可用余额转入保证金账户"
          : "充值订单已创建。仅在支付成功并核实回调后才会入账";
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "业务处理失败";
  } finally {
    busy.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => controller?.abort());
</script>
<template>
  <div>
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
    <section
      v-if="
        kind === 'deposits' &&
        area === 'merchant' &&
        balances.deposit_money !== undefined
      "
      class="panel account-section"
    >
      <dl class="record-detail">
        <dt>保证金余额（元）</dt>
        <dd>{{ displayValue(balances.deposit_money) }}</dd>
        <dt>释放申请中（元）</dt>
        <dd>{{ displayValue(balances.pending_release_money) }}</dd>
      </dl>
    </section>
    <section
      v-if="receipt && kind === 'recharges'"
      class="panel account-section"
    >
      <h2>充值订单</h2>
      <p>
        订单号 {{ displayValue(receipt.trade_no) }} · 状态
        {{ displayValue(receipt.status) }}
      </p>
      <a
        v-if="safePaymentUrl(receipt.pay_url)"
        class="payment-link"
        :href="safePaymentUrl(receipt.pay_url)"
        target="_blank"
        rel="noopener noreferrer"
        >前往收银台</a
      >
      <p v-else class="muted">
        支付链接尚未就绪，请刷新记录核实订单，或联系管理员。
      </p>
    </section>
    <section class="panel records">
      <div class="toolbar">
        <p class="muted">
          {{
            kind === "memberships"
              ? "会员购买会按已配置的价格扣减账户余额，价格以服务端确认为准。"
              : kind === "deposits"
                ? "保证金冻结与释放申请均记录资金流水。释放须经管理员核准。"
                : "充值记录反映真实订单状态，创建订单不会直接增加余额。"
          }}
        </p>
        <div class="toolbar-actions">
          <button :disabled="loading" @click="load">刷新</button
          ><button
            class="primary"
            :disabled="busy || (kind === 'memberships' && !groups.length)"
            @click="open"
          >
            {{ title }}
          </button>
        </div>
      </div>
      <div v-if="loading" class="loading-row" role="status">正在读取记录…</div>
      <DataTable v-else :columns="columns" :items="items"
        ><template
          v-if="area === 'admin' && kind === 'deposits'"
          #actions="{ row }"
          ><div
            v-if="row.action === 'release' && row.status === 'pending'"
            class="row-actions"
          >
            <button
              class="link-button"
              @click="review = { row, action: 'approve' }"
            >
              核准释放</button
            ><button
              class="link-button danger-text"
              @click="review = { row, action: 'reject' }"
            >
              拒绝释放
            </button>
          </div>
          <span v-else class="muted">无需审批</span></template
        ></DataTable
      >
      <div class="pagination">
        <span>共 {{ total }} 条记录</span>
        <div>
          <button
            :disabled="page <= 1 || loading"
            @click="
              page--;
              load();
            "
          >
            上一页</button
          ><span>第 {{ page }} 页</span
          ><button
            :disabled="page * 20 >= total || loading"
            @click="
              page++;
              load();
            "
          >
            下一页
          </button>
        </div>
      </div>
    </section>
    <div v-if="review" class="modal-backdrop">
      <section
        class="modal panel"
        role="dialog"
        aria-modal="true"
        aria-label="保证金审批确认"
      >
        <h2>保证金审批确认</h2>
        <p>
          申请 {{ review.row.id }} · 金额 {{ review.row.amount }} 元。{{
            review.action === "approve"
              ? "核准后将从保证金账户释放至可用余额。"
              : "拒绝后会保留在保证金账户。"
          }}
        </p>
        <div v-if="error" class="alert error" role="alert">{{ error }}</div>
        <div class="modal-actions">
          <button :disabled="busy" @click="review = null">取消</button
          ><button class="primary" :disabled="busy" @click="decide">
            {{
              busy
                ? "正在处理…"
                : "确认" + (review.action === "approve" ? "核准" : "拒绝")
            }}
          </button>
        </div>
      </section>
    </div>
    <div v-if="dialog" class="modal-backdrop">
      <section
        class="modal panel"
        role="dialog"
        aria-modal="true"
        :aria-label="title"
      >
        <h2>{{ title }}</h2>
        <div v-if="error" class="alert error" role="alert">{{ error }}</div>
        <form @submit.prevent="submit">
          <label v-if="area === 'admin'"
            >商户 ID<input
              v-model="form.merchant_id"
              required
              inputmode="numeric" /></label
          ><label v-if="kind === 'memberships'"
            >购买用户组<select v-model="form.group_id" required>
              <option value="">请选择</option>
              <option
                v-for="group in groups"
                :key="String(group.id)"
                :value="String(group.id)"
              >
                {{ group.name }} · {{ group.price_cents }} 分
              </option>
            </select></label
          ><template v-if="kind === 'deposits'"
            ><label
              >保证金操作<select v-model="form.action">
                <option value="hold">转入保证金</option>
                <option value="release">申请释放保证金</option>
              </select></label
            ></template
          ><label v-if="kind !== 'memberships'"
            >金额（元）<input
              v-model="form.amount"
              required
              inputmode="decimal" /></label
          ><template v-if="kind === 'recharges'"
            ><label
              >支付平台<select
                v-model="form.type"
                @change="
                  form.method = form.type === 'wxpay' ? 'native' : 'page'
                "
              >
                <option value="alipay">支付宝</option>
                <option value="wxpay">微信支付</option>
              </select></label
            ><label
              >支付场景<select v-model="form.method">
                <option
                  v-for="method in browserRechargeMethods(form.type)"
                  :key="method"
                  :value="method"
                >
                  {{ method }}
                </option>
              </select></label
            ></template
          ><label
            >业务请求号<input
              v-model="form.external_no"
              required
              maxlength="128"
          /></label>
          <p class="muted">请核对业务信息。重复请求请保留相同请求号。</p>
          <div class="modal-actions">
            <button type="button" :disabled="busy" @click="dialog = false">
              取消</button
            ><button class="primary" :disabled="busy">
              {{ busy ? "正在处理…" : "确认" + title }}
            </button>
          </div>
        </form>
      </section>
    </div>
  </div>
</template>
