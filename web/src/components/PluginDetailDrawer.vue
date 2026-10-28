<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import ShellIcon from "./ShellIcon.vue";
import {
  builtinPlugin,
  pluginTypes,
  priceLabel,
  templateKinds,
  type CatalogEntry,
} from "../plugin-catalog";
const props = defineProps<{ entry: CatalogEntry; title: string }>();
const emit = defineEmits<{ close: [] }>();
const drawer = ref<HTMLElement | null>(null);
const builtin = computed(() => builtinPlugin(props.entry));
const typeLabel = computed(
  () =>
    (props.entry.type === "template" && props.entry.sub
      ? templateKinds[props.entry.sub]
      : undefined) ||
    pluginTypes.find((t) => t[0] === props.entry.type)?.[1] ||
    props.entry.type,
);
onMounted(() => drawer.value?.focus());
</script>
<template>
  <div
    class="modal-backdrop drawer-backdrop"
    @keydown.esc="emit('close')"
    @click.self="emit('close')"
  >
    <section
      ref="drawer"
      class="drawer plugin-drawer"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
      tabindex="-1"
    >
      <header class="drawer-head">
        <h2>{{ title }}</h2>
        <button
          type="button"
          class="icon-button"
          :aria-label="'关闭' + title"
          title="关闭"
          @click="emit('close')"
        >
          <ShellIcon name="close" />
        </button>
      </header>
      <div class="drawer-body">
        <div class="plugin-drawer-title">
          <img v-if="entry.icon" :src="entry.icon" alt="" />
          <span v-else class="plugin-mark" aria-hidden="true">{{
            Array.from(entry.title)[0]
          }}</span>
          <div>
            <strong>{{ entry.title }}</strong>
            <span class="badge" :data-state="builtin ? 'succeeded' : 'none'">{{
              builtin ? "已内置" : "暂不支持"
            }}</span>
          </div>
        </div>
        <dl class="record-detail">
          <dt>标识</dt>
          <dd class="numeric">{{ entry.name }}</dd>
          <dt>类型</dt>
          <dd>{{ typeLabel }}</dd>
          <dt>开发商</dt>
          <dd>{{ entry.author || "-" }}</dd>
          <dt>原版版本</dt>
          <dd>{{ entry.version || "-" }}</dd>
          <dt>原版售价</dt>
          <dd>{{ priceLabel(entry.price) }}</dd>
          <template v-if="entry.type === 'payment'">
            <dt>包含的支付方式</dt>
            <dd>{{ entry.pay.join(",") || "-" }}</dd>
            <dt>包含的转账方式</dt>
            <dd>{{ entry.trans.join(",") || "-" }}</dd>
          </template>
          <dt>简介</dt>
          <dd>{{ entry.desc || "-" }}</dd>
          <template v-if="entry.link">
            <dt>链接</dt>
            <dd>
              <a :href="entry.link" target="_blank" rel="noreferrer noopener">{{
                entry.link
              }}</a>
            </dd>
          </template>
          <dt>Go 版状态</dt>
          <dd>
            {{
              builtin
                ? builtin.note
                : "Go 版尚未实现该插件。原 PHP 插件不能在 Go 服务中运行，需要按原接口移植后才能使用。"
            }}
          </dd>
        </dl>
      </div>
      <div class="modal-actions drawer-foot">
        <button type="button" @click="emit('close')">关闭</button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.plugin-drawer-title {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}
.plugin-drawer-title img,
.plugin-drawer-title .plugin-mark {
  width: 40px;
  height: 40px;
  flex: none;
  border-radius: var(--radius-md);
  object-fit: contain;
}
.plugin-drawer-title > div {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 15px;
}
</style>
