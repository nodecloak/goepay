<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from "vue";
import { ApiClient } from "../api";
import { readSettlementRecipient } from "../settlement-recipient";
import type { SettlementSchedulePrefs } from "../settlement-plans";
const props = defineProps<{ client: ApiClient; ownSession: boolean }>();
const current = ref<SettlementSchedulePrefs | null>(null),
  enabled = ref(false),
  busy = ref(false),
  loading = ref(false),
  confirming = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref("");
let active = true,
  controller: AbortController | undefined,
  frozen: Record<string, unknown> | null = null;
const allowed = () => props.client.area === "merchant" && props.ownSession;
async function load() {
  if (!allowed() || !active) {
    error.value = "仅商户本人会话可设置参与";
    return;
  }
  controller?.abort();
  const c = new AbortController();
  controller = c;
  current.value = null;
  frozen = null;
  confirming.value = false;
  loading.value = true;
  error.value = "";
  try {
    const p = await props.client.request<SettlementSchedulePrefs>(
      "/account/settlement-schedule",
      { signal: c.signal },
    );
    if (!active || c.signal.aborted) return;
    const recipient = readSettlementRecipient(p.recipient);
    if (
      typeof p.enabled !== "boolean" ||
      !/^(0|[1-9]\d*)$/.test(p.revision) ||
      !/^(0|[1-9]\d*)$/.test(p.recipient_config_id)
    )
      throw new Error("参与或收款配置版本不匹配，请重新读取");
    current.value = { ...p, recipient };
    enabled.value = p.enabled && p.recipient_config_id === recipient.config_id;
    uncertain.value = false;
  } catch (e) {
    if (active && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "参与设置读取失败";
  } finally {
    if (active && controller === c) loading.value = false;
  }
}
function review() {
  if (!current.value || busy.value || uncertain.value || !allowed()) return;
  if (enabled.value && current.value.recipient.state !== "configured") {
    error.value = "请先保存当前可用的结算收款配置";
    return;
  }
  frozen = {
    expected_revision: current.value.revision,
    enabled: enabled.value,
    recipient_config_id: current.value.recipient.config_id,
  };
  confirming.value = true;
  error.value = "";
}
async function save() {
  if (
    !frozen ||
    !confirming.value ||
    !allowed() ||
    busy.value ||
    uncertain.value
  )
    return;
  busy.value = true;
  confirming.value = false;
  error.value = "";
  const body = frozen;
  frozen = null;
  try {
    await props.client.request("/account/settlement-schedule", {
      method: "PUT",
      body,
      signal: controller?.signal,
    });
    if (!active) return;
    notice.value =
      "参与设置已保存；只允许计划生成申请，仍需平台独立审批和出款。";
    await load();
  } catch (e) {
    if (active) {
      uncertain.value = true;
      error.value = e instanceof Error ? e.message : "保存结果未知";
      notice.value = "不会自动重试，请先重新读取参与与收款版本。";
    }
  } finally {
    if (active) busy.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => {
  active = false;
  controller?.abort();
  frozen = null;
  current.value = null;
});
</script>
<template>
  <section class="panel" aria-label="自动结算参与设置">
    <h2>自动结算参与设置</h2>
    <p>
      默认关闭。启用只允许符合已授权计划的待审核申请生成并冻结；不会自动审批或付款，也不授权平台更换收款账户。修改收款配置后需重新确认。
    </p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <button :disabled="busy || loading || !allowed()" @click="load">
      重新读取参与与收款版本
    </button>
    <section v-if="current">
      <p>
        参与修订 {{ current.revision }}，原已绑定配置
        {{ current.recipient_config_id }}；当前可选收款配置
        {{ current.recipient.config_id }} ·
        {{ current.recipient.type || "未配置" }}
      </p>
      <p>{{ current.recipient.account_masked }}</p>
      <p>{{ current.recipient.name_masked }}</p>
      <p v-if="current.recipient_config_id !== current.recipient.config_id">
        收款配置已变化或尚未绑定，旧同意不会用于新配置，请重新明确确认。
      </p>
      <p>仅当前配置快照参与；旧申请资料不会被改写。</p>
      <label
        ><input
          v-model="enabled"
          type="checkbox"
          :disabled="busy || confirming || uncertain"
        />同意参与计划生成</label
      ><button :disabled="busy || confirming || uncertain" @click="review">
        核对参与设置
      </button>
      <section v-if="confirming" class="case-panel">
        <p>
          确认{{ enabled ? "参与" : "关闭参与" }}，绑定当前收款配置
          {{ current.recipient.config_id }}，原参与修订
          {{ current.revision }}。保存不创建结算申请。
        </p>
        <button :disabled="busy" @click="save">确认保存参与设置</button
        ><button
          @click="
            confirming = false;
            frozen = null;
          "
        >
          取消
        </button>
      </section>
    </section>
  </section>
</template>
