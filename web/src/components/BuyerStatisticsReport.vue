<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ApiClient } from "../api";
import { reportDateRange, validDateRange } from "../reports";
import CsvExportButton from "./CsvExportButton.vue";
import OrderBlacklistDialog from "./OrderBlacklistDialog.vue";
import KitPagination from "./KitPagination.vue";
import KitHelpPopover from "./KitHelpPopover.vue";
import ShellIcon from "./ShellIcon.vue";
import KitField from "./KitField.vue";
import KitSelect from "./KitSelect.vue";

const props = defineProps<{
  client: ApiClient;
  ownSession?: boolean;
  adminClass?: string;
}>();
type BuyerRow = {
  user: string;
  order_count: string;
  pay_money: string;
  trade_no: string;
  is_black: boolean;
  shownMoney?: string;
  moneyError?: string;
  moneyBusy?: boolean;
};
const today = reportDateRange("today")!;
const from = ref(today.from),
  to = ref(today.to),
  method = ref("0"),
  type = ref(""),
  sort = ref("order_count"),
  direction = ref("desc"),
  page = ref(1);
const limit = ref(20);
const items = ref<BuyerRow[]>([]),
  total = ref(0),
  loading = ref(false),
  error = ref(""),
  unavailable = ref(""),
  blacklist = ref<BuyerRow | null>(null);
const canWrite = computed(
  () =>
    props.client.area === "admin" &&
    props.ownSession &&
    ["super", "operator"].includes(props.adminClass || ""),
);
const label = computed(
  () =>
    ({ "0": "支付账号", "1": "支付IP", "2": "手机号" })[method.value] ||
    "支付账号",
);
let controller: AbortController | undefined,
  epoch = 0,
  applied = "",
  alive = true;
function query() {
  return new URLSearchParams({
    view: "original",
    from: from.value,
    to: to.value,
    method: method.value,
    type: type.value,
    sort: sort.value,
    direction: direction.value,
    page: String(page.value),
    limit: String(limit.value),
  });
}
const csvPath = computed(() => {
  const q = query();
  q.set("format", "csv");
  return "/reports/buyers?" + q;
});
function invalidate() {
  controller?.abort();
  epoch++;
  applied = "";
  items.value = [];
  total.value = 0;
  loading.value = false;
  error.value = "";
  unavailable.value = "";
  blacklist.value = null;
}
watch([from, to, method, type], () => {
  page.value = 1;
  invalidate();
});
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
  ],
  invalidate,
);
function validTrade(value: unknown): boolean {
  return typeof value === "string" && /^[A-Za-z0-9_-]{1,64}$/.test(value);
}
function orderLink(value: string, column: string) {
  return "/admin/trade/order?" + new URLSearchParams({ column, value });
}
async function load() {
  invalidate();
  if (!validDateRange(from.value, to.value) || !from.value || !to.value) {
    error.value = "请选择有效查询日期，日期范围最多367天";
    return;
  }
  const localEpoch = epoch,
    client = props.client,
    csrf = client.csrfToken,
    q = query();
  controller = new AbortController();
  const c = controller;
  loading.value = true;
  try {
    const data = await client.request<{
      items: BuyerRow[];
      total: number;
      available?: boolean;
      unavailable_reason?: string;
    }>("/reports/buyers?" + q, { signal: c.signal });
    if (
      !alive ||
      c.signal.aborted ||
      localEpoch !== epoch ||
      client !== props.client ||
      csrf !== client.csrfToken
    )
      return;
    if (data.available === false) {
      unavailable.value = data.unavailable_reason || "该统计维度暂无可信来源";
      return;
    }
    // Keep counts and money as exact strings. Never round a JSON number into a
    // plausible-looking account count or amount.
    items.value = data.items
      .filter(
        (row) =>
          typeof row.user === "string" &&
          row.user.length > 0 &&
          typeof row.order_count === "string" &&
          /^(0|[1-9]\d*)$/.test(row.order_count) &&
          typeof row.pay_money === "string" &&
          /^(0|[1-9]\d*)\.\d{2}$/.test(row.pay_money),
      )
      .map((row) => ({ ...row, shownMoney: undefined }));
    if (items.value.length !== data.items.length) {
      items.value = [];
      error.value = "统计响应缺少精确来源，请重新查询";
      return;
    }
    total.value = data.total;
    applied = q.toString();
  } catch (e) {
    if (alive && !c.signal.aborted && localEpoch === epoch)
      error.value = e instanceof Error ? e.message : "统计读取失败";
  } finally {
    if (alive && localEpoch === epoch) loading.value = false;
  }
}
async function readMoney(row: BuyerRow) {
  if (!applied || row.moneyBusy) return;
  const localEpoch = epoch,
    binding = applied,
    client = props.client,
    csrf = client.csrfToken,
    q = new URLSearchParams(applied);
  q.set("user", row.user);
  q.set("page", "1");
  row.moneyBusy = true;
  row.moneyError = "";
  try {
    const data = await client.request<{ items: BuyerRow[] }>(
      "/reports/buyers?" + q,
      { signal: controller?.signal },
    );
    if (
      !alive ||
      localEpoch !== epoch ||
      applied !== binding ||
      client !== props.client ||
      csrf !== client.csrfToken
    )
      return;
    const exact = data.items.find((item) => item.user === row.user);
    if (
      !exact ||
      typeof exact.pay_money !== "string" ||
      !/^(0|[1-9]\d*)\.\d{2}$/.test(exact.pay_money)
    ) {
      row.shownMoney = undefined;
      row.moneyError = "未读取到原用户金额，请重新查询";
      return;
    }
    row.shownMoney = exact.pay_money;
  } catch (e) {
    if (alive && localEpoch === epoch) {
      row.shownMoney = undefined;
      row.moneyError = e instanceof Error ? e.message : "金额读取失败";
    }
  } finally {
    if (alive && localEpoch === epoch) row.moneyBusy = false;
  }
}
function changeSort(key: string) {
  direction.value =
    sort.value === key && direction.value === "desc" ? "asc" : "desc";
  sort.value = key;
  page.value = 1;
  void load();
}
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  invalidate();
});
</script>

<template>
  <div class="list-page buyer-statistics">
    <form
      class="toolbar list-filter-card panel"
      @submit.prevent="
        page = 1;
        load();
      "
    >
      <div class="pa-filter-fields list-filters-grid">
        <KitField label="开始日期">
          <input v-model="from" type="date" aria-label="开始日期" required />
        </KitField>
        <KitField label="结束日期">
          <input v-model="to" type="date" aria-label="结束日期" required />
        </KitField>
        <KitField label="支付方式">
          <KitSelect
            v-model="type"
            :options="[
              { value: '', label: '所有支付方式' },
              { value: 'alipay', label: '支付宝' },
              { value: 'wxpay', label: '微信支付' },
              { value: 'sandbox', label: '本地模拟' },
            ]"
            aria-label="支付方式"
          />
        </KitField>
        <KitField label="统计方式">
          <KitSelect
            v-model="method"
            :options="[
              { value: '0', label: '支付账号统计' },
              { value: '1', label: '支付IP统计' },
              { value: '2', label: '支付手机号统计' },
            ]"
            aria-label="统计方式"
          />
        </KitField>
      </div>
      <div class="pa-filter-actions">
        <button
          type="button"
          class="pa-btn"
          :disabled="loading"
          @click="
            from = today.from;
            to = today.to;
            type = '';
            method = '0';
            page = 1;
            $nextTick(load);
          "
        >
          重置
        </button>
        <button
          type="submit"
          class="pa-btn pa-btn--primary"
          :class="{ 'pa-btn--loading': loading }"
          :disabled="loading"
        >
          查询
        </button>
      </div>
    </form>
    <section class="panel records">
      <div class="table-tools">
        <CsvExportButton
          :client="client"
          :path="csvPath"
          filename="buyer-statistics.csv"
          :disabled="
            loading || !applied || !!unavailable || !validDateRange(from, to)
          "
        />
        <span class="table-summary"
          >共 <strong>{{ total }}</strong> 条记录</span
        >
        <div class="table-tools-end">
          <KitHelpPopover
            text="只统计查询日期内已付款订单，日期按北京时间。未记录的账号或IP不合并为虚构用户；付款金额可按行刷新。"
          />
          <button
            type="button"
            class="icon-button table-refresh"
            :aria-label="loading ? '正在更新…' : '刷新数据'"
            :title="loading ? '正在更新…' : '刷新数据'"
            :disabled="loading"
            @click="load"
          >
            <ShellIcon name="refresh" />
          </button>
        </div>
      </div>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <div v-if="unavailable" class="alert" role="status">
        {{ unavailable }}
      </div>
      <p v-if="loading" role="status">正在读取支付用户统计…</p>
      <div v-else-if="!error && !unavailable" class="table-scroll">
        <table>
          <thead>
            <tr>
              <th scope="col">{{ label }}</th>
              <th
                scope="col"
                :aria-sort="
                  sort === 'order_count'
                    ? direction === 'desc'
                      ? 'descending'
                      : 'ascending'
                    : 'none'
                "
              >
                <button class="link-button" @click="changeSort('order_count')">
                  付款笔数
                </button>
              </th>
              <th
                scope="col"
                :aria-sort="
                  sort === 'pay_money'
                    ? direction === 'desc'
                      ? 'descending'
                      : 'ascending'
                    : 'none'
                "
              >
                <button class="link-button" @click="changeSort('pay_money')">
                  付款金额（元）
                </button>
              </th>
              <th scope="col">最新订单号</th>
              <th scope="col">是否拉黑</th>
              <th scope="col">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in items" :key="row.user">
              <td>{{ row.user }}</td>
              <td>
                <RouterLink
                  :to="orderLink(row.user, method === '1' ? 'ip' : 'buyer')"
                  >{{ row.order_count }}</RouterLink
                >
              </td>
              <td>
                <button
                  class="link-button numeric"
                  :aria-label="'刷新付款金额 ' + row.user"
                  :disabled="row.moneyBusy || !applied"
                  @click="readMoney(row)"
                >
                  {{
                    row.moneyBusy ? "正在读取…" : (row.shownMoney ?? "[刷新]")
                  }}</button
                ><small v-if="row.moneyError" class="cell error" role="alert">{{
                  row.moneyError
                }}</small>
              </td>
              <td>
                <RouterLink
                  v-if="validTrade(row.trade_no)"
                  :to="orderLink(row.trade_no, 'trade_no')"
                  >{{ row.trade_no }}</RouterLink
                ><span v-else>未记录</span>
              </td>
              <td>
                {{
                  row.is_black === true
                    ? "是"
                    : row.is_black === false
                      ? "否"
                      : "未核对"
                }}
              </td>
              <td>
                <button
                  v-if="canWrite && validTrade(row.trade_no)"
                  class="link-button"
                  :aria-label="'拉黑 ' + row.user"
                  @click="blacklist = row"
                >
                  拉黑</button
                ><span v-else>—</span>
              </td>
            </tr>
            <tr v-if="!items.length">
              <td colspan="6" class="empty">暂无记录</td>
            </tr>
          </tbody>
        </table>
      </div>
      <KitPagination
        v-model:page="page"
        v-model:page-size="limit"
        :total="total"
        @change="load"
      />
      <OrderBlacklistDialog
        v-if="blacklist && canWrite"
        :key="blacklist.trade_no + method"
        :client="client"
        :trade-no="blacklist.trade_no"
        :expected="blacklist.user"
        :kind="method === '1' ? 'ip' : 'buyer'"
        :own-session="!!ownSession"
        :admin-class="adminClass"
        @close="blacklist = null"
        @updated="
          blacklist = null;
          load();
        "
      />
    </section>
  </div>
</template>

<style scoped>
.buyer-statistics {
  min-width: 0;
}
.table-summary {
  margin-left: auto;
  font-size: 13px;
  color: var(--ink-muted);
}
.table-note {
  margin: 0 var(--space-5) var(--space-3);
  color: var(--ink-muted);
  font-size: 12px;
  line-height: 1.7;
}
.buyer-statistics td {
  overflow-wrap: anywhere;
}
small.cell {
  display: block;
  margin-top: 4px;
}
</style>
