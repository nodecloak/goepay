export function refundNotice(data: unknown, httpStatus?: number): string {
  const value = data as
    { status?: string; refund_no?: string } | null | undefined;
  const suffix = value?.refund_no ? `（${value.refund_no}）` : "";
  if (httpStatus === 202)
    return `退款结果尚未核实${suffix}，请保留原请求号并查看退款记录，勿重复发起新的退款。`;
  if (value?.status === "succeeded")
    return `退款成功${suffix}，请核对退款记录和资金明细。`;
  if (value?.status === "failed")
    return `退款失败${suffix}，请查看退款记录核实原因。`;
  if (
    ["pending", "processing", "submitted", "reserved"].includes(
      value?.status || "",
    )
  )
    return `退款处理中${suffix}，请保留请求号并查看退款记录。`;
  return `退款申请已受理，结果尚未核实${suffix}，请查看退款记录。`;
}
export function operationNotice(
  action: "query" | "close",
  data: unknown,
  httpStatus?: number,
): string {
  if (httpStatus === 202 || (action === "close" && httpStatus !== 200))
    return "通道处理结果尚未核实，请稍后再次查询；请勿重新发起写操作。";
  if (action === "close") return "通道关单已确认，请核对最新订单状态。";
  const order = data as { status?: string } | null;
  return order?.status
    ? `已核实通道查询结果，当前订单状态：${order.status}`
    : "通道处理结果尚未核实，请稍后再次查询。";
}
