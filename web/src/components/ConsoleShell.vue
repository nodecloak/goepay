<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from "vue";
import { useRouter } from "vue-router";
import type { Area, MenuNode } from "../menu";
import MenuTree from "./MenuTree.vue";
import ShellIcon from "./ShellIcon.vue";
import SiteBrandIcon193 from "./SiteBrandIcon193.vue";
const props = defineProps<{
  area: Area;
  navigation: MenuNode[];
  active: string;
  title: string;
  owner: object;
  brandName?: string;
}>();
const router = useRouter();
const collapsed = ref(false),
  navOpen = ref(false),
  width = ref(window.innerWidth);
const mobileButton = ref<HTMLButtonElement | null>(null);
const mobile = computed(() => width.value <= 768);
type Tab = { path: string; title: string };
const tabs = ref<Tab[]>([]);
function trail(nodes: MenuNode[], path: string): MenuNode[] {
  for (const node of nodes) {
    if (node.path === path) return [node];
    const children = trail(node.children, path);
    if (children.length) return [node, ...children];
  }
  return [];
}
const crumbs = computed(() => trail(props.navigation, props.active));
const home = computed(() =>
  props.navigation.find((node) => node.path === "/dashboard"),
);
function recordTab() {
  if (props.area !== "admin") return;
  if (home.value && !tabs.value.length)
    tabs.value.push({ path: home.value.path, title: home.value.title });
  const node = crumbs.value.at(-1);
  if (
    node &&
    !node.children.length &&
    !tabs.value.some((tab) => tab.path === node.path)
  ) {
    if (tabs.value.length >= 20) tabs.value.splice(1, 1);
    tabs.value.push({ path: node.path, title: node.title });
  }
}
watch(
  () => [props.area, props.owner],
  () => {
    tabs.value = [];
    navOpen.value = false;
    collapsed.value = false;
    recordTab();
  },
  { immediate: true },
);
watch(
  () => props.active,
  () => {
    navOpen.value = false;
    recordTab();
  },
);
watch(
  () => props.navigation,
  () => {
    tabs.value = tabs.value.filter(
      (tab) => trail(props.navigation, tab.path).length,
    );
    recordTab();
  },
);
function selectTab(path: string) {
  void router.push(`/${props.area}${path}`);
}
function closeTab(path: string) {
  if (path === home.value?.path) return;
  const index = tabs.value.findIndex((tab) => tab.path === path);
  if (index < 0) return;
  tabs.value.splice(index, 1);
  if (props.active === path)
    selectTab(tabs.value[Math.max(0, index - 1)]?.path || "/dashboard");
}
function tabKey(event: KeyboardEvent, index: number) {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  const next =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? tabs.value.length - 1
        : (index + (event.key === "ArrowRight" ? 1 : -1) + tabs.value.length) %
          tabs.value.length;
  const tab = tabs.value[next];
  if (tab) selectTab(tab.path);
  (event.currentTarget as HTMLElement)
    .closest('[role="tablist"]')
    ?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
    [next]?.focus();
}
function resize() {
  width.value = window.innerWidth;
  if (!mobile.value) navOpen.value = false;
}
function keyboard(event: KeyboardEvent) {
  if (event.key === "Escape" && navOpen.value) {
    navOpen.value = false;
    mobileButton.value?.focus();
  }
}
window.addEventListener("resize", resize);
window.addEventListener("keydown", keyboard);
onBeforeUnmount(() => {
  tabs.value = [];
  window.removeEventListener("resize", resize);
  window.removeEventListener("keydown", keyboard);
});
</script>
<template>
  <div
    class="workspace"
    :data-area="area"
    :class="{ 'sidebar-collapsed': collapsed && !mobile }"
  >
    <aside
      id="console-sidebar"
      class="console-sidebar"
      :class="{ open: navOpen }"
      :inert="mobile && !navOpen ? true : undefined"
      :aria-hidden="mobile && !navOpen ? true : undefined"
    >
      <div class="brand">
        <SiteBrandIcon193
          ><span class="brand-mark">{{
            Array.from(brandName || "GOEPAY")[0]
          }}</span></SiteBrandIcon193
        ><span class="brand-name">{{ brandName || "GOEPAY" }}</span>
      </div>
      <div class="area-label">
        {{ area === "admin" ? "管理后台" : "商户后台" }}
      </div>
      <nav aria-label="后台菜单">
        <MenuTree
          :nodes="navigation"
          :area="area"
          :active="active"
          :collapsed="collapsed && !mobile"
          @expand="collapsed = false"
        />
      </nav>
      <div class="sidebar-foot">
        <span class="sidebar-status" aria-hidden="true"></span
        ><span>支付运营工作台</span>
      </div>
    </aside>
    <button
      v-if="navOpen"
      class="scrim"
      aria-label="关闭菜单"
      @click="navOpen = false"
    ></button>
    <div class="main-content">
      <header class="topbar">
        <div class="header-navigation">
          <button
            class="mobile-nav icon-button"
            ref="mobileButton"
            aria-label="展开菜单"
            aria-controls="console-sidebar"
            :aria-expanded="navOpen"
            @click="navOpen = !navOpen"
          >
            <ShellIcon name="menu" />
          </button>
          <button
            class="desktop-nav icon-button"
            :aria-label="collapsed ? '展开侧栏' : '收起侧栏'"
            aria-controls="console-sidebar"
            :aria-expanded="!collapsed"
            @click="collapsed = !collapsed"
          >
            <ShellIcon :name="collapsed ? 'unfold' : 'fold'" />
          </button>
          <nav class="breadcrumbs" aria-label="面包屑">
            <span>{{ area === "admin" ? "平台运营" : "商户服务" }}</span
            ><template v-for="node in crumbs" :key="node.path"
              ><ShellIcon name="chevron" /><span
                :aria-current="node.path === active ? 'location' : undefined"
                >{{ node.title }}</span
              ></template
            ><template v-if="!crumbs.length"
              ><ShellIcon name="chevron" /><span>{{ title }}</span></template
            >
          </nav>
        </div>
        <slot name="account" />
      </header>
      <div
        v-if="area === 'admin' && tabs.length"
        class="work-tabs"
        role="tablist"
        aria-label="工作页签"
      >
        <div
          v-for="(tab, index) in tabs"
          :key="tab.path"
          class="work-tab"
          :class="{ selected: tab.path === active }"
        >
          <button
            role="tab"
            :aria-selected="tab.path === active"
            :tabindex="tab.path === active ? 0 : -1"
            @click="selectTab(tab.path)"
            @keydown="tabKey($event, index)"
          >
            <ShellIcon v-if="tab.path === '/dashboard'" name="home" /><span>{{
              tab.title
            }}</span>
          </button>
          <button
            v-if="tab.path !== home?.path"
            class="tab-close"
            :aria-label="'关闭页签：' + tab.title"
            @click="closeTab(tab.path)"
          >
            <ShellIcon name="close" />
          </button>
        </div>
      </div>
      <slot />
    </div>
    <slot name="overlays" />
  </div>
</template>
