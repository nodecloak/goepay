<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from "vue";
import { ApiClient } from "../api";
const props = defineProps<{
  client: ApiClient;
  path: string;
  filename: string;
  disabled?: boolean;
}>();
const pending = ref(false),
  error = ref(""),
  notice = ref("");
let controller: AbortController | undefined;
function cancel() {
  controller?.abort();
  controller = undefined;
  pending.value = false;
  error.value = "";
  notice.value = "";
}
watch(() => [props.path, props.client], cancel);
onBeforeUnmount(cancel);
async function download() {
  if (pending.value || props.disabled) return;
  const c = new AbortController();
  controller = c;
  pending.value = true;
  error.value = "";
  notice.value = "";
  try {
    await props.client.downloadCSV(props.path, props.filename, {
      signal: c.signal,
    });
    if (controller === c && !c.signal.aborted)
      notice.value = "筛选结果已交给浏览器下载，请核对保存的文件。";
  } catch (e) {
    if (controller === c && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "导出失败，请稍后重试";
  } finally {
    if (controller === c) {
      pending.value = false;
      controller = undefined;
    }
  }
}
</script>
<template>
  <div class="csv-export">
    <button type="button" :disabled="disabled || pending" @click="download">
      {{ pending ? "正在导出…" : "导出筛选结果 CSV" }}
    </button>
    <button v-if="pending" type="button" @click="cancel">取消导出</button>
    <small class="muted"
      >全部筛选结果，最多 50,000 行或 8
      MiB；超限请缩小筛选范围，不会截断导出。</small
    >
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
  </div>
</template>
