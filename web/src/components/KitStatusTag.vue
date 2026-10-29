<script setup lang="ts">
import { computed } from "vue";
import { consoleValue } from "../pro-console-fields";
type Tone = "success" | "warning" | "danger" | "info" | "neutral";
const props = defineProps<{
  status?: string;
  orders?: boolean;
  text?: string;
  tone?: Tone;
}>();
// Fixed tone per status key. Color is never the only signal: a word always shows.
const toneByStatus: Record<string, Tone> = {
  paid: "success",
  succeeded: "success",
  completed: "success",
  released: "success",
  revealed: "success",
  ready: "success",
  pending: "warning",
  held: "warning",
  queued: "warning",
  running: "warning",
  submitting: "warning",
  processing: "warning",
  reversal_pending: "warning",
  failed: "danger",
  dead: "danger",
  reversed: "danger",
  created: "info",
  none: "neutral",
  closed: "neutral",
  cancelled: "neutral",
  unknown: "neutral",
};
const resolvedTone = computed<Tone>(
  () => props.tone ?? toneByStatus[String(props.status)] ?? "neutral",
);
const label = computed(
  () =>
    props.text ?? consoleValue("status", props.status, props.orders ?? false),
);
</script>
<template>
  <span class="pa-tag" :class="`pa-tag--${resolvedTone}`">{{ label }}</span>
</template>
