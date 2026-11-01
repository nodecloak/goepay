<script setup lang="ts">
import { ref, watch } from "vue";
import type { ApiClient } from "../api";
import LocalPluginsPage from "./LocalPluginsPage.vue";
import ResourcePage from "./ResourcePage.vue";
const props = defineProps<{ client: ApiClient }>();
const loaded = ref(false),
  open = ref(false);
watch(
  () => props.client,
  () => {
    open.value = false;
  },
);
</script>
<template>
  <LocalPluginsPage @loaded="loaded = true" />
  <details
    v-if="loaded"
    class="panel plugin-data-panel"
    :open="open"
    @toggle="open = ($event.target as HTMLDetailsElement).open"
  >
    <summary>本地扩展资料</summary>
    <p class="muted">单独维护扩展资料；插件运行状态以上方列表为准。</p>
    <ResourcePage v-if="open" :client="client" resource="plugin-configs" />
  </details>
</template>
<style scoped>
.plugin-data-panel {
  margin-top: 20px;
  padding: 16px;
  min-width: 0;
}
summary {
  cursor: pointer;
  font-weight: 600;
}
.plugin-data-panel p {
  margin: 12px 0;
}
</style>
