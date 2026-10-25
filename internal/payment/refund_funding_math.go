package payment

func allocateRefundFunding(g, n, f, ug, um, uf, q, remaining int64, payer string) (m, fee, h, a int64, err error) {
	if g <= 0 || n < 0 || f < 0 || n > g || f != g-n || ug < 0 || ug > g || um < 0 || uf < 0 || q <= 0 || q > g-ug || remaining < 0 || remaining > n {
		err = ErrInvalid
		return
	}
	if payer == "platform" {
		if um > n || uf > f {
			err = ErrConflict
			return
		}
		m = q
		if m > n-um {
			m = n - um
		}
		fee = q - m
		if fee > f-uf {
			err = ErrConflict
			return
		}
	} else if payer == "merchant" {
		m = q
	} else {
		err = ErrInvalid
		return
	}
	h = m
	if h > remaining {
		h = remaining
	}
	a = m - h
	return
}
