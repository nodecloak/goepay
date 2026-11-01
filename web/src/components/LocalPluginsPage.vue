<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import ShellIcon from "./ShellIcon.vue";
import PluginDetailDrawer from "./PluginDetailDrawer.vue";
import KitPagination from "./KitPagination.vue";
import {
  catalogKey,
  loadPluginCatalog,
  localPlugins,
  pluginTypes,
  type CatalogEntry,
  type PluginType,
} from "../plugin-catalog";
// The original page has no template tab; templates live in 模板配置.
const tabs = pluginTypes.filter((t) => t[0] !== "template");
const items = ref<CatalogEntry[]>([]),
  loading = ref(true),
  error = ref(""),
  type = ref<PluginType>("payment"),
  query = ref(""),
  applied = ref(""),
  selected = ref<CatalogEntry | null>(null),
  page = ref(1),
  pageSize = ref(20);
const rows = computed(() =>
  localPlugins(items.value, type.value, applied.value),
);
const pageRows = computed(() =>
  rows.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value,
  ),
);
watch([type, applied], () => (page.value = 1));
const emit = defineEmits<{ loaded: [] }>();
let catalogPending = false;
async function reloadCatalog() {
  if (catalogPending) return;
  catalogPending = true;
  loading.value = true;
  error.value = "";
  selected.value = null;
  try {
    items.value = await loadPluginCatalog();
    page.value = Math.min(
      page.value,
      Math.max(1, Math.ceil(rows.value.length / pageSize.value)),
    );
  } catch {
    items.value = [];
    page.value = 1;
    error.value = "插件列表读取失败，请重试刷新列表";
  } finally {
    catalogPending = false;
    loading.value = false;
    emit("loaded");
  }
}
onMounted(reloadCatalog);
function reset() {
  query.value = "";
  applied.value = "";
}
</script>
<template>
  <div class="list-page">
    <form
      class="plugin-search list-filter-card panel"
      role="search"
      @submit.prevent="applied = query"
    >
      <input
        v-model="query"
        aria-label="搜索插件名称或标识"
        placeholder="搜索插件名称 / 标识"
        maxlength="64"
      /><button class="primary" :disabled="loading">搜索</button
      ><button type="button" :disabled="loading" @click="reset">重置</button>
    </form>
    <section class="panel records plugin-page" aria-label="本地插件">
      <div class="table-tools">
        <div class="pill-tabs" role="tablist" aria-label="插件分类">
          <button
            v-for="t in tabs"
            :key="t[0]"
            type="button"
            role="tab"
            :aria-selected="type === t[0]"
            @click="type = t[0]"
          >
            {{ t[1] }}
          </button>
        </div>
        <div class="table-tools-end">
          <button
            type="button"
            class="pa-btn"
            :disabled="loading"
            @click="reloadCatalog"
          >
            <ShellIcon name="refresh" />刷新本地插件列表
          </button>
        </div>
      </div>
      <p v-if="loading" class="loading-row" role="status">正在读取插件列表…</p>
      <p v-else-if="error" class="alert error" role="alert">{{ error }}</p>
      <div v-else class="table-scroll">
        <table>
          <thead>
            <tr>
              <th scope="col">插件名称</th>
              <th scope="col">插件标识</th>
              <th scope="col">链接</th>
              <th scope="col">包含的支付方式</th>
              <th scope="col">包含的转账方式</th>
              <th scope="col">开发商</th>
              <th scope="col">版本号</th>
              <th scope="col" class="plugin-actions">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pageRows" :key="catalogKey(item)">
              <td>
                <span class="plugin-name"
                  ><img
                    v-if="item.icon"
                    :src="item.icon"
                    alt=""
                    loading="lazy"
                  />
                  <span v-else class="plugin-mark" aria-hidden="true">{{
                    Array.from(item.title)[0]
                  }}</span
                  >{{ item.title }}</span
                >
              </td>
              <td class="numeric">{{ item.name }}</td>
              <td>
                <a
                  v-if="item.link"
                  class="plugin-link"
                  :href="item.link"
                  target="_blank"
                  rel="noreferrer noopener"
                  :aria-label="item.title + '官网'"
                  :title="item.link"
                  ><ShellIcon name="link"
                /></a>
                <span v-else>-</span>
              </td>
              <td>{{ item.pay.join(",") || "-" }}</td>
              <td>{{ item.trans.join(",") || "-" }}</td>
              <td>{{ item.author || "-" }}</td>
              <td>内置</td>
              <td class="plugin-actions">
                <button
                  type="button"
                  class="link-button"
                  :aria-label="'插件详情 ' + item.title"
                  @click="selected = item"
                >
                  详情
                </button>
              </td>
            </tr>
            <tr v-if="!rows.length">
              <td colspan="8" class="empty">暂无数据</td>
            </tr>
          </tbody>
        </table>
      </div>
      <KitPagination
        v-if="!loading && !error && rows.length"
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="rows.length"
      />
    </section>
    <PluginDetailDrawer
      v-if="selected"
      :entry="selected"
      title="插件详情"
      @close="selected = null"
    />
  </div>
</template>
<style scoped>
/* The search card is a standard filter card; keep the input flexible and the
   two buttons at its natural width. */
.plugin-search {
  display: flex;
  align-items: center;
  gap: 8px;
}
.plugin-search input {
  flex: 1;
  max-width: 440px;
}
/* The category pills sit on the card's top edge inside the toolbar; the
   toolbar already supplies the spacing below. */
.table-tools .pill-tabs {
  margin: 0;
}
.plugin-actions {
  white-space: nowrap;
  text-align: right;
}
.plugin-link {
  display: inline-grid;
  place-content: center;
  width: 26px;
  height: 26px;
  border: 1px solid var(--line-control);
  border-radius: var(--radius-sm);
  color: var(--ink-muted);
}
.plugin-link:hover {
  color: var(--primary-text);
  border-color: var(--primary-text);
}
.plugin-link .shell-icon {
  width: 13px;
  height: 13px;
}
</style>
