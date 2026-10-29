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
  // Icons below are the payment-admin UI kit's own line icons.
  sun: "M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0zM12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4",
  moon: "M20 14.5A8 8 0 0 1 9.5 4 8 8 0 1 0 20 14.5z",
  refresh: "M20 12a8 8 0 1 1-2.3-5.7M20 4v4.5h-4.5",
  fullscreen: "M8 4H4v4M16 4h4v4M16 20h4v-4M8 20H4v-4",
  chevronDown: "M6 9l6 6 6-6",
  chevronLeft: "M15 6l-6 6 6 6",
  chevronRight: "M9 6l6 6-6 6",
  more: "M6.3 12a1.3 1.3 0 1 1-2.6 0 1.3 1.3 0 0 1 2.6 0zM13.3 12a1.3 1.3 0 1 1-2.6 0 1.3 1.3 0 0 1 2.6 0zM20.3 12a1.3 1.3 0 1 1-2.6 0 1.3 1.3 0 0 1 2.6 0z",
  export: "M12 4v11M7.5 10.5 12 15l4.5-4.5M4 17v3h16v-3",
  search: "M17.5 11a6.5 6.5 0 1 1-13 0 6.5 6.5 0 0 1 13 0zM16 16l4.5 4.5",
  filter: "M4 5h16l-6 7.5V19l-4 2v-8.5z",
  info: "M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0zM12 11v5.5M12 7.5v.5",
  plus: "M12 5v14M5 12h14",
  // Settings menu icons.
  "st-globe":
    "M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0zM3 12h18M12 3c2.5 2.6 3.8 5.6 3.8 9s-1.3 6.4-3.8 9M12 3c-2.5 2.6-3.8 5.6-3.8 9s1.3 6.4 3.8 9",
  "st-store": "M4 9h16l-1.6-5H5.6zM5 9v11h14V9M9.5 20v-5.5h5V20",
  "st-card": "M3 6h18v12H3zM3 10h18M7 15h4",
  "st-alert": "M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0zM12 7.5V13M12 16.5v.5",
  "st-coin":
    "M20.5 12a8.5 8.5 0 1 1-17 0 8.5 8.5 0 0 1 17 0zM9 7.5l3 4 3-4M12 11.5V17M9 12.5h6M9 15h6",
  "st-transfer": "M4 8h15l-3.5-3.5M20 16H5l3.5 3.5",
  "st-login": "M10 4h9v16h-9M4 12h11M11.5 8.5 15 12l-3.5 3.5",
  "st-bell": "M6 16v-5a6 6 0 1 1 12 0v5l1.5 2h-15zM10 20.5a2 2 0 0 0 4 0",
  "st-badge": "M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0zM8 12.5l3 3 5-6",
  "st-scan": "M4 8V4h4M16 4h4v4M20 16v4h-4M8 20H4v-4M8 9h8M8 12h8M8 15h5",
  "st-doc": "M6 3h9l4 4v14H6zM14.5 3v4.5H19M9 12h7M9 16h7",
  "st-mail": "M3 6h18v12H3zM3.5 6.5 12 13l8.5-6.5",
  "st-message": "M4 5h16v11H9l-5 4zM8 9.5h8M8 12.5h5",
  "st-pin":
    "M12 21s-7-6.2-7-11.5a7 7 0 0 1 14 0C19 14.8 12 21 12 21zM14.5 9.5a2.5 2.5 0 1 1-5 0 2.5 2.5 0 0 1 5 0z",
  "st-server":
    "M4 4h16v7H4zM4 13h16v7H4zM8 7.5h.01M8 16.5h.01M12 7.5h4M12 16.5h4",
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
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path :d="path" />
  </svg>
</template>
