<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import QRCode from "qrcode";
import { ApiClient } from "../api";
const props = defineProps<{ client: ApiClient; ownSession: boolean }>();
const enabled = ref(false),
  opened = ref(false),
  qr = ref(""),
  error = ref(""),
  entry = ref<HTMLButtonElement | null>(null);
const paymentApp = computed(() =>
  /MicroMessenger|AlipayClient/i.test(navigator.userAgent),
);
let alive = true;
const controller = new AbortController();
onMounted(async () => {
  if (!props.ownSession) return;
  try {
    const v = await props.client.request<{
      platform_open: boolean;
      can_prepare: boolean;
    }>("/scanpay/capabilities", { signal: controller.signal });
    if (alive)
      enabled.value = v.platform_open === true && v.can_prepare === true;
  } catch {
    enabled.value = false;
  }
});
async function open() {
  opened.value = true;
  error.value = "";
  try {
    const v = await QRCode.toDataURL(
      new URL("/merchant/scanpay", location.origin).href,
      { width: 240, margin: 2 },
    );
    if (alive && opened.value) qr.value = v;
  } catch {
    error.value = "二维码生成失败，可直接打开收款页面";
  }
}
function close() {
  opened.value = false;
  entry.value?.focus();
}
onBeforeUnmount(() => {
  alive = false;
  controller.abort();
});
</script>
<template>
  <template v-if="enabled && ownSession">
    <RouterLink v-if="paymentApp" class="button-link" to="/merchant/scanpay"
      >付款码收款</RouterLink
    >
    <button v-else ref="entry" @click="open">付款码收款</button>
    <div
      v-if="opened"
      class="scan-entry-overlay192"
      @click.self="close"
      @keydown.esc="close"
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label="手机收款入口"
        class="scan-entry-dialog192"
      >
        <h2>付款码收款</h2>
        <p>使用手机打开收款页面，扫描顾客付款码收款</p>
        <img
          v-if="qr"
          :src="qr"
          alt="付款码收款页面二维码"
          width="240"
          height="240"
        />
        <p v-if="error" role="alert">{{ error }}</p>
        <RouterLink to="/merchant/scanpay">打开收款页面</RouterLink
        ><button @click="close">关闭二维码</button>
      </section>
    </div>
  </template>
</template>
<style scoped>
.scan-entry-overlay192 {
  position: fixed;
  inset: 0;
  background: #0006;
  z-index: 1300;
  display: grid;
  place-items: center;
  padding: 16px;
}
.scan-entry-dialog192 {
  background: #fff;
  box-sizing: border-box;
  padding: 24px;
  border-radius: 8px;
  width: min(100%, 380px);
  text-align: center;
}
.scan-entry-dialog192 h2 {
  font-size: 18px;
  font-weight: 500;
}
.scan-entry-dialog192 p {
  font-size: 14px;
  line-height: 1.6;
  color: #606266;
}
.scan-entry-dialog192 img {
  display: block;
  max-width: 100%;
  height: auto;
  margin: auto;
}
.scan-entry-dialog192 a {
  display: block;
  margin: 12px;
  color: #409eff;
}
</style>
