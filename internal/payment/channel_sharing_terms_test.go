package payment

import (
	"errors"
	"testing"
)

func TestChannelSharingTermsRoundOnceCapOriginalMerchantMoneyAndNeverOverflow(t *testing.T) {
	rules := []ChannelSharingRuleItem{{ReceiverID: 1, Mode: "rate", RateBPS: 3333}, {ReceiverID: 2, Mode: "rate", RateBPS: 3333}, {ReceiverID: 3, Mode: "rate", RateBPS: 3334}}
	amounts, total, e := calculateChannelSharing(101, 101, 0, rules)
	if e != nil || len(amounts) != 3 || amounts[0] != 33 || amounts[1] != 33 || amounts[2] != 33 || total != 99 {
		t.Fatal("per-recipient floor must leave original remainder", amounts, total, e)
	}
	if _, _, e = calculateChannelSharing(101, 98, 0, rules); !errors.Is(e, ErrInsufficient) {
		t.Fatal("sharing consumed original platform fee", e)
	}
	if amounts, total, e = calculateChannelSharing(999999999999, 999999999999, 0, []ChannelSharingRuleItem{{ReceiverID: 1, Mode: "rate", RateBPS: 10000}}); e != nil || total != 999999999999 || amounts[0] != total {
		t.Fatal("overflow at maximum payment", amounts, total, e)
	}
	if amounts, total, e = calculateChannelSharing(100, 100, 101, rules); e != nil || total != 0 || len(amounts) != 0 {
		t.Fatal("below minimum should not declare sharing", amounts, total, e)
	}
	if amounts, total, e = calculateChannelSharing(1, 1, 0, []ChannelSharingRuleItem{{ReceiverID: 1, Mode: "rate", RateBPS: 1}}); e != nil || total != 0 || len(amounts) != 1 || amounts[0] != 0 {
		t.Fatal("sub-cent shares are never rounded upward", amounts, total, e)
	}
}
func TestChannelSharingTermsRejectDuplicateRecipientsAndMixedRuleInterpretations(t *testing.T) {
	for _, rules := range [][]ChannelSharingRuleItem{
		{{ReceiverID: 1, Mode: "rate", RateBPS: 1}, {ReceiverID: 1, Mode: "rate", RateBPS: 2}},
		{{ReceiverID: 1, Mode: "rate", RateBPS: 10001}},
		{{ReceiverID: 1, Mode: "fixed", FixedAmount: 0}},
		{{ReceiverID: 1, Mode: "rate", RateBPS: 10, FixedAmount: 10}},
		{{ReceiverID: 1, Mode: "fixed", RateBPS: 10, FixedAmount: 10}},
		{{ReceiverID: 1, Mode: "unknown", RateBPS: 10}},
	} {
		if _, _, e := calculateChannelSharing(100, 100, 0, rules); !errors.Is(e, ErrInvalid) {
			t.Fatal("ambiguous rule accepted", rules, e)
		}
	}
	if _, _, e := calculateChannelSharing(100, 100, 0, []ChannelSharingRuleItem{{ReceiverID: 1, Mode: "fixed", FixedAmount: 101}}); !errors.Is(e, ErrInsufficient) {
		t.Fatal("fixed share exceeded original money", e)
	}
}
