<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import KitModal from "./KitModal.vue";
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{ busy: [boolean] }>();
interface State {
  revision: string;
  templates: Record<string, string>;
  active_provider: string;
  ready: Record<string, boolean>;
}
const providers = [
  ["aliyun", "阿里云"],
  ["qcloud", "腾讯云"],
  ["smsbao", "短信宝"],
] as const;
const scenes = [
  ["balance", "余额不足"],
  ["complain", "交易投诉"],
  ["group", "会员到期"],
] as const;
const state = ref<State>(),
  draft = ref<Record<string, string>>({}),
  loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  reloadRequired = ref(false),
  confirming = ref(false),
  error = ref(""),
  notice = ref("");
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.ownSession &&
    !!props.client.csrfToken,
);
const dirty = computed(
  () =>
    !!state.value &&
    Object.keys(state.value.templates).some(
      (k) => draft.value[k] !== state.value?.templates[k],
    ),
);
const { confirmDiscard } = useSettingsDraft212(
  "业务短信模板",
  () => dirty.value || uncertain.value,
);
let alive = true,
  epoch = 0,
  controller: AbortController | undefined,
  readyClient: ApiClient | undefined,
  readyCSRF = "";
function current(run: number, client: ApiClient, csrf: string) {
  return (
    alive &&
    epoch === run &&
    allowed.value &&
    client === toRaw(props.client) &&
    csrf === props.client.csrfToken
  );
}
function apply(v: State) {
  if (
    !v ||
    !/^\d+$/.test(v.revision) ||
    !v.templates ||
    Object.keys(v.templates).length !== 9 ||
    providers.some(([p]) =>
      scenes.some(([s]) => typeof v.templates[p + "_" + s] !== "string"),
    )
  )
    throw Error("业务短信模板响应不完整");
  state.value = v;
  draft.value = { ...v.templates };
  uncertain.value = false;
  reloadRequired.value = false;
  confirming.value = false;
}
async function load() {
  const before = epoch,
    beforeClient = toRaw(props.client),
    beforeCSRF = props.client.csrfToken;
  if (
    !allowed.value ||
    busy.value ||
    props.disabled ||
    !(await confirmDiscard())
  )
    return;
  if (
    !allowed.value ||
    before !== epoch ||
    beforeClient !== toRaw(props.client) ||
    beforeCSRF !== props.client.csrfToken ||
    busy.value ||
    props.disabled
  )
    return;
  const run = ++epoch,
    client = toRaw(props.client),
    csrf = client.csrfToken;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = await client.request<State>("/settings/business-sms", {
      signal: controller.signal,
    });
    if (current(run, client, csrf)) {
      apply(v);
      readyClient = client;
      readyCSRF = csrf;
    }
  } catch {
    if (current(run, client, csrf))
      error.value = "业务短信模板读取失败，请重新读取";
  } finally {
    if (current(run, client, csrf)) loading.value = false;
  }
}
async function save() {
  if (
    !allowed.value ||
    props.disabled ||
    busy.value ||
    loading.value ||
    uncertain.value ||
    reloadRequired.value ||
    !state.value ||
    !dirty.value ||
    readyClient !== toRaw(props.client) ||
    readyCSRF !== props.client.csrfToken
  )
    return;
  const run = ++epoch,
    client = toRaw(props.client),
    csrf = client.csrfToken;
  const input = {
    expected_revision: state.value.revision,
    templates: { ...draft.value },
  };
  confirming.value = false;
  busy.value = true;
  error.value = "";
  notice.value = "";
  controller?.abort();
  controller = new AbortController();
  try {
    const v = await client.request<State>("/settings/business-sms", {
      method: "PUT",
      body: input,
      signal: controller.signal,
    });
    if (current(run, client, csrf)) {
      apply(v);
      notice.value = "业务短信模板已保存，未发送短信";
    }
  } catch (e) {
    if (current(run, client, csrf)) {
      uncertain.value = !(
        e instanceof ApiError && [400, 403, 409, 422].includes(e.status)
      );
      reloadRequired.value =
        e instanceof ApiError && (e.status === 409 || e.status === 403);
      error.value = uncertain.value
        ? "保存结果无法确认，请重新读取核对；不会自动重试"
        : reloadRequired.value
          ? "配置或会话已变化，请重新读取后编辑"
          : "模板未保存，请核对模板格式";
    }
  } finally {
    if (current(run, client, csrf)) busy.value = false;
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.adminClass,
    props.ownSession,
  ],
  () => {
    epoch++;
    controller?.abort();
    state.value = undefined;
    draft.value = {};
    readyClient = undefined;
    readyCSRF = "";
    loading.value = false;
    busy.value = false;
    uncertain.value = false;
    reloadRequired.value = false;
    confirming.value = false;
    error.value = "";
    notice.value = "";
    void load();
  },
  { immediate: true, flush: "sync" },
);
watch(
  () => busy.value || loading.value || confirming.value,
  (v) => emit("busy", v),
  { flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  emit("busy", false);
});
</script>
<template>
  <section aria-label="业务短信模板" class="business-sms231">
    <h3 class="settings-group">业务短信模板</h3>
    <p class="muted">
      独立于验证码模板，使用上方当前启用的短信网关及签名。余额不足参数为提醒阈值，交易投诉参数为系统订单号，会员到期参数为用户组名称。短信宝模板需包含一个
      {code}；阿里云使用 code 参数，腾讯云使用第一个模板参数。
    </p>
    <p class="muted">
      商户需验证绑定手机，并开启“消息提醒 →
      短信消息”。会员到期短信额外发送，不依赖商户选择邮件或关闭该提醒。修改配置仅适用于后续新事件，不会补发历史消息。
    </p>
    <p v-if="!allowed" class="muted">业务短信配置需要超级管理员本人会话。</p>
    <template v-else>
      <button :disabled="busy || loading || disabled" @click="load">
        重新读取业务短信模板
      </button>
      <p v-if="loading" role="status">正在读取业务短信模板…</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
      <template v-if="state">
        <p>
          当前业务短信网关：{{
            providers.find(([p]) => p === state?.active_provider)?.[1] ||
            "未启用"
          }}
        </p>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>服务商</th>
                <th v-for="[s, label] in scenes" :key="s">{{ label }}模板</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="[p, label] in providers" :key="p">
                <th scope="row">{{ label }}</th>
                <td v-for="[s, title] in scenes" :key="s">
                  <input
                    v-model="draft[p + '_' + s]"
                    :aria-label="label + ' ' + title + '模板'"
                    :disabled="
                      busy || loading || disabled || uncertain || reloadRequired
                    "
                    :maxlength="p === 'smsbao' ? 1000 : 128"
                    :placeholder="
                      p === 'smsbao'
                        ? '业务提醒：{code}'
                        : '模板 ID（留空停用此场景）'
                    "
                    autocomplete="off"
                  />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="muted">空模板表示停用对应场景；保存配置不会发送测试短信。</p>
        <button
          class="primary"
          :disabled="
            !dirty || busy || loading || disabled || uncertain || reloadRequired
          "
          @click="confirming = true"
        >
          保存业务短信模板
        </button>
      </template>
    </template>
    <KitModal
      v-if="confirming"
      title="确认保存业务短信模板"
      :loading="busy"
      submit-text="确认保存"
      @submit="save"
      @close="confirming = false"
    >
      <p>
        本次仅保存业务短信模板。空模板将停用对应场景，验证码模板保持不变；不会发送短信或补发历史事件。
      </p>
    </KitModal>
  </section>
</template>
<style scoped>
.business-sms231 {
  margin-top: 24px;
}
.business-sms231 input {
  width: 100%;
  min-width: 140px;
}
</style>
