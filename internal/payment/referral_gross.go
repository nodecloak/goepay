package payment

// A gross basis changes the percentage input, never the source or budget of
// the reward. Quotient/remainder rounding preserves cents without overflow.
func calculateReferralPolicyReward(amount, fee, rate int64, basis string) (int64, error) {
	if amount < 0 || fee < 0 || basis != "fee" && basis != "gross" {
		return 0, ErrInvalid
	}
	base := fee
	if basis == "gross" {
		base = amount
	}
	reward, e := calculateReferralReward(base, rate)
	if e != nil {
		return 0, e
	}
	if reward > fee {
		reward = fee
	}
	return reward, nil
}
