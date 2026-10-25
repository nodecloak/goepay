package payment

import adapter "goepay/internal/adapters/payment"

// PaymentCostQuote comes only from approved server routing; merchant JSON cannot
// populate its private fields or invent an estimate for historical orders.
type PaymentCostQuote struct {
	channelID int64
	method    string
	estimate  adapter.PaymentCostEstimate
}

func (g ProviderFlow) PaymentCostQuote(channelID int64, method string) (*PaymentCostQuote, error) {
	if channelID <= 0 || !providerMethodPattern.MatchString(method) {
		return nil, ErrInvalid
	}
	p := g.Providers[channelID]
	if p == nil {
		return nil, ErrInvalid
	}
	source, ok := p.(adapter.PaymentCostEstimator)
	if !ok {
		return nil, nil
	}
	estimate, known := source.PaymentCostEstimate()
	if !known {
		return nil, nil
	}
	return &PaymentCostQuote{channelID: channelID, method: method, estimate: estimate}, nil
}
