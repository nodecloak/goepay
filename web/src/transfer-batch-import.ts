import {
  batchChannelTypes,
  type BatchDraftRow,
  type BatchPolicy,
} from "./transfer-batches";
export interface ImportedRow {
  source_row: number;
  serial: string;
  account: string;
  name: string;
  amount: string;
  remark: string;
}
export function importErrorLabel(message: string): string {
  const match = /^row ([1-9]\d{0,2}): ([a-z_]+)$/.exec(message),
    code = match?.[2] || message;
  const labels: Record<string, string> = {
    positive_decimal_amount_required: "金额须为正数且最多两位小数",
    account_required: "请填写收款账号",
    account_must_be_text: "账号须使用文本单元格，请勿用Excel数值账号",
    unique_positive_sequence_required: "序号须为唯一正整数",
    formula_not_allowed: "不支持公式",
    formula_or_external_content_not_allowed: "不支持公式或外链",
    invalid_text_length_or_control: "文字长度或控制字符不合法",
    maximum_100_rows: "最多导入100条",
    header_required: "请使用原五列标题",
    empty_file_no_payment_rows: "没有有效付款项目",
    utf8_required: "CSV须为UTF-8编码",
    invalid_csv_five_columns: "CSV须为五列且引号合法",
    duplicate_cell_coordinates: "表格单元格坐标重复",
    invalid_cell_coordinates: "表格仅支持A至E五列",
  };
  return `文件校验失败${match ? "，第 " + match[1] + " 行" : ""}：${labels[code] || "请检查文件格式、五列内容与100条上限"}`;
}
export interface ImportPreview {
  merchant_id: string;
  format: string;
  count: number;
  total_cents: string;
  rows: {
    source_row: number;
    serial: string;
    account_masked: string;
    name_masked: string;
    amount: string;
    has_remark: boolean;
  }[];
  draft_items: ImportedRow[];
}
export function validateImportPreview(value: unknown): ImportPreview {
  const v = value as ImportPreview;
  const fail = () => {
    throw new Error("导入预览格式不符，请重新选择文件");
  };
  if (
    !v ||
    !Array.isArray(v.rows) ||
    !Array.isArray(v.draft_items) ||
    v.count < 1 ||
    v.count > 100 ||
    v.count !== v.rows.length ||
    v.count !== v.draft_items.length ||
    !/^\d+$/.test(v.total_cents) ||
    !["csv", "xlsx"].includes(v.format)
  )
    fail();
  let sum = 0n;
  const seen = new Set<string>();
  for (let i = 0; i < v.draft_items.length; i++) {
    const row = v.draft_items[i]!,
      masked = v.rows[i]!;
    if (
      !row ||
      !masked ||
      !Number.isInteger(row.source_row) ||
      row.source_row < 2 ||
      !/^[1-9]\d*$/.test(row.serial) ||
      seen.has(row.serial) ||
      typeof row.account !== "string" ||
      !row.account ||
      row.account.length > 256 ||
      typeof row.name !== "string" ||
      row.name.length > 128 ||
      typeof row.remark !== "string" ||
      row.remark.length > 256 ||
      typeof row.amount !== "string" ||
      !/^\d+\.\d{2}$/.test(row.amount) ||
      masked.source_row !== row.source_row ||
      masked.serial !== row.serial ||
      masked.amount !== row.amount ||
      typeof masked.account_masked !== "string" ||
      typeof masked.name_masked !== "string" ||
      typeof masked.has_remark !== "boolean"
    )
      fail();
    const cents = BigInt(row.amount.replace(".", ""));
    if (cents <= 0n || cents > 100000000000n) fail();
    sum += cents;
    seen.add(row.serial);
  }
  if (sum.toString() !== v.total_cents) fail();
  return v;
}
export function importDraftRows(
  preview: unknown,
  policy: BatchPolicy,
  channel: string,
  type: string,
): BatchDraftRow[] {
  const v = validateImportPreview(preview);
  if (
    !batchChannelTypes(policy.channels.find((c) => c.id === channel)).includes(
      type,
    )
  )
    throw new Error("请选择当前已确认能力的导入通道和付款方式");
  return v.draft_items.map((row) => ({
    channel_id: channel,
    type,
    account: row.account,
    name: row.name,
    amount: row.amount,
    remark: row.remark,
    bank_code: "",
  }));
}
