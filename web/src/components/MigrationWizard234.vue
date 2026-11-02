<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import KitSelect from "./KitSelect.vue";
import KitStatusTag from "./KitStatusTag.vue";
import {
  migrationIssueText234,
  migrationPath234,
  migrationUploadLimit234,
  parseMigrationCapabilities234,
  parseMigrationView234,
  prepareMigration234,
  type MigrationCapabilities234,
  type MigrationView234,
} from "../migration234";

const props = defineProps<{
  client: ApiClient;
  ownSession: boolean;
  adminClass?: string;
}>();
const capabilities = ref<MigrationCapabilities234>(),
  task = ref<MigrationView234>();
const source = ref("epay_pro"),
  prefix = ref(""),
  file = ref<File>(),
  picker = ref<HTMLInputElement>();
const operation = ref<"" | "loading" | "preparing" | "applying" | "reading">(
  "",
);
const error = ref(""),
  uncertain = ref<"" | "prepare" | "apply">(""),
  applyDispatched = ref(false);
const frozen = ref(false),
  archive = ref(false),
  confirmation = ref("");
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.ownSession &&
    props.adminClass === "super" &&
    !!props.client.csrfToken,
);
const busy = computed(() => !!operation.value);
const prefixValid = computed(() => /^[A-Za-z0-9_]{0,59}$/.test(prefix.value));
const sourceValid = computed(() =>
  ["epay_pro", "epay_release"].includes(source.value),
);
const canApply = computed(
  () =>
    allowed.value &&
    !busy.value &&
    !uncertain.value &&
    !applyDispatched.value &&
    task.value?.status === "prepared" &&
    task.value.target_ready &&
    capabilities.value?.target_ready &&
    !task.value.issues.length &&
    frozen.value &&
    archive.value &&
    confirmation.value === "MIGRATE",
);
const labels: Record<MigrationView234["status"], string> = {
  blocked: "预检查未通过",
  prepared: "预检查通过，等待确认",
  applying: "正在导入",
  completed: "导入完成，待配置和启用",
  failed: "导入未完成",
};
const step = computed(() =>
  task.value?.status === "completed"
    ? 3
    : operation.value === "applying" || task.value?.status === "applying"
      ? 2
      : task.value
        ? 1
        : 0,
);
const { confirmDiscard } = useSettingsDraft212(
  "易支付迁移",
  () => !!file.value || busy.value || !!uncertain.value,
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
    props.adminClass,
  ] as const;
}
function current(run: number, c: readonly unknown[]) {
  return (
    alive &&
    epoch === run &&
    allowed.value &&
    c.every((v, i) => v === context()[i])
  );
}
function begin(kind: typeof operation.value) {
  const run = ++epoch,
    c = context();
  controller?.abort();
  controller = new AbortController();
  operation.value = kind;
  error.value = "";
  return { run, c, signal: controller.signal };
}
function clearFile() {
  file.value = undefined;
  if (picker.value) picker.value.value = "";
}
function resetConsent() {
  frozen.value = false;
  archive.value = false;
  confirmation.value = "";
}
function validFile(event: Event) {
  const input = event.target as HTMLInputElement,
    selected = input.files?.[0];
  file.value = undefined;
  error.value = "";
  if (!selected) return;
  if (
    !/\.sql$/i.test(selected.name) ||
    selected.size < 1 ||
    selected.size >
      Math.min(
        capabilities.value?.max_upload_bytes ?? migrationUploadLimit234,
        migrationUploadLimit234,
      )
  ) {
    input.value = "";
    error.value = "请选择单个非空SQL文件，最大64MiB；压缩文件请先在本机解压。";
    return;
  }
  file.value = selected;
}
function showRefusal(e: unknown, kind: "prepare" | "apply") {
  if (
    e instanceof ApiError &&
    e.responseDecoded &&
    [400, 401, 403, 409, 413, 415, 422, 429].includes(e.status)
  ) {
    error.value =
      e.status === 429
        ? "迁移繁忙或暂存次数、空间达到上限，请先读取迁移记录；持续出现请联系管理员处理暂存。"
        : kind === "prepare"
          ? "预检查未被接受，请检查会话、备份格式和大小后重新选择。"
          : "导入未被接受，请读取当前任务状态并核对目标库；不要重复提交。";
  } else {
    uncertain.value = kind;
    error.value =
      kind === "prepare"
        ? "预检查结果尚未确认。请读取迁移记录查找已创建的任务，不要重复上传。"
        : "导入结果尚未确认。只能读取当前任务状态，不会自动重复导入。";
  }
}
async function load() {
  if (!allowed.value || busy.value) return;
  const { run, c, signal } = begin("loading");
  try {
    const v = parseMigrationCapabilities234(
      await props.client.request(migrationPath234, { signal }),
    );
    if (current(run, c)) capabilities.value = v;
  } catch {
    if (current(run, c))
      error.value = "无法读取迁移条件，请检查本人管理员会话后重新读取。";
  } finally {
    if (current(run, c)) operation.value = "";
  }
}
async function prepare() {
  if (
    !allowed.value ||
    busy.value ||
    uncertain.value ||
    !file.value ||
    !capabilities.value ||
    !prefixValid.value ||
    !sourceValid.value
  )
    return;
  const selected = file.value,
    { run, c, signal } = begin("preparing");
  resetConsent();
  task.value = undefined;
  applyDispatched.value = false;
  try {
    const v = await prepareMigration234(
      props.client,
      source.value,
      prefix.value,
      selected,
      signal,
    );
    if (current(run, c)) task.value = v;
  } catch (e) {
    if (current(run, c)) showRefusal(e, "prepare");
  } finally {
    if (current(run, c)) {
      operation.value = "";
      clearFile();
    }
  }
}
async function readTask(id: string) {
  if (!allowed.value || busy.value || !/^[a-f0-9]{32}$/.test(id)) return;
  const { run, c, signal } = begin("reading");
  try {
    const v = parseMigrationView234(
      await props.client.request(`${migrationPath234}/${id}`, { signal }),
    );
    if (v.id !== id) throw Error("mismatched task");
    if (task.value?.id === id && task.value.sha256 !== v.sha256)
      throw Error("mismatched backup");
    if (current(run, c)) {
      const original = task.value?.id === id;
      task.value = v;
      clearFile();
      resetConsent();
      if (!original) applyDispatched.value = false;
      if (["completed", "failed", "blocked"].includes(v.status))
        uncertain.value = "";
      else if (uncertain.value === "prepare") uncertain.value = "";
    }
  } catch {
    if (current(run, c))
      error.value = "当前任务状态读取失败，请稍后手动读取；不会重试导入。";
  } finally {
    if (current(run, c)) operation.value = "";
  }
}
async function apply() {
  if (!canApply.value || !task.value) return;
  const id = task.value.id,
    sha = task.value.sha256,
    { run, c, signal } = begin("applying");
  applyDispatched.value = true;
  try {
    const v = parseMigrationView234(
      await props.client.request(`${migrationPath234}/${id}/apply`, {
        method: "POST",
        body: {
          expected_sha256: sha,
          source_frozen: frozen.value,
          archive_acknowledged: archive.value,
          confirmation: confirmation.value,
        },
        signal,
      }),
    );
    if (v.id !== id || v.sha256 !== sha) throw Error("mismatched task");
    if (current(run, c)) {
      task.value = v;
      resetConsent();
    }
  } catch (e) {
    if (current(run, c)) showRefusal(e, "apply");
  } finally {
    if (current(run, c)) operation.value = "";
  }
}
async function chooseAgain() {
  if (busy.value || uncertain.value || !(await confirmDiscard())) return;
  task.value = undefined;
  applyDispatched.value = false;
  clearFile();
  resetConsent();
  error.value = "";
}
watch(
  context,
  () => {
    ++epoch;
    controller?.abort();
    operation.value = "";
    capabilities.value = undefined;
    task.value = undefined;
    clearFile();
    resetConsent();
    error.value = "";
    uncertain.value = "";
    applyDispatched.value = false;
    if (allowed.value) void load();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  alive = false;
  ++epoch;
  controller?.abort();
  clearFile();
  resetConsent();
});
</script>

<template>
  <section
    class="panel migration234"
    aria-label="易支付迁移向导"
    :aria-busy="busy"
  >
    <div class="migration234-heading">
      <h2>易支付迁移</h2>
      <button type="button" :disabled="!allowed || busy" @click="load">
        读取迁移记录
      </button>
    </div>
    <p class="hint">
      上传易支付Pro或Release的MySQL备份，预检查后一次导入到全新的GOEPAY实例。不会连接原站、执行备份SQL或发起资金交易。
    </p>
    <p v-if="!allowed" role="alert">
      此工具仅限本人超级管理员会话使用，请重新登录有权限的管理员账号。
    </p>
    <template v-else>
      <ol class="migration234-steps" aria-label="迁移步骤">
        <li
          v-for="(s, i) in ['上传备份', '预检查与确认', '执行导入', '结果核对']"
          :key="s"
          :aria-current="step === i ? 'step' : undefined"
          :class="{ current: step === i, done: step > i }"
        >
          <span>{{ i + 1 }}</span
          >{{ s }}
        </li>
      </ol>
      <p v-if="error" role="alert" class="error">{{ error }}</p>
      <p v-if="uncertain" role="status">
        {{
          uncertain === "prepare"
            ? "请手动读取迁移记录，再选择刚才的任务。即使暂未出现记录，也不要立即重复上传。"
            : "结果未知时不会再次发送导入请求。请手动读取当前任务状态。"
        }}
      </p>
      <p
        v-if="capabilities && !capabilities.target_ready"
        class="migration234-warning"
        role="status"
      >
        当前实例已有业务数据，或不满足空库条件，不能执行导入。可以预检查备份和查看迁移记录；此工具不提供清空、覆盖或合并现有业务数据的操作。
      </p>
      <p v-if="!capabilities && operation === 'loading'" role="status">
        正在读取目标库迁移条件…
      </p>
      <section class="migration234-scope" aria-label="迁移范围">
        <div>
          <h3>原生导入</h3>
          <p>
            符合校验规则的商户、历史订单及期初余额。历史付款不会再次入账，也不会生成支付通知、退款或出款任务。
          </p>
        </div>
        <div>
          <h3>仅归档</h3>
          <p>
            完整原始备份加密保全。用户组、站点配置、通道、原密钥与登录资料、插件及其它未转换记录不会自动成为可用业务。导入的商户保持停用并生成新密钥，切流前需重新配置和核验。
          </p>
        </div>
      </section>
      <div v-if="!task" class="migration234-upload">
        <label
          ><span>原系统版本</span
          ><KitSelect
            v-model="source"
            aria-label="原系统版本"
            :disabled="busy || !!uncertain"
            :options="[
              { value: 'epay_pro', label: '易支付Pro' },
              { value: 'epay_release', label: '易支付Release' },
            ]"
        /></label>
        <label
          ><span>源表前缀</span
          ><input
            v-model="prefix"
            aria-label="源表前缀"
            :aria-invalid="!prefixValid"
            placeholder="留空自动检测，例如 pre_"
            maxlength="59"
            autocomplete="off"
            :disabled="busy || !!uncertain"
          /><small v-if="!prefixValid" role="alert"
            >表前缀仅允许字母、数字和下划线，最多59字符。</small
          ><small v-else
            >多组源表并存时必须明确前缀，不会自动猜测。</small
          ></label
        >
        <div class="migration234-file">
          <input
            ref="picker"
            type="file"
            accept=".sql"
            aria-label="易支付SQL备份"
            class="migration234-file-input"
            :disabled="busy || !!uncertain || !capabilities"
            @change="validFile"
          /><button
            type="button"
            :disabled="busy || !!uncertain || !capabilities"
            @click="picker?.click()"
          >
            选择SQL备份</button
          ><span v-if="file" role="status"
            >已选择备份（{{ (file.size / 1024 / 1024).toFixed(2) }} MiB）</span
          ><span v-else>单个UTF-8 .sql文件，最大64MiB</span>
        </div>
        <p class="hint">
          请先停止原站写入并导出一致备份。上传内容可能含密钥和身份资料，保留在当前实例的加密存档中，不会回显明文。
        </p>
        <button
          type="button"
          class="primary"
          :disabled="
            busy ||
            !!uncertain ||
            !file ||
            !capabilities ||
            !prefixValid ||
            !sourceValid
          "
          @click="prepare"
        >
          {{ operation === "preparing" ? "正在上传并检查…" : "上传并预检查" }}
        </button>
      </div>
      <section
        v-if="task"
        class="migration234-preview"
        aria-label="迁移任务结果"
      >
        <div class="migration234-heading">
          <KitStatusTag
            :status="task.status"
            :text="labels[task.status]"
          /><button type="button" :disabled="busy" @click="readTask(task.id)">
            读取当前任务状态
          </button>
        </div>
        <p class="hint">
          任务编号 <code>{{ task.id }}</code>
        </p>
        <p class="hint">
          来源
          {{
            task.source === "epay_pro" ? "易支付Pro" : "易支付Release"
          }}；表前缀
          <code>{{ task.prefix || "无前缀" }}</code>
        </p>
        <p class="hint migration234-digest">
          备份SHA-256 <code>{{ task.sha256 }}</code>
        </p>
        <p v-if="task.expires_at" class="hint">
          预检查有效期至 {{ new Date(task.expires_at).toLocaleString() }}
        </p>
        <div v-if="busy || task.status === 'applying'" role="status">
          <p>
            {{
              operation === "applying"
                ? "正在导入，请保留页面；网络中断时先读取任务状态。"
                : "正在读取，请稍候…"
            }}
          </p>
          <progress
            max="100"
            aria-label="迁移进度"
            :value="busy ? undefined : task.progress"
          />
        </div>
        <dl class="migration234-totals">
          <div
            v-for="(value, label) in {
              商户: task.merchant_count,
              历史订单: task.order_count,
              历史退款: task.refund_count,
              历史转账: task.transfer_count,
              期初可用余额: task.available_total + ' 元',
              期初冻结余额: task.reserved_total + ' 元',
            }"
            :key="label"
          >
            <dt>{{ label }}</dt>
            <dd>{{ value }}</dd>
          </div>
        </dl>
        <section
          v-if="task.issues.length"
          class="migration234-warning"
          aria-label="阻塞项"
        >
          <h3>先解决以下阻塞项</h3>
          <ul>
            <li v-for="(issue, i) in task.issues" :key="i">
              <code>{{ issue.path }}</code>
              {{ migrationIssueText234(issue.code) }}
            </li>
          </ul>
          <p>不会跳过、猜测或静默修复这些记录。</p>
        </section>
        <details v-if="task.warnings.length">
          <summary>需要核对的事项（{{ task.warnings.length }}）</summary>
          <ul>
            <li v-for="(issue, i) in task.warnings" :key="i">
              <code>{{ issue.path }}</code>
              {{ migrationIssueText234(issue.code) }}
            </li>
          </ul>
        </details>
        <div
          class="table-scroll"
          role="region"
          aria-label="源表处理方式，可横向滚动"
          tabindex="0"
        >
          <table aria-label="源表迁移范围">
            <thead>
              <tr>
                <th>源表</th>
                <th>记录数</th>
                <th>处理方式</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="t in task.table_counts" :key="t.name">
                <td>
                  <code>{{ t.name }}</code>
                </td>
                <td>{{ t.rows }}</td>
                <td>
                  {{
                    t.mode === "native"
                      ? "原生导入；其余字段保留于加密归档"
                      : "仅归档，不自动启用"
                  }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <fieldset
          v-if="task.status === 'prepared'"
          :disabled="busy || !!uncertain || applyDispatched"
        >
          <legend>确认本次导入</legend>
          <label class="migration234-check"
            ><input
              v-model="frozen"
              type="checkbox"
              aria-label="确认源站停止写入且备份一致"
            />我已停止源站写入，并确认这是同一时点的一致备份。</label
          ><label class="migration234-check"
            ><input
              v-model="archive"
              type="checkbox"
              aria-label="确认仅归档数据不会自动启用"
            />我理解仅归档记录不会激活原配置、通道、账号密码或插件；导入后仍需配置通道、核验余额并启用商户。</label
          ><label
            ><span>输入 MIGRATE 确认导入</span
            ><input
              v-model="confirmation"
              aria-label="输入MIGRATE确认导入"
              autocomplete="off"
              spellcheck="false"
              maxlength="7"
              placeholder="MIGRATE" /></label
          ><button
            type="button"
            class="primary"
            :disabled="!canApply"
            @click="apply"
          >
            开始导入
          </button>
        </fieldset>
        <div
          v-if="task.status === 'completed'"
          class="migration234-success"
          role="status"
        >
          <h3>导入完成，待配置和启用</h3>
          <p>
            期初余额和历史记录已提交；同一备份不会重复增加余额。请核对结果，重新配置支付通道及商户接入密钥，逐项启用后再切换域名。原始备份保留为加密存档。
          </p>
          <p>没有自动发起支付、退款、结算或通知。</p>
        </div>
        <p v-if="task.status === 'failed'" role="status">
          本次未完成导入，请核对记录和备份。不要直接重复提交；先读取状态确认事务结果。
        </p>
        <button
          v-if="task.status !== 'applying'"
          type="button"
          :disabled="busy || !!uncertain"
          @click="chooseAgain"
        >
          重新选择备份
        </button>
      </section>
      <section
        v-if="capabilities?.runs.length"
        class="migration234-history"
        aria-label="本账号迁移记录"
      >
        <h3>本账号迁移记录</h3>
        <p class="hint">
          上传响应丢失时，先读取记录并选择对应任务；不会自动重传或重做导入。
        </p>
        <ul>
          <li v-for="run in capabilities.runs" :key="run.id">
            <span
              ><code>{{ run.id }}</code> ·
              {{ run.source === "epay_pro" ? "Pro" : "Release" }} ·
              {{ labels[run.status] }}</span
            ><button type="button" :disabled="busy" @click="readTask(run.id)">
              查看任务 {{ run.id.slice(0, 8) }}
            </button>
          </li>
        </ul>
      </section>
    </template>
  </section>
</template>

<style scoped>
.migration234 {
  display: grid;
  gap: 18px;
  min-width: 0;
}
.migration234 h2,
.migration234 h3,
.migration234 p {
  margin: 0;
}
.migration234-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.migration234-steps {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}
.migration234-steps li {
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 12px;
  border-bottom: 2px solid var(--line);
  color: var(--ink-muted);
}
.migration234-steps li.current {
  border-color: var(--primary-text);
  color: var(--primary-text);
  font-weight: 600;
}
.migration234-steps li.done {
  color: var(--ink);
}
.migration234-steps li span {
  border: 1px solid currentColor;
  border-radius: 50%;
  width: 23px;
  height: 23px;
  display: inline-grid;
  place-items: center;
  flex-shrink: 0;
}
.migration234-scope {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.migration234-scope > div {
  padding: 16px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: grid;
  gap: 8px;
}
.migration234-upload,
.migration234-preview,
.migration234 fieldset {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.migration234-upload > label,
.migration234 fieldset > label:not(.migration234-check) {
  display: grid;
  gap: 8px;
  max-width: 600px;
}
.migration234-file {
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
}
.migration234-file-input {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
.migration234-warning,
.migration234-success {
  padding: 16px;
  border: 1px solid var(--line);
  border-left: 3px solid var(--warning);
  border-radius: 5px;
  display: grid;
  gap: 10px;
}
.migration234-success {
  border-left-color: var(--success);
}
.migration234-totals {
  margin: 0;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}
.migration234-totals > div {
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 5px;
}
.migration234-totals dt {
  color: var(--ink-muted);
}
.migration234-totals dd {
  margin: 6px 0 0;
  font-weight: 600;
  font-size: 18px;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}
.migration234 code,
.migration234-digest {
  overflow-wrap: anywhere;
  word-break: break-word;
}
.migration234 table {
  width: 100%;
  min-width: 440px;
  border-collapse: collapse;
  text-align: left;
}
.migration234 td:first-child code {
  white-space: nowrap;
  overflow-wrap: normal;
  word-break: normal;
}
.migration234 th,
.migration234 td {
  padding: 10px;
  border-bottom: 1px solid var(--line);
}
.migration234 .table-scroll {
  overflow: auto;
}
.migration234 fieldset {
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 18px;
}
.migration234-check {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  line-height: 1.7;
}
.migration234-check input {
  margin-top: 5px;
  flex-shrink: 0;
}
.migration234-history ul {
  padding: 0;
  list-style: none;
  display: grid;
  gap: 12px;
}
.migration234-history li {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
}
.migration234 progress {
  width: 100%;
  accent-color: var(--primary-text);
}
.migration234 button {
  justify-self: start;
  cursor: pointer;
}
.migration234 button:disabled {
  cursor: not-allowed;
}
.migration234 summary {
  cursor: pointer;
  padding: 8px 0;
}
.migration234 input:not([type="checkbox"]):not([type="file"]) {
  width: 100%;
  box-sizing: border-box;
}
.migration234 small {
  color: var(--ink-muted);
}
@media (max-width: 700px) {
  .migration234-steps {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .migration234-scope {
    grid-template-columns: 1fr;
  }
  .migration234-totals {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .migration234-heading {
    align-items: flex-start;
  }
}
@media (prefers-reduced-motion: reduce) {
  .migration234 * {
    transition: none !important;
  }
}
</style>
