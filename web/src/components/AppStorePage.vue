<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import PluginDetailDrawer from "./PluginDetailDrawer.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitPagination from "./KitPagination.vue";
import { templatePreviewImage } from "../page-template";
import {
  builtinPlugin,
  catalogKey,
  filterCatalog,
  loadPluginCatalog,
  pluginTypes,
  priceLabel,
  templateKinds,
  type CatalogEntry,
  type PluginType,
} from "../plugin-catalog";
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
  filterCatalog(items.value, type.value, applied.value),
);
const pageRows = computed(() =>
  rows.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value,
  ),
);
watch([type, applied], () => (page.value = 1));
onMounted(async () => {
  try {
    items.value = await loadPluginCatalog();
  } catch {
    error.value = "应用列表读取失败，请刷新页面重试";
  } finally {
    loading.value = false;
  }
});
function reset() {
  query.value = "";
  applied.value = "";
}
</script>
<template>
  <section class="panel plugin-page" aria-label="应用市场">
    <form class="plugin-search" role="search" @submit.prevent="applied = query">
      <input
        v-model="query"
        aria-label="搜索应用名称或标识"
        placeholder="搜索应用名称 / 标识"
        maxlength="64"
      /><button class="primary" :disabled="loading">搜索</button
      ><button type="button" :disabled="loading" @click="reset">重置</button>
    </form>
    <div class="pill-tabs" role="tablist" aria-label="应用分类">
      <button
        v-for="t in pluginTypes"
        :key="t[0]"
        type="button"
        role="tab"
        :aria-selected="type === t[0]"
        @click="type = t[0]"
      >
        {{ t[1] }}
      </button>
    </div>
    <p v-if="loading" class="loading-row" role="status">正在读取应用列表…</p>
    <p v-else-if="error" class="alert error" role="alert">{{ error }}</p>
    <div v-else class="table-scroll">
      <table>
        <thead>
          <tr>
            <th scope="col">应用名称</th>
            <th scope="col">标识</th>
            <th scope="col">简介</th>
            <th scope="col">售价</th>
            <th scope="col">开发商</th>
            <th scope="col">版本</th>
            <th scope="col" class="plugin-actions">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in pageRows" :key="catalogKey(item)">
            <td>
              <span class="plugin-name"
                ><img
                  v-if="item.type === 'template' && item.sub && builtinPlugin(item)"
                  class="plugin-shot"
                  :src="templatePreviewImage(item.sub, item.name)"
                  alt=""
                  loading="lazy"
                /><img
                  v-else-if="item.icon"
                  :src="item.icon"
                  alt=""
                  loading="lazy"
                />
                <span v-else class="plugin-mark" aria-hidden="true">{{
                  Array.from(item.title)[0]
                }}</span
                >{{ item.title
                }}<KitStatusTag
                  v-if="item.type === 'template' && item.sub"
                  tone="neutral"
                  :text="templateKinds[item.sub] || item.sub"
              /></span>
            </td>
            <td class="numeric">{{ item.name }}</td>
            <td class="plugin-desc">{{ item.desc || "-" }}</td>
            <td>
              <span
                :class="Number(item.price) === 0 ? 'price-free' : 'price-paid'"
                >{{ priceLabel(item.price) }}</span
              >
            </td>
            <td>{{ item.author || "-" }}</td>
            <td class="numeric">{{ item.version || "-" }}</td>
            <td class="plugin-actions">
              <button
                type="button"
                class="link-button"
                :aria-label="'应用详情 ' + item.title"
                @click="selected = item"
              >
                详情</button
              ><KitStatusTag
                :tone="builtinPlugin(item) ? 'success' : 'neutral'"
                :text="builtinPlugin(item) ? '已内置' : '暂不支持'"
              />
            </td>
          </tr>
          <tr v-if="!rows.length">
            <td colspan="7" class="empty">暂无数据</td>
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
    <PluginDetailDrawer
      v-if="selected"
      :entry="selected"
      title="应用详情"
      @close="selected = null"
    />
  </section>
</template>
<style scoped>
.plugin-search {
  display: flex;
  gap: 8px;
  max-width: 520px;
  margin-bottom: 16px;
}
.plugin-search input {
  flex: 1;
}
.plugin-desc {
  min-width: 160px;
  max-width: 360px;
  white-space: normal;
  color: var(--ink-muted);
}
.plugin-actions {
  white-space: nowrap;
  text-align: right;
}
.plugin-actions .link-button {
  margin-right: 8px;
}
.price-free {
  color: var(--success-text);
  font-weight: 600;
}
.price-paid {
  color: var(--warning-text);
  font-weight: 600;
}
.plugin-name > img.plugin-shot {
  width: 72px;
  height: 40px;
  object-fit: cover;
  object-position: top;
  border: 1px solid var(--line);
  border-radius: var(--radius-xs);
}
</style>
