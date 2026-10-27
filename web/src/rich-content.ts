export type RichNode =
  | { text: string }
  | { tag: string; attrs: Record<string, string>; children: RichNode[] };
const tags = new Set([
  "p",
  "div",
  "span",
  "br",
  "strong",
  "b",
  "em",
  "i",
  "u",
  "s",
  "strike",
  "blockquote",
  "ul",
  "ol",
  "li",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "a",
  "img",
  "table",
  "thead",
  "tbody",
  "tfoot",
  "tr",
  "th",
  "td",
  "hr",
  "pre",
  "code",
  "sub",
  "sup",
]);
const forbidden = new Set([
  "script",
  "style",
  "svg",
  "math",
  "iframe",
  "object",
  "embed",
  "template",
  "noscript",
  "meta",
  "link",
  "base",
  "form",
  "input",
  "button",
  "textarea",
  "select",
  "video",
  "audio",
  "source",
  "canvas",
]);
function safeURL(value: string, image = false): string | undefined {
  if (!value || /[\u0000-\u0020\u007f\\]/.test(value)) return;
  if (
    image &&
    /^data:image\/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/]+={0,2}$/.test(
      value,
    ) &&
    value.length <= 16000
  )
    return value;
  let url: URL;
  try {
    url = new URL(value, window.location.href);
  } catch {
    return;
  }
  if (url.username || url.password) return;
  if (image) {
    if (
      !["http:", "https:"].includes(url.protocol) ||
      url.origin !== window.location.origin ||
      !/\.(?:png|jpe?g|gif|webp)$/i.test(url.pathname)
    )
      return;
  } else if (!["http:", "https:", "mailto:", "tel:"].includes(url.protocol))
    return;
  return value;
}
function color(value: string): boolean {
  return /^(?:#[0-9a-f]{3}(?:[0-9a-f]{3})?|(?:rgb|rgba)\([\d., %]+\)|black|white|red|green|blue|yellow|gray|grey|orange|purple|transparent)$/i.test(
    value,
  );
}
function safeStyle(node: HTMLElement): string {
  const values: string[] = [];
  for (const key of [
    "color",
    "background-color",
    "text-align",
    "font-weight",
    "font-style",
    "text-decoration-line",
  ]) {
    const value = node.style.getPropertyValue(key).trim();
    const valid = key.includes("color")
      ? color(value)
      : key === "text-align"
        ? /^(left|right|center|justify)$/.test(value)
        : key === "font-weight"
          ? /^(normal|bold|[1-9]00)$/.test(value)
          : key === "font-style"
            ? /^(normal|italic)$/.test(value)
            : /^(none|underline|line-through|underline line-through)$/.test(
                value,
              );
    if (valid) values.push(`${key}: ${value}`);
  }
  return values.join("; ");
}
export function richContent(input: string, format = "text"): RichNode[] {
  if (input.length > 20000) throw new Error("内容不能超过20000字");
  if (format !== "html") return [{ text: input }];
  // A template's document is inert. Never adopt its nodes into the live DOM.
  const template = document.createElement("template");
  template.innerHTML = input;
  let count = 0;
  function walk(node: Node, depth: number): RichNode[] {
    if (++count > 2000) throw new Error("内容结构过于复杂");
    if (depth > 24) return [];
    if (node.nodeType === Node.TEXT_NODE)
      return [{ text: node.textContent || "" }];
    if (node.nodeType !== Node.ELEMENT_NODE) return [];
    const el = node as HTMLElement,
      tag = el.tagName.toLowerCase();
    if (
      forbidden.has(tag) ||
      tag.includes("-") ||
      el.namespaceURI !== "http://www.w3.org/1999/xhtml"
    )
      return [];
    const children = Array.from(el.childNodes).flatMap((n) =>
      walk(n, depth + 1),
    );
    if (!tags.has(tag)) return children;
    const attrs: Record<string, string> = {},
      style = safeStyle(el);
    if (style) attrs.style = style;
    if (tag === "a") {
      const href = safeURL(el.getAttribute("href") || "");
      if (href) attrs.href = href;
      if (el.getAttribute("target") === "_blank") {
        attrs.target = "_blank";
        attrs.rel = "noopener noreferrer";
      }
      if (el.hasAttribute("title"))
        attrs.title = (el.getAttribute("title") || "").slice(0, 200);
    }
    if (tag === "img") {
      const src = safeURL(el.getAttribute("src") || "", true);
      if (!src) return [];
      attrs.src = src;
      attrs.alt = (el.getAttribute("alt") || "").slice(0, 200);
      attrs.loading = "lazy";
      attrs.referrerpolicy = "no-referrer";
      for (const size of ["width", "height"]) {
        const s = el.getAttribute(size) || "";
        if (/^[1-9]\d{0,3}$/.test(s) && Number(s) <= 2000) attrs[size] = s;
      }
    }
    if (tag === "td" || tag === "th")
      for (const name of ["colspan", "rowspan"]) {
        const v = el.getAttribute(name) || "";
        if (/^[1-9]\d?$/.test(v) && Number(v) <= 20) attrs[name] = v;
      }
    return [{ tag, attrs, children }];
  }
  return Array.from(template.content.childNodes).flatMap((n) => walk(n, 0));
}
export function richFragment(nodes: RichNode[]): DocumentFragment {
  const out = document.createDocumentFragment();
  for (const node of nodes) {
    if ("text" in node) {
      out.append(document.createTextNode(node.text));
      continue;
    }
    const el = document.createElement(node.tag);
    for (const [key, value] of Object.entries(node.attrs))
      el.setAttribute(key, value);
    el.append(richFragment(node.children));
    out.append(el);
  }
  return out;
}
export function safeRichHTML(input: string): string {
  const box = document.createElement("div");
  box.append(richFragment(richContent(input, "html")));
  return box.innerHTML;
}
export function richText(input: string): string {
  return richFragment(richContent(input, "html")).textContent || "";
}
