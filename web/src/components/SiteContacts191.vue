<script setup lang="ts">
import type { PublicSite191 } from "../site191";
defineProps<{ site: PublicSite191 }>();
</script>
<template>
  <div class="site-contacts191">
    <p v-if="site.orgname">{{ site.orgname }}</p>
    <nav aria-label="联系网站">
      <a v-if="site.email" :href="'mailto:' + site.email">{{ site.email }}</a>
      <a
        v-if="site.kfqq_url"
        :href="site.kfqq_url"
        target="_blank"
        rel="noopener noreferrer"
        >客服QQ{{ site.kfqq ? "：" + site.kfqq : "" }}</a
      >
      <span v-else-if="site.kfqq">客服QQ：{{ site.kfqq }}</span>
      <a
        v-if="site.wework_url"
        :href="site.wework_url"
        target="_blank"
        rel="noopener noreferrer"
        >企业微信</a
      >
      <a
        v-if="site.qqqun"
        :href="site.qqqun"
        target="_blank"
        rel="noopener noreferrer"
        >加入QQ群</a
      >
    </nav>
  </div>
</template>
<style scoped>
.site-contacts191 {
  font-size: 13px;
  overflow-wrap: anywhere;
}
nav {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 16px;
}
a {
  color: inherit;
  text-underline-offset: 3px;
}
</style>
