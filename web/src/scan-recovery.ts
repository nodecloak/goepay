export interface ScanRecoveryView {
  state: string;
  queries: number;
  maximum_queries: number;
  last_provider_state?: string;
  next_query_at?: string;
  started_at: string;
  expires_at: string;
  requires_reconciliation: boolean;
}
