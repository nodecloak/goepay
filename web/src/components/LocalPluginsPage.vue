<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import ShellIcon from "./ShellIcon.vue";
import PluginDetailDrawer from "./PluginDetailDrawer.vue";
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
  selected = ref<CatalogEntry | null>(null);
const rows = computed(() =>
  localPlugins(items.value, type.value, applied.value),
);
onMounted(async () => {
  try {
    items.value = await loadPluginCatalog();
  } catch {
    error.value = "插件列表读取失败，请刷新页面重试";
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
  <section class="panel plugin-page" aria-label="本地插件">
    <form class="plugin-search" role="search" @submit.prevent="applied = query">
      <input
        v-model="query"
        aria-label="搜索插件名称或标识"
        placeholder="搜索插件名称 / 标识"
        maxlength="64"
      /><button class="primary" :disabled="loading">搜索</button
      ><button type="button" :disabled="loading" @click="reset">重置</button>
    </form>
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
          <tr v-for="item in rows" :key="catalogKey(item)">
            <td>
              <span class="plugin-name"
                ><img v-if="item.icon" :src="item.icon" alt="" loading="lazy" />
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
    <p class="plugin-note muted">
      <ShellIcon name="info" />Go 版插件随服务一起发布，不从 /plugins/payment/
      目录加载 PHP 源码。其它插件需要在 Go
      服务中实现后才会出现在这里，原应用市场列表见“应用市场”。
    </p>
    <PluginDetailDrawer
      v-if="selected"
      :entry="selected"
      title="插件详情"
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
.plugin-note {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin: 16px 0 0;
  padding: 10px 14px;
  border-radius: var(--radius-md);
  background: var(--bg-raised);
  font-size: 12px;
}
.plugin-note .shell-icon {
  width: 14px;
  height: 14px;
  flex: none;
  margin-top: 3px;
}
</style>
