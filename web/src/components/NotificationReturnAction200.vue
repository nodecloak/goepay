<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { ApiClient } from "../api";

const props = defineProps<{
  client: ApiClient;
  tradeNo: string;
  orderStatus: unknown;
  allowed: boolean;
  ownSession: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{ busy: [value: boolean] }>();
const busy = ref(false);
const error = ref("");
const notice = ref("");
const available = computed(
  () =>
    props.allowed === true &&
    props.ownSession === true &&
    props.orderStatus === "paid" &&
    !!props.tradeNo,
);
let alive = true;
let epoch = 0;
let controller: AbortController | undefined;
let reserved: Window | null = null;

function closeReserved() {
  const popup = reserved;
  reserved = null;
  try {
    if (popup && !popup.closed) popup.close();
  } catch {
    // A window which has already navigated or closed grants no retry authority.
  }
}
function cancel() {
  epoch++;
  controller?.abort();
  controller = undefined;
  closeReserved();
  busy.value = false;
  error.value = "";
  notice.value = "";
  emit("busy", false);
}
function publicIPv4(host: string): boolean | undefined {
  if (!/^\d+\.\d+\.\d+\.\d+$/.test(host)) return;
  const parts = host.split(".").map(Number);
  if (parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255))
    return false;
  const [a, b, c] = parts;
  return !(
    a === 0 ||
    a === 10 ||
    a === 127 ||
    a >= 224 ||
    (a === 100 && b >= 64 && b <= 127) ||
    (a === 169 && b === 254) ||
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && (b === 168 || (b === 0 && (c === 0 || c === 2)))) ||
    (a === 198 && (b === 18 || b === 19 || (b === 51 && c === 100))) ||
    (a === 203 && b === 0 && c === 113)
  );
}
function publicIPv6(host: string): boolean {
  if (!host.startsWith("[") || !host.endsWith("]")) return false;
  const halves = host.slice(1, -1).split("::");
  if (halves.length > 2) return false;
  const left = halves[0] ? halves[0].split(":") : [];
  const right = halves[1] ? halves[1].split(":") : [];
  const missing = 8 - left.length - right.length;
  if (
    (halves.length === 1 && missing !== 0) ||
    (halves.length === 2 && missing < 1)
  )
    return false;
  const words: string[] = [
    ...left,
    ...Array<string>(missing).fill("0"),
    ...right,
  ];
  if (
    words.length !== 8 ||
    words.some((word) => !/^[a-f0-9]{1,4}$/i.test(word))
  )
    return false;
  const address = words.reduce(
    (value, word) => (value << 16n) | BigInt("0x" + word),
    0n,
  );
  const inPrefix = (prefix: bigint, bits: number) =>
    address >> BigInt(128 - bits) === prefix >> BigInt(128 - bits);
  // Match the existing public endpoint policy. Mapped IPv4, NAT64, local,
  // multicast and other non-global spaces fall outside 2000::/3.
  return (
    inPrefix(0x20000000000000000000000000000000n, 3) &&
    !inPrefix(0x20010000000000000000000000000000n, 23) &&
    !inPrefix(0x20010db8000000000000000000000000n, 32) &&
    !inPrefix(0x20020000000000000000000000000000n, 16)
  );
}
function publicReturnHost(url: URL): boolean {
  // URL has already applied the browser's IPv4 and IDNA normalization. This
  // does not resolve or pin DNS answers and cannot prevent DNS rebinding.
  const host = url.hostname.toLowerCase().replace(/\.$/, "");
  if (host.startsWith("[")) return publicIPv6(host);
  const ipv4 = publicIPv4(host);
  if (ipv4 !== undefined) return ipv4;
  return (
    host.includes(".") &&
    !host.includes("%") &&
    host.split(".").every((label) => !!label) &&
    ![".localhost", ".local", ".internal", ".home", ".lan"].some((suffix) =>
      host.endsWith(suffix),
    )
  );
}
function returnAddress(data: unknown): string | undefined {
  if (!data || typeof data !== "object" || Array.isArray(data)) return;
  const fields = data as Record<string, unknown>;
  if (
    Object.keys(fields).length !== 1 ||
    typeof fields.url !== "string" ||
    !fields.url ||
    fields.url.length > 16384 ||
    /[\u0000-\u0020\u007f\\]/.test(fields.url) ||
    !/^https?:\/\//i.test(fields.url)
  )
    return;
  try {
    const url = new URL(fields.url);
    if (
      !["http:", "https:"].includes(url.protocol) ||
      !url.hostname ||
      !publicReturnHost(url) ||
      url.username ||
      url.password ||
      fields.url.includes("#") ||
      /^https?:\/\/([^/?#]*)/i.exec(fields.url)?.[1].includes("@")
    )
      return;
    return fields.url;
  } catch {
    return;
  }
}
async function openReturn() {
  if (!available.value || props.disabled || busy.value) return;
  error.value = "";
  notice.value = "";
  closeReserved();
  try {
    reserved = window.open("about:blank", "_blank");
    if (reserved) reserved.opener = null;
  } catch {
    closeReserved();
  }
  if (!reserved) {
    error.value = "浏览器阻止了新窗口，请允许弹出窗口后重试。";
    return;
  }
  const run = ++epoch;
  const client = props.client;
  const area = client.area;
  const csrf = client.csrfToken;
  const trade = props.tradeNo;
  const c = new AbortController();
  controller = c;
  const current = () =>
    alive &&
    epoch === run &&
    !c.signal.aborted &&
    props.client === client &&
    client.area === area &&
    client.csrfToken === csrf &&
    props.tradeNo === trade &&
    available.value;
  busy.value = true;
  emit("busy", true);
  try {
    const response = await client.requestResult<unknown>(
      `/orders/${encodeURIComponent(trade)}/return-notification`,
      { method: "POST", body: {}, signal: c.signal },
    );
    if (!current()) return;
    const target = response.status === 200 && returnAddress(response.data);
    if (!target || !reserved || reserved.closed)
      throw new Error("return unavailable");
    reserved.location.replace(target);
    // Do not retain or render the signed address after navigation.
    reserved = null;
    notice.value = "已打开商户同步返回页面，请在新窗口核对处理结果。";
  } catch {
    if (current())
      error.value = "无法打开商户同步返回页面，请核对会话和订单后重试。";
  } finally {
    if (epoch === run) {
      closeReserved();
      controller = undefined;
      busy.value = false;
      emit("busy", false);
    }
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.tradeNo,
    props.allowed,
    props.orderStatus,
    props.ownSession,
  ],
  cancel,
  { flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  cancel();
});
</script>
<template>
  <section v-if="available" class="notification-return200">
    <div class="toolbar-actions">
      <button :disabled="disabled || busy" @click="openReturn">
        重新通知(同步)
      </button>
    </div>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status" class="muted">{{ notice }}</p>
  </section>
</template>
