package channelconfig

// CertificationSnapshot233 is for trusted, authenticated account ciphertext
// only. The payment store must authenticate its head, policy and account proof
// before passing the plaintext snapshot here. It performs no external I/O.
func (f *ParentChannelFactory) CertificationSnapshot233(id int64, raw []byte) (Channel, error) {
	if f == nil || f.PayType() != "alipay" {
		return Channel{}, ErrImport
	}
	in, err := f.decodeParentWithBindings(id, raw)
	if err != nil {
		return Channel{}, err
	}
	if _, _, err = f.buildParentSnapshot(id, in.Channel, in.Bindings); err != nil {
		return Channel{}, err
	}
	return cloneParentChannel(in.Channel), nil
}
