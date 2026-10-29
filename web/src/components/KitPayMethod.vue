<script setup lang="ts">
import { computed } from "vue";
import { consoleValue } from "../pro-console-fields";
const props = defineProps<{ type: string }>();
// Fixed mark per method (README): 支 Alipay, 微 WeChat, Q QQ, 银 bank.
const marks: Record<string, { cls: string; char: string }> = {
  alipay: { cls: "alipay", char: "支" },
  wxpay: { cls: "wxpay", char: "微" },
  qqpay: { cls: "qqpay", char: "Q" },
  bank: { cls: "bank", char: "银" },
};
const mark = computed(() => marks[String(props.type)]);
const name = computed(() => consoleValue("type", props.type));
</script>
<template>
  <span class="pa-method">
    <span
      v-if="mark"
      class="pa-method-mark"
      :class="`pa-method-mark--${mark.cls}`"
      aria-hidden="true"
      >{{ mark.char }}</span
    >
    <span>{{ name }}</span>
  </span>
</template>
