// Keep a list on a real page when a deletion or concurrent update shrinks it.
export function listPageWithinTotal(
  page: number,
  total: number,
  size: number,
): number {
  return Math.max(1, Math.min(page, Math.max(1, Math.ceil(total / size))));
}
