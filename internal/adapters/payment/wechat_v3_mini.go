package payment

func (w *WechatV3) MiniProgram() (MiniProgramConfig, bool) {
	if w.config.MiniProgram == nil {
		return MiniProgramConfig{}, false
	}
	return *w.config.MiniProgram, true
}
func (w *WechatV3) MiniLaunch(r MiniLaunchRequest) (CreateResult, error) {
	return miniLaunch("wxpay", w.config.MiniProgram, r)
}
