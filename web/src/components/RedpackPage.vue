<script setup lang="ts">
import { onMounted, ref } from "vue";
import QRCode from "qrcode";
import type { ApiClient } from "../api";
import {
  defaultRedpackBizNo,
  readRedpacks,
  redpackStatusLabel,
  validRedpackForm,
  type Redpack,
} from "../redpacks";
const props = defineProps<{ client: ApiClient }>();
const admin = props.client.area === "admin";
const form = ref({
  merchant_id: "",
  channel_id: "",
  type: "alipay",
  out_biz_no: defaultRedpackBizNo(),
  amount: "",
  remark: "",
});
const items = ref<Redpack[]>([]),
  open = ref(admin),
  busy = ref(false),
  loading = ref(false),
  error = ref(""),
  notice = ref(""),
  qr = ref(""),
  qrLink = ref("");
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const q =
      admin && form.value.merchant_id
        ? "?merchant_id=" + form.value.merchant_id
        : "";
    const data = readRedpacks(await props.client.request("/redpacks" + q));
    items.value = data.items;
    open.value = data.open;
  } catch {
    error.value = "红包记录读取失败，请刷新重试。";
  } finally {
    loading.value = false;
  }
}
async function show(r: Redpack) {
  qrLink.value = r.claim_url;
  qr.value = await QRCode.toDataURL(r.claim_url, { width: 240, margin: 2 });
}
async function create() {
  const problem = validRedpackForm(form.value, admin);
  if (problem) {
    error.value = problem;
    return;
  }
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const body: Record<string, string> = { ...form.value };
    if (!admin) delete body.merchant_id;
    const r = (await props.client.request("/redpacks", {
      method: "POST",
      body,
    })) as Redpack;
    notice.value = "红包创建成功，请让收款人扫码领取（24小时内有效）。";
    await show(r);
    form.value.out_biz_no = defaultRedpackBizNo();
    form.value.amount = "";
    await load();
  } catch (e) {
    error.value =
      e instanceof Error && e.message ? e.message : "红包创建失败。";
  } finally {
    busy.value = false;
  }
}
async function refresh(r: Redpack) {
  try {
    await props.client.request("/redpacks/" + r.id + "/query", {
      method: "POST",
      body: {},
    });
  } finally {
    await load();
  }
}
onMounted(load);
</script>
<template>
  <section class="panel" aria-label="创建红包">
    <h2>创建红包</h2>
    <p>
      收款人扫描二维码授权后领取，领取时通过支付宝/微信商家转账接口付款，并非现金红包接口。领取时才按转账费率预留余额，过期未领取不占用余额。
    </p>
    <p v-if="!open" role="alert">未开启创建红包功能。</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <form v-if="open" @submit.prevent="create">
      <label v-if="admin"
        >付款商户ID<input v-model.trim="form.merchant_id" inputmode="numeric"
      /></label>
      <label
        >通道选择<select v-model="form.type">
          <option value="alipay">支付宝</option>
          <option value="wxpay">微信</option>
        </select></label
      >
      <label
        >转账通道ID<input
          v-model.trim="form.channel_id"
          inputmode="numeric"
          placeholder="留空自动选择"
      /></label>
      <label
        >交易号<input
          v-model.trim="form.out_biz_no"
          maxlength="19"
          inputmode="numeric"
      /></label>
      <label
        >红包金额<input
          v-model.trim="form.amount"
          placeholder="RMB/元"
          inputmode="decimal"
      /></label>
      <label>红包备注<input v-model="form.remark" maxlength="32" /></label>
      <button type="submit" :disabled="busy">确认创建</button>
    </form>
    <div v-if="qr" class="qr">
      <img :src="qr" alt="红包领取二维码" width="240" height="240" />
      <p>{{ qrLink }}</p>
    </div>
    <h3>红包记录</h3>
    <button type="button" :disabled="loading" @click="load">刷新</button>
    <table>
      <thead>
        <tr>
          <th>交易号</th>
          <th>商户</th>
          <th>类型</th>
          <th>金额</th>
          <th>备注</th>
          <th>状态</th>
          <th>过期时间</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in items" :key="r.id">
          <td>{{ r.out_biz_no }}</td>
          <td>{{ r.merchant_id }}</td>
          <td>{{ r.type === "alipay" ? "支付宝" : "微信" }}</td>
          <td>{{ r.money }}</td>
          <td>{{ r.remark }}</td>
          <td>{{ redpackStatusLabel(r.status) }}</td>
          <td>{{ new Date(r.expires_at).toLocaleString() }}</td>
          <td>
            <button
              v-if="r.status === 'claimable'"
              type="button"
              @click="show(r)"
            >
              二维码
            </button>
            <button
              v-if="r.status === 'claiming'"
              type="button"
              @click="refresh(r)"
            >
              查询
            </button>
          </td>
        </tr>
      </tbody>
    </table>
  </section>
</template>
<style scoped>
form {
  display: grid;
  gap: 10px;
  max-width: 480px;
  margin: 12px 0;
}
label {
  display: grid;
  gap: 4px;
}
input,
select {
  min-height: 34px;
}
.qr {
  margin: 16px 0;
}
.qr p {
  overflow-wrap: anywhere;
}
table {
  width: 100%;
  border-collapse: collapse;
  margin-top: 8px;
}
th,
td {
  padding: 6px;
  border-bottom: 1px solid var(--line, #e5e5e5);
  text-align: left;
}
</style>
