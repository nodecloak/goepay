<script setup lang="ts">
import { computed, nextTick, ref, onMounted, onBeforeUnmount } from "vue";
import { ApiClient } from "../api";
import { readSettlementRecipient } from "../settlement-recipient";
import type { SettlementSchedulePrefs } from "../settlement-plans";
import KitSwitch from "./KitSwitch.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitPayMethod from "./KitPayMethod.vue";
const props = defineProps<{ client: ApiClient; ownSession: boolean }>();
const current = ref<SettlementSchedulePrefs | null>(null),
  enabled = ref(false),
  busy = ref(false),
  loading = ref(false),
  confirming = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref("");
const reviewButton = ref<HTMLButtonElement>();
const cancelButton = ref<HTMLButtonElement>();
const bound = computed(
  () =>
    !!current.value &&
    current.value.recipient.state === "configured" &&
    current.value.recipient_config_id === current.value.recipient.config_id,
);
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
      throw new Error("参与或收款配置版本不匹配，请刷新页面后核对");
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
  void nextTick(() => cancelButton.value?.focus());
}
function cancelReview() {
  confirming.value = false;
  frozen = null;
  void nextTick(() => reviewButton.value?.focus());
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
    notice.value = "参与设置已保存。计划生成的申请仍需平台审批和出款。";
    await load();
  } catch (e) {
    if (active) {
      uncertain.value = true;
      error.value = e instanceof Error ? e.message : "保存结果未知";
      notice.value = "不会自动重试，请刷新页面后核对。";
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
  <section class="panel schedule-settings" aria-label="自动结算参与设置">
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status" class="alert">{{ notice }}</p>
    <p v-if="loading" role="status">正在加载参与设置…</p>
    <template v-else-if="current">
      <div class="schedule-settings-layout">
        <div class="schedule-participation">
          <div class="schedule-title">
            <h2>参与状态</h2>
            <KitStatusTag
              :text="
                current.enabled ? (bound ? '已参与' : '待重新确认') : '未参与'
              "
              :tone="
                current.enabled ? (bound ? 'success' : 'warning') : 'neutral'
              "
            />
          </div>
          <p class="muted">
            参与后，符合计划条件的金额将被冻结，并生成待审核结算申请。出款需另行审批。
          </p>
          <KitSwitch
            v-model="enabled"
            label="同意参与计划生成"
            :disabled="busy || confirming || uncertain"
          />
          <button
            ref="reviewButton"
            :disabled="busy || confirming || uncertain"
            class="pa-btn pa-btn--primary"
            type="button"
            @click="review"
          >
            核对参与设置
          </button>
        </div>
        <div class="schedule-recipient">
          <h2>当前收款账号</h2>
          <dl>
            <div>
              <dt>结算方式</dt>
              <dd>
                <KitPayMethod
                  v-if="current.recipient.type"
                  :type="current.recipient.type"
                />
                <template v-else>未配置</template>
              </dd>
            </div>
            <div>
              <dt>账号</dt>
              <dd>{{ current.recipient.account_masked || "未配置" }}</dd>
            </div>
            <div>
              <dt>姓名</dt>
              <dd>{{ current.recipient.name_masked || "未配置" }}</dd>
            </div>
            <div>
              <dt>收款配置版本</dt>
              <dd>{{ current.recipient.config_id }}</dd>
            </div>
            <div>
              <dt>已绑定版本</dt>
              <dd>{{ current.recipient_config_id }}</dd>
            </div>
            <div>
              <dt>参与修订</dt>
              <dd>{{ current.revision }}</dd>
            </div>
          </dl>
          <p v-if="current.enabled && !bound" class="schedule-rebind">
            收款配置已变化或尚未绑定。请核对上方账号并重新确认。
          </p>
        </div>
      </div>
      <section
        v-if="confirming"
        class="schedule-confirm"
        aria-label="核对参与变更"
        @keydown.esc.stop="cancelReview"
      >
        <h2>{{ enabled ? "确认参与自动结算" : "确认停止参与" }}</h2>
        <p>
          当前收款配置版本 {{ current.recipient.config_id }}，参与修订
          {{ current.revision }}。保存后仅更新参与授权。
        </p>
        <div class="toolbar-actions">
          <button
            ref="cancelButton"
            type="button"
            class="pa-btn"
            @click="cancelReview"
          >
            取消
          </button>
          <button
            type="button"
            class="pa-btn pa-btn--primary"
            :disabled="busy"
            @click="save"
          >
            确认保存参与设置
          </button>
        </div>
      </section>
    </template>
  </section>
</template>
<style scoped>
.schedule-settings {
  max-width: 980px;
  margin: 0;
}
.schedule-settings-layout {
  display: grid;
  gap: var(--space-5);
}
.schedule-title {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}
.schedule-settings-layout h2,
.schedule-confirm h2 {
  font-size: 16px;
  margin: 0;
}
.schedule-participation {
  display: grid;
  gap: var(--space-3);
  align-content: start;
  justify-items: start;
}
.schedule-participation p {
  margin: 0;
  line-height: 1.7;
}
.schedule-recipient,
.schedule-confirm {
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  padding: var(--space-4);
  background: var(--bg-raised);
}
.schedule-recipient dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-3);
  margin-block: var(--space-3) 0;
}
.schedule-recipient dt {
  color: var(--ink-muted);
  font-size: 12px;
}
.schedule-recipient dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
  font-variant-numeric: tabular-nums;
}
.schedule-rebind {
  margin-bottom: 0;
  color: var(--warning-text);
}
.schedule-confirm {
  margin-top: var(--space-4);
}
@media (min-width: 1040px) {
  .schedule-settings-layout {
    grid-template-columns: minmax(0, 1fr) minmax(320px, 1.1fr);
  }
}
</style>
