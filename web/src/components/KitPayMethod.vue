<script setup lang="ts">
import { computed } from "vue";
import { consoleValue } from "../pro-console-fields";
import alipayIcon from "../assets/paytype/alipay.png";
import wxpayIcon from "../assets/paytype/wxpay.png";
const props = defineProps<{ type: string; label?: string }>();
// Alipay and WeChat Pay show their brand icon; the other methods keep a fixed
// letter mark (Q QQ, 银 bank). The name text always follows.
const icons: Record<string, string> = {
  alipay: alipayIcon,
  wxpay: wxpayIcon,
};
const marks: Record<string, { cls: string; char: string }> = {
  qqpay: { cls: "qqpay", char: "Q" },
  bank: { cls: "bank", char: "银" },
};
const icon = computed(() => icons[String(props.type)]);
const mark = computed(() => marks[String(props.type)]);
const name = computed(
  () => props.label?.trim() || consoleValue("type", props.type),
);
</script>
<template>
  <span class="pa-method">
    <img
      v-if="icon"
      class="pa-method-icon"
      :class="`pa-method-icon--${type}`"
      :src="icon"
      alt=""
      aria-hidden="true"
      width="16"
      height="16"
      draggable="false"
    />
    <span
      v-else-if="mark"
      class="pa-method-mark"
      :class="`pa-method-mark--${mark.cls}`"
      aria-hidden="true"
      >{{ mark.char }}</span
    >
    <span>{{ name }}</span>
  </span>
</template>
