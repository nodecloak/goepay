<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import QRCode from "qrcode";
import ShellIcon from "./ShellIcon.vue";
import KitModal from "./KitModal.vue";
import KitField from "./KitField.vue";
import KitSelect from "./KitSelect.vue";
import KitInput from "./KitInput.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitPayMethod from "./KitPayMethod.vue";
import KitEmpty from "./KitEmpty.vue";
import CopyButton from "./CopyButton.vue";
import MerchantRecords from "./MerchantRecords.vue";
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
  open = ref<boolean | null>(admin ? true : null),
  creating = ref(false),
  querying = ref(""),
  busy = ref(false),
  loading = ref(false),
  error = ref(""),
  notice = ref(""),
  qr = ref(""),
  qrLink = ref(""),
  qrError = ref(""),
  qrLoading = ref(false);
let qrRequest = 0;
let alive = true;
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
  closeQR();
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
    if (!alive) return;
    items.value = data.items;
    open.value = data.open;
  } catch {
    error.value = "红包记录读取失败，请刷新重试。";
  } finally {
    loading.value = false;
  }
}
function closeQR() {
  qrRequest++;
  qrLink.value = "";
  qr.value = "";
  qrError.value = "";
  qrLoading.value = false;
}
async function show(r: Redpack) {
  if (!alive) return;
  closeQR();
  const request = qrRequest;
  qrLoading.value = true;
  try {
    const image = await QRCode.toDataURL(r.claim_url, {
      width: 240,
      margin: 2,
    });
    if (request !== qrRequest) return;
    qrLink.value = r.claim_url;
    qr.value = image;
  } catch {
    if (request === qrRequest)
      qrError.value = "二维码生成失败，请从红包记录再次打开二维码。";
  } finally {
    if (request === qrRequest) qrLoading.value = false;
  }
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
    if (!alive) return;
    notice.value = "红包创建成功，请让收款人扫码领取（24小时内有效）。";
    creating.value = false;
    form.value.out_biz_no = defaultRedpackBizNo();
    form.value.amount = "";
    await load();
    await show(r);
  } catch (e) {
    error.value =
      e instanceof Error && e.message ? e.message : "红包创建失败。";
  } finally {
    busy.value = false;
  }
}
async function refresh(r: Redpack) {
  if (querying.value) return;
  querying.value = r.id;
  let problem = "";
  try {
    await props.client.request("/redpacks/" + r.id + "/query", {
      method: "POST",
      body: {},
    });
  } catch (e) {
    problem = e instanceof Error ? e.message : "红包状态查询失败。";
  } finally {
    if (alive) await load();
    if (problem) error.value = problem;
    querying.value = "";
  }
}
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  closeQR();
});
</script>
<template>
  <section
    class="panel records redpack-records"
    :class="{ 'merchant-redpack-records': !admin }"
    aria-label="红包记录"
  >
    <p v-if="open === false && !loading" class="alert error" role="alert">
      未开启创建红包功能。
    </p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <p class="table-note">
      收款人扫描二维码授权后领取，领取时通过支付宝/微信商家转账接口付款，并非现金红包接口。领取时才按转账费率预留余额，过期未领取不占用余额。
    </p>
    <div class="table-tools">
      <button
        type="button"
        class="pa-btn pa-btn--primary"
        :disabled="!open || busy || loading"
        @click="openCreate"
      >
        <ShellIcon name="plus" />创建红包
      </button>
    </div>
    <p v-if="qrError" class="alert error" role="alert">{{ qrError }}</p>
    <p v-if="qrLoading" class="loading-row" role="status">
      正在生成领取二维码…
    </p>
    <section v-if="qr" class="redpack-qr" aria-label="红包领取二维码">
      <img :src="qr" alt="红包领取二维码" width="240" height="240" />
      <div>
        <h2>扫码领取红包</h2>
        <div class="redpack-qr-link">
          <code>{{ qrLink }}</code>
          <CopyButton :text="qrLink" label="领取链接" />
        </div>
        <button type="button" class="pa-btn" @click="closeQR">
          关闭二维码
        </button>
      </div>
    </section>
    <p v-if="loading" class="loading-row" role="status">正在加载红包记录…</p>
    <MerchantRecords :stacked="!admin" class="table-scroll">
      <table class="pa-table" role="table">
        <thead role="rowgroup">
          <tr role="row">
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
        <tbody role="rowgroup">
          <tr v-for="r in items" :key="r.id" role="row">
            <td
              class="pa-c-mono"
              data-cell-label="交易号"
              data-cell-wide
              role="cell"
            >
              {{ r.out_biz_no }}
            </td>
            <td v-if="admin" role="cell">{{ r.merchant_id || "-" }}</td>
            <td data-cell-label="类型" role="cell">
              <KitPayMethod :type="r.type" />
            </td>
            <td class="pa-c-num" data-cell-label="金额" role="cell">
              ¥{{ r.money }}
            </td>
            <td data-cell-label="备注" role="cell">{{ r.remark || "-" }}</td>
            <td data-cell-label="状态" role="cell">
              <KitStatusTag
                :text="redpackStatusLabel(r.status)"
                :tone="redTone(r.status)"
              />
            </td>
            <td data-cell-label="过期时间" data-cell-wide role="cell">
              {{ beijingTimestamp(r.expires_at) }}
            </td>
            <td data-cell-actions role="cell">
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
                :disabled="!!querying || loading"
                @click="refresh(r)"
              >
                {{ querying === r.id ? "查询中…" : "查询" }}
              </button>
              <span
                v-if="!['claimable', 'claiming'].includes(r.status)"
                class="muted"
                >-</span
              >
            </td>
          </tr>
          <tr v-if="!items.length && !loading" role="row">
            <td class="empty" :colspan="admin ? 8 : 7" role="cell">
              <KitEmpty text="暂无数据" />
            </td>
          </tr>
        </tbody>
      </table>
    </MerchantRecords>
  </section>
  <KitModal
    v-if="creating"
    title="创建红包"
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
.redpack-qr {
  display: grid;
  align-items: center;
  gap: var(--space-4);
  margin: 0 var(--space-5) var(--space-4);
  padding: var(--space-4);
  background: var(--bg-raised);
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  min-width: 0;
}
.redpack-qr img {
  width: min(100%, 240px);
  height: auto;
  border-radius: var(--radius-sm);
  justify-self: center;
}
.redpack-qr h2 {
  font-size: 16px;
  margin: 0 0 var(--space-3);
}
.redpack-qr-link {
  display: flex;
  align-items: start;
  gap: var(--space-2);
  margin-bottom: var(--space-3);
  min-width: 0;
}
.redpack-qr-link code {
  overflow-wrap: anywhere;
}
.redpack-qr-link :deep(button) {
  flex: none;
}
@media (min-width: 900px) {
  .redpack-qr {
    grid-template-columns: minmax(120px, 240px) minmax(0, 1fr);
    max-width: 720px;
  }
}
@media (max-width: 520px) {
  .merchant-redpack-records .table-tools button {
    min-height: 42px;
  }
}
</style>
