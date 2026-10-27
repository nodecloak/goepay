<script lang="ts">
import { defineComponent, h, computed, type VNodeChild } from "vue";
import { richContent, type RichNode } from "../rich-content";
export default defineComponent({
  props: {
    content: { type: String, default: "" },
    format: { type: String, default: "text" },
    color: { type: String, default: "" },
  },
  setup(props) {
    const nodes = computed(() => {
      try {
        return richContent(props.content, props.format);
      } catch {
        return [{ text: "内容格式无法显示" }];
      }
    });
    function render(node: RichNode): VNodeChild {
      return "text" in node
        ? node.text
        : h(node.tag, node.attrs, node.children.map(render));
    }
    return () =>
      h(
        "div",
        {
          class: ["rich-content", props.format !== "html" ? "rich-plain" : ""],
          style: /^#[0-9a-f]{6}$/i.test(props.color)
            ? { color: props.color }
            : undefined,
        },
        nodes.value.map(render),
      );
  },
});
</script>
<style>
.rich-content {
  overflow-wrap: anywhere;
  min-width: 0;
  line-height: 1.65;
}
.rich-plain {
  white-space: pre-wrap;
}
.rich-content > :first-child {
  margin-top: 0;
}
.rich-content > :last-child {
  margin-bottom: 0;
}
.rich-content img {
  max-width: 100%;
  height: auto;
}
.rich-content table {
  display: block;
  max-width: 100%;
  overflow-x: auto;
  border-collapse: collapse;
}
.rich-content td,
.rich-content th {
  border: 1px solid var(--border, #dce3ed);
  padding: 6px 10px;
}
.rich-content pre {
  white-space: pre-wrap;
}
.rich-content blockquote {
  border-left: 3px solid #dce3ed;
  padding-left: 14px;
  margin-left: 0;
}
</style>
