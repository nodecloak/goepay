<script setup lang="ts">
import { ref, computed, onBeforeUnmount } from "vue";
const props = defineProps<{
  refresh: () => Promise<void>;
  blocked?: boolean;
}>();
const expanded = ref(false),
  active = ref(false),
  seconds = ref("10"),
  interval = ref(10),
  error = ref(""),
  visible = ref(document.visibilityState !== "hidden");
const paused = computed(
  () => active.value && (!visible.value || props.blocked),
);
let alive = true,
  timer: ReturnType<typeof setTimeout> | undefined,
  refreshing = false;
function stop() {
  active.value = false;
  if (timer) clearTimeout(timer);
  timer = undefined;
}
function schedule() {
  if (timer) clearTimeout(timer);
  if (!alive || !active.value) return;
  timer = setTimeout(async () => {
    if (
      alive &&
      active.value &&
      visible.value &&
      !props.blocked &&
      !refreshing
    ) {
      refreshing = true;
      try {
        await props.refresh();
      } finally {
        refreshing = false;
      }
    }
    schedule();
  }, interval.value * 1000);
}
function start() {
  error.value = "";
  if (
    !/^[1-9][0-9]{0,3}$/.test(seconds.value) ||
    Number(seconds.value) < 3 ||
    Number(seconds.value) > 3600
  ) {
    error.value = "刷新间隔不能小于3秒，最大3600秒";
    return;
  }
  interval.value = Number(seconds.value);
  active.value = true;
  schedule();
}
function visibility() {
  visible.value = document.visibilityState !== "hidden";
}
document.addEventListener("visibilitychange", visibility);
onBeforeUnmount(() => {
  alive = false;
  stop();
  document.removeEventListener("visibilitychange", visibility);
});
</script>
<template>
  <div class="auto-refresh">
    <button
      type="button"
      :aria-expanded="expanded"
      @click="expanded = !expanded"
    >
      自动刷新</button
    ><span v-if="active" class="muted" role="status">{{
      paused ? "已暂停" : "每 " + interval + " 秒刷新"
    }}</span>
    <div v-if="expanded" class="auto-options">
      <label
        >刷新间隔（秒）<input
          v-model="seconds"
          inputmode="numeric"
          type="text"
          maxlength="4"
          @keydown.enter.prevent="start" /></label
      ><button type="button" @click="start">
        {{ active ? "更新刷新间隔" : "开始自动刷新" }}</button
      ><button v-if="active" type="button" @click="stop">停止自动刷新</button
      ><span v-if="error" class="error" role="alert">{{ error }}</span>
    </div>
  </div>
</template>
<style scoped>
.auto-refresh,
.auto-options {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
  min-width: 0;
}
.auto-options {
  flex-basis: 100%;
}
.auto-options label {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
}
.auto-options input {
  width: 72px;
  min-width: 0;
}
.auto-refresh {
  max-width: 100%;
}
</style>
