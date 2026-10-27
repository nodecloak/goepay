<script setup lang="ts">
import {
  computed,
  nextTick,
  onMounted,
  onBeforeUnmount,
  ref,
  toRaw,
  watch,
} from "vue";
import { ApiError, type ApiClient } from "../api";
import {
  readCoreRiskActions,
  readCoreRiskRelease,
  coreRiskKind,
  coreRiskRule,
  type CoreRiskAction,
} from "../core-risk-actions";
const props = defineProps<{
  client: ApiClient;
  allowed: boolean;
  policyFingerprint: string;
  blocked: boolean;
}>();
const emit = defineEmits<{ busy: [boolean] }>();
const items = ref<CoreRiskAction[]>([]),
  loaded = ref(false),
  loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref(""),
  target = ref<CoreRiskAction>(),
  page = ref(1),
  refresh = ref<HTMLButtonElement>(),
  dialog = ref<HTMLElement>();
const pages = computed(() => Math.max(1, Math.ceil(items.value.length / 20))),
  visible = computed(() =>
    items.value.slice((page.value - 1) * 20, page.value * 20),
  );
let alive = true,
  epoch = 0,
  abort: AbortController | undefined,
  client = toRaw(props.client),
  csrf = client.csrfToken,
  opener: HTMLElement | undefined;
function reset() {
  ++epoch;
  abort?.abort();
  items.value = [];
  loaded.value = false;
  loading.value = false;
  busy.value = false;
  uncertain.value = false;
  target.value = undefined;
  notice.value = "";
  opener = undefined;
  client = toRaw(props.client);
  csrf = client.csrfToken;
  emit("busy", false);
}
function context() {
  if (client === toRaw(props.client) && csrf === client.csrfToken) return true;
  reset();
  error.value = "会话已变化，请刷新阻断记录后重新核对。";
  return false;
}
function valid(stamp: number, c: AbortController) {
  return (
    alive && stamp === epoch && !c.signal.aborted && props.allowed && context()
  );
}
async function load() {
  if (!props.allowed || busy.value) return;
  context();
  abort?.abort();
  const c = new AbortController(),
    stamp = ++epoch;
  abort = c;
  loading.value = true;
  error.value = "";
  target.value = undefined;
  try {
    const result = readCoreRiskActions(
      await client.request("/payment-risk/actions", { signal: c.signal }),
    );
    if (!valid(stamp, c)) return;
    items.value = result;
    page.value = 1;
    loaded.value = true;
    uncertain.value = false;
  } catch {
    if (valid(stamp, c)) {
      loaded.value = false;
      items.value = [];
      error.value = "阻断记录无法核实，请刷新后查看。";
    }
  } finally {
    if (valid(stamp, c)) loading.value = false;
  }
}
async function begin(row: CoreRiskAction, event: Event) {
  if (
    !context() ||
    !props.allowed ||
    props.blocked ||
    busy.value ||
    loading.value ||
    uncertain.value ||
    !row.active
  )
    return;
  opener = event.currentTarget as HTMLElement;
  target.value = { ...row, source_order_ids: [...row.source_order_ids] };
  await nextTick();
  dialog.value?.focus();
}
async function cancel() {
  if (busy.value) return;
  target.value = undefined;
  await nextTick();
  if (opener?.isConnected) opener.focus();
  else refresh.value?.focus();
}
async function release() {
  if (
    !context() ||
    !target.value ||
    !props.allowed ||
    props.blocked ||
    busy.value
  )
    return;
  const original = target.value,
    expectedPolicy = props.policyFingerprint,
    c = new AbortController(),
    stamp = ++epoch;
  abort = c;
  busy.value = true;
  emit("busy", true);
  error.value = "";
  notice.value = "";
  try {
    const after = readCoreRiskRelease(
      await client.request(
        "/payment-risk/actions/" + original.id + "/release",
        {
          method: "POST",
          signal: c.signal,
          body: {
            expected_fingerprint: original.fingerprint,
            expected_policy_fingerprint: expectedPolicy,
          },
        },
      ),
      original,
    );
    if (!valid(stamp, c) || expectedPolicy !== props.policyFingerprint) return;
    items.value = items.value.map((v) => (v.id === original.id ? after : v));
    notice.value =
      "此项风控限制已解除；账户、支付权限和通道的其它关闭状态继续生效。";
    target.value = undefined;
    await nextTick();
    refresh.value?.focus();
  } catch (e) {
    if (valid(stamp, c)) {
      target.value = undefined;
      uncertain.value = true;
      error.value =
        e instanceof ApiError && e.status === 409
          ? "阻断记录或策略已变化，请刷新后重新核对。"
          : "解除结果无法确认，请刷新列表核对；不会自动重复解除。";
    }
  } finally {
    if (valid(stamp, c)) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
function keydown(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    void cancel();
  }
  if (e.key === "Tab" && dialog.value) {
    const controls = Array.from(
        dialog.value.querySelectorAll<HTMLButtonElement>(
          "button:not(:disabled)",
        ),
      ),
      first = controls[0],
      last = controls.at(-1);
    if (!first) {
      e.preventDefault();
      return;
    }
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last?.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.allowed,
    props.policyFingerprint,
  ],
  () => {
    reset();
    error.value = "会话或策略已变化，请刷新阻断记录。";
  },
);
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  ++epoch;
  abort?.abort();
  emit("busy", false);
});
</script>
<template>
  <section class="risk-actions" aria-label="风控阻断记录">
    <div class="toolbar">
      <h3>风控阻断记录</h3>
      <button
        ref="refresh"
        :disabled="busy || loading || !allowed"
        @click="load"
      >
        刷新阻断记录
      </button>
    </div>
    <p>
      自动检查开关控制后续检测。已有阻断按期限继续生效，解除仅针对选中的一项风控原因。
    </p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <p v-if="loading" role="status">正在核对阻断记录…</p>
    <p v-else-if="loaded && !items.length">
      当前没有核心风控阻断记录。其它黑名单、权限及通道限制仍按原规则生效。
    </p>
    <div v-if="loaded && items.length" class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>记录 / 对象</th>
            <th>规则与有效状态</th>
            <th>关联订单</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in visible" :key="row.id">
            <td>
              <div>{{ row.id }}</div>
              <small
                >{{ coreRiskKind(row.kind)
                }}<span v-if="row.target_id !== '0'">
                  {{ row.target_id }}</span
                ></small
              >
            </td>
            <td>
              {{ coreRiskRule(row.rule)
              }}<small class="cell">{{
                row.active ? "新付款受限" : "已解除或到期"
              }}</small
              ><small class="cell"
                >{{ row.until ? "期限：" + row.until : "无到期时间" }} ·
                {{ row.changed_at }}</small
              >
            </td>
            <td>
              共 {{ row.source_count }} 笔<small class="cell">{{
                row.source_order_ids.join("、")
              }}</small
              ><small
                v-if="row.source_count > row.source_order_ids.length"
                class="cell muted"
                >仅显示前 {{ row.source_order_ids.length }} 个订单 ID</small
              >
            </td>
            <td>
              <button
                :disabled="
                  !allowed ||
                  blocked ||
                  busy ||
                  loading ||
                  uncertain ||
                  !row.active
                "
                @click="begin(row, $event)"
              >
                解除此项风控
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <div class="toolbar">
        <button :disabled="busy || page <= 1" @click="page--">上一页</button
        ><span>{{ page }} / {{ pages }} 页，共 {{ items.length }} 项</span
        ><button :disabled="busy || page >= pages" @click="page++">
          下一页
        </button>
      </div>
    </div>
    <div v-if="target" class="modal-backdrop">
      <section
        ref="dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="risk177-release-title"
        tabindex="-1"
        class="modal"
        @keydown="keydown"
      >
        <h3 id="risk177-release-title">确认解除此项风控</h3>
        <p>
          记录 {{ target.id }} · {{ coreRiskKind(target.kind) }}
          {{ target.target_id === "0" ? "" : target.target_id }}
        </p>
        <p>
          {{
            coreRiskRule(target.rule)
          }}。只解除这项风控，不会开启管理员关闭的账户、支付权限或通道；其它原因继续生效。
        </p>
        <div class="toolbar">
          <button :disabled="busy" @click="cancel">取消</button
          ><button class="primary" :disabled="busy" @click="release">
            {{ busy ? "正在确认…" : "确认解除此项" }}
          </button>
        </div>
      </section>
    </div>
  </section>
</template>
<style scoped>
.risk-actions {
  margin-top: 32px;
  min-width: 0;
}
.table-wrap {
  max-width: 100%;
  overflow-x: auto;
}
.risk-actions table {
  min-width: 700px;
}
.risk-actions td:first-child div {
  white-space: nowrap;
}
.risk-actions td {
  max-width: 360px;
  overflow-wrap: anywhere;
}
.cell {
  display: block;
  margin-top: 6px;
}
.modal {
  max-width: min(600px, calc(100vw - 28px));
  max-height: 85dvh;
  overflow: auto;
}
</style>
