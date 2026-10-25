<script setup lang="ts">
import { ref, onBeforeUnmount, watch } from "vue";
import QRCode from "qrcode";
import { ApiClient } from "../api";
import { safePaymentUrl } from "../commerce";
import {
  readCollectionLayout,
  renderCollectionPoster,
} from "../collection-style";
const props = defineProps<{ client: ApiClient; id: string }>();
const busy = ref(false),
  error = ref(""),
  notice = ref(""),
  url = ref(""),
  expires = ref(""),
  qr = ref("");
let generation = 0,
  alive = true,
  controller: AbortController | undefined;
function reset() {
  generation++;
  controller?.abort();
  busy.value = false;
  error.value = "";
  notice.value = "";
  url.value = "";
  qr.value = "";
  expires.value = "";
}
watch(() => [props.client, props.id], reset);
async function generate() {
  if (busy.value) return;
  const current = ++generation;
  controller?.abort();
  const c = new AbortController();
  controller = c;
  busy.value = true;
  error.value = "";
  notice.value = "";
  url.value = "";
  qr.value = "";
  try {
    const data = await props.client.request<{
      url: string;
      expires_at: string | null;
      layout?: unknown;
      merchant_name?: string;
    }>(`/collection-codes/${encodeURIComponent(props.id)}/link`, {
      signal: c.signal,
    });
    if (!alive || current !== generation || c.signal.aborted) return;
    const target = safePaymentUrl(data.url);
    if (!target) throw new Error("收款链接格式错误，请联系管理员");
    url.value = target;
    expires.value = data.expires_at || "";
    const rendered = data.layout
      ? await renderCollectionPoster(
          readCollectionLayout(data.layout),
          target,
          data.merchant_name || "商户",
          { signal: c.signal },
        )
      : await QRCode.toDataURL(target, { width: 280, margin: 2 });
    if (alive && current === generation && !c.signal.aborted)
      qr.value = rendered;
  } catch (e) {
    if (alive && current === generation && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "收款链接生成失败";
  } finally {
    if (alive && current === generation) busy.value = false;
  }
}
async function copy() {
  try {
    await navigator.clipboard.writeText(url.value);
    notice.value = "收款链接已复制";
  } catch {
    error.value = "无法访问剪贴板，请手动复制收款链接";
  }
}
function download() {
  const a = document.createElement("a");
  a.href = qr.value;
  a.download = `collection-code-${props.id}.png`;
  a.click();
}
onBeforeUnmount(() => {
  alive = false;
  reset();
  url.value = "";
  qr.value = "";
});
</script>
<template>
  <section class="case-panel">
    <h3>聚合收款链接与二维码</h3>
    <p class="muted">
      链接打开公开收款页，由付款人选择金额和支付方式。仅在付款成功并核实支付回调后入账。
    </p>
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
    <button :disabled="busy" @click="generate">
      {{ busy ? "正在生成…" : "生成收款链接" }}</button
    ><template v-if="url"
      ><label>收款链接<input :value="url" readonly autocomplete="off" /></label>
      <p class="muted">
        {{ expires ? "有效期至 " + expires : "长期有效，停用后失效" }}
      </p>
      <img
        v-if="qr"
        :src="qr"
        width="280"
        style="height: auto; max-width: 100%"
        alt="聚合收款二维码"
        class="collection-qr"
      />
      <div class="toolbar-actions">
        <button @click="copy">复制收款链接</button
        ><button v-if="qr" @click="download">下载二维码</button
        ><a :href="url" target="_blank" rel="noopener noreferrer">打开收款页</a>
      </div></template
    >
  </section>
</template>
