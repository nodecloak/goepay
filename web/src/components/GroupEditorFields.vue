<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  useId,
  watch,
} from "vue";
import { readonly, type ResourceField } from "../resources";
import ResourceFields from "./ResourceFields.vue";
const props = defineProps<{
  fields: ResourceField[];
  values: Record<string, unknown>;
  errors: Record<string, string>;
  editing: boolean;
  disabled: boolean;
  initialTab?: string;
  returnFocus?: HTMLElement;
}>();
const emit = defineEmits<{ change: [name: string, value: unknown] }>();
const tab = ref(props.initialTab || "basic"),
  root = ref<HTMLElement>(),
  id = useId();
const opener =
  props.returnFocus || (document.activeElement as HTMLElement | null);
const definitions = [
  {
    name: "basic",
    label: "基本信息",
    keys: ["name", "description", "enabled"],
  },
  { name: "payment", label: "支付配置", keys: ["rate_bps"] },
  {
    name: "settlement",
    label: "结算配置",
    keys: ["settle_open", "settle_rate"],
  },
  {
    name: "membership",
    label: "会员配置",
    keys: ["price_cents", "duration_days"],
  },
  { name: "notice", label: "通知配置", keys: ["user_transfer"] },
];
const known = new Set(definitions.flatMap((group) => group.keys));
const groups = computed(() =>
  [
    ...definitions.map((group) => ({
      ...group,
      fields: props.fields.filter((field) => group.keys.includes(field.name)),
    })),
    {
      name: "other",
      label: "其它配置",
      fields: props.fields.filter((field) => !known.has(field.name)),
    },
  ]
    .map((group) => ({
      ...group,
      fields: group.fields
        .filter((field) => !readonly(field, props.editing))
        .map((field) =>
          field.name === "name" ? { ...field, label: "用户组名称" } : field,
        ),
    }))
    .filter((group) => group.fields.length),
);
const selected = computed(
  () =>
    groups.value.find((group) => group.name === tab.value) || groups.value[0],
);
watch(groups, () => {
  if (!groups.value.some((group) => group.name === tab.value))
    tab.value = groups.value[0]?.name || "basic";
});
watch(
  () => props.initialTab,
  (value) => {
    if (value && groups.value.some((group) => group.name === value))
      tab.value = value;
  },
);
watch(
  () => props.errors,
  async (errors) => {
    const group = groups.value.find((group) =>
      group.fields.some((field) => errors[field.name]),
    );
    if (!group) return;
    tab.value = group.name;
    await nextTick();
    const field = group.fields.find((field) => errors[field.name]);
    Array.from(root.value?.querySelectorAll<HTMLElement>("[id]") || [])
      .find((element) => element.id === "field-" + field?.name)
      ?.focus();
  },
  { flush: "post" },
);
function keydown(event: KeyboardEvent) {
  if (
    props.disabled ||
    !["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key) ||
    !groups.value.length
  )
    return;
  event.preventDefault();
  const current = groups.value.findIndex((group) => group.name === tab.value),
    delta = event.key === "ArrowLeft" ? -1 : 1;
  const target =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? groups.value.length - 1
        : (current + delta + groups.value.length) % groups.value.length;
  tab.value = groups.value[target]!.name;
  void nextTick(() =>
    root.value
      ?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')
      ?.focus(),
  );
}
onMounted(async () => {
  await nextTick();
  root.value
    ?.querySelector<HTMLElement>(
      "fieldset input:not([disabled]),fieldset select:not([disabled]),fieldset textarea:not([disabled])",
    )
    ?.focus();
});
onBeforeUnmount(() => {
  void nextTick(() => {
    if (opener?.isConnected && !document.querySelector('[role="dialog"]'))
      opener.focus();
  });
});
</script>
<template>
  <div ref="root" class="group-editor">
    <div
      role="tablist"
      aria-label="用户组配置分类"
      class="group-editor-tabs"
      @keydown="keydown"
    >
      <button
        v-for="group in groups"
        :id="id + '-' + group.name + '-tab'"
        :key="group.name"
        type="button"
        role="tab"
        :data-group="group.name"
        :disabled="disabled"
        :aria-selected="selected?.name === group.name"
        :aria-controls="id + '-' + group.name + '-panel'"
        :tabindex="selected?.name === group.name ? 0 : -1"
        @click="tab = group.name"
      >
        {{ group.label }}
      </button>
    </div>
    <fieldset v-if="selected" :disabled="disabled">
      <section
        :id="id + '-' + selected.name + '-panel'"
        role="tabpanel"
        :aria-labelledby="id + '-' + selected.name + '-tab'"
      >
        <p v-if="selected.name === 'payment'" class="muted">
          当前统一支付费率用于新订单，单位为万分比。按支付方式的独立费率和通道分配尚未接入。
        </p>
        <p v-if="selected.name === 'settlement'" class="muted">
          结算开关按原选项生效；余额自动转账比例按现有规则单独配置，留空继承。修改组配置不会自动创建出款。
        </p>
        <p v-if="selected.name === 'notice'" class="muted">
          此开关控制日报代付统计，不等于开通商户代付权限。
        </p>
        <ResourceFields
          :fields="selected.fields"
          :values="values"
          :errors="errors"
          :editing="editing"
          @change="(name, value) => emit('change', name, value)"
        />
      </section>
    </fieldset>
  </div>
</template>
<style scoped>
.group-editor {
  min-width: 0;
}
.group-editor-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
}
.group-editor-tabs button[aria-selected="true"] {
  color: #2563eb;
  background: #eff6ff;
  border-color: #93c5fd;
}
.group-editor fieldset {
  min-width: 0;
  border: 0;
  padding: 0;
  margin: 0;
}
</style>
