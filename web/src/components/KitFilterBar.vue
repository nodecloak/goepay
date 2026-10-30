<script setup lang="ts">
import { ref, useSlots } from "vue";
defineProps<{ loading?: boolean; blocked?: boolean }>();
const emit = defineEmits<{ query: []; reset: [] }>();
const slots = useSlots();
const hasExtra = () => !!slots.extra;
const expanded = ref(false);
</script>
<template>
  <form
    class="pa-filter"
    @submit.prevent="emit('query')"
    @reset.prevent="emit('reset')"
  >
    <div class="pa-filter-fields">
      <slot />
      <template v-if="expanded"><slot name="extra" /></template>
    </div>
    <div class="pa-filter-actions">
      <button type="reset" class="pa-btn">重置</button>
      <button
        type="submit"
        class="pa-btn pa-btn--primary"
        :class="{ 'pa-btn--loading': loading }"
        :disabled="loading || blocked"
      >
        查询
      </button>
      <button
        v-if="hasExtra()"
        type="button"
        class="pa-btn pa-btn--text pa-filter-toggle"
        :aria-expanded="expanded"
        @click="expanded = !expanded"
      >
        {{ expanded ? "收起" : "展开"
        }}<i class="pa-icon pa-filter-chev" aria-hidden="true"></i>
      </button>
    </div>
  </form>
</template>
