<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import ShellIcon from "./ShellIcon.vue";
import PluginDetailDrawer from "./PluginDetailDrawer.vue";
import {
  builtinCount,
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
  selected = ref<CatalogEntry | null>(null);
const rows = computed(() =>
  filterCatalog(items.value, type.value, applied.value),
);
const builtin = computed(() => builtinCount(items.value));
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
    <p class="alert plugin-notice" role="note">
      <ShellIcon name="info" /><span
        >Go 版插件随服务内置，不能在线购买、下载或安装 PHP
        插件。下表是原应用市场的插件列表{{
          loading ? "" : `，已内置 ${builtin} 个，其余暂不支持`
        }}。</span
      >
    </p>
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
          <tr v-for="item in rows" :key="catalogKey(item)">
            <td>
              <span class="plugin-name"
                ><img v-if="item.icon" :src="item.icon" alt="" loading="lazy" />
                <span v-else class="plugin-mark" aria-hidden="true">{{
                  Array.from(item.title)[0]
                }}</span
                >{{ item.title
                }}<span
                  v-if="item.type === 'template' && item.sub"
                  class="badge"
                  data-state="none"
                  >{{ templateKinds[item.sub] || item.sub }}</span
                ></span
              >
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
              ><span
                class="badge"
                :data-state="builtinPlugin(item) ? 'succeeded' : 'none'"
                >{{ builtinPlugin(item) ? "已内置" : "暂不支持" }}</span
              >
            </td>
          </tr>
          <tr v-if="!rows.length">
            <td colspan="7" class="empty">暂无数据</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="!loading && !error" class="plugin-foot">共 {{ rows.length }} 个</p>
    <PluginDetailDrawer
      v-if="selected"
      :entry="selected"
      title="应用详情"
      @close="selected = null"
    />
  </section>
</template>
<style scoped>
.plugin-notice {
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--warning-soft);
  color: var(--ink);
}
.plugin-notice .shell-icon {
  width: 16px;
  height: 16px;
  flex: none;
  color: var(--warning-text);
}
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
.plugin-foot {
  margin: 12px 0 0;
  font-size: 12px;
  color: var(--ink-muted);
}
</style>
