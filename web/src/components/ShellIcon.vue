<script setup lang="ts">
import { computed } from "vue";
const props = defineProps<{ name?: string }>();
const paths: Record<string, string> = {
  menu: "M4 6h16M4 12h16M4 18h16",
  fold: "M9 4v16M4 4h16v16H4zM16 9l-3 3 3 3",
  unfold: "M9 4v16M4 4h16v16H4zM13 9l3 3-3 3",
  home: "m3 11 9-8 9 8M5 10v11h5v-7h4v7h5V10",
  trade: "M6 3h12v18l-3-2-3 2-3-2-3 2V3zM9 7h6M9 11h6M9 15h3",
  people:
    "M16 21v-3a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4v3M16 4a4 4 0 0 1 0 8M21 21v-3a4 4 0 0 0-3-3.87M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0z",
  wallet: "M20 8V5H5a2 2 0 0 1 0-4h13v4M5 5H3v15h18V8H5M16 12h5v5h-5z",
  chart: "M4 3v18h17M8 17v-5M13 17V7M18 17V3",
  settings:
    "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8zM9 3l-.5 3-3 1L3 6l-2 4 2.5 2L3 15l2 3 3-.5L10 21h4l2-3.5 3 .5 2-3-.5-3L23 10l-2-4-2.5 1-3-1L15 3z",
  grid: "M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z",
  shield: "M12 2 3 6v6c0 5 9 10 9 10s9-5 9-10V6zM8 12l3 3 5-6",
  link: "M10 13a5 5 0 0 0 7 0l4-4a5 5 0 0 0-7-7l-2 2M14 11a5 5 0 0 0-7 0l-4 4a5 5 0 0 0 7 7l2-2",
  close: "m6 6 12 12M6 18 18 6",
  chevron: "m9 5 7 7-7 7",
};
const path = computed(() => {
  const name = props.name || "grid";
  if (paths[name]) return paths[name];
  if (/dashboard|home/i.test(name)) return paths.home;
  if (/trade|order|refund/i.test(name)) return paths.trade;
  if (/merchant|user|agent|invite/i.test(name)) return paths.people;
  if (/settle|transfer|balance|money/i.test(name)) return paths.wallet;
  if (/stat|report|ledger/i.test(name)) return paths.chart;
  if (/system|config|setting/i.test(name)) return paths.settings;
  if (/account|security/i.test(name)) return paths.shield;
  if (/channel|link/i.test(name)) return paths.link;
  return paths.grid;
});
</script>
<template>
  <svg
    class="shell-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path :d="path" />
  </svg>
</template>
