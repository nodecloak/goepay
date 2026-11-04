<script setup lang="ts">
import { computed, ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useData, useRoute, withBase } from 'vitepress'
import VPNavBarSearch from 'vitepress/dist/client/theme-default/components/VPNavBarSearch.vue'
import DocIcon from './DocIcon.vue'
import SideItem from './SideItem.vue'
import DocsProduct from './DocsProduct.vue'
const { isDark, site, page } = useData()
const route = useRoute()
const menuOpen = ref(false), notice = ref(true), copied = ref(false), headings = ref<any[]>([]), activeHeading = ref(''), tocOpen = ref(false)
const article = ref<HTMLElement>(), menuButton = ref<HTMLButtonElement>()
const sidebar = ref<HTMLElement>(), mainContent = ref<HTMLElement>(), tocButton = ref<HTMLButtonElement>()
const nav = computed(() => site.value.themeConfig.sidebar || [])
const current = computed(() => {
  const value = route.path.slice(site.value.base.length).replace(/\.html$/, '').replace(/\/$/, '')
  return value === 'index' ? '/' : '/' + value
})
const getLink = (link: string) => withBase(link === '/' ? '/' : link + '.html')
const flat = computed(() => {
  const all: any[] = []
  for (const group of nav.value) for (const item of group.items || []) { all.push(item); for (const child of item.items || []) all.push(child) }
  return all
})
const index = computed(() => flat.value.findIndex(i => i.link === current.value))
const previous = computed(() => flat.value[index.value - 1]), next = computed(() => flat.value[index.value + 1])
const location = computed(() => {
  for (const group of nav.value) for (const item of group.items || []) {
    if (item.link === current.value) return [group.text, item.text]
    const child = item.items?.find(i => i.link === current.value)
    if (child) return [item.text, child.text]
  }
  return ['使用文档', page.value.title || '快速开始']
})
async function updateHeadings() {
  await nextTick()
  headings.value = [...(article.value?.querySelectorAll('h2[id],h3[id]') || [])].map(e => ({ id: e.id, text: e.textContent?.replace(/#$/, '').trim(), level: Number(e.tagName.slice(1)) }))
  updateActive()
}
function updateActive() {
  const visible = headings.value.filter(h => (document.getElementById(h.id)?.getBoundingClientRect().top ?? Infinity) < 155)
  activeHeading.value = visible.at(-1)?.id || headings.value[0]?.id || ''
}
function toggleTheme() { isDark.value = !isDark.value }
async function copyPage() {
  try { await navigator.clipboard.writeText(article.value?.innerText || ''); copied.value = true; setTimeout(() => copied.value = false, 2000) }
  catch { copied.value = false }
}
function closeMenu(restoreFocus = true) {
  const wasOpen = menuOpen.value
  menuOpen.value = false
  if (wasOpen && restoreFocus) void nextTick(() => menuButton.value?.focus())
}
function navigate() {
  const wasOpen = menuOpen.value
  closeMenu(false)
  if (wasOpen) void nextTick(() => mainContent.value?.focus({ preventScroll: true }))
}
function resize() {
  if (window.innerWidth >= 1024) closeMenu(false)
  if (window.innerWidth >= 1280) tocOpen.value = false
}
function keydown(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') closeMenu(false)
  if (event.defaultPrevented) return
  if (event.key === 'Escape') {
    if (menuOpen.value) { event.preventDefault(); closeMenu() }
    else if (tocOpen.value) { event.preventDefault(); tocOpen.value = false; tocButton.value?.focus() }
  }
  if (event.key === 'Tab' && menuOpen.value && !document.querySelector('.VPLocalSearchBox')) {
    const items = [...(sidebar.value?.querySelectorAll<HTMLElement>('a[href],button,summary') || [])].filter(el => el.getClientRects().length)
    const outside = !sidebar.value?.contains(document.activeElement)
    if (event.shiftKey && (outside || document.activeElement === items[0])) { event.preventDefault(); items.at(-1)?.focus() }
    else if (!event.shiftKey && (outside || document.activeElement === items.at(-1))) { event.preventDefault(); items[0]?.focus() }
  }
}
watch(() => route.path, () => { navigate(); tocOpen.value = false; void updateHeadings() })
watch(() => page.value.relativePath, () => void updateHeadings(), { flush: 'post' })
watch(menuOpen, async (open) => {
  if (typeof document === 'undefined') return
  document.body.classList.toggle('manual-menu-open', open)
  if (open) {
    await nextTick()
    if (!menuOpen.value) return
    sidebar.value?.querySelector<HTMLButtonElement>('button')?.focus({ preventScroll: true })
    sidebar.value?.querySelector('[aria-current="page"]')?.scrollIntoView({ block: 'nearest' })
  }
})
onMounted(() => { void updateHeadings(); window.addEventListener('scroll', updateActive, { passive: true }); window.addEventListener('keydown', keydown); window.addEventListener('resize', resize) })
onBeforeUnmount(() => { window.removeEventListener('scroll', updateActive); window.removeEventListener('keydown', keydown); window.removeEventListener('resize', resize); document.body.classList.remove('manual-menu-open') })
</script>

<template>
  <div class="docs-app" :class="{ 'without-notice': !notice }" :style="{ '--header-offset': notice ? '90px' : '60px' }">
    <a href="#main-content" class="docs-skip">跳到正文</a>
    <div v-if="notice" class="docs-notice"><span><strong>GOEPAY 使用文档</strong> · 从后台配置到收款、退款与结算的完整流程</span><button aria-label="关闭顶部提示" @click="notice = false"><DocIcon name="close" :size="17" /></button></div>
    <header class="docs-header">
      <button ref="menuButton" class="icon-button mobile-menu" aria-label="目录" :aria-expanded="menuOpen" aria-controls="docs-sidebar" @click="menuOpen = !menuOpen"><DocIcon name="menu" /></button>
      <a :href="withBase('/')" class="docs-brand" aria-label="GOEPAY 文档首页" @click="navigate"><img v-if="!isDark" :src="withBase('/brand/wordmark-light.svg')" alt="GOEPAY" /><img v-else :src="withBase('/brand/wordmark-dark.svg')" alt="GOEPAY" /><span>DOCS</span></a>
      <DocsProduct />
      <nav class="docs-topnav" aria-label="站点导航"><a :href="withBase('/')" class="selected">文档</a><a :href="getLink('/modules/merchant-management')">功能模块</a><a :href="getLink('/admin/quickstart')">管理员</a><a :href="getLink('/merchant/quickstart')">商户</a><a href="https://goepay.cc/doc/" target="_blank" rel="noopener">开发文档 ↗</a></nav>
      <div class="docs-header-right" @click.capture="closeMenu(false)"><VPNavBarSearch /><button class="icon-button theme-toggle" role="switch" :aria-checked="isDark" :aria-label="isDark ? '切换到浅色模式' : '切换到深色模式'" @click="toggleTheme"><DocIcon :name="isDark ? 'moon' : 'sun'" :size="20" /></button></div>
    </header>
    <button v-if="menuOpen" class="docs-backdrop" aria-label="关闭菜单" tabindex="-1" @click="closeMenu()" />
    <aside ref="sidebar" id="docs-sidebar" class="docs-sidebar" :class="{ open: menuOpen }" :role="menuOpen ? 'dialog' : undefined" :aria-modal="menuOpen ? true : undefined" aria-label="按功能模块浏览">
      <div class="sidebar-caption">操作指南 <span>GOEPAY</span><button v-if="menuOpen" class="icon-button sidebar-close" aria-label="关闭目录" @click="closeMenu()"><DocIcon name="close" /></button></div>
      <nav><section v-for="group in nav" :key="group.text" class="side-section"><h2>{{ group.text }}</h2><SideItem v-for="item in group.items" :key="item.link" :item="item" :current="current" @navigate="navigate" /></section></nav>
      <div class="sidebar-bottom">管理员与商户 · 按功能查阅</div>
    </aside>
    <div class="docs-main" :inert="menuOpen || undefined">
      <main ref="mainContent" id="main-content" class="docs-article-column" tabindex="-1">
        <nav class="docs-breadcrumb" aria-label="当前位置"><a :href="withBase('/')" aria-label="文档首页"><DocIcon name="home" :size="16" /></a><DocIcon name="chevron" :size="12" /><span>{{ location[0] }}</span><DocIcon name="chevron" :size="12" /><span class="breadcrumb-current">{{ location[1] }}</span></nav>
        <div v-if="!page.isNotFound" class="article-tools"><span class="mobile-page-toc"><button ref="tocButton" @click="tocOpen = !tocOpen" :aria-expanded="tocOpen" aria-controls="mobile-page-toc">本页内容 <span>⌄</span></button></span><button class="copy-page" @click="copyPage"><DocIcon name="copy" :size="14" />{{ copied ? '已复制' : '复制本页' }}</button><span class="sr-only" aria-live="polite">{{ copied ? '正文已复制到剪贴板' : '' }}</span></div>
        <nav v-if="tocOpen" id="mobile-page-toc" class="mobile-toc" aria-label="本页内容"><a v-for="h in headings" :key="h.id" :href="'#' + h.id" @click="tocOpen = false">{{ h.text }}</a></nav>
        <article ref="article" class="docs-article"><div v-if="page.isNotFound" class="docs-not-found"><span>404</span><h1>没有找到这篇文档</h1><p>页面可能已调整位置。可以返回首页按功能模块查阅，或使用顶部搜索。</p><a :href="withBase('/')">返回文档首页 →</a></div><Content v-else class="vp-doc" /></article>
        <footer v-if="!page.isNotFound" class="docs-pagination"><a v-if="previous" :href="getLink(previous.link)"><span>← 上一篇</span><strong>{{ previous.text }}</strong></a><a v-if="next" :href="getLink(next.link)" class="pagination-next"><span>下一篇 →</span><strong>{{ next.text }}</strong></a></footer>
        <div class="docs-footnote">GOEPAY 使用手册 <span>·</span> 面向管理员与普通商户</div>
      </main>
      <aside class="docs-toc" aria-label="本页目录"><nav><a v-for="h in headings" :key="h.id" :href="'#' + h.id" :class="{ active: activeHeading === h.id, nested: h.level === 3 }">{{ h.text }}</a></nav><div class="toc-help"><strong>需要接入接口？</strong><p>查看签名、参数、回调验签及 Go SDK。</p><a href="https://goepay.cc/doc/" target="_blank" rel="noopener">打开开发文档 →</a></div></aside>
    </div>
  </div>
</template>
