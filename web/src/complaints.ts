import type { ApiClient } from "./api";

// Provider-neutral complaint shapes the ComplaintsPage renders. The list/detail
// reads and the reply/complete writes run against the existing backoffice
// `complaints` resource + cases endpoints (GET /complaints, GET /complaints/:id,
// POST /complaints/:id/comments, POST /complaints/:id/review). Everything is
// gated behind the complain_open site setting: the merchant 交易投诉 menu entry
// and its endpoints only surface when an administrator turns it on, and the
// admin console pages live under the super-administrator 交易投诉 menu.

export type ComplaintRow = {
  id: string;
  merchant_id: string;
  trade_no: string;
  subject: string;
  content: string;
  status: string; // open | processing | resolved | closed
  pay_type: string;
  amount: string; // integer cents, or "" when unknown
  reply: string;
  times: string; // 投诉次数
  third_mch_id: string; // 渠道子商户号
  created_at: string;
  updated_at: string;
};

// 投诉统计 banner summary (今日/昨日 totals), from /reports/complaints.
export type ComplaintSummary = {
  today_count: string;
  today_alipay: string;
  today_wxpay: string;
  yesterday_count: string;
  yesterday_alipay: string;
  yesterday_wxpay: string;
};

export type ComplaintTaskRow = {
  channel_id: string;
  name: string;
  provider: string;
  enabled: boolean;
  days: number; // 获取条件: only channels with traffic in N days (0 = 不限)
  frequency: number; // minutes
  num: number;
  consecutive_failures: number;
  last_run_at: string;
  last_status: string; // ok | failed | ""
  last_reason: string;
  next_run_at: string;
};

export type ComplaintChannelOption = {
  channel_id: string;
  name: string;
  provider: string;
  enabled: boolean;
};

// Pro's 处理状态 words. The backoffice resource stores open/processing/resolved/
// closed; Pro shows 待处理/处理中/已解决/已关闭 and 待处理 maps to open.
export const complaintStatusWords: Record<string, string> = {
  open: "待处理",
  processing: "处理中",
  done: "已处理",
  resolved: "已解决",
  closed: "已关闭",
};

export const complaintStatusTone: Record<
  string,
  "success" | "warning" | "danger" | "info" | "neutral"
> = {
  open: "warning",
  processing: "info",
  done: "success",
  resolved: "success",
  closed: "neutral",
};

// Pro's 搜索字段 options for 投诉记录 (merchant audit: 综合搜索 plus these).
export const complaintSearchFields = [
  { value: "", label: "综合搜索" },
  { value: "trade_no", label: "关联订单号" },
  { value: "third_id", label: "第三方单号" },
  { value: "category", label: "问题类型" },
  { value: "title", label: "投诉原因" },
  { value: "content", label: "投诉详情" },
  { value: "phone", label: "电话" },
  { value: "third_mch_id", label: "渠道子商户号" },
  { value: "merchant_id", label: "商户号" },
];

const str = (v: unknown): string =>
  v === null || v === undefined ? "" : String(v);

export function parseComplaint(v: Record<string, unknown>): ComplaintRow {
  return {
    id: str(v.id),
    merchant_id: str(v.merchant_id),
    trade_no: str(v.trade_no),
    subject: str(v.subject),
    content: str(v.content),
    status: str(v.status) || "open",
    pay_type: str(v.pay_type),
    amount: str(v.amount),
    reply: str(v.reply),
    times: str(v.times ?? v.complaint_times),
    third_mch_id: str(v.third_mch_id),
    created_at: str(v.created_at),
    updated_at: str(v.updated_at),
  };
}

export type ComplaintListResult = { items: ComplaintRow[]; total: number };

// The provider-synced store (complaints133) is what the Pro pages list — NOT the
// backoffice `complaints` local-ticket resource (which stays on CasePanel / order
// detail). list -> GET /complaints133, detail -> GET /complaints133/:id.
export async function fetchComplaints(
  client: ApiClient,
  query: string,
  signal?: AbortSignal,
): Promise<ComplaintListResult> {
  const data = await client.request<{
    items?: Record<string, unknown>[];
    total?: number;
  }>(`/complaints133${query ? "?" + query : ""}`, { signal });
  return {
    items: (data.items || []).map(parseComplaint),
    total: Number(data.total || 0),
  };
}

export type ComplaintHistoryTurn = {
  operator: string;
  role: string;
  content: string;
  images: string[];
  created_at: string;
};
export type ComplaintDetailRow = ComplaintRow & {
  images: string[];
  negotiation: ComplaintHistoryTurn[];
};

export async function fetchComplaint(
  client: ApiClient,
  id: string,
  signal?: AbortSignal,
): Promise<ComplaintDetailRow> {
  const data = await client.request<Record<string, unknown>>(
    `/complaints133/${encodeURIComponent(id)}`,
    { signal },
  );
  const row = parseComplaint(data) as ComplaintDetailRow;
  row.images = Array.isArray(data.images) ? (data.images as string[]) : [];
  row.negotiation = Array.isArray(data.negotiation)
    ? (data.negotiation as ComplaintHistoryTurn[])
    : [];
  return row;
}

// reply = 回复用户 (a local case comment). The underlying endpoint records a
// local comment only; no upstream provider action is taken.
export async function replyComplaint(
  client: ApiClient,
  id: string,
  content: string,
): Promise<void> {
  await client.request(`/complaints/${encodeURIComponent(id)}/comments`, {
    method: "POST",
    body: { content },
  });
}

// complete/transition = 完成/处理 (local review state). status is one of
// processing/resolved/closed.
export async function transitionComplaint(
  client: ApiClient,
  id: string,
  status: string,
  comment: string,
): Promise<void> {
  const body: Record<string, string> = { status };
  if (comment) body.comment = comment;
  await client.request(`/complaints/${encodeURIComponent(id)}/review`, {
    method: "POST",
    body,
  });
}

// --- provider-backed actions (gated by complain_open) ---

// 回复用户 through the channel API. images are provider media ids.
export async function providerReply(
  client: ApiClient,
  id: string,
  content: string,
  images: string[],
): Promise<void> {
  await client.request(`/complaints/${encodeURIComponent(id)}/provider-reply`, {
    method: "POST",
    body: { content, images },
  });
}

// 处理/反馈完成 through the channel API (feedbackCode is Alipay's category).
export async function providerComplete(
  client: ApiClient,
  id: string,
  content: string,
  feedbackCode: string,
  images: string[],
): Promise<void> {
  await client.request(
    `/complaints/${encodeURIComponent(id)}/provider-complete`,
    { method: "POST", body: { content, feedback_code: feedbackCode, images } },
  );
}

// 补充凭证 (Alipay only).
export async function providerSupplement(
  client: ApiClient,
  id: string,
  content: string,
  images: string[],
): Promise<void> {
  await client.request(
    `/complaints/${encodeURIComponent(id)}/provider-supplement`,
    { method: "POST", body: { content, images } },
  );
}

// Manual per-channel sync now (admin).
export async function syncComplaintChannel(
  client: ApiClient,
  channelId: string,
  num = 20,
): Promise<number> {
  const data = await client.request<{ synced?: number }>(`/complaints-sync`, {
    method: "POST",
    body: { channel_id: Number(channelId), num },
  });
  return Number(data.synced || 0);
}

// Refund-on-complaint: this goes through GOEPAY's EXISTING order refund flow
// (/orders/:trade/refund), which keeps admin funds authorization and the
// immutable ledger. amountYuan is a yuan string; externalNo a stable request id.
export async function refundOnComplaint(
  client: ApiClient,
  tradeNo: string,
  amountYuan: string,
  externalNo: string,
): Promise<void> {
  await client.request(`/orders/${encodeURIComponent(tradeNo)}/refund`, {
    method: "POST",
    body: { amount: amountYuan, external_no: externalNo },
  });
}

// 投诉统计 banner summary, computed from complaints133 (scoped for merchants).
export async function fetchComplaintSummary(
  client: ApiClient,
  signal?: AbortSignal,
): Promise<ComplaintSummary | null> {
  const data = await client.request<{ summary?: ComplaintSummary }>(
    `/complaints-summary`,
    { signal },
  );
  return data.summary || null;
}

// --- 同步任务 (complaint_tasks133 + channel) ---

const num = (v: unknown): number => {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
};

export function parseTask(v: Record<string, unknown>): ComplaintTaskRow {
  return {
    channel_id: str(v.channel_id),
    name: str(v.name),
    provider: str(v.provider),
    enabled: v.enabled === true,
    days: num(v.days),
    frequency: num(v.frequency),
    num: num(v.num),
    consecutive_failures: num(v.consecutive_failures),
    last_run_at: str(v.last_run_at),
    last_status: str(v.last_status),
    last_reason: str(v.last_reason),
    next_run_at: str(v.next_run_at),
  };
}

// 同步任务 page list (admin).
export async function fetchComplaintTasks(
  client: ApiClient,
  signal?: AbortSignal,
): Promise<ComplaintTaskRow[]> {
  const data = await client.request<{ items?: Record<string, unknown>[] }>(
    `/complaint-sync-tasks`,
    { signal },
  );
  return (data.items || []).map(parseTask);
}

// Channels whose provider supports complaints, for the 自动获取 picker (admin).
export async function fetchComplaintChannels(
  client: ApiClient,
  signal?: AbortSignal,
): Promise<ComplaintChannelOption[]> {
  const data = await client.request<{ items?: Record<string, unknown>[] }>(
    `/complaint-channels`,
    { signal },
  );
  return (data.items || []).map((v) => ({
    channel_id: str(v.channel_id),
    name: str(v.name),
    provider: str(v.provider),
    enabled: v.enabled === true,
  }));
}

// Upsert one channel's sync task (自动获取 toggle on the list page, and the
// 同步任务 page's per-row 编辑 dialog: 获取条件 days / 获取频率 frequency /
// 每次同步获取数量 num). Admin.
export async function saveComplaintTask(
  client: ApiClient,
  channelId: string,
  enabled: boolean,
  frequency = 6,
  perRun = 20,
  days = 0,
): Promise<ComplaintTaskRow> {
  const data = await client.request<Record<string, unknown>>(
    `/complaint-sync-tasks/${encodeURIComponent(channelId)}`,
    { method: "PUT", body: { enabled, days, frequency, num: perRun } },
  );
  return parseTask(data);
}

// Inline 状态 toggle on the 同步任务 page (setTaskStatus). Admin.
export async function setComplaintTaskStatus(
  client: ApiClient,
  channelId: string,
  enabled: boolean,
): Promise<ComplaintTaskRow> {
  const data = await client.request<Record<string, unknown>>(
    `/complaint-sync-tasks/${encodeURIComponent(channelId)}`,
    { method: "PATCH", body: { enabled } },
  );
  return parseTask(data);
}

// Delete one channel's sync task (per-row 删除). Admin.
export async function deleteComplaintTask(
  client: ApiClient,
  channelId: string,
): Promise<void> {
  await client.request(
    `/complaint-sync-tasks/${encodeURIComponent(channelId)}`,
    { method: "DELETE" },
  );
}

// Batch delete sync tasks (批量删除). Admin.
export async function deleteComplaintTasks(
  client: ApiClient,
  channelIds: string[],
): Promise<number> {
  const data = await client.request<{ deleted?: number }>(
    `/complaint-sync-tasks/delete`,
    { method: "POST", body: { channel_ids: channelIds.map((c) => Number(c)) } },
  );
  return Number(data.deleted || 0);
}

// 导出投诉记录: download the filtered list as a CSV via the client's standard
// CSV-download flow (BOM/attachment/size checks + browser save). `query` is the
// SAME query string the list uses (搜索字段/状态/支付方式/通道ID/日期范围).
export async function exportComplaints(
  client: ApiClient,
  query: string,
  filename = "complaints.csv",
  signal?: AbortSignal,
): Promise<void> {
  await client.downloadCSV(
    `/complaints133/export${query ? "?" + query : ""}`,
    filename,
    { signal },
  );
}
