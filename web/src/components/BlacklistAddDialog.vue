<script setup lang="ts">
import { computed, ref } from "vue";
import { ApiClient } from "../api";
import type { ResourceField } from "../resources";
import KitModal from "./KitModal.vue";
import KitField from "./KitField.vue";
import KitSelect from "./KitSelect.vue";
import KitInput from "./KitInput.vue";
// Pro's /trade/blacklist 添加 dialog: 拉黑类型, a 拉黑内容 textarea (one entry per
// line, batched), 有效期 天 (0 = 永久) and 备注. Posts them to the dedicated
// batch-create endpoint; the parent reloads the list and shows a notice.
const props = defineProps<{
  client: ApiClient;
  // The blacklist schema's kind field, so the type options and labels match the
  // single editor (ip / domain / buyer) without hardcoding them here.
  kindField?: ResourceField;
}>();
const emit = defineEmits<{ close: []; saved: [message: string] }>();
const kindOptions = computed(() =>
  (props.kindField?.options || ["ip", "domain", "buyer"]).map((o) =>
    typeof o === "string"
      ? { value: o, label: o }
      : { value: String(o.value), label: o.label },
  ),
);
const kind = ref<string>(kindOptions.value[0]?.value || "ip");
const content = ref("");
const days = ref("0");
const remark = ref("");
const busy = ref(false);
const error = ref("");
const lineCount = computed(
  () => content.value.split(/[\r\n]+/).filter((l) => l.trim() !== "").length,
);
const daysInvalid = computed(() => {
  if (days.value.trim() === "") return false;
  const n = Number(days.value);
  return !Number.isInteger(n) || n < 0 || n > 36500;
});
async function submit() {
  if (busy.value) return;
  error.value = "";
  const lines = content.value
    .split(/[\r\n]+/)
    .map((l) => l.trim())
    .filter((l) => l !== "");
  if (!lines.length) {
    error.value = "请至少填写一行拉黑内容";
    return;
  }
  if (lines.length > 500) {
    error.value = "一次最多添加 500 条";
    return;
  }
  if (daysInvalid.value) {
    error.value = "有效期需为 0-36500 的整数天（0 为永久）";
    return;
  }
  busy.value = true;
  try {
    const res = await props.client.request<{
      created?: string[];
      skipped?: number;
    }>("/resources/blacklist/batch-create", {
      method: "POST",
      body: {
        kind: kind.value,
        lines,
        days: Number(days.value || "0"),
        remark: remark.value.trim(),
      },
    });
    const created = res?.created?.length ?? 0;
    const skipped = res?.skipped ?? 0;
    emit(
      "saved",
      `已添加 ${created} 条黑名单${skipped ? `，跳过 ${skipped} 条已存在` : ""}`,
    );
  } catch (e) {
    error.value = e instanceof Error ? e.message : "添加失败，请重试";
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <KitModal
    title="添加黑名单"
    submit-text="添加"
    width="520px"
    :loading="busy"
    :submit-disabled="daysInvalid"
    @close="emit('close')"
    @submit="submit"
  >
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <KitField label="拉黑类型">
      <KitSelect
        v-model="kind"
        :options="kindOptions"
        aria-label="拉黑类型"
        :disabled="busy"
      />
    </KitField>
    <KitField label="拉黑内容">
      <textarea
        v-model="content"
        class="pa-textarea blacklist-add-textarea"
        rows="8"
        aria-label="拉黑内容"
        placeholder="一行一条，可批量粘贴"
        :disabled="busy"
      ></textarea>
    </KitField>
    <p class="muted blacklist-add-count">当前有效行数：{{ lineCount }}</p>
    <KitField label="有效期（天）">
      <KitInput
        v-model="days"
        inputmode="numeric"
        aria-label="有效期天数"
        placeholder="0 为永久"
        :error="daysInvalid"
        :disabled="busy"
      />
    </KitField>
    <KitField label="备注">
      <KitInput
        v-model="remark"
        aria-label="备注"
        :maxlength="1000"
        :disabled="busy"
      />
    </KitField>
  </KitModal>
</template>
<style scoped>
.blacklist-add-textarea {
  width: 100%;
  box-sizing: border-box;
  resize: vertical;
  font-family: inherit;
  font-size: 14px;
  line-height: 1.5;
  padding: 8px 12px;
}
.blacklist-add-count {
  margin: 4px 0 0;
  font-size: 12px;
}
</style>
