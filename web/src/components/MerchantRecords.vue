<script setup lang="ts">
withDefaults(defineProps<{ stacked?: boolean }>(), { stacked: true });
</script>

<template>
  <div
    class="merchant-records"
    :class="{ 'merchant-records--stacked': stacked }"
  >
    <slot />
  </div>
</template>

<style scoped>
.merchant-records {
  min-width: 0;
  max-width: 100%;
  overflow-x: auto;
}
.merchant-records--stacked {
  container: merchant-records / inline-size;
}
@container merchant-records (max-width: 760px) {
  .merchant-records.merchant-records--stacked :deep(> table) {
    display: block;
    width: 100%;
    min-width: 0;
  }
  .merchant-records--stacked :deep(> table > thead) {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
  .merchant-records--stacked :deep(> table > tbody) {
    display: grid;
    gap: var(--space-3);
    padding: var(--space-3);
  }
  .merchant-records--stacked :deep(> table > tbody > tr) {
    display: grid;
    min-width: 0;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: start;
    gap: var(--space-3);
    padding: var(--space-3);
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--bg-card);
  }
  .merchant-records.merchant-records--stacked :deep(> table > tbody > tr > td) {
    display: block;
    min-width: 0;
    max-width: none;
    padding: 0;
    border: 0;
    overflow: visible;
    white-space: normal;
    overflow-wrap: anywhere;
    text-align: start;
  }
  .merchant-records--stacked
    :deep(> table > tbody > tr > td :is(.pa-order-no, .pa-c-mono)) {
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .merchant-records--stacked
    :deep(> table > tbody > tr > td[data-cell-label]::before) {
    content: attr(data-cell-label);
    content: attr(data-cell-label) / "";
    display: block;
    margin-bottom: var(--space-1);
    color: var(--ink-muted);
    font-family: var(--font-sans);
    font-size: 0.75rem;
    line-height: 1.4;
  }
  .merchant-records--stacked
    :deep(> table > tbody > tr > td:is([data-cell-wide], [colspan])) {
    grid-column: 1 / -1;
  }
  .merchant-records--stacked
    :deep(> table > tbody > tr > td[data-cell-actions]) {
    grid-column: 1 / -1;
    border-top: 1px solid var(--line);
    padding-top: var(--space-2);
  }
  .merchant-records--stacked
    :deep(> table > tbody > tr > td[data-cell-actions]:empty) {
    display: none;
  }
  .merchant-records--stacked
    :deep(> table > tbody > tr > td[data-cell-actions] button) {
    min-height: 44px;
    max-width: 100%;
    white-space: normal;
  }
}
</style>
