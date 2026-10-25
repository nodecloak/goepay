package payment

// Terms are calculated from the original order's gross and merchant-net
// snapshot. Current balances and later fee-policy edits are not funding proof.
type ChannelSharingRuleItem struct {
	ReceiverID  int64  `json:"receiver_id,string"`
	Mode        string `json:"mode"`
	RateBPS     int64  `json:"rate_bps,string"`
	FixedAmount int64  `json:"fixed_amount,string"`
}

const maxChannelSharingMoney int64 = 999999999999

func validateChannelSharingRules(items []ChannelSharingRuleItem) error {
	if len(items) == 0 || len(items) > 50 {
		return ErrInvalid
	}
	seen := map[int64]bool{}
	rates, fixed := int64(0), int64(0)
	for _, item := range items {
		if item.ReceiverID <= 0 || seen[item.ReceiverID] {
			return ErrInvalid
		}
		seen[item.ReceiverID] = true
		switch item.Mode {
		case "rate":
			if item.RateBPS <= 0 || item.RateBPS > 10000 || item.FixedAmount != 0 || item.RateBPS > 10000-rates {
				return ErrInvalid
			}
			rates += item.RateBPS
		case "fixed":
			if item.FixedAmount <= 0 || item.FixedAmount > maxChannelSharingMoney-fixed || item.RateBPS != 0 {
				return ErrInvalid
			}
			fixed += item.FixedAmount
		default:
			return ErrInvalid
		}
	}
	return nil
}
func calculateChannelSharing(gross, merchantNet, minimum int64, items []ChannelSharingRuleItem) ([]int64, int64, error) {
	if gross <= 0 || gross > maxChannelSharingMoney || merchantNet < 0 || merchantNet > gross || minimum < 0 || minimum > maxChannelSharingMoney {
		return nil, 0, ErrInvalid
	}
	if e := validateChannelSharingRules(items); e != nil {
		return nil, 0, e
	}
	if gross < minimum {
		return nil, 0, nil
	}
	amounts := make([]int64, len(items))
	total := int64(0)
	for i, item := range items {
		amount := item.FixedAmount
		if item.Mode == "rate" {
			amount = gross/10000*item.RateBPS + gross%10000*item.RateBPS/10000
		}
		if amount > merchantNet-total {
			return nil, 0, ErrInsufficient
		}
		amounts[i] = amount
		total += amount
	}
	return amounts, total, nil
}
