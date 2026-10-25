<script setup lang="ts">
import { ref } from "vue";
import { ApiClient } from "../api";
const props = defineProps<{
  client: ApiClient;
  merchantId: string;
  name: string;
}>();
const emit = defineEmits<{ close: [] }>();
const busy = ref(false),
  error = ref(""),
  uncertain = ref(false),
  confirmed = ref(false);
async function check() {
  const session = await new ApiClient("merchant").session();
  if (String(session.user.id) !== props.merchantId || !session.delegated_by)
    throw new Error("尚未确认目标商户代登录状态，请勿重复消费授权");
  confirmed.value = true;
  return session;
}
async function consume() {
  if (busy.value || uncertain.value || props.client.area !== "admin") return;
  busy.value = true;
  error.value = "";
  let grant = "";
  try {
    const data = await props.client.request<{
      grant: string;
      expires_at: string;
    }>("/sso/grants", {
      method: "POST",
      body: { merchant_id: props.merchantId },
    });
    grant = data.grant;
    if (!grant) throw new Error("授权响应不完整");
    await props.client.request("/sso/consume", {
      method: "POST",
      body: { grant },
    });
    await check();
  } catch (e) {
    uncertain.value = true;
    error.value = `代登录结果尚未核实，不会自动重试授权或消费。${e instanceof Error ? e.message : "请求失败"}`;
    try {
      await check();
    } catch {}
  } finally {
    grant = "";
    busy.value = false;
  }
}
async function verify() {
  busy.value = true;
  error.value = "";
  try {
    await check();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "会话状态读取失败";
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <div class="modal-backdrop">
    <section
      class="modal panel"
      role="dialog"
      aria-modal="true"
      aria-label="确认代登录"
    >
      <h2>代登录商户 {{ name }}（{{ merchantId }}）</h2>
      <p>
        此操作将替换当前浏览器的商户会话，建立最长 15
        分钟的管理员代登录。管理员退出或权限撤销后会失效，商户后台会展示实际代登录管理员。
      </p>
      <p>
        授权只在当前组件内用于一次消费，不放入链接或浏览器持久存储。网络结果未知时只回读当前会话，不自动重试。
      </p>
      <div v-if="error" role="alert" class="alert error">{{ error }}</div>
      <p v-if="confirmed" role="status">
        已核对当前浏览器的目标商户代登录会话。
      </p>
      <a
        v-if="confirmed"
        href="/merchant/dashboard"
        target="_blank"
        rel="noopener noreferrer"
        >在新标签打开商户后台</a
      >
      <div class="modal-actions">
        <button :disabled="busy" @click="emit('close')">关闭代登录确认</button
        ><button
          v-if="!uncertain && !confirmed"
          class="primary"
          :disabled="busy"
          @click="consume"
        >
          确认建立代登录会话</button
        ><button v-if="uncertain" :disabled="busy" @click="verify">
          核对当前商户会话
        </button>
      </div>
    </section>
  </div>
</template>
