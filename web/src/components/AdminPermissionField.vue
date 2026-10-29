<script setup lang="ts">
// AdminPermissionField — per-menu permission tree for the admin-user editor
// (migration 000132). Renders the grantable menu catalog (served in the session
// payload as admin_permission_catalog) grouped by its parent menu, as a
// checkbox tree. An empty selection means an unrestricted operator; the server
// is the enforcement authority. Shown only when admin_class is operator.
import { computed } from "vue";
import KitCheckbox from "./KitCheckbox.vue";

export interface PermissionCatalogEntry {
  key: string;
  title: string;
  group: string;
}

const props = defineProps<{
  catalog: PermissionCatalogEntry[];
  value: string[];
  disabled?: boolean;
}>();
const emit = defineEmits<{ change: [keys: string[]] }>();

const groups = computed(() => {
  const order: string[] = [];
  const byGroup: Record<string, PermissionCatalogEntry[]> = {};
  for (const entry of props.catalog) {
    if (!byGroup[entry.group]) {
      byGroup[entry.group] = [];
      order.push(entry.group);
    }
    byGroup[entry.group].push(entry);
  }
  return order.map((group) => ({ group, items: byGroup[group] }));
});

const selected = computed(() => new Set(props.value));

function toggle(key: string, on: boolean) {
  const next = new Set(props.value);
  if (on) next.add(key);
  else next.delete(key);
  emit(
    "change",
    props.catalog.map((e) => e.key).filter((k) => next.has(k)),
  );
}

function toggleGroup(items: PermissionCatalogEntry[], on: boolean) {
  const next = new Set(props.value);
  for (const item of items) {
    if (on) next.add(item.key);
    else next.delete(item.key);
  }
  emit(
    "change",
    props.catalog.map((e) => e.key).filter((k) => next.has(k)),
  );
}

const groupAllSelected = (items: PermissionCatalogEntry[]) =>
  items.every((i) => selected.value.has(i.key));
const groupPartial = (items: PermissionCatalogEntry[]) =>
  !groupAllSelected(items) && items.some((i) => selected.value.has(i.key));
</script>
<template>
  <div class="admin-permission-tree">
    <p class="admin-permission-hint">
      留空表示该管理员可访问全部业务菜单；勾选后仅限所选菜单。
    </p>
    <div v-for="g in groups" :key="g.group" class="admin-permission-group">
      <KitCheckbox
        class="admin-permission-group-head"
        :model-value="groupAllSelected(g.items)"
        :indeterminate="groupPartial(g.items)"
        :disabled="disabled"
        :label="g.group"
        @change="toggleGroup(g.items, $event)"
      />
      <div class="admin-permission-items">
        <KitCheckbox
          v-for="item in g.items"
          :key="item.key"
          class="admin-permission-item"
          :model-value="selected.has(item.key)"
          :disabled="disabled"
          :label="item.title"
          @change="toggle(item.key, $event)"
        />
      </div>
    </div>
  </div>
</template>
