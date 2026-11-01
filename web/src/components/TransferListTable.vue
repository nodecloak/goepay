<script setup lang="ts">
import { computed, nextTick, ref } from "vue";
import QRCode from "qrcode";
import type { Area } from "../menu";
import { transferReadActions } from "../transfers";
import { originalTransferColumns, readTransferListRow } from "../transfer-list";
import KitStatusTag from "./KitStatusTag.vue";
import KitPayMethod from "./KitPayMethod.vue";
import KitEmpty from "./KitEmpty.vue";
const props = defineProps<{
  items: Record<string, unknown>[];
  area: Area;
  busy: boolean;
}>();
const emit = defineEmits<{
  details: [row: Record<string, unknown>];
  query: [row: Record<string, unknown>];
  proof: [row: Record<string, unknown>];
}>();
const columns = computed(() => originalTransferColumns(props.area));
type KitTone = "success" | "warning" | "danger" | "info" | "neutral";
const statusTone = (tone: string): KitTone =>
  tone === "processing"
    ? "warning"
    : tone === "success" || tone === "danger" || tone === "neutral"
      ? tone
      : "neutral";
// 获取凭证: enabled only where the provider offers an electronic receipt — that is
// Alipay transfers (TransferProofProvider). 微信/QQ/银行卡 have no receipt API, so
// the action shows disabled with a one-line reason.
const proofState = (raw: Record<string, unknown>) => {
  const actions = transferReadActions(raw);
  if (actions.proof) return { enabled: true, reason: "" };
  if (raw.type !== "alipay")
    return { enabled: false, reason: "仅支付宝付款支持电子回单" };
  return { enabled: false, reason: "该付款状态暂不支持获取凭证" };
};
// Original showQrcode: the payee scans the 确认收款 link in WeChat.
const confirm = ref<{ url: string; image: string } | null>(null),
  copied = ref(""),
  dialog = ref<HTMLElement | null>(null);
async function showConfirm(link: string) {
  const url = new URL(link, window.location.origin).href;
  copied.value = "";
  confirm.value = {
    url,
    image: await QRCode.toDataURL(url, { width: 230, margin: 2 }),
  };
  await nextTick();
  dialog.value?.focus();
}
async function copyConfirm() {
  try {
    await navigator.clipboard.writeText(confirm.value!.url);
    copied.value = "链接已复制";
  } catch {
    copied.value = "复制失败，请长按二维码下方链接手动复制";
  }
}
const data = computed(() => {
  try {
    const rows = props.items.map(readTransferListRow);
    if (new Set(rows.map((r) => r.id)).size !== rows.length)
      throw Error("付款记录编号重复，请刷新后重试");
    return { rows, error: "" };
  } catch (e) {
    return {
      rows: [],
      error: e instanceof Error ? e.message : "付款记录无法核对",
    };
  }
});
</script>
<template>
  <p v-if="data.error" class="alert error" role="alert">{{ data.error }}</p>
  <template v-else>
    <div class="table-scroll original-transfer-list">
      <table :class="area">
        <thead>
          <tr>
            <th v-for="column in columns" :key="column.key" scope="col">
              <span class="transfer-th-main">{{ column.label }}</span>
              <span v-if="column.sub" class="transfer-th-sub muted">{{
                column.sub
              }}</span>
            </th>
            <th scope="col" class="transfer-actions">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in data.rows" :key="row.id" :data-transfer-id="row.id">
            <td
              v-for="column in columns"
              :key="column.key"
              :class="{
                'transfer-number': column.key === 'number',
                'transfer-method': column.key === 'method',
              }"
            >
              <template v-if="column.key === 'number'"
                ><span>{{ row.number }}</span
                ><small class="transfer-secondary muted"
                  >商户交易号：{{ row.externalNo }}</small
                ><small v-if="row.providerNo" class="transfer-secondary muted"
                  >接口交易号：{{ row.providerNo }}</small
                ></template
              >
              <template v-else-if="column.key === 'method'"
                ><KitPayMethod
                  v-if="
                    ['alipay', 'wxpay', 'bank'].includes(String(row.raw.type))
                  "
                  :type="String(row.raw.type)"
                /><span v-else>{{ row.method }}</span
                ><small class="transfer-secondary muted transfer-remark">{{
                  row.remark || "-"
                }}</small></template
              >
              <template v-else-if="column.key === 'fee'"
                >¥{{ row.fee }}</template
              >
              <template v-else-if="column.key === 'account'"
                >{{ row.account
                }}<small class="transfer-secondary">{{
                  row.name
                }}</small></template
              >
              <template v-else-if="column.key === 'amount'"
                ><strong>¥{{ row.amount }}</strong
                ><small class="transfer-secondary muted"
                  >花费金额：¥{{ row.cost }}</small
                ></template
              >
              <template v-else-if="column.key === 'createdAt'"
                >{{ row.createdAt
                }}<small class="transfer-secondary muted"
                  >付款时间：{{ row.payDate }}</small
                ></template
              >
              <template v-else-if="column.key === 'status'"
                ><KitStatusTag
                  :text="row.status"
                  :tone="statusTone(row.tone)"
                /><button
                  v-if="row.confirmURL"
                  type="button"
                  class="link-button transfer-secondary transfer-confirm"
                  :disabled="busy"
                  @click="showConfirm(row.confirmURL)"
                >
                  确认收款
                </button></template
              >
              <template v-else>{{
                row[column.key as keyof typeof row]
              }}</template>
            </td>
            <td class="transfer-actions">
              <button
                type="button"
                class="link-button"
                :disabled="busy"
                @click="emit('details', row.raw)"
              >
                付款详情
              </button>
              <button
                v-if="transferReadActions(row.raw).query"
                type="button"
                class="link-button"
                :disabled="busy"
                @click="emit('query', row.raw)"
              >
                查询状态
              </button>
              <button
                type="button"
                class="link-button"
                :disabled="busy || !proofState(row.raw).enabled"
                :title="proofState(row.raw).reason || undefined"
                @click="emit('proof', row.raw)"
              >
                获取凭证
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <KitEmpty v-if="!data.rows.length" text="暂无数据" />
  </template>
  <div v-if="confirm" class="modal-backdrop" @click.self="confirm = null">
    <section
      ref="dialog"
      tabindex="-1"
      role="dialog"
      aria-modal="true"
      aria-label="确认收款二维码"
      class="modal panel transfer-confirm-qr"
      @keydown.esc.prevent="confirm = null"
    >
      <h3>收款方使用微信扫描以下二维码</h3>
      <img :src="confirm.image" width="230" height="230" alt="确认收款二维码" />
      <p class="muted transfer-confirm-link">{{ confirm.url }}</p>
      <p v-if="copied" class="muted" role="status">{{ copied }}</p>
      <div class="toolbar">
        <button type="button" @click="copyConfirm">复制链接</button>
        <button type="button" @click="confirm = null">关闭</button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.original-transfer-list {
  isolation: isolate;
}
.original-transfer-list table.admin {
  min-width: 1250px;
}
.original-transfer-list table.merchant {
  min-width: 1160px;
}
.original-transfer-list th,
.original-transfer-list td {
  vertical-align: middle;
  white-space: nowrap;
}
.original-transfer-list thead th {
  vertical-align: top;
}
.transfer-th-main {
  display: block;
}
.transfer-th-sub {
  display: block;
  margin-top: 2px;
  font-weight: 400;
  font-size: 12px;
}
.transfer-number {
  min-width: 240px;
  max-width: 350px;
  white-space: normal !important;
  overflow-wrap: anywhere;
}
.transfer-secondary {
  display: block;
  margin-top: 4px;
}
.transfer-method {
  min-width: 190px;
  max-width: 280px;
  white-space: normal !important;
}
.transfer-remark {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.transfer-actions {
  position: sticky;
  right: 0;
  background: var(--panel, var(--bg-card));
  min-width: 100px;
  z-index: 1;
  box-shadow: -4px 0 5px -5px var(--line-control);
}
th.transfer-actions {
  background: var(--table-header, var(--bg-raised));
}
.transfer-confirm {
  color: var(--chart-2);
}
.transfer-confirm-qr {
  text-align: center;
  max-width: 320px;
}
.transfer-confirm-link {
  overflow-wrap: anywhere;
  font-size: 12px;
}
</style>
