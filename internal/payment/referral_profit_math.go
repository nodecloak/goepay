package payment

const referralCostDenominator int64 = 100000000

type referralProfitCalculation struct {
	CostWhole, CostRemainder, CostDenominator int64
	Profit, Reward                            int64
}

// calculateReferralProfitCost rounds the fee-minus-exact-estimated-cost once.
// An exact half-cent of cost is therefore rounded down when subtracting it
// from an integer fee. No estimated cost becomes a ledger expense.
func calculateReferralProfitCost(amount, fee, units, rewardRate int64) (referralProfitCalculation, error) {
	var out referralProfitCalculation
	if amount < 0 || fee < 0 || fee > amount || units < 0 || units > referralCostDenominator || rewardRate < 0 || rewardRate > 10000 {
		return out, ErrInvalid
	}
	product := amount % referralCostDenominator * units
	out.CostWhole = amount/referralCostDenominator*units + product/referralCostDenominator
	out.CostRemainder = product % referralCostDenominator
	out.CostDenominator = referralCostDenominator
	if fee > out.CostWhole {
		out.Profit = fee - out.CostWhole
		if out.CostRemainder > referralCostDenominator/2 {
			out.Profit--
		}
	}
	var err error
	out.Reward, err = calculateReferralReward(out.Profit, rewardRate)
	return out, err
}
