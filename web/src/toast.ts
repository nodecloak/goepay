import { ref } from "vue";

// Central transient-notice ("toast") mechanism for the consoles. Successful
// saves surface here as a floating kit toast; errors stay as inline .alert
// banners next to the control that failed.
export type ToastTone = "success" | "warning" | "danger" | "info";
export interface Toast {
  id: number;
  text: string;
  tone: ToastTone;
}

const toasts = ref<Toast[]>([]);
let seq = 0;
const timers = new Map<number, ReturnType<typeof setTimeout>>();

export function dismissToast(id: number): void {
  const timer = timers.get(id);
  if (timer) {
    clearTimeout(timer);
    timers.delete(id);
  }
  toasts.value = toasts.value.filter((t) => t.id !== id);
}

export function pushToast(
  text: string,
  tone: ToastTone = "success",
  duration = 3200,
): number {
  const id = ++seq;
  toasts.value = [...toasts.value, { id, text, tone }];
  if (duration > 0)
    timers.set(
      id,
      setTimeout(() => dismissToast(id), duration),
    );
  return id;
}

// Convenience used by successful-save flows.
export function toastSaved(text = "保存成功"): number {
  return pushToast(text, "success");
}

export function useToasts() {
  return { toasts, pushToast, toastSaved, dismissToast };
}
