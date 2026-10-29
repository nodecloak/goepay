<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import type { Area, MenuNode } from "../menu";
import ShellIcon from "./ShellIcon.vue";
const props = defineProps<{
  area: Area;
  navigation: MenuNode[];
  displayName: string;
  theme: "dark" | "light";
  logoutBusy?: boolean;
}>();
const emit = defineEmits<{
  (e: "navigate", path: string): void;
  (e: "toggle-theme"): void;
  (e: "account"): void;
  (e: "logout"): void;
}>();

const avatarLetter = computed(
  () => Array.from((props.displayName || "").trim() || "用")[0] || "用",
);

// Flatten the role-filtered navigation to its visible leaf entries, keeping a
// breadcrumb-style label so the operator can tell duplicate titles apart.
interface Entry {
  path: string;
  title: string;
  trail: string;
}
const entries = computed<Entry[]>(() => {
  const out: Entry[] = [];
  const walk = (nodes: MenuNode[], trail: string[]) => {
    for (const node of nodes) {
      if (node.hidden) continue;
      const next = [...trail, node.title];
      if (node.children.length) walk(node.children, next);
      else
        out.push({
          path: node.path,
          title: node.title,
          trail: next.join(" / "),
        });
    }
  };
  walk(props.navigation, []);
  return out;
});

const searchOpen = ref(false);
const keyword = ref("");
const activeIndex = ref(0);
const searchInput = ref<HTMLInputElement | null>(null);
const searchTrigger = ref<HTMLButtonElement | null>(null);
const results = computed(() => {
  const q = keyword.value.trim().toLowerCase();
  const list = q
    ? entries.value.filter(
        (e) =>
          e.title.toLowerCase().includes(q) ||
          e.trail.toLowerCase().includes(q),
      )
    : entries.value;
  return list.slice(0, 50);
});
watch(results, () => {
  activeIndex.value = 0;
});

async function openSearch() {
  searchOpen.value = true;
  keyword.value = "";
  activeIndex.value = 0;
  await nextTick();
  searchInput.value?.focus();
}
function closeSearch() {
  if (!searchOpen.value) return;
  searchOpen.value = false;
  nextTick(() => searchTrigger.value?.focus());
}
function pickResult(path: string) {
  searchOpen.value = false;
  emit("navigate", path);
}
function searchKey(event: KeyboardEvent) {
  if (event.key === "ArrowDown") {
    event.preventDefault();
    activeIndex.value = Math.min(
      activeIndex.value + 1,
      results.value.length - 1,
    );
  } else if (event.key === "ArrowUp") {
    event.preventDefault();
    activeIndex.value = Math.max(activeIndex.value - 1, 0);
  } else if (event.key === "Enter") {
    event.preventDefault();
    const hit = results.value[activeIndex.value];
    if (hit) pickResult(hit.path);
  } else if (event.key === "Escape") {
    event.preventDefault();
    closeSearch();
  }
}

const avatarOpen = ref(false);
const avatarButton = ref<HTMLButtonElement | null>(null);
const avatarMenu = ref<HTMLElement | null>(null);
function toggleAvatar() {
  avatarOpen.value = !avatarOpen.value;
}
function closeAvatar() {
  avatarOpen.value = false;
}

function toggleFullscreen() {
  const target = document.documentElement;
  if (!document.fullscreenElement)
    void target.requestFullscreen?.().catch(() => {});
  else void document.exitFullscreen?.().catch(() => {});
}

function onDocumentPointer(event: PointerEvent) {
  const node = event.target as Node;
  if (
    avatarOpen.value &&
    !avatarButton.value?.contains(node) &&
    !avatarMenu.value?.contains(node)
  )
    avatarOpen.value = false;
}
function onKeydown(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    if (searchOpen.value) closeSearch();
    else void openSearch();
  } else if (event.key === "Escape" && avatarOpen.value) {
    avatarOpen.value = false;
    avatarButton.value?.focus();
  }
}
onMounted(() => {
  window.addEventListener("keydown", onKeydown);
  document.addEventListener("pointerdown", onDocumentPointer, true);
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  document.removeEventListener("pointerdown", onDocumentPointer, true);
});
</script>
<template>
  <div class="topbar-tools">
    <div class="topbar-search">
      <button
        ref="searchTrigger"
        type="button"
        class="search-trigger"
        aria-haspopup="menu"
        :aria-expanded="searchOpen"
        @click="searchOpen ? closeSearch() : openSearch()"
      >
        <ShellIcon name="search" /><span class="search-trigger-label">搜索</span
        ><span class="search-kbd">Ctrl K</span>
      </button>
      <div
        v-if="searchOpen"
        class="search-scrim"
        @pointerdown.self="closeSearch"
      >
        <div
          class="search-menu"
          role="dialog"
          aria-modal="true"
          aria-label="菜单搜索"
        >
          <div class="search-field">
            <ShellIcon name="search" /><input
              ref="searchInput"
              v-model="keyword"
              type="text"
              class="search-input"
              placeholder="搜索菜单"
              aria-label="搜索菜单"
              role="combobox"
              aria-expanded="true"
              aria-controls="topbar-search-list"
              @keydown="searchKey"
            /><span class="search-kbd">Esc</span>
          </div>
          <ul
            v-if="results.length"
            id="topbar-search-list"
            class="search-list"
            role="listbox"
          >
            <li
              v-for="(entry, index) in results"
              :key="entry.path"
              class="search-item"
              :class="{ active: index === activeIndex }"
              role="option"
              :aria-selected="index === activeIndex"
              @mouseenter="activeIndex = index"
              @click="pickResult(entry.path)"
            >
              <span class="search-item-title">{{ entry.title }}</span>
              <span class="search-item-trail">{{ entry.trail }}</span>
            </li>
          </ul>
          <p v-else class="search-empty">未找到相关菜单</p>
        </div>
      </div>
    </div>
    <button
      type="button"
      class="icon-button topbar-tool"
      aria-label="全屏"
      title="全屏"
      @click="toggleFullscreen"
    >
      <ShellIcon name="fullscreen" />
    </button>
    <button
      type="button"
      class="icon-button topbar-tool theme-toggle"
      :aria-label="theme === 'dark' ? '切换到浅色主题' : '切换到深色主题'"
      :title="theme === 'dark' ? '切换到浅色主题' : '切换到深色主题'"
      @click="emit('toggle-theme')"
    >
      <ShellIcon :name="theme === 'dark' ? 'sun' : 'moon'" />
    </button>
    <div class="topbar-avatar">
      <button
        ref="avatarButton"
        type="button"
        class="avatar"
        aria-haspopup="menu"
        :aria-expanded="avatarOpen"
        :aria-label="displayName"
        :title="displayName"
        @click="toggleAvatar"
      >
        {{ avatarLetter }}
      </button>
      <div v-if="avatarOpen" ref="avatarMenu" class="avatar-menu" role="menu">
        <p class="avatar-name">{{ displayName }}</p>
        <button
          type="button"
          class="avatar-item"
          role="menuitem"
          @click="
            closeAvatar();
            emit('account');
          "
        >
          <ShellIcon name="shield" />账户安全
        </button>
        <button
          type="button"
          class="avatar-item avatar-logout"
          role="menuitem"
          :disabled="logoutBusy"
          @click="
            closeAvatar();
            emit('logout');
          "
        >
          <ShellIcon name="st-login" />退出登录
        </button>
      </div>
    </div>
  </div>
</template>
