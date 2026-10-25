<script setup lang="ts">
import { ref, watch } from "vue";
import type { MenuNode } from "../menu";
import ShellIcon from "./ShellIcon.vue";
const props = withDefaults(
  defineProps<{
    nodes: MenuNode[];
    area: string;
    active: string;
    collapsed?: boolean;
    level?: number;
  }>(),
  { collapsed: false, level: 0 },
);
const emit = defineEmits<{ expand: [] }>();
const opened = ref("");
function contains(node: MenuNode): boolean {
  return node.path === props.active || node.children.some(contains);
}
watch(
  () => [props.active, props.nodes],
  () => {
    opened.value =
      props.nodes.find((node) => node.children.length && contains(node))?.id ||
      "";
  },
  { immediate: true },
);
function toggle(id: string) {
  if (props.collapsed) {
    emit("expand");
    opened.value = id;
    return;
  }
  opened.value = opened.value === id ? "" : id;
}
</script>
<template>
  <ul class="menu" :data-level="level">
    <li v-for="node in nodes" :key="node.id">
      <details v-if="node.children.length" :open="opened === node.id">
        <summary
          :class="{ 'menu-group-active': contains(node) }"
          :title="collapsed ? node.title : undefined"
          @click.prevent="toggle(node.id)"
        >
          <ShellIcon v-if="level === 0" :name="node.id" /><span
            class="menu-title"
            >{{ node.title }}</span
          ><ShellIcon class="menu-chevron" name="chevron" />
        </summary>
        <MenuTree
          :nodes="node.children"
          :area="area"
          :active="active"
          :level="level + 1"
          @expand="emit('expand')"
        />
      </details>
      <RouterLink
        v-else
        :to="'/' + area + node.path"
        :aria-current="active === node.path ? 'page' : undefined"
        :title="collapsed ? node.title : undefined"
        ><ShellIcon v-if="level === 0" :name="node.id" /><span
          v-else
          class="menu-child-dot"
          aria-hidden="true"
        ></span
        ><span class="menu-title">{{ node.title }}</span></RouterLink
      >
    </li>
  </ul>
</template>
