import { sharingID } from "./sharing-overview";

export interface SharingReturnIntent {
  version: 1;
  merchant_id: string;
  sharing_id: string;
  sharing_external_no: string;
  receiver_id: string;
  external_no: string;
  amount: string;
  state: "unknown" | "unsent" | "claimed" | "pending" | "succeeded" | "failed";
  record_id?: string;
}
const states = [
  "unknown",
  "unsent",
  "claimed",
  "pending",
  "succeeded",
  "failed",
];
const problem =
  "无法安全保存或读取原回退请求，请恢复浏览器存储后核对；尚未发起新的回退";
export class SharingReturnIntentStore {
  readonly key: string;
  constructor(
    readonly principal: string,
    readonly merchant: string,
    readonly sharing: string,
    readonly external: string,
    readonly storage: Storage = localStorage,
  ) {
    if (
      !/^[a-f0-9]{64}$/.test(principal) ||
      !sharingID(merchant) ||
      !sharingID(sharing) ||
      !/^[A-Za-z0-9_-]{1,128}$/.test(external)
    )
      throw Error(problem);
    this.key = `goepay.sharing-return.v1.${principal}.${merchant}.${sharing}`;
  }
  private validate(raw: unknown): SharingReturnIntent {
    const v = raw as SharingReturnIntent;
    if (
      !v ||
      typeof v !== "object" ||
      Array.isArray(v) ||
      v.version !== 1 ||
      v.merchant_id !== this.merchant ||
      v.sharing_id !== this.sharing ||
      v.sharing_external_no !== this.external ||
      !sharingID(v.receiver_id) ||
      !sharingID(v.amount) ||
      !/^return_[a-f0-9]{24}$/.test(v.external_no) ||
      !states.includes(v.state) ||
      (v.record_id !== undefined && !sharingID(v.record_id)) ||
      Object.keys(v).some(
        (k) =>
          ![
            "version",
            "merchant_id",
            "sharing_id",
            "sharing_external_no",
            "receiver_id",
            "external_no",
            "amount",
            "state",
            "record_id",
          ].includes(k),
      )
    )
      throw Error(problem);
    return Object.freeze({ ...v });
  }
  read(): SharingReturnIntent | null {
    try {
      const raw = this.storage.getItem(this.key);
      if (raw === null) return null;
      if (raw.length > 2048) throw Error();
      return this.validate(JSON.parse(raw));
    } catch {
      throw Error(problem);
    }
  }
  save(value: SharingReturnIntent): SharingReturnIntent {
    try {
      const checked = this.validate(value),
        raw = JSON.stringify(checked);
      const old = this.read();
      if (old) {
        for (const key of [
          "merchant_id",
          "sharing_id",
          "sharing_external_no",
          "receiver_id",
          "external_no",
          "amount",
        ] as const)
          if (old[key] !== checked[key]) throw Error();
        if (
          old.record_id &&
          checked.record_id &&
          old.record_id !== checked.record_id
        )
          throw Error();
        if (["succeeded", "failed"].includes(old.state)) return old;
      }
      this.storage.setItem(this.key, raw);
      if (this.storage.getItem(this.key) !== raw) throw Error();
      return checked;
    } catch {
      throw Error(problem);
    }
  }
  clear(expected?: SharingReturnIntent): void {
    try {
      const old = this.read();
      if (
        expected &&
        old &&
        Object.keys(old).some(
          (k) =>
            old[k as keyof SharingReturnIntent] !==
            expected[k as keyof SharingReturnIntent],
        )
      )
        throw Error();
      this.storage.removeItem(this.key);
      if (this.storage.getItem(this.key) !== null) throw Error();
    } catch {
      throw Error(problem);
    }
  }

  // Callers hold this browser-origin lock across the original dispatch. A
  // second tab can inspect the saved intent, but cannot create another one.
  async exclusive<T>(operation: () => Promise<T> | T): Promise<T> {
    if (!navigator.locks?.request)
      throw Error(
        "当前浏览器不支持安全的跨窗口回退，请使用新版浏览器后核对原请求",
      );
    return navigator.locks.request(
      this.key,
      { mode: "exclusive", ifAvailable: true },
      (lock) => {
        if (!lock)
          throw Error("另一个窗口正在处理该回退，请核对原请求，不要重复提交");
        return operation();
      },
    );
  }
}
