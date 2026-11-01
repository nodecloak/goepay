<script setup lang="ts">
function reload() {
  location.reload();
}
</script>
<template>
  <section class="panel" role="alert">
    <h2>地区统计加载失败</h2>
    <p>请检查网络连接后重新加载页面。</p>
    <button type="button" class="primary" @click="reload">重新加载页面</button>
  </section>
</template>
