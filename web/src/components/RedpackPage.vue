<script setup lang="ts">
import { onMounted, ref } from "vue";
import QRCode from "qrcode";
import ShellIcon from "./ShellIcon.vue";
import KitModal from "./KitModal.vue";
import KitField from "./KitField.vue";
import KitSelect from "./KitSelect.vue";
import KitInput from "./KitInput.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitPayMethod from "./KitPayMethod.vue";
import KitEmpty from "./KitEmpty.vue";
import { beijingTimestamp } from "../console-datetime";
import type { ApiClient } from "../api";
import {
  defaultRedpackBizNo,
  readRedpacks,
  redpackStatusLabel,
  validRedpackForm,
  type Redpack,
} from "../redpacks";
const props = defineProps<{ client: ApiClient }>();
const admin = props.client.area === "admin";
const form = ref({
  merchant_id: "",
  channel_id: "",
  type: "alipay",
  out_biz_no: defaultRedpackBizNo(),
  amount: "",
  remark: "",
});
const items = ref<Redpack[]>([]),
  open = ref(admin),
  creating = ref(false),
  busy = ref(false),
  loading = ref(false),
  error = ref(""),
  notice = ref(""),
  qr = ref(""),
  qrLink = ref("");
// Red states: 待领取/领取处理中 pending, 已领取 ok, 领取失败 bad, 已过期 neutral.
type KitTone = "success" | "warning" | "danger" | "info" | "neutral";
const redTone = (s: string): KitTone =>
  s === "succeeded"
    ? "success"
    : s === "failed"
      ? "danger"
      : s === "expired"
        ? "neutral"
        : "warning";
function openCreate() {
  error.value = "";
  notice.value = "";
  qr.value = "";
  form.value.out_biz_no = defaultRedpackBizNo();
  form.value.amount = "";
  creating.value = true;
}
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const q =
      admin && form.value.merchant_id
        ? "?merchant_id=" + form.value.merchant_id
        : "";
    const data = readRedpacks(await props.client.request("/redpacks" + q));
    items.value = data.items;
    open.value = data.open;
  } catch {
    error.value = "红包记录读取失败，请刷新重试。";
  } finally {
    loading.value = false;
  }
}
async function show(r: Redpack) {
  qrLink.value = r.claim_url;
  qr.value = await QRCode.toDataURL(r.claim_url, { width: 240, margin: 2 });
}
async function create() {
  const problem = validRedpackForm(form.value, admin);
  if (problem) {
    error.value = problem;
    return;
  }
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const body: Record<string, string> = { ...form.value };
    if (!admin) delete body.merchant_id;
    const r = (await props.client.request("/redpacks", {
      method: "POST",
      body,
    })) as Redpack;
    notice.value = "红包创建成功，请让收款人扫码领取（24小时内有效）。";
    creating.value = false;
    await show(r);
    form.value.out_biz_no = defaultRedpackBizNo();
    form.value.amount = "";
    await load();
  } catch (e) {
    error.value =
      e instanceof Error && e.message ? e.message : "红包创建失败。";
  } finally {
    busy.value = false;
  }
}
async function refresh(r: Redpack) {
  try {
    await props.client.request("/redpacks/" + r.id + "/query", {
      method: "POST",
      body: {},
    });
  } finally {
    await load();
  }
}
onMounted(load);
</script>
<template>
  <section class="panel records redpack-records" aria-label="红包记录">
    <p v-if="!open" class="alert error" role="alert">未开启创建红包功能。</p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <p class="table-note">
      收款人扫描二维码授权后领取，领取时通过支付宝/微信商家转账接口付款，并非现金红包接口。领取时才按转账费率预留余额，过期未领取不占用余额。
    </p>
    <div class="table-tools">
      <button
        type="button"
        class="pa-btn pa-btn--primary"
        :disabled="!open || busy"
        @click="openCreate"
      >
        <ShellIcon name="plus" />创建红包
      </button>
      <div class="table-tools-end">
        <button
          type="button"
          class="pa-icon-btn"
          aria-label="刷新"
          title="刷新"
          :disabled="loading"
          @click="load"
        >
          <ShellIcon name="refresh" />
        </button>
      </div>
    </div>
    <div v-if="qr" class="qr">
      <img :src="qr" alt="红包领取二维码" width="240" height="240" />
      <p class="muted">{{ qrLink }}</p>
    </div>
    <div class="table-scroll">
      <table class="pa-table">
        <thead>
          <tr>
            <th scope="col">交易号</th>
            <th v-if="admin" scope="col">商户</th>
            <th scope="col">类型</th>
            <th scope="col" class="pa-c-num">金额</th>
            <th scope="col">备注</th>
            <th scope="col">状态</th>
            <th scope="col">过期时间</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in items" :key="r.id">
            <td class="pa-c-mono">{{ r.out_biz_no }}</td>
            <td v-if="admin">{{ r.merchant_id || "-" }}</td>
            <td><KitPayMethod :type="r.type" /></td>
            <td class="pa-c-num">¥{{ r.money }}</td>
            <td>{{ r.remark || "-" }}</td>
            <td>
              <KitStatusTag
                :text="redpackStatusLabel(r.status)"
                :tone="redTone(r.status)"
              />
            </td>
            <td>{{ beijingTimestamp(r.expires_at) }}</td>
            <td>
              <button
                v-if="r.status === 'claimable'"
                type="button"
                class="link-button"
                @click="show(r)"
              >
                二维码
              </button>
              <button
                v-if="r.status === 'claiming'"
                type="button"
                class="link-button"
                @click="refresh(r)"
              >
                查询
              </button>
              <span
                v-if="!['claimable', 'claiming'].includes(r.status)"
                class="muted"
                >-</span
              >
            </td>
          </tr>
          <tr v-if="!items.length && !loading">
            <td class="empty" :colspan="admin ? 8 : 7">
              <KitEmpty text="暂无数据" />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
  <KitModal
    v-if="creating"
    title="创建红包"
    width="480px"
    :loading="busy"
    submit-text="确认创建"
    @close="creating = false"
    @submit="create"
  >
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <KitField v-if="admin" label="付款商户" required>
      <KitInput
        :model-value="form.merchant_id"
        inputmode="numeric"
        aria-label="付款商户ID"
        @update:model-value="form.merchant_id = $event.trim()"
      />
    </KitField>
    <KitField label="通道选择">
      <KitSelect
        :model-value="form.type"
        :options="[
          { value: 'alipay', label: '支付宝' },
          { value: 'wxpay', label: '微信' },
        ]"
        aria-label="通道选择"
        @update:model-value="form.type = $event"
      />
    </KitField>
    <KitField label="转账通道">
      <KitInput
        :model-value="form.channel_id"
        inputmode="numeric"
        placeholder="留空自动选择"
        aria-label="转账通道ID"
        @update:model-value="form.channel_id = $event.trim()"
      />
    </KitField>
    <KitField label="交易号" required>
      <KitInput
        :model-value="form.out_biz_no"
        :maxlength="19"
        inputmode="numeric"
        aria-label="交易号"
        @update:model-value="form.out_biz_no = $event.trim()"
      />
    </KitField>
    <KitField label="红包金额" required>
      <KitInput
        :model-value="form.amount"
        inputmode="decimal"
        placeholder="RMB/元"
        aria-label="红包金额"
        @update:model-value="form.amount = $event.trim()"
      />
    </KitField>
    <KitField label="红包备注">
      <KitInput
        :model-value="form.remark"
        :maxlength="32"
        aria-label="红包备注"
        @update:model-value="form.remark = $event"
      />
    </KitField>
  </KitModal>
</template>
<style scoped>
/* Full-width records card (as 付款记录): text, toolbar and the first/last
   table cells share the card's 20px inset. */
.redpack-records > .alert {
  margin: var(--space-4) var(--space-5) 0;
}
.redpack-records > .table-note {
  padding-top: var(--space-4);
}
.redpack-records > .alert + .table-note {
  padding-top: var(--space-3);
}
.redpack-records > .table-tools {
  padding-top: var(--space-2);
}
.redpack-records tbody tr:last-child > td {
  border-bottom: 0;
}
.qr {
  margin: 0;
  padding: 0 var(--space-5) var(--space-4);
}
.qr p {
  overflow-wrap: anywhere;
}
</style>
