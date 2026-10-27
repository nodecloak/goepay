import { describe, it, expect } from "vitest";
import { richContent, safeRichHTML, richText } from "../src/rich-content";

describe("original announcement rich content", () => {
  it("preserves basic original formatting, colors, lists and tables", () => {
    const html = safeRichHTML(
      '<p style="color:#123456;background-color:rgb(255, 0, 0);text-align:center">欢迎<strong>商户</strong><em>使用</em><u>支付</u><s>旧版</s></p><ul><li>第一项</li></ul><table><tbody><tr><th>标题</th><td colspan="2">内容</td></tr></tbody></table>',
    );
    const el = document.createElement("div");
    el.innerHTML = html;
    expect(el.querySelector("p")?.style.color).toBe("rgb(18, 52, 86)");
    expect(el.querySelector("p")?.style.backgroundColor).toBe("rgb(255, 0, 0)");
    expect(el.querySelector("p")?.style.textAlign).toBe("center");
    for (const tag of [
      "strong",
      "em",
      "u",
      "s",
      "ul",
      "li",
      "table",
      "th",
      "td",
    ])
      expect(el.querySelector(tag)).not.toBeNull();
    expect(el.querySelector("td")?.getAttribute("colspan")).toBe("2");
  });
  it("removes executable namespaces, embedded documents, handlers and arbitrary CSS", () => {
    const html = safeRichHTML(
      '<script>window.pwned=1</script><svg><a href="javascript:alert(1)">svg</a></svg><math><mtext>bad</mtext></math><iframe src="https://tracker.invalid"></iframe><p id="x" class="modal" onclick="alert(1)" style="position:fixed;background-image:url(https://tracker.invalid/x);color:red">正常</p>',
    );
    expect(html).not.toMatch(
      /script|svg|math|iframe|onclick|position|background-image|tracker|id=|class=/i,
    );
    expect(richText(html)).toBe("正常");
  });
  it("validates decoded URLs and disables credentialed, obfuscated and executable links", () => {
    for (const href of [
      "javascript:alert(1)",
      "java&#10;script:alert(1)",
      "data:text/html,boom",
      "https://user:secret@example.com/",
      "\\\\evil.invalid/x",
    ]) {
      const html = safeRichHTML(`<a href="${href}" target="_blank">点击</a>`);
      expect(html).not.toContain("href=");
      expect(richText(html)).toBe("点击");
    }
    const el = document.createElement("div");
    el.innerHTML = safeRichHTML(
      '<a href="https://example.com/help?q=1" target="_blank">帮助</a><a href="/merchant/dashboard">首页</a>',
    );
    expect(el.querySelector("a")?.getAttribute("rel")).toBe(
      "noopener noreferrer",
    );
    expect(el.querySelectorAll("a")[1]?.getAttribute("href")).toBe(
      "/merchant/dashboard",
    );
  });
  it("permits bounded bitmap images and rejects remote tracking and SVG data", () => {
    const html = safeRichHTML(
      '<img src="https://tracker.invalid/a.png"><img src="data:image/svg+xml;base64,PHN2Zz4="><img src="/uploads/logo.png" onerror="alert(1)" width="200"><img src="data:image/png;base64,iVBORw0KGgo=">',
    );
    const el = document.createElement("div");
    el.innerHTML = html;
    expect(el.querySelectorAll("img")).toHaveLength(2);
    expect(html).not.toMatch(/onerror|tracker|svg/);
    expect(el.querySelector("img")?.getAttribute("loading")).toBe("lazy");
  });
  it("keeps legacy text literal and accepts only explicit html", () => {
    expect(richContent("<img src=x onerror=alert(1)>", "text")).toEqual([
      { text: "<img src=x onerror=alert(1)>" },
    ]);
    expect(richContent("<strong>内容</strong>", "unknown")).toEqual([
      { text: "<strong>内容</strong>" },
    ]);
    expect(richText("<p>第一行</p><p>第二行</p>")).toContain("第一行");
  });
  it("bounds input, node count and depth and is idempotent", () => {
    expect(() => safeRichHTML("a".repeat(20001))).toThrow(/20000/);
    const html = safeRichHTML(
      "<div>".repeat(100) + "内容" + "</div>".repeat(100),
    );
    expect(html.length).toBeLessThan(1000);
    const result = safeRichHTML(
      '<p style="color:#ff0000">文本<a href="/help">链接</a></p>',
    );
    expect(safeRichHTML(result)).toBe(result);
  });
});
