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
import ShellIcon from "./ShellIcon.vue";
import KitPagination from "./KitPagination.vue";
import KitModal from "./KitModal.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitSelect from "./KitSelect.vue";
import { centsMoney } from "../settlement-terms";
const props = defineProps<{
  client: ApiClient;
  kind: CommerceKind;
  area: Area;
}>();
const items = ref<Record<string, unknown>[]>([]),
  groups = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  page = ref(1),
  limit = ref(20),
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
  quantity: "1",
});
function membershipPeriod(g: Record<string, unknown>) {
  return g.permanent === true
    ? "永久"
    : typeof g.duration_months === "number"
      ? `${g.duration_months}个月`
      : `${g.duration_days}天`;
}
const selectedGroup = computed(() =>
  groups.value.find((g) => String(g.id) === form.value.group_id),
);
const membershipTotal = computed(() => {
  const price = String(selectedGroup.value?.price_cents ?? "");
  if (
    !/^(0|[1-9]\d*)$/.test(price) ||
    !/^[1-9]\d{0,2}$/.test(form.value.quantity) ||
    Number(form.value.quantity) > 300
  )
    return "—";
  return centsMoney((BigInt(price) * BigInt(form.value.quantity)).toString());
});
const title = computed(
  () =>
    ({
      memberships: "购买会员",
      deposits: "保证金操作",
      recharges: "余额充值",
    })[props.kind],
);
const depositActionOptions = [
  { value: "hold", label: "转入保证金" },
  { value: "release", label: "申请释放保证金" },
];
const rechargeTypeOptions = [
  { value: "alipay", label: "支付宝" },
  { value: "wxpay", label: "微信支付" },
];
const groupOptions = computed(() => [
  { value: "", label: "请选择" },
  ...groups.value.map((g) => ({
    value: String(g.id),
    label: `${g.name} · ${centsMoney(String(g.price_cents))} 元 / ${membershipPeriod(g)}`,
  })),
]);
const rechargeMethodOptions = computed(() =>
  browserRechargeMethods(form.value.type).map((m) => ({ value: m, label: m })),
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
    price: "购买金额（元）",
    price_cents: "会员价格（分）",
    group_name: "用户组",
    expires_at: "有效期至",
    group_id: "用户组 ID",
    quantity: "购买数量",
    unit_price_cents: "单价（分）",
    permanent: "永久有效",
    period_months: "每份有效期（月）",
    membership_period: "有效期",
    action: "操作类型",
    trade_no: "充值订单号",
    type: "支付类型",
    paid_at: "支付时间",
  };
  const keys =
    props.kind === "memberships"
      ? [
          "id",
          ...(props.area === "admin" ? ["merchant_id"] : []),
          "group_name",
          "price",
          "quantity",
          "membership_period",
          "expires_at",
          "external_no",
        ]
      : [...new Set(items.value.flatMap(Object.keys))];
  return keys.map((key) => ({
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
    }>(`/workflows/${props.kind}?page=${page.value}&limit=${limit.value}`, {
      signal: c.signal,
    });
    if (!c.signal.aborted) {
      items.value =
        props.kind === "memberships"
          ? data.items.map((row) => ({
              ...row,
              membership_period:
                row.permanent === true
                  ? "永久"
                  : Number(row.period_months) > 0
                    ? `${row.period_months}个月 × ${row.quantity || 1}`
                    : `${row.duration_days}天`,
              expires_at: row.permanent === true ? "永久" : row.expires_at,
            }))
          : data.items;
      total.value = data.total;
      balances.value = data;
    }
    if (props.kind === "memberships") {
      const data = await props.client.request<{
        items: Record<string, unknown>[];
      }>("/membership-sales", { signal: c.signal });
      if (!c.signal.aborted) groups.value = data.items;
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
    quantity: "1",
  };
  error.value = "";
  dialog.value = true;
  receipt.value = null;
}
async function submit() {
  if (busy.value) return;
  if (
    props.kind === "memberships" &&
    (!selectedGroup.value ||
      !/^[1-9]\d{0,2}$/.test(form.value.quantity) ||
      Number(form.value.quantity) > 300)
  ) {
    error.value = "请选择可购买用户组，购买数量为1至300的整数";
    return;
  }
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
    const body: Record<string, string | number> = {
      external_no: form.value.external_no,
    };
    if (props.area === "admin") body.merchant_id = form.value.merchant_id;
    if (props.kind === "memberships") {
      body.group_id = form.value.group_id;
      body.quantity = Number(form.value.quantity);
    } else body.amount = form.value.amount;
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
    <section
      v-if="kind === 'memberships' && groups.length"
      class="panel membership-catalog"
    >
      <h2>可购买用户组</h2>
      <div class="membership-offers">
        <article
          v-for="g in groups"
          :key="String(g.id)"
          class="membership-offer"
        >
          <h3>{{ g.name }}</h3>
          <strong
            >{{ centsMoney(String(g.price_cents)) }} 元 /
            {{ membershipPeriod(g) }}</strong
          >
          <p class="membership-description">{{ g.description }}</p>
          <button
            :disabled="busy || loading"
            @click="
              open();
              form.group_id = String(g.id);
            "
          >
            购买此用户组
          </button>
        </article>
      </div>
    </section>
    <section class="panel records">
      <div class="table-tools">
        <button
          type="button"
          class="primary"
          :disabled="busy || (kind === 'memberships' && !groups.length)"
          @click="open"
        >
          <ShellIcon name="plus" />{{ title }}
        </button>
        <div class="table-tools-end">
          <button type="button" :disabled="loading" @click="load">
            <ShellIcon name="refresh" />刷新
          </button>
        </div>
      </div>
      <p class="table-note">
        {{
          kind === "memberships"
            ? "会员购买会按已配置的价格扣减账户余额，价格以服务端确认为准。"
            : kind === "deposits"
              ? "保证金冻结与释放申请均记录资金流水。释放须经管理员核准。"
              : "充值记录反映真实订单状态，创建订单不会直接增加余额。"
        }}
      </p>
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
      <KitPagination
        v-model:page="page"
        v-model:page-size="limit"
        :total="total"
        @change="load"
      />
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
    <KitModal
      v-if="dialog"
      :title="title"
      :loading="busy"
      :submit-text="'确认' + title"
      @close="dialog = false"
      @submit="submit"
    >
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <KitField v-if="area === 'admin'" label="商户 ID" required>
        <KitInput
          v-model="form.merchant_id"
          inputmode="numeric"
          aria-label="商户 ID"
        />
      </KitField>
      <KitField v-if="kind === 'memberships'" label="购买用户组" required>
        <KitSelect
          v-model="form.group_id"
          :options="groupOptions"
          aria-label="购买用户组"
        />
      </KitField>
      <template v-if="kind === 'memberships'">
        <KitField label="购买数量" required>
          <KitInput
            v-model="form.quantity"
            inputmode="numeric"
            :disabled="busy"
            aria-label="购买数量"
          />
        </KitField>
        <p role="status">
          合计：{{ membershipTotal }} 元<span v-if="selectedGroup">
            · {{ membershipPeriod(selectedGroup) }} × {{ form.quantity }}</span
          >
        </p>
        <p class="muted">
          按已保存售价扣减余额。月有效期按日历月计算，同组未到期会续期；永久用户组不能重复购买。提交时服务端重新核对售价及可见范围。
        </p>
      </template>
      <KitField v-if="kind === 'deposits'" label="保证金操作">
        <KitSelect
          v-model="form.action"
          :options="depositActionOptions"
          aria-label="保证金操作"
        />
      </KitField>
      <KitField v-if="kind !== 'memberships'" label="金额（元）" required>
        <KitInput
          v-model="form.amount"
          inputmode="decimal"
          aria-label="金额（元）"
        />
      </KitField>
      <template v-if="kind === 'recharges'">
        <KitField label="支付平台">
          <KitSelect
            :model-value="form.type"
            :options="rechargeTypeOptions"
            aria-label="支付平台"
            @update:model-value="
              (v) => {
                form.type = v;
                form.method = v === 'wxpay' ? 'native' : 'page';
              }
            "
          />
        </KitField>
        <KitField label="支付场景">
          <KitSelect
            v-model="form.method"
            :options="rechargeMethodOptions"
            aria-label="支付场景"
          />
        </KitField>
      </template>
      <KitField label="业务请求号" required>
        <KitInput
          v-model="form.external_no"
          :maxlength="128"
          aria-label="业务请求号"
        />
      </KitField>
      <p class="muted">请核对业务信息。重复请求请保留相同请求号。</p>
    </KitModal>
  </div>
</template>
<style scoped>
.membership-offers {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 240px), 1fr));
  gap: 16px;
}
.membership-offer {
  border: 1px solid var(--line);
  border-radius: var(--custom-radius);
  padding: 20px;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}
.membership-offer h3 {
  margin: 0 0 12px;
}
.membership-offer strong {
  color: var(--accent);
}
.membership-description {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  flex: 1;
}
</style>
