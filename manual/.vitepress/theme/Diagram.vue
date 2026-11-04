<script setup lang="ts">
import { computed, ref, onBeforeUnmount, nextTick } from 'vue'
import { withBase, useData } from 'vitepress'
import DocIcon from './DocIcon.vue'
const props = defineProps<{ name: string; caption: string }>()
const { isDark } = useData()
const light = computed(() => withBase(`/diagrams/${props.name}.light.svg`))
const dark = computed(() => withBase(`/diagrams/${props.name}.dark.svg`))
const source = computed(() => isDark.value ? dark.value : light.value)
const kind = computed(() => props.name.includes('sequence') ? '时序图' : props.name.includes('architecture') ? '架构图' : '流程图')
const open = ref(false), scale = ref(100), naturalWidth = ref(1100), naturalHeight = ref(700)
const viewer = ref<HTMLElement>(), closer = ref<HTMLButtonElement>(), trigger = ref<HTMLButtonElement>()
let oldOverflow = '', drag: any
async function show() {
  open.value = true; scale.value = 100
  oldOverflow = document.body.style.overflow; document.body.style.overflow = 'hidden'
  window.addEventListener('keydown', keyboard)
  window.addEventListener('resize', fit)
  await nextTick(); closer.value?.focus()
}
function close() { open.value = false; document.body.style.overflow = oldOverflow; window.removeEventListener('keydown', keyboard); window.removeEventListener('resize', fit); trigger.value?.focus() }
function keyboard(e: KeyboardEvent) {
  if (e.key === 'Escape') { e.preventDefault(); close() }
  if (e.key === 'Tab') {
    const items = [...(document.querySelectorAll<HTMLElement>('.diagram-modal button,.diagram-modal a') || [])]
    if (e.shiftKey && document.activeElement === items[0]) { e.preventDefault(); items.at(-1)?.focus() }
    else if (!e.shiftKey && document.activeElement === items.at(-1)) { e.preventDefault(); items[0]?.focus() }
  }
}
function zoom(delta: number) { scale.value = Math.max(10, Math.min(250, scale.value + delta)) }
function fit() {
  if (!viewer.value) return
  const style = getComputedStyle(viewer.value)
  const width = viewer.value.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)
  const height = viewer.value.clientHeight - parseFloat(style.paddingTop) - parseFloat(style.paddingBottom)
  scale.value = Math.max(1, Math.min(100, Math.floor(Math.min(width / naturalWidth.value, height / naturalHeight.value) * 100)))
  viewer.value.scrollTo(0, 0)
}
function loaded(event: Event) {
  const image = event.target as HTMLImageElement
  naturalWidth.value = image.naturalWidth
  naturalHeight.value = image.naturalHeight
  fit()
}
function pointerDown(event: PointerEvent) { if (!viewer.value || event.button !== 0) return; drag = { x: event.clientX, y: event.clientY, left: viewer.value.scrollLeft, top: viewer.value.scrollTop }; viewer.value.setPointerCapture(event.pointerId) }
function pointerMove(event: PointerEvent) { if (drag && viewer.value) { viewer.value.scrollLeft = drag.left - event.clientX + drag.x; viewer.value.scrollTop = drag.top - event.clientY + drag.y } }
onBeforeUnmount(() => { if (open.value) { document.body.style.overflow = oldOverflow; window.removeEventListener('keydown', keyboard); window.removeEventListener('resize', fit) } })
</script>

<template>
  <figure class="manual-diagram" :data-diagram="name">
    <div class="diagram-heading"><div class="diagram-title"><span class="diagram-kind">{{ kind }}</span><span>{{ caption }}</span></div><div class="diagram-actions"><button ref="trigger" @click="show" :aria-label="`放大${kind}：${caption}`"><DocIcon name="expand" :size="14" />放大</button><a :href="source" :download="name + '.svg'" :aria-label="`下载${kind}：${caption}`"><DocIcon name="download" :size="14" /></a></div></div>
    <div class="diagram-canvas"><img class="diagram-light" :src="light" :alt="caption" loading="lazy" /><img class="diagram-dark" :src="dark" :alt="caption" loading="lazy" /></div>
    <figcaption>点击「放大」查看完整{{ kind }}，支持缩放和拖动。</figcaption>
  </figure>
  <Teleport to="body"><div v-if="open" class="diagram-modal" role="dialog" aria-modal="true" :aria-label="caption"><div class="diagram-modal-header"><strong>{{ kind }} · {{ caption }}</strong><div class="diagram-modal-tools"><button class="icon-button" aria-label="缩小图表" @click="zoom(-25)"><DocIcon name="minus" /></button><span aria-live="polite">{{ scale }}%</span><button class="icon-button" aria-label="放大图表" @click="zoom(25)"><DocIcon name="plus" /></button><button class="icon-button" aria-label="适合窗口" @click="fit"><DocIcon name="expand" /></button><a class="icon-button" :href="source" :download="name + '.svg'" aria-label="下载图表"><DocIcon name="download" /></a><button ref="closer" class="icon-button" aria-label="关闭图表" @click="close"><DocIcon name="close" /></button></div></div><div ref="viewer" class="diagram-modal-body" @pointerdown="pointerDown" @pointermove="pointerMove" @pointerup="drag = null" @pointercancel="drag = null"><img :src="source" :alt="caption" draggable="false" :style="{ width: `${naturalWidth * scale / 100}px` }" @load="loaded" /></div><div class="diagram-modal-hint">拖动查看 · 使用 ＋ / − 调整大小 · Esc 关闭</div></div></Teleport>
</template>
