<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
const shown = ref(true),
  brandFallback = ref(true),
  revision = ref(0);
const iconURL = "/static/img/icon.png";
const brandMarkURL = "/static/goepay-mark.svg";
function refresh() {
  shown.value = true;
  brandFallback.value = true;
  revision.value++;
}
onMounted(() => window.addEventListener("goepay-branding193-updated", refresh));
onBeforeUnmount(() =>
  window.removeEventListener("goepay-branding193-updated", refresh),
);
</script>
<template>
  <img
    v-if="shown"
    :key="revision"
    :src="iconURL"
    alt="全局品牌图标"
    class="site-brand-icon193"
    @error="shown = false"
  /><img
    v-else-if="brandFallback"
    :key="'brand-' + revision"
    :src="brandMarkURL"
    alt="GOEPAY 品牌标识"
    class="site-brand-icon193"
    @error="brandFallback = false"
  /><slot v-else />
</template>
<style scoped>
.site-brand-icon193 {
  display: block;
  width: 34px;
  height: 34px;
  object-fit: contain;
  flex-shrink: 0;
}
</style>
