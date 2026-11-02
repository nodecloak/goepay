import { ApiError } from "./api";
// A dispatched creation may have committed even if its reply was lost.
export function uncertainCreationResult(error: unknown): boolean {
  return !(
    error instanceof ApiError &&
    error.responseDecoded &&
    error.status >= 400 &&
    error.status < 500 &&
    error.status !== 408 &&
    error.status !== 429
  );
}
