import { centsMoney } from "./settlement-terms";
export interface PrintReceipt {
  trade_no: string;
  merchant_name: string;
  name: string;
  amount: string;
  type: string;
  paid_at: string;
  remark: string;
}
export interface PrintJob {
  id: string;
  trade_no: string;
  external_no: string;
  request_no: string;
  state: string;
  reason: string;
  provider: string;
  device_masked: string;
  copies: number;
  voice: boolean;
  receipt: PrintReceipt;
  task_id?: string;
  task_state?: string;
  can_recover?: boolean;
}
export interface PlatformPrinting {
  revision: string;
  enabled: boolean;
  provider: string;
  app_id_masked: string;
  secret_present: boolean;
  external_execution_available: boolean;
}
export interface MerchantPrinting {
  revision: string;
  mode: number;
  copies: number;
  voice: boolean;
  device_masked: string;
  device_present: boolean;
}
export function printingState(state: string, provider: string): string {
  switch (state) {
    case "queued":
      return "等待处理，尚未发送";
    case "sending":
      return "发送处理中；请勿重复创建";
    case "accepted":
      return `${provider === "local_fake" ? "本地模拟受理" : "打印网关受理"}；不代表实际打印完成`;
    case "rejected":
      return "打印请求被拒绝；支付结果不受影响";
    case "unknown":
      return "打印结果未知，禁止自动重发；请核对实体设备";
    default:
      return "未识别的打印状态，请查询原记录";
  }
}
export function printingMoney(value: unknown): string {
  try {
    return centsMoney(value);
  } catch {
    throw new Error("收据金额格式无效");
  }
}
export function manualPrintBody(externalNo: string) {
  if (!/^[A-Za-z0-9_-]{1,64}$/.test(externalNo))
    throw new Error("打印请求号格式无效");
  return {
    external_no: externalNo,
    previous_job_id: "0",
    acknowledge_duplicate: false,
  };
}
export function recoverPrintBody(taskID: unknown) {
  if (
    typeof taskID !== "string" ||
    !/^[1-9][0-9]{0,18}$/.test(taskID) ||
    BigInt(taskID) > 9223372036854775807n
  )
    throw new Error("原打印任务号无效");
  return { expected_task_id: taskID };
}
