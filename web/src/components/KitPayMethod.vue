<script setup lang="ts">
import { computed } from "vue";
import { consoleValue } from "../pro-console-fields";
import alipayIcon from "../assets/paytype/alipay.png";
import wxpayIcon from "../assets/paytype/wxpay.png";
import qqpayIcon from "../assets/paytype/qqpay.svg?no-inline";
import bankIcon from "../assets/paytype/bank.svg?no-inline";
import jdpayIcon from "../assets/paytype/jdpay.svg?no-inline";
import paypalIcon from "../assets/paytype/paypal.svg?no-inline";
import douyinpayIcon from "../assets/paytype/douyinpay.svg?no-inline";
const props = defineProps<{ type: string; label?: string }>();
// Bundle the brand assets locally. The adjacent name labels the method even
// when an image is unavailable or a custom display name is configured.
const icons: Record<string, string> = {
  alipay: alipayIcon,
  wxpay: wxpayIcon,
  qqpay: qqpayIcon,
  bank: bankIcon,
  jdpay: jdpayIcon,
  paypal: paypalIcon,
  douyinpay: douyinpayIcon,
};
const icon = computed(() => icons[String(props.type)]);
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
    <span>{{ name }}</span>
  </span>
</template>
