<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { withBase } from 'vitepress'
import DocIcon from './DocIcon.vue'
const props = defineProps<{ item: any; current: string }>()
defineEmits(['navigate'])
const active = computed(() => props.item.link === props.current)
const expanded = computed(() => active.value || props.item.items?.some(i => i.link === props.current))
const open = ref(Boolean(expanded.value))
const childrenId = `docs-section-${useId()}`
watch(() => props.current, () => { if (expanded.value) open.value = true })
const target = (link: string) => withBase(link === '/' ? '/' : link + '.html')
</script>
<template>
  <div v-if="item.items?.length" class="module-nav-item" :class="{ 'module-current': expanded }">
    <div class="module-row">
      <a :href="target(item.link)" :class="{ active }" :aria-current="active ? 'page' : undefined" @click="$emit('navigate')">{{ item.text }}</a>
      <button class="module-toggle" :aria-label="`${open ? '收起' : '展开'}${item.text}`" :aria-expanded="open" :aria-controls="childrenId" @click="open = !open"><DocIcon name="chevron" :size="14" /></button>
    </div>
    <div v-show="open" :id="childrenId" class="module-children"><SideItem v-for="child in item.items" :key="child.link" :item="child" :current="current" @navigate="$emit('navigate')" /></div>
  </div>
  <a v-else :href="target(item.link)" class="side-link" :class="{ active }" :aria-current="active ? 'page' : undefined" @click="$emit('navigate')">{{ item.text }}</a>
</template>
