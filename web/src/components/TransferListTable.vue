<script setup lang="ts">
import { computed, nextTick, ref } from "vue";
import QRCode from "qrcode";
import type { Area } from "../menu";
import { transferReadActions } from "../transfers";
import { originalTransferColumns, readTransferListRow } from "../transfer-list";
const props = defineProps<{
  items: Record<string, unknown>[];
  area: Area;
  busy: boolean;
}>();
const emit = defineEmits<{
  details: [row: Record<string, unknown>];
  query: [row: Record<string, unknown>];
}>();
const columns = computed(() => originalTransferColumns(props.area));
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
  <div v-else class="table-scroll original-transfer-list">
    <table :class="area">
      <thead>
        <tr>
          <th v-for="column in columns" :key="column.key" scope="col">
            {{ column.label }}
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
              >{{ row.method
              }}<small
                v-if="row.remark"
                class="transfer-secondary transfer-remark muted"
                >备注：{{ row.remark }}</small
              ></template
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
              ><span
                class="badge"
                :data-state="
                  {
                    success: 'on',
                    danger: 'bad',
                    processing: 'warn',
                    neutral: 'none',
                  }[row.tone] || 'none'
                "
                >{{ row.status }}</span
              ><button
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
          </td>
        </tr>
        <tr v-if="!data.rows.length">
          <td class="empty" :colspan="columns.length + 1">暂无付款记录</td>
        </tr>
      </tbody>
    </table>
  </div>
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
  max-width: 260px;
  white-space: normal !important;
}
.transfer-remark {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  max-width: 260px;
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
