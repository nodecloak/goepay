import { inject, onBeforeUnmount, provide, type InjectionKey } from "vue";

export interface SettingsDraftRegistry212 {
  register(label: string, isDirty: () => boolean): () => void;
  confirmDiscard?(label: string): Promise<boolean>;
}

// Getters read each panel's live, in-memory state. No credentials, snapshots or
// business requests are copied into a global singleton or browser storage.
export const settingsDraftRegistry212Key: InjectionKey<SettingsDraftRegistry212> =
  Symbol.for("goepay:settings-drafts212");

export function useSettingsDraft212(label: string, isDirty: () => boolean) {
  const registry = inject(settingsDraftRegistry212Key, undefined);
  const unregister = registry?.register(label, isDirty);
  // A panel reload may also unmount its children. Keep the confirmation scoped
  // to that panel's subtree while forwarding every getter to the page guard.
  const descendants = new Map<
    symbol,
    { label: string; isDirty: () => boolean }
  >();
  provide(settingsDraftRegistry212Key, {
    register(childLabel, childDirty) {
      const id = Symbol();
      descendants.set(id, { label: childLabel, isDirty: childDirty });
      const remove = registry?.register(childLabel, childDirty);
      return () => {
        descendants.delete(id);
        remove?.();
      };
    },
    confirmDiscard: registry?.confirmDiscard,
  });
  onBeforeUnmount(() => unregister?.());
  return {
    async confirmDiscard(
      descendantLabels?: readonly string[],
    ): Promise<boolean> {
      const labels = [
        ...(isDirty() ? [label] : []),
        ...[...descendants.values()]
          .filter(
            (entry) =>
              (!descendantLabels || descendantLabels.includes(entry.label)) &&
              entry.isDirty(),
          )
          .map((entry) => entry.label),
      ];
      if (!labels.length || !registry?.confirmDiscard) return true;
      return registry.confirmDiscard([...new Set(labels)].join("、"));
    },
  };
}
