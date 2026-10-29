<script setup lang="ts">
defineProps<{ text?: string }>();
</script>
<template>
  <div class="pa-empty">
    <svg viewBox="0 0 96 72" aria-hidden="true">
      <polygon class="pa-e-side" points="12,30 48,12 48,40 12,58" />
      <polygon class="pa-e-side" points="84,30 48,12 48,40 84,58" />
      <polygon class="pa-e-top" points="48,12 72,24 48,36 24,24" />
      <rect class="pa-e-front" x="20" y="38" width="56" height="26" rx="2" />
    </svg>
    <span>{{ text || "暂无数据" }}</span>
  </div>
</template>
