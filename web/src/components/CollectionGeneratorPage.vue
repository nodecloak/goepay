<script lang="ts">
type GeneratorIntent = {
  external_no: string;
  recipient_name: string;
  style_id: string;
  origin: string;
  amount: string;
};
const generatorIntents = new WeakMap<object, Map<string, GeneratorIntent>>();
</script>
<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from "vue";
import QRCode from "qrcode";
import { ApiClient, ApiError, validAmount } from "../api";
import { safePaymentUrl } from "../commerce";
import {
  readCollectionLayout,
  renderCollectionPoster,
  type CollectionStyleView,
} from "../collection-style";
import ResourcePage from "./ResourcePage.vue";
import MerchantScanShortcut192 from "./MerchantScanShortcut192.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitSelect from "./KitSelect.vue";
import KitEmpty from "./KitEmpty.vue";
interface GeneratedCode {
  code_id: string;
  recipient_name: string;
  style_id: string;
  origin: string;
  fixed_amount_cents: string;
  config_hash: string;
  url: string;
  enabled: boolean;
  replayed: boolean;
}
const props = defineProps<{ client: ApiClient; ownSession: boolean }>();
const name = ref(""),
  origin = ref(""),
  style = ref("0"),
  amount = ref(""),
  styles = ref<CollectionStyleView[]>([]),
  domains = ref<{ origin: string; label: string }[]>([]),
  result = ref<GeneratedCode | null>(null),
  qr = ref(""),
  error = ref(""),
  notice = ref(""),
  loading = ref(false),
  busy = ref(false),
  history = ref(false),
  savingCodename = ref(false),
  defaultCodename = ref(""),
  codenameCustomized = ref(false),
  unknown = ref(false);
const initialCSRF = props.client.csrfToken;
let intents = generatorIntents.get(props.client);
if (!intents) {
  intents = new Map();
  generatorIntents.set(props.client, intents);
}
const pending = ref<GeneratorIntent | null>(intents.get(initialCSRF) || null);
const canWrite = computed(
  () =>
    props.client.area === "merchant" &&
    props.ownSession &&
    props.client.csrfToken === initialCSRF,
);
const formDisabled = computed(
  () => loading.value || busy.value || unknown.value || !canWrite.value,
);
const domainOptions = computed(() =>
  domains.value.map((d) => ({ value: d.origin, label: d.label })),
);
const styleOptions = computed(() => [
  { value: "0", label: "普通二维码" },
  ...styles.value.map((item) => ({
    value: item.id,
    label: String(
      item.fields.label || item.fields.title || item.fields.name || item.id,
    ),
  })),
]);
let alive = true,
  epoch = 0,
  renderEpoch = 0,
  controller: AbortController | undefined,
  renderController: AbortController | undefined;
function restore(v: GeneratorIntent) {
  name.value = v.recipient_name;
  origin.value = v.origin;
  style.value = v.style_id;
  amount.value = v.amount;
}
function remember() {
  if (pending.value) intents!.set(initialCSRF, { ...pending.value });
  else intents!.delete(initialCSRF);
}
function clearResult() {
  result.value = null;
  qr.value = "";
  notice.value = "";
  renderEpoch++;
  renderController?.abort();
}
watch([name, origin, amount], clearResult);
async function render() {
  const g = result.value;
  qr.value = "";
  renderController?.abort();
  const c = new AbortController();
  renderController = c;
  const current = ++renderEpoch;
  if (!g?.enabled || !g.url) return;
  try {
    const target = safePaymentUrl(g.url);
    if (!target || new URL(target).origin !== g.origin)
      throw Error("收款链接格式无效");
    const selected = styles.value.find((s) => s.id === style.value);
    const rendered = selected?.style
      ? await renderCollectionPoster(
          readCollectionLayout(selected.style),
          target,
          g.recipient_name,
          { signal: c.signal },
        )
      : await QRCode.toDataURL(target, { width: 280, margin: 2 });
    if (alive && current === renderEpoch && !c.signal.aborted)
      qr.value = rendered;
  } catch (e) {
    if (alive && current === renderEpoch && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "收款码绘制失败";
  }
}
watch(style, () => {
  if (result.value) void render();
});
async function load() {
  if (busy.value) return;
  controller?.abort();
  const c = new AbortController();
  controller = c;
  const current = ++epoch;
  loading.value = true;
  error.value = "";
  try {
    const data = await props.client.request<{
      recipient_name: string;
      default_codename?: string;
      codename_customized?: boolean;
      styles: CollectionStyleView[];
      domains: { origin: string; label: string }[];
      latest: GeneratedCode | null;
    }>("/collection-generator", { signal: c.signal });
    if (!alive || current !== epoch || c.signal.aborted) return;
    styles.value = data.styles;
    domains.value = data.domains;
    defaultCodename.value = data.default_codename ?? data.recipient_name;
    codenameCustomized.value = data.codename_customized === true;
    if (pending.value) {
      restore(pending.value);
      unknown.value = true;
    } else if (data.latest) {
      const g = data.latest;
      name.value = g.recipient_name;
      origin.value = g.origin;
      style.value = g.style_id;
      amount.value =
        g.fixed_amount_cents === "0"
          ? ""
          : (Number(g.fixed_amount_cents) / 100).toFixed(2);
      await Promise.resolve();
      result.value = g;
      await render();
    } else {
      name.value = data.recipient_name;
      origin.value = data.domains[0]?.origin || "";
      style.value = data.styles[0]?.id || "0";
    }
  } catch (e) {
    if (alive && current === epoch && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "收款码资料加载失败";
  } finally {
    if (alive && current === epoch) loading.value = false;
  }
}
async function generate() {
  if (busy.value || loading.value || !canWrite.value) return;
  if (!pending.value) {
    const trimmed = name.value.trim();
    if (
      !trimmed ||
      Array.from(trimmed).length > 100 ||
      /[\x00-\x1f\x7f]/.test(trimmed)
    ) {
      error.value = "请填写收款方名称，最多100字";
      return;
    }
    if (!domains.value.some((d) => d.origin === origin.value)) {
      error.value = "请选择可用收款域名";
      return;
    }
    if (
      amount.value &&
      (!validAmount(amount.value) || Number(amount.value) > 1000000)
    ) {
      error.value = "收款金额应为0.01至1000000元，最多两位小数";
      return;
    }
    pending.value = {
      external_no: crypto.randomUUID(),
      recipient_name: trimmed,
      style_id: style.value,
      origin: origin.value,
      amount: amount.value,
    };
    remember();
  }
  const intent = { ...pending.value };
  controller?.abort();
  const c = new AbortController();
  controller = c;
  const current = ++epoch;
  busy.value = true;
  error.value = "";
  clearResult();
  try {
    const g = await props.client.request<GeneratedCode>(
      "/collection-generator",
      { method: "POST", body: intent, signal: c.signal },
    );
    if (!alive || current !== epoch || c.signal.aborted) return;
    pending.value = null;
    remember();
    unknown.value = false;
    result.value = g;
    await render();
    notice.value = g.enabled
      ? g.replayed
        ? "已找回上次生成的收款码"
        : "收款码已生成"
      : "原收款码已停用，请明确生成新码";
  } catch (e) {
    if (!alive || current !== epoch || c.signal.aborted) return;
    if (
      e instanceof ApiError &&
      e.status >= 400 &&
      e.status < 500 &&
      e.status !== 408 &&
      e.status !== 429
    ) {
      pending.value = null;
      remember();
      unknown.value = false;
      error.value = e.message;
    } else {
      unknown.value = true;
      error.value =
        "生成结果尚未确认，请核对上次结果；重试会使用原请求号和原参数";
    }
  } finally {
    if (alive && current === epoch) busy.value = false;
  }
}
async function copy() {
  if (!result.value?.url) return;
  try {
    await navigator.clipboard.writeText(result.value.url);
    notice.value = "收款链接已复制";
  } catch {
    error.value = "无法访问剪贴板，请手动复制链接";
  }
}
function download() {
  if (!qr.value || !result.value) return;
  const a = document.createElement("a");
  a.href = qr.value;
  a.download = `collection-code-${result.value.code_id}.png`;
  a.click();
}
// F10 account/editCodename: persist the typed name as this account's default
// aggregate-code name, prefilled on the next visit. Empty clears it.
async function saveCodename() {
  if (savingCodename.value || !canWrite.value) return;
  savingCodename.value = true;
  error.value = "";
  try {
    const out = await props.client.request<{ default_codename: string }>(
      "/collection-generator",
      { method: "PATCH", body: { recipient_name: name.value.trim() } },
    );
    if (!alive) return;
    defaultCodename.value = out.default_codename;
    codenameCustomized.value = name.value.trim() !== "";
    notice.value = "默认收款名称已保存";
  } catch (e) {
    if (alive)
      error.value = e instanceof Error ? e.message : "默认名称保存失败";
  } finally {
    if (alive) savingCodename.value = false;
  }
}
onMounted(load);
watch(
  () => [props.client, props.client.csrfToken, props.ownSession],
  () => {
    epoch++;
    controller?.abort();
    clearResult();
    busy.value = false;
    loading.value = false;
    error.value = "会话已变化，请重新进入此页面";
  },
);
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  renderEpoch++;
  controller?.abort();
  renderController?.abort();
});
</script>
<template>
  <section class="collection-generator">
    <div class="toolbar">
      <MerchantScanShortcut192 :client="client" :own-session="ownSession" />
      <button :disabled="busy || loading" @click="load">重新读取</button
      ><button :disabled="busy" @click="history = !history">
        {{ history ? "返回生成收款码" : "历史收款码" }}
      </button>
    </div>
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
    <p v-if="!ownSession" class="alert warning">
      代登录仅可查看，请使用商户本人会话生成收款码。
    </p>
    <ResourcePage
      v-if="history"
      :client="client"
      resource="collection-codes"
      :own-session="ownSession"
    />
    <template v-else>
      <section class="generator-intro">
        <h3>产品介绍</h3>
        <p>聚合收款支持使用一个固定二维码，通过微信、支付宝完成收款。</p>
        <p class="muted">收款方可以分享链接，付款人打开后选择支付方式付款。</p>
      </section>
      <form class="generator-form" @submit.prevent="generate">
        <section class="generator-card">
          <h3>聚合收款配置</h3>
          <fieldset :disabled="formDisabled">
            <KitField label="收款方名称" required>
              <KitInput
                v-model="name"
                :maxlength="100"
                aria-label="收款方名称"
              />
            </KitField>
            <p class="codename-hint muted">
              默认收款名称：{{ defaultCodename || "商户名称"
              }}<span v-if="codenameCustomized">（已自定义）</span>
              <button
                type="button"
                class="link-button"
                :disabled="savingCodename || !canWrite"
                @click="saveCodename"
              >
                保存为默认
              </button>
            </p>
            <KitField label="选择域名" required>
              <KitSelect
                v-model="origin"
                :options="domainOptions"
                placeholder="请选择域名"
                aria-label="选择域名"
              />
            </KitField>
          </fieldset>
          <p class="muted">
            收款金额可留空。新生成的收款码长期有效，停用后失效；修改名称、金额或域名后请重新生成。
          </p>
          <button
            type="submit"
            class="primary"
            :disabled="busy || loading || !canWrite"
          >
            {{
              busy ? "正在核对…" : unknown ? "核对上次生成结果" : "生成收款码"
            }}
          </button>
          <section class="generator-result" aria-label="生成结果">
            <h3>你的聚合收款链接</h3>
            <p v-if="result && !result.enabled" class="alert warning">
              此收款码已停用
            </p>
            <template v-else-if="result"
              ><label
                >收款链接<input :value="result.url" readonly autocomplete="off"
              /></label>
              <div class="toolbar-actions">
                <button type="button" @click="copy">复制收款链接</button
                ><a :href="result.url" target="_blank" rel="noopener noreferrer"
                  >打开收款页</a
                >
              </div></template
            >
            <p v-else class="muted">生成后可复制链接，发送给付款人。</p>
          </section>
        </section>
        <section class="generator-card generator-poster">
          <h3>你的聚合收款码</h3>
          <fieldset :disabled="formDisabled">
            <KitField label="选择收款码风格">
              <KitSelect
                v-model="style"
                :options="styleOptions"
                placeholder="请选择收款码风格"
                aria-label="选择收款码风格"
              />
            </KitField>
            <KitField label="自定义收款金额">
              <KitInput
                v-model="amount"
                inputmode="decimal"
                placeholder="留空由付款人填写"
                aria-label="自定义收款金额"
              />
            </KitField>
          </fieldset>
          <img v-if="qr" :src="qr" alt="聚合收款二维码" width="280" />
          <KitEmpty v-else text="填写收款配置并生成后，在这里预览收款码" />
          <button
            v-if="result?.enabled"
            type="button"
            :disabled="!qr"
            @click="download"
          >
            下载收款码
          </button>
        </section>
      </form>
    </template>
  </section>
</template>
<style scoped>
.collection-generator {
  padding: 20px;
  min-width: 0;
}
.toolbar {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 18px;
  padding: 0;
}
.generator-form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  align-items: start;
  gap: 20px;
}
.generator-intro,
.generator-card {
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 20px;
  min-width: 0;
  background: var(--paper);
}
.generator-intro {
  margin-bottom: 20px;
}
.generator-card h3,
.generator-intro h3 {
  margin: 0 0 20px;
  font-size: 16px;
  font-weight: 600;
}
.generator-poster {
  text-align: center;
}
.generator-poster label {
  text-align: left;
}
.generator-poster img {
  display: block;
  max-width: 100%;
  height: auto;
  margin: 20px auto;
}
.generator-poster > p {
  margin-top: 24px;
}
.generator-form .toolbar-actions {
  margin-top: 14px;
}
.codename-hint {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  margin: -6px 0 0;
  font-size: 12px;
}
.generator-form p {
  line-height: 1.7;
}
@media (max-width: 1000px) {
  .generator-form {
    grid-template-columns: minmax(0, 1fr);
  }
}
.generator-form fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 18px;
}
.generator-form label,
.generator-result label {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin: 0;
}
.generator-form input,
.generator-form select,
.generator-result input {
  width: 100%;
  min-width: 0;
}
.generator-result {
  border-top: 1px solid var(--line);
  margin-top: 24px;
  padding-top: 20px;
  max-width: 660px;
}
.toolbar-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  align-items: center;
}
@media (max-width: 600px) {
  .collection-generator {
    padding: 14px;
  }
}
</style>
