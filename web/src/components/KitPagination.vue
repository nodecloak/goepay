<script setup lang="ts">
import { computed, ref, watch } from "vue";
import KitSelect from "./KitSelect.vue";
const props = defineProps<{
  page: number;
  pageSize: number;
  total: number;
  pageSizes?: number[];
}>();
const emit = defineEmits<{
  "update:page": [value: number];
  "update:pageSize": [value: number];
  change: [];
}>();
const sizes = computed(() => props.pageSizes ?? [20, 50, 100]);
const pageCount = computed(() =>
  Math.max(1, Math.ceil(props.total / Math.max(1, props.pageSize))),
);
// 1 … current-1 current current+1 … last, collapsing runs to a single ellipsis.
const items = computed<(number | "…")[]>(() => {
  const last = pageCount.value;
  const cur = props.page;
  const set = new Set<number>([1, last, cur, cur - 1, cur + 1]);
  const pages = [...set]
    .filter((p) => p >= 1 && p <= last)
    .sort((a, b) => a - b);
  const out: (number | "…")[] = [];
  let prev = 0;
  for (const p of pages) {
    if (prev && p - prev > 1) out.push("…");
    out.push(p);
    prev = p;
  }
  return out;
});
function goto(p: number) {
  const next = Math.min(pageCount.value, Math.max(1, Math.trunc(p)));
  if (next === props.page) return;
  emit("update:page", next);
  emit("change");
}
function changeSize(value: string) {
  const size = Number(value);
  if (!Number.isFinite(size) || size === props.pageSize) return;
  emit("update:pageSize", size);
  emit("update:page", 1);
  emit("change");
}
const jump = ref("");
watch(
  () => props.page,
  () => (jump.value = ""),
);
function submitJump() {
  const n = Number(jump.value);
  if (Number.isFinite(n) && n >= 1) goto(n);
}
const sizeOptions = computed(() =>
  sizes.value.map((s) => ({ value: String(s), label: `${s} 条/页` })),
);
</script>
<template>
  <div class="pa-pager">
    <span class="pa-pager-total">共 {{ total }} 条</span>
    <KitSelect
      class="pa-pager-size"
      :model-value="String(pageSize)"
      :options="sizeOptions"
      aria-label="每页条数"
      @change="changeSize"
    />
    <button
      type="button"
      class="pa-page"
      :disabled="page <= 1"
      aria-label="上一页"
      @click="goto(page - 1)"
    >
      ‹
    </button>
    <template v-for="(it, i) in items" :key="i">
      <span v-if="it === '…'" class="pa-page pa-page--ellipsis">…</span>
      <button
        v-else
        type="button"
        class="pa-page"
        :aria-current="it === page ? 'page' : undefined"
        @click="goto(it)"
      >
        {{ it }}
      </button>
    </template>
    <button
      type="button"
      class="pa-page"
      :disabled="page >= pageCount"
      aria-label="下一页"
      @click="goto(page + 1)"
    >
      ›
    </button>
    <span class="pa-pager-jump"
      >前往<input
        v-model="jump"
        inputmode="numeric"
        aria-label="前往页码"
        @keydown.enter="submitJump"
        @blur="submitJump"
      />页</span
    >
  </div>
</template>
