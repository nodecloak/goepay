// Order and refund numbers are shown in the code font (.pa-order-no) wherever
// the console prints them, so long mixed digits and letters are easy to read
// and compare. These are the row fields that hold one.
export const orderNumberKeys: ReadonlySet<string> = new Set([
  "trade_no",
  "out_trade_no",
  "api_trade_no",
  "sub_trade_no",
  "refund_no",
  "out_refund_no",
  "platform_refund_no",
]);
export const isOrderNumberKey = (key: string): boolean =>
  orderNumberKeys.has(key);

// copyText puts text on the clipboard. It uses the async Clipboard API, and
// falls back to a hidden textarea with execCommand("copy") where that API is
// missing (a plain-http deployment). The fallback textarea is placed inside
// `host` so focus stays inside an open dialog. Resolves true when copied.
export async function copyText(
  text: string,
  host?: HTMLElement | null,
): Promise<boolean> {
  if (!text) return false;
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Permission denied or not allowed here: try the fallback below.
  }
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.setAttribute("aria-hidden", "true");
  area.style.position = "fixed";
  area.style.left = "-9999px";
  area.style.opacity = "0";
  const parent = host ?? document.body;
  const previous = document.activeElement as HTMLElement | null;
  parent.appendChild(area);
  area.select();
  let copied = false;
  try {
    copied = document.execCommand("copy");
  } catch {
    copied = false;
  }
  area.remove();
  previous?.focus?.();
  return copied;
}
