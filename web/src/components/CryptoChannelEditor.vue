<script setup lang="ts">
import { onBeforeUnmount, reactive, ref, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import {
  cryptoChannelInput,
  readCryptoChannel,
  type CryptoChannel,
  type CryptoChannelDraft,
} from "../crypto-channels";
import KitModal from "./KitModal.vue";
const props = defineProps<{ client: ApiClient; row?: CryptoChannel }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const draft = reactive<CryptoChannelDraft>({
  name: props.row?.name || "",
  network: props.row?.network || "tron",
  enabled: props.row?.enabled ?? false,
  archived: props.row?.archived ?? false,
  fee: String((props.row?.fee_bps ?? 0) / 100),
  min_amount: props.row?.min_amount || "0",
  max_amount: props.row?.max_amount || "0",
  day_limit: props.row?.day_limit || "0",
  merchants: props.row?.merchant_ids.join(", ") || "",
});
const busy = ref(false),
  error = ref(""),
  unknown = ref(false);
const requestID = crypto.randomUUID().replaceAll("-", "");
const client = props.client,
  csrf = client.csrfToken,
  originalID = props.row?.id;
let alive = true;
function current() {
  return (
    alive &&
    client === props.client &&
    csrf === props.client.csrfToken &&
    client.area === "admin" &&
    originalID === props.row?.id
  );
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.client.area,
    props.row?.id,
  ],
  () => emit("close"),
);
onBeforeUnmount(() => {
  alive = false;
});
async function save() {
  if (props.row?.archived || busy.value || unknown.value || !current()) return;
  error.value = "";
  let body: Record<string, unknown>;
  try {
    body = cryptoChannelInput(draft, props.row?.revision || "0");
  } catch (e) {
    error.value = e instanceof Error ? e.message : "请检查填写内容";
    return;
  }
  if (!props.row) body.request_id = requestID;
  busy.value = true;
  try {
    const row = readCryptoChannel(
      await client.request(
        "/payment-channels/crypto" + (originalID ? "/" + originalID : ""),
        { method: originalID ? "PATCH" : "POST", body },
      ),
    );
    if (!current()) return;
    if ((originalID && row.id !== originalID) || row.network !== draft.network)
      throw new Error("通道保存结果不匹配");
    emit("saved");
  } catch (e) {
    if (!current()) return;
    const rejected =
      e instanceof ApiError &&
      e.responseDecoded &&
      [400, 401, 403, 409, 422, 429].includes(e.status);
    unknown.value = !rejected;
    error.value = rejected
      ? e.message
      : "保存结果尚未确认，请关闭后刷新通道列表核对，勿重复新增或保存。";
  } finally {
    if (current()) busy.value = false;
  }
}
</script>
<template>
  <KitModal
    :title="
      row?.archived
        ? 'USDT 通道详情'
        : row
          ? '编辑 USDT 通道'
          : '新增 USDT 通道'
    "
    drawer
    width="640px"
    :loading="busy"
    :submit-disabled="unknown"
    :submit-text="row?.archived ? '关闭' : undefined"
    :hide-cancel="row?.archived"
    @close="emit('close')"
    @submit="row?.archived ? emit('close') : save()"
  >
    <p v-if="row?.archived" class="crypto-channel-note" role="status">
      此通道已归档，只能查看，不能恢复或修改；历史订单继续保留原通道资料。
    </p>
    <p
      v-if="row && !row.runtime_enabled"
      class="crypto-channel-note"
      role="status"
    >
      主网收款关闭。这里保存通道配置，不能开启部署端的收款、签名或广播服务。
    </p>
    <p v-if="!row" class="crypto-channel-note">
      实际收款还受部署开关控制；保存配置不会启动签名或广播服务。
    </p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <fieldset
      class="crypto-channel-form"
      :disabled="!!row?.archived || busy || unknown"
    >
      <label class="crypto-channel-wide"
        >通道名称<input
          v-model="draft.name"
          name="name"
          maxlength="120"
          required
          autocomplete="off"
      /></label>
      <label
        >收款网络<select
          v-model="draft.network"
          name="network"
          :disabled="!!row"
        >
          <option value="tron">TRON（TRC20）</option>
          <option value="ethereum">Ethereum（ERC20）</option>
        </select></label
      >
      <label
        >手续费率（%）<input
          v-model="draft.fee"
          name="fee"
          inputmode="decimal"
          required
      /></label>
      <label
        >最低单笔（USDT）<input
          v-model="draft.min_amount"
          name="min_amount"
          inputmode="decimal"
          required
      /></label>
      <label
        >最高单笔（USDT）<input
          v-model="draft.max_amount"
          name="max_amount"
          inputmode="decimal"
          required
      /></label>
      <label
        >每日收款上限（USDT）<input
          v-model="draft.day_limit"
          name="day_limit"
          inputmode="decimal"
          required
      /></label>
      <p class="muted crypto-channel-wide">
        金额最多六位小数，0 表示不限制。每日上限按 UTC
        日期累计新建收款金额，待付款订单也计入。用户组单独配置分成比例时，按用户组费率创建新单。
      </p>
      <label class="crypto-channel-wide"
        >授权商户 ID<textarea
          v-model="draft.merchants"
          name="merchants"
          rows="3"
          placeholder="例如：1001, 1002；可用逗号或换行分隔"
        />
      </label>
      <p class="muted crypto-channel-wide">
        留空表示不授权任何商户。通道必须同时满足商户授权、用户组规则和部署状态才能收款。
      </p>
      <label class="crypto-channel-toggle"
        ><input
          v-model="draft.enabled"
          type="checkbox"
          name="enabled"
          :disabled="draft.archived"
        />允许新收款</label
      >
      <label v-if="row" class="crypto-channel-toggle"
        ><input
          v-model="draft.archived"
          type="checkbox"
          name="archived"
          @change="draft.archived && (draft.enabled = false)"
        />归档通道</label
      >
      <p v-if="row" class="muted crypto-channel-wide">
        网络不可修改；关闭或归档不改变历史订单的网络、金额和地址。
      </p>
    </fieldset>
  </KitModal>
</template>
<style scoped>
.crypto-channel-form {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem 1.25rem;
}
.crypto-channel-form label {
  display: grid;
  gap: 0.45rem;
  min-width: 0;
  align-content: start;
}
.crypto-channel-form input:not([type="checkbox"]),
.crypto-channel-form select,
.crypto-channel-form textarea {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.crypto-channel-wide {
  grid-column: 1/-1;
}
.crypto-channel-toggle {
  display: flex !important;
  align-items: center;
}
.crypto-channel-toggle input {
  width: auto;
}
.crypto-channel-note {
  padding: 0.85rem;
  border: 1px solid var(--pa-border, #d8dee8);
  border-radius: 8px;
  line-height: 1.6;
  overflow-wrap: anywhere;
}
.muted {
  font-size: 0.9rem;
  line-height: 1.6;
  margin: 0;
  overflow-wrap: anywhere;
}
@media (max-width: 540px) {
  .crypto-channel-form {
    grid-template-columns: minmax(0, 1fr);
  }
  .crypto-channel-wide {
    grid-column: auto;
  }
}
</style>
