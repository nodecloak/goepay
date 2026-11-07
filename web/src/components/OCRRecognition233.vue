<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient } from "../api";
import { ocrDocuments233, ocrLabels233, recognizeOCR233 } from "../kyc233";
import KitSelect from "./KitSelect.vue";
import KitModal from "./KitModal.vue";
const props = defineProps<{
  client: ApiClient;
  ownSession: boolean;
  provider: string;
  configRevision: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  prefill: [fields: Record<string, string>, documentType: string];
  busy: [boolean];
}>();
const type = ref("idcard"),
  file = ref<File>(),
  input = ref<HTMLInputElement>(),
  result = ref<Record<string, string>>(),
  busy = ref(false),
  uncertain = ref(false),
  error = ref(""),
  consent = ref(false),
  confirming = ref<"recognize" | "clear">();
const allowed = computed(
  () =>
    props.ownSession &&
    props.client.area === "merchant" &&
    !!props.client.csrfToken &&
    ["aliyun", "baidu"].includes(props.provider),
);
const blocked = computed(() => busy.value || props.disabled || uncertain.value);
const imageLimit = computed(() => (props.provider === "baidu" ? 2 : 4));
const canPrefill = computed(
  () => !!result.value && ["idcard", "business_license"].includes(type.value),
);
let alive = true,
  epoch = 0,
  controller: AbortController | undefined;
function context() {
  return [
    toRaw(props.client),
    props.client.area,
    props.client.csrfToken,
    props.ownSession,
    props.provider,
    props.configRevision,
  ] as const;
}
function clear() {
  file.value = undefined;
  result.value = undefined;
  uncertain.value = false;
  consent.value = false;
  error.value = "";
  confirming.value = undefined;
  if (input.value) input.value.value = "";
}
function choose(event: Event) {
  if (blocked.value || !allowed.value) return;
  file.value = (event.target as HTMLInputElement).files?.[0];
  result.value = undefined;
  consent.value = false;
  error.value = "";
  if (
    file.value &&
    (!file.value.size ||
      file.value.size > imageLimit.value * 1048576 ||
      !["image/png", "image/jpeg"].includes(file.value.type))
  ) {
    clear();
    error.value = `请选择不超过${imageLimit.value}MiB的PNG或JPEG证照图片。`;
  }
}
function prepare() {
  if (allowed.value && !blocked.value && file.value && consent.value)
    confirming.value = "recognize";
}
async function recognize() {
  if (
    !allowed.value ||
    blocked.value ||
    !file.value ||
    !consent.value ||
    confirming.value !== "recognize"
  )
    return;
  const selected = file.value,
    documentType = type.value,
    c = context(),
    run = ++epoch;
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  const operation = [...bytes]
    .map((v) => v.toString(16).padStart(2, "0"))
    .join("");
  controller?.abort();
  controller = new AbortController();
  busy.value = true;
  emit("busy", true);
  error.value = "";
  confirming.value = undefined;
  file.value = undefined;
  if (input.value) input.value.value = "";
  try {
    const fields = await recognizeOCR233(
      props.client,
      props.provider,
      props.configRevision,
      documentType,
      selected,
      operation,
      controller.signal,
    );
    if (
      alive &&
      run === epoch &&
      allowed.value &&
      c.every((v, i) => v === context()[i])
    )
      result.value = fields;
  } catch {
    if (alive && run === epoch && c.every((v, i) => v === context()[i])) {
      uncertain.value = true;
      error.value =
        "本次识别未取得可核实结果，不会自动重试。若服务已受理，可能已产生费用；请核对后再决定是否发起新的识别。";
    }
  } finally {
    if (alive && run === epoch) {
      busy.value = false;
      emit("busy", false);
      consent.value = false;
    }
  }
}
function prefill() {
  if (canPrefill.value && !blocked.value && allowed.value) {
    emit("prefill", { ...result.value }, type.value);
    result.value = undefined;
  }
}
watch(
  () => context(),
  () => {
    epoch++;
    controller?.abort();
    busy.value = false;
    clear();
    emit("busy", false);
  },
);
watch(type, () => {
  if (!busy.value && !uncertain.value) clear();
});
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  clear();
  emit("busy", false);
});
</script>
<template>
  <section v-if="allowed" class="ocr-recognition233" aria-label="证照OCR识别">
    <h3>证照OCR识别</h3>
    <p class="muted">
      可选操作。图片仅用于本次{{
        provider === "aliyun" ? "阿里云" : "百度云"
      }}识别，本站不保存原图。请勿上传他人的证照；识别后请逐项核对。
    </p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <form class="console-config-form" @submit.prevent="prepare">
      <fieldset :disabled="blocked || !!confirming">
        <label class="ocr-row233"
          ><span>证照类型</span
          ><KitSelect
            v-model="type"
            aria-label="证照类型"
            :options="ocrDocuments233"
            :disabled="blocked"
        /></label>
        <label class="ocr-row233"
          ><span>证照图片</span
          ><input
            ref="input"
            aria-label="证照图片"
            type="file"
            accept="image/png,image/jpeg"
            :disabled="blocked"
            @change="choose"
        /></label>
        <p class="muted">
          PNG/JPEG，最大{{
            imageLimit
          }}MiB；图片尺寸15～8192像素，总像素不超过3200万。
          <template v-if="provider === 'baidu'"
            >百度云编码后的请求最大4MiB，过大的请求会在发送前拒绝。</template
          >
        </p>
        <label class="ocr-consent233"
          ><input
            v-model="consent"
            type="checkbox"
            aria-label="同意将所选证照发送至OCR服务商识别"
            :disabled="blocked"
          />我有权使用此证照，同意将所选图片发送至{{
            provider === "aliyun" ? "阿里云" : "百度云"
          }}识别，并知悉识别可能产生服务商费用。</label
        >
        <button type="submit" :disabled="blocked || !file || !consent">
          {{ busy ? "正在识别…" : "识别证照" }}
        </button>
      </fieldset>
    </form>
    <template v-if="result">
      <h4>识别结果，请核对</h4>
      <dl class="ocr-result233">
        <template v-for="(value, key) in result" :key="key"
          ><dt>{{ ocrLabels233[key] }}</dt>
          <dd>{{ value || "—" }}</dd></template
        >
      </dl>
      <p class="muted">
        识别结果不是认证结果。使用预填后仍需核对信息并主动提交认证。
      </p>
      <button
        v-if="canPrefill"
        type="button"
        :disabled="blocked"
        @click="prefill"
      >
        已核对，预填认证表单
      </button>
    </template>
    <button
      v-if="result || uncertain"
      type="button"
      :disabled="busy || disabled"
      @click="confirming = 'clear'"
    >
      清除本次识别
    </button>
    <KitModal
      v-if="confirming === 'recognize'"
      title="确认识别证照"
      submit-text="确认识别"
      :loading="busy"
      @close="confirming = undefined"
      @submit="recognize"
    >
      <p>
        将所选{{
          ocrDocuments233.find((v) => v.value === type)?.label
        }}图片发送至{{
          provider === "aliyun" ? "阿里云" : "百度云"
        }}进行OCR识别，可能产生服务商费用。本次仅识别证照，不会提交实名认证。
      </p>
    </KitModal>
    <KitModal
      v-if="confirming === 'clear'"
      title="清除本次识别"
      submit-text="清除本地结果"
      @close="confirming = undefined"
      @submit="clear"
    >
      <p>
        清除本页面中的图片与识别结果。已发送的识别无法撤回；再次识别是新的服务请求，可能再次计费。
      </p>
    </KitModal>
  </section>
</template>
<style scoped>
.ocr-recognition233 {
  border-top: 1px solid var(--border, #e6ebf2);
  margin-top: 28px;
  padding-top: 12px;
}
.ocr-recognition233 fieldset {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
.ocr-recognition233 p {
  line-height: 1.7;
}
.ocr-row233 {
  display: grid;
  grid-template-columns: 180px minmax(0, 1fr);
  gap: 16px;
  margin: 18px 0;
  align-items: center;
}
.ocr-consent233 {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin: 20px 0;
  line-height: 1.7;
}
.ocr-consent233 input {
  width: auto;
  margin-top: 6px;
}
.ocr-result233 {
  display: grid;
  grid-template-columns: minmax(100px, 180px) minmax(0, 1fr);
  gap: 12px;
}
.ocr-result233 dd {
  margin: 0;
  overflow-wrap: anywhere;
}
@media (max-width: 760px) {
  .ocr-row233 {
    grid-template-columns: 1fr;
    gap: 8px;
  }
  .ocr-result233 {
    grid-template-columns: minmax(90px, 120px) minmax(0, 1fr);
  }
}
</style>
