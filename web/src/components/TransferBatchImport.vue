<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from "vue";
import { ApiClient, ApiError } from "../api";
import {
  batchChannelTypes,
  type BatchDraftRow,
  type BatchPolicy,
} from "../transfer-batches";
import {
  importDraftRows,
  importErrorLabel,
  validateImportPreview,
  type ImportPreview,
} from "../transfer-batch-import";
import {
  transferTypeLabel,
  bankProductLabel,
  bankChannelProduct,
} from "../transfers";
const props = defineProps<{
  client: ApiClient;
  policy: BatchPolicy;
  merchantID: string;
  disabled: boolean;
}>();
const emit = defineEmits<{ apply: [rows: BatchDraftRow[]] }>();
const preview = ref<ImportPreview | null>(null),
  channel = ref(""),
  type = ref(""),
  busy = ref(false),
  error = ref(""),
  fileInput = ref<HTMLInputElement>();
let controller: AbortController | undefined;
const types = computed(() =>
  batchChannelTypes(props.policy.channels.find((c) => c.id === channel.value)),
);
function reset() {
  controller?.abort();
  controller = undefined;
  preview.value = null;
  busy.value = false;
  if (fileInput.value) fileInput.value.value = "";
}
watch(
  () => [props.client, props.merchantID, props.policy],
  () => {
    reset();
    channel.value = "";
    type.value = "";
  },
);
watch(channel, () => {
  type.value = types.value[0] || "";
});
watch(
  () => props.disabled,
  (disabled) => {
    if (disabled) reset();
  },
);
function template() {
  const blob = new Blob(
    ["\ufeff序号,收款账号,收款人姓名,转账金额（元）,转账备注\r\n1,,,,\r\n"],
    { type: "text/csv;charset=utf-8" },
  );
  const url = URL.createObjectURL(blob),
    a = document.createElement("a");
  try {
    a.href = url;
    a.download = "transfer-five-columns.csv";
    a.click();
  } finally {
    URL.revokeObjectURL(url);
  }
}
async function choose(event: Event) {
  const input = event.target as HTMLInputElement,
    file = input.files?.[0];
  reset();
  error.value = "";
  if (!file) return;
  const format = file.name.split(".").pop()?.toLowerCase();
  if (format !== "csv" && format !== "xlsx") {
    error.value =
      "仅支持 CSV、XLSX；旧版 XLS 暂不支持，请另存为 XLSX 或 UTF-8 CSV。";
    return;
  }
  if (file.size < 1 || file.size > 512 * 1024) {
    error.value = "文件须为 1 字节至 512 KiB。";
    return;
  }
  const c = new AbortController();
  controller = c;
  busy.value = true;
  try {
    const bytes = new Uint8Array(await file.arrayBuffer());
    if (c.signal.aborted) return;
    let binary = "";
    for (let i = 0; i < bytes.length; i += 4096)
      binary += String.fromCharCode(...bytes.subarray(i, i + 4096));
    const data = await props.client.request("/transfer-batch-imports/preview", {
      method: "POST",
      body: {
        ...(props.client.area === "admin"
          ? { merchant_id: props.merchantID }
          : {}),
        format,
        file_base64: btoa(binary),
      },
      signal: c.signal,
    });
    if (c.signal.aborted) return;
    const validated = validateImportPreview(data);
    if (
      props.client.area === "admin" &&
      validated.merchant_id !== props.merchantID
    )
      throw new Error("导入商户不匹配");
    preview.value = validated;
  } catch (e) {
    if (!c.signal.aborted)
      error.value =
        e instanceof ApiError
          ? importErrorLabel(e.message)
          : e instanceof Error
            ? e.message
            : "文件校验失败";
  } finally {
    if (controller === c) busy.value = false;
  }
}
function apply() {
  if (!preview.value || props.disabled) return;
  try {
    const rows = importDraftRows(
      preview.value,
      props.policy,
      channel.value,
      type.value,
    );
    reset();
    emit("apply", rows);
  } catch (e) {
    error.value = e instanceof Error ? e.message : "请重新核对导入设置";
  }
}
onBeforeUnmount(reset);
</script>
<template>
  <section class="import-panel" aria-label="五列文件导入">
    <h3>从文件填入付款草稿</h3>
    <p>
      五列顺序：序号、收款账号、收款人姓名、转账金额（元）、转账备注。最多100条，512
      KiB；账号请设为文本，金额最多两位小数。
    </p>
    <p class="muted">
      文件仅校验，不预留或发送付款。支持 UTF-8 CSV、XLSX；暂不支持
      XLS、公式和外链。
    </p>
    <button type="button" :disabled="disabled || busy" @click="template">
      下载五列 CSV 模板
    </button>
    <label
      >导入文件<input
        ref="fileInput"
        type="file"
        accept=".csv,.xlsx"
        :disabled="disabled || busy"
        @change="choose"
    /></label>
    <p v-if="busy" role="status">正在校验文件…</p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <section v-if="preview" aria-label="文件校验预览">
      <p>
        已校验 {{ preview.count }} 条，本金合计
        {{ preview.total_cents }}
        分。预览隐藏账号姓名；应用后仅在当前页面草稿中编辑。
      </p>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>文件行</th>
              <th>序号</th>
              <th>收款账号</th>
              <th>姓名</th>
              <th>金额（元）</th>
              <th>备注</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in preview.rows" :key="row.serial">
              <td>{{ row.source_row }}</td>
              <td>{{ row.serial }}</td>
              <td>{{ row.account_masked }}</td>
              <td>{{ row.name_masked }}</td>
              <td>{{ row.amount }}</td>
              <td>{{ row.has_remark ? "已填写" : "未填写" }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <label
        >导入通道<select v-model="channel" :disabled="disabled">
          <option value="">请选择通道</option>
          <option
            v-for="c in policy.channels.filter(
              (c) => batchChannelTypes(c).length,
            )"
            :key="c.id"
            :value="c.id"
          >
            {{ c.id
            }}{{
              bankChannelProduct(c)
                ? " · " + bankProductLabel[bankChannelProduct(c)!]
                : ""
            }}
          </option>
        </select></label
      >
      <label
        >导入付款方式<select v-model="type" :disabled="disabled">
          <option value="">请选择方式</option>
          <option v-for="t in types" :key="t" :value="t">
            {{ transferTypeLabel[t] || t }}
          </option>
        </select></label
      >
      <p v-if="type === 'bank'" class="muted">
        五列文件不包含银行代码；微信银行产品须应用后逐条补填，不能猜测。应用后会替换当前草稿。
      </p>
      <button
        type="button"
        :disabled="disabled || !channel || !type"
        @click="apply"
      >
        应用到当前草稿
      </button>
      <button type="button" :disabled="disabled" @click="reset">
        清除文件预览
      </button>
    </section>
  </section>
</template>
<style scoped>
.import-panel {
  border: 1px solid var(--border, #dce3ec);
  border-radius: 8px;
  padding: 1rem;
  margin-block: 1rem;
  min-width: 0;
}
.import-panel label {
  display: block;
  margin-block: 0.75rem;
}
.import-panel input,
.import-panel select {
  max-width: 100%;
  min-height: 44px;
}
.import-panel button {
  margin: 0.3rem 0.5rem 0.3rem 0;
  min-height: 44px;
}
.import-panel table {
  min-width: 600px;
}
.import-panel p {
  overflow-wrap: anywhere;
}
.table-scroll {
  overflow: auto;
  max-width: 100%;
}
</style>
