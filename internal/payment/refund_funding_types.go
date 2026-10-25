package payment

type RefundFundingSnapshot struct {
	ExistingRefundID int64  `json:"existing_refund_id,string"`
	OrderID          int64  `json:"order_id,string"`
	HoldID           int64  `json:"hold_id,string"`
	HoldFundingID    int64  `json:"hold_funding_id,string"`
	Gross            int64  `json:"gross,string"`
	MerchantNet      int64  `json:"merchant_net,string"`
	OriginalFee      int64  `json:"original_fee,string"`
	MerchantAmount   int64  `json:"merchant_amount,string"`
	FeeAmount        int64  `json:"fee_amount,string"`
	HeldOriginal     int64  `json:"held_original,string"`
	HeldRemaining    int64  `json:"held_remaining,string"`
	HeldPending      int64  `json:"held_pending,string"`
	HeldSucceeded    int64  `json:"held_succeeded,string"`
	HeldAmount       int64  `json:"held_amount,string"`
	AvailableAmount  int64  `json:"available_amount,string"`
	FundingMode      string `json:"funding_mode"`
	SourceHash       []byte `json:"-"`
	HistoryHash      []byte `json:"-"`
	RequestHash      []byte `json:"-"`
	ResultHash       []byte `json:"-"`
}
