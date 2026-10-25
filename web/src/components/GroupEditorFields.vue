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
import {
  groupPaymentTypes,
  readGroupPaymentRates,
  setGroupPaymentRate,
} from "../group-payment-rates";
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
  {
    name: "payment",
    label: "支付配置",
    keys: ["rate_bps", "pay_maxmoney", "pay_minmoney", "info"],
  },
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
  { name: "transfer", label: "代付配置", keys: ["user_transfer"] },
];
const known = new Set(definitions.flatMap((group) => group.keys));
const typeRates = computed(() => {
  try {
    return readGroupPaymentRates(props.values.info);
  } catch {
    return null;
  }
});
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
          field.name === "name"
            ? { ...field, label: "用户组名称" }
            : field.name === "user_transfer"
              ? {
                  ...field,
                  label: "代付功能",
                  options: [
                    { value: "", label: "缺省（与系统设置一致）" },
                    ...(["0", "_"].includes(String(props.values.user_transfer))
                      ? [
                          {
                            value: String(props.values.user_transfer),
                            label: "缺省（保留原值，与系统设置一致）",
                          },
                        ]
                      : []),
                    { value: "1", label: "开启" },
                  ],
                }
              : field,
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
          统一支付费率单位为万分比。下方分成比例为商户保留的百分比，留空或0使用统一费率；仅在支付手续费开启时用于新订单。通道分配仍须单独配置。
          最大、最小支付金额单位为元，留空继承系统设置，0不限制；修改后仅约束新订单。
        </p>
        <p v-if="selected.name === 'settlement'" class="muted">
          结算开关按原选项生效；余额自动转账比例按现有规则单独配置，留空继承。修改组配置不会自动创建出款。
        </p>
        <p v-if="selected.name === 'transfer'" class="muted">
          缺省继承系统设置，旧值0同样继承；开启用于该组普通代付。仍需商户自身权限、可用通道和执行开关，修改不会自动发起付款。
        </p>
        <ResourceFields
          :fields="selected.fields.filter((field) => field.name !== 'info')"
          :values="values"
          :errors="errors"
          :editing="editing"
          @change="(name, value) => emit('change', name, value)"
        />
        <div
          v-if="selected.fields.some((f) => f.name === 'info')"
          id="field-info"
          role="group"
          aria-label="支付方式分成比例"
          tabindex="-1"
        >
          <div v-if="!typeRates" role="alert">
            支付方式分成比例格式不正确，请核对原配置。
          </div>
          <div v-else class="table-scroll">
            <table>
              <thead>
                <tr>
                  <th scope="col">支付方式</th>
                  <th scope="col">分成比例（%）</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="kind in groupPaymentTypes" :key="kind.key">
                  <th scope="row">{{ kind.label }}</th>
                  <td>
                    <input
                      :aria-label="kind.label + '分成比例（%）'"
                      :value="typeRates[kind.key]?.rate ?? ''"
                      :aria-invalid="!!errors.info"
                      inputmode="decimal"
                      maxlength="6"
                      placeholder="留空继承"
                      @input="
                        emit(
                          'change',
                          'info',
                          setGroupPaymentRate(
                            values.info,
                            kind.key,
                            ($event.target as HTMLInputElement).value,
                          ),
                        )
                      "
                    />
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-if="errors.info" role="alert" class="field-error">
            {{ errors.info }}
          </p>
        </div>
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
