<script setup lang="ts">
// 扩展配置 as in the original console: three sections, 已启用的扩展 (green
// count), 未启用的扩展 (orange) and 未安装的扩展 (grey), each with its own empty
// state. GOEPAY extensions are local parameter records, so nothing can be
// "not installed"; that section stays empty, as it does in the original when
// every downloaded extension is installed.
import { computed } from "vue";
import { displayValue } from "../api";
import { consoleValue } from "../pro-console-fields";
import ShellIcon from "./ShellIcon.vue";
import KitStatusTag from "./KitStatusTag.vue";

type Row = Record<string, unknown>;
const props = defineProps<{
  items: Row[];
  fields: { name: string; label: string; type?: string }[];
  entityLabel: string;
  canCreate: boolean;
  canUpdate: boolean;
  canDelete: boolean;
  busy: boolean;
  loading: boolean;
  note?: string;
}>();
defineEmits<{
  create: [];
  refresh: [];
  open: [action: "detail" | "edit" | "delete", row: Row];
}>();
const groups = computed(() => [
  {
    key: "enabled",
    title: "已启用的扩展",
    state: "on",
    empty: "暂无已启用的扩展",
    rows: props.items.filter((r) => r.enabled === true),
  },
  {
    key: "disabled",
    title: "未启用的扩展",
    state: "off",
    empty: "暂无未启用的扩展",
    rows: props.items.filter((r) => r.enabled !== true),
  },
  {
    key: "missing",
    title: "未安装的扩展",
    state: "none",
    empty: "暂无未安装的扩展",
    rows: [] as Row[],
  },
]);
</script>
<template>
  <section
    v-for="g in groups"
    :key="g.key"
    class="panel records addon-group"
    :aria-labelledby="'addon-group-' + g.key"
  >
    <div class="table-tools">
      <h2 :id="'addon-group-' + g.key" class="addon-group-title">
        {{ g.title }}
      </h2>
      <KitStatusTag
        :tone="g.state === 'on' ? 'success' : 'neutral'"
        :text="String(g.rows.length)"
      />
      <div v-if="g.key === 'enabled'" class="table-tools-end">
        <button
          v-if="canCreate"
          type="button"
          class="primary"
          :disabled="busy"
          @click="$emit('create')"
        >
          <ShellIcon name="plus" />新增{{ entityLabel }}
        </button>
        <button
          type="button"
          :disabled="loading || busy"
          @click="$emit('refresh')"
        >
          <ShellIcon name="refresh" />刷新
        </button>
      </div>
    </div>
    <p v-if="note && g.key === 'enabled'" class="table-note">{{ note }}</p>
    <div
      v-if="loading && g.key === 'enabled'"
      class="loading-row"
      role="status"
    >
      正在加载记录…
    </div>
    <div v-else-if="g.rows.length" class="table-scroll">
      <table>
        <thead>
          <tr>
            <th scope="col">编号</th>
            <th v-for="f in fields" :key="f.name" scope="col">
              {{ f.label }}
            </th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in g.rows" :key="String(row.id)">
            <td>{{ row.id }}</td>
            <td v-for="f in fields" :key="f.name">
              <KitStatusTag
                v-if="f.name === 'enabled'"
                :text="consoleValue('enabled', row.enabled)"
                :tone="row.enabled === true ? 'success' : 'neutral'"
              />
              <span v-else class="cell">{{
                f.type === "boolean"
                  ? row[f.name]
                    ? "是"
                    : "否"
                  : displayValue(row[f.name])
              }}</span>
            </td>
            <td>
              <div class="row-actions">
                <button
                  class="link-button"
                  :disabled="busy"
                  @click="$emit('open', 'detail', row)"
                >
                  详情</button
                ><button
                  v-if="canUpdate"
                  class="link-button"
                  :disabled="busy"
                  @click="$emit('open', 'edit', row)"
                >
                  编辑</button
                ><button
                  v-if="canDelete"
                  class="link-button danger-text"
                  :disabled="busy"
                  @click="$emit('open', 'delete', row)"
                >
                  删除
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else class="empty addon-empty">{{ g.empty }}</p>
  </section>
</template>
<style scoped>
.addon-group + .addon-group {
  margin-top: var(--space-4);
}
.addon-group-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}
.addon-empty {
  margin: 0;
  padding: 28px 20px 32px;
  border-top: 1px solid var(--line);
}
</style>
