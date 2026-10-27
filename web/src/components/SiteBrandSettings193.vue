<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { ApiClient } from "../api";
import {
  readBrand193,
  uploadBrand193,
  refreshBrand193,
  type Brand193,
} from "../site-brand193";
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
}>();
const emit = defineEmits<{ busy: [value: boolean] }>();
const state = ref<Brand193 | null>(null),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.ownSession,
);
let epoch = 0,
  alive = true,
  controller: AbortController | undefined;
function valid(run: number, signal: AbortSignal) {
  return alive && run === epoch && !signal.aborted && allowed.value;
}
async function load() {
  if (!allowed.value || busy.value || loading.value) return;
  const run = ++epoch,
    ac = new AbortController();
  controller?.abort();
  controller = ac;
  state.value = null;
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = readBrand193(
      await props.client.request("/settings/branding", { signal: ac.signal }),
    );
    if (valid(run, ac.signal)) state.value = v;
  } catch {
    if (valid(run, ac.signal)) error.value = "品牌图片设置无法确认，请重新读取";
  } finally {
    if (valid(run, ac.signal)) loading.value = false;
  }
}
async function upload(slot: "logo" | "icon", event: Event) {
  const input = event.target as HTMLInputElement,
    file = input.files?.[0];
  if (!file || !state.value || !allowed.value || busy.value) return;
  const before = state.value,
    run = ++epoch,
    ac = new AbortController();
  controller?.abort();
  controller = ac;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = await uploadBrand193(
      props.client,
      slot,
      file,
      before.fingerprint,
      ac.signal,
    );
    if (!valid(run, ac.signal)) return;
    if (
      v.fingerprint === before.fingerprint ||
      v[slot] === null ||
      (slot === "logo"
        ? v.icon?.id !== before.icon?.id
        : v.logo?.id !== before.logo?.id)
    )
      throw new Error("上传结果无法确认，请重新读取");
    state.value = v;
    notice.value = (slot === "logo" ? "首页LOGO" : "全局LOGO") + "上传成功";
    refreshBrand193();
  } catch (e) {
    if (valid(run, ac.signal)) {
      state.value = null;
      error.value =
        e instanceof Error ? e.message : "上传结果无法确认，请重新读取";
    }
  } finally {
    input.value = "";
    if (valid(run, ac.signal)) busy.value = false;
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.adminClass,
    props.ownSession,
  ],
  () => {
    epoch++;
    controller?.abort();
    state.value = null;
    loading.value = false;
    busy.value = false;
    error.value = "";
    notice.value = "";
    void load();
  },
  { immediate: true, flush: "sync" },
);
watch(busy, (v) => emit("busy", v), { flush: "sync" });
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  emit("busy", false);
});
</script>
<template>
  <section class="site-brand-settings193" aria-label="网站图片设置">
    <p v-if="!allowed" class="muted">图片修改需要超级管理员本人会话。</p>
    <template v-else>
      <button :disabled="loading || busy" @click="load">
        重新读取品牌图片
      </button>
      <p v-if="loading" role="status">正在读取品牌图片…</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <div v-if="state" class="brand-slots193">
        <div
          v-for="slot in ['logo', 'icon'] as const"
          :key="slot"
          class="brand-slot193"
        >
          <label :for="'brand193-' + slot">{{
            slot === "logo" ? "首页LOGO" : "全局LOGO"
          }}</label>
          <img
            v-if="state[slot]"
            :key="slot + state.revision"
            :src="'/static/img/' + slot + '.png'"
            :alt="slot === 'logo' ? '当前首页LOGO' : '当前全局LOGO'"
          />
          <p v-else class="muted">尚未上传</p>
          <input
            :id="'brand193-' + slot"
            type="file"
            accept="image/png,image/jpeg,image/jpg,image/gif"
            :disabled="busy || loading"
            @change="upload(slot, $event)"
          />
        </div>
      </div>
      <p class="muted">
        支持小于2MB的PNG、JPEG、GIF图片，最多100万像素。GIF使用静态首帧。选择后立即上传，首页与全局图片分别保存。
      </p>
    </template>
  </section>
</template>
<style scoped>
.site-brand-settings193 {
  padding: 16px 0;
  border-bottom: 1px solid #ebeef5;
  margin-bottom: 20px;
}
.brand-slots193 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 24px;
  margin: 20px 0;
}
.brand-slot193 {
  min-width: 0;
}
.brand-slot193 label {
  display: block;
  font-size: 14px;
  margin-bottom: 14px;
}
.brand-slot193 img {
  display: block;
  max-width: 100%;
  width: auto;
  height: 60px;
  object-fit: contain;
  margin: 14px 0;
}
.brand-slot193 input {
  display: block;
  box-sizing: border-box;
  max-width: 100%;
  font-size: 13px;
}
.site-brand-settings193 .muted {
  font-size: 13px;
  line-height: 1.7;
}
@media (max-width: 600px) {
  .brand-slots193 {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
