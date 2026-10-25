export function merchantPaymentStatisticsFixture(id = "9007199254740993") {
  const trend = Array.from({ length: 7 }, (_, index) => {
    const items = ["alipay", "wxpay", "sandbox"].map((type, i) => ({
      type,
      showname: ["支付宝", "微信支付", "本地模拟"][i]!,
      money: "0.00",
      count: "0",
      success_count: "0",
      rate: "0.00",
    }));
    const summary = {
      type: "total",
      showname: "总计",
      money: "0.00",
      count: "0",
      success_count: "0",
      rate: "0.00",
    };
    if (index === 6) {
      Object.assign(items[0]!, {
        money: "90071992547409.93",
        count: "1",
        success_count: "1",
        rate: "100.00",
      });
      Object.assign(items[1]!, {
        money: "0.01",
        count: "2",
        success_count: "1",
        rate: "50.00",
      });
      Object.assign(summary, {
        money: "90071992547409.94",
        count: "3",
        success_count: "2",
        rate: "66.67",
      });
    } else if (index === 5) {
      Object.assign(items[0]!, {
        money: "1.00",
        count: "2",
        success_count: "1",
        rate: "50.00",
      });
      Object.assign(summary, {
        money: "1.00",
        count: "2",
        success_count: "1",
        rate: "50.00",
      });
    }
    return { date: "2026-10-" + String(index + 20), items, summary };
  });
  return {
    merchant_id: id,
    timezone: "Asia/Shanghai",
    cohort: "merchant_orders_created",
    trend,
    days: { today: trend[6]!, yesterday: trend[5]! },
  };
}
