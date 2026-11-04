<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, withBase } from 'vitepress'
import DocIcon from './DocIcon.vue'

const route = useRoute()
const disclosure = ref<HTMLDetailsElement>()
const trigger = ref<HTMLElement>()
const links = [
  { label: '全部功能模块', path: '/' },
  { label: '管理员快速开始', path: '/admin/quickstart.html' },
  { label: '商户快速开始', path: '/merchant/quickstart.html' }
]

function close() { if (disclosure.value) disclosure.value.open = false }
function dismissOutside(event: Event) {
  if (event.target instanceof Node && !disclosure.value?.contains(event.target)) close()
}
function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && disclosure.value?.open) {
    event.preventDefault()
    close()
    trigger.value?.focus()
  }
}
function onResize() {
  // Hidden desktop navigation must not reopen when returning from a narrow viewport.
  if (disclosure.value && !disclosure.value.getClientRects().length) close()
}
watch(() => route.path, close)
onMounted(() => {
  document.addEventListener('pointerdown', dismissOutside, true)
  document.addEventListener('focusin', dismissOutside)
  document.addEventListener('keydown', onKeydown)
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', dismissOutside, true)
  document.removeEventListener('focusin', dismissOutside)
  document.removeEventListener('keydown', onKeydown)
  window.removeEventListener('resize', onResize)
})
</script>

<template>
  <details ref="disclosure" class="docs-product">
    <summary ref="trigger" aria-controls="docs-product-options">
      后台使用指南 <DocIcon name="chevron" :size="14" />
    </summary>
    <nav id="docs-product-options" class="docs-product-options" aria-label="使用指南入口">
      <a v-for="link in links" :key="link.path" :href="withBase(link.path)"
        :aria-current="route.path === withBase(link.path) ? 'page' : undefined" @click="close">
        {{ link.label }}
      </a>
    </nav>
  </details>
</template>
