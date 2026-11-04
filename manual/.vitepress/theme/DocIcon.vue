<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 18 })
const paths = {
  search: 'm21 21-4.4-4.4M19 10.5a8.5 8.5 0 1 1-17 0 8.5 8.5 0 0 1 17 0Z',
  chevron: 'm9 5 7 7-7 7',
  copy: 'M9 9h11v12H9zM5 15H3V3h11v2',
  menu: 'M3 6h18M3 12h18M3 18h18',
  close: 'm6 6 12 12M6 18 18 6',
  home: 'm3 10 9-7 9 7v11h-7v-7h-4v7H3z',
  moon: 'M21 13a9 9 0 0 1-10-10A9 9 0 1 0 21 13Z',
  sun: 'M12 3v1M12 20v1M3 12h1M20 12h1M5.6 5.6l.7.7M17.7 17.7l.7.7M5.6 18.4l.7-.7M17.7 6.3l.7-.7M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z',
  expand: 'M8 3H3v5M16 3h5v5M3 16v5h5M21 16v5h-5',
  minus: 'M5 12h14', plus: 'M5 12h14M12 5v14', reset: 'M4 10a8 8 0 1 1 1 8M4 4v6h6',
  download: 'M12 3v12m-5-5 5 5 5-5M4 17v4h16v-4',
}
</script>
<template><svg :width="size" :height="size" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="paths[name] || paths.chevron" /></svg></template>
