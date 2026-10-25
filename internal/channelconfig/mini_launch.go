package channelconfig

import provider "goepay/internal/adapters/payment"

func miniProgramConfig(c Channel) (*provider.MiniProgramConfig, error) {
	switch c.Config["enable_mini_launch"] {
	case "", "false":
		return nil, nil
	case "true":
		return &provider.MiniProgramConfig{AppID: c.Config["mini_appid"], Path: c.Config["mini_path"], OriginalID: c.Config["mini_program_id"]}, nil
	default:
		return nil, ErrImport
	}
}
func (g guarded) MiniProgram() (provider.MiniProgramConfig, bool) {
	p, ok := g.Provider.(provider.MiniLaunchProvider)
	if !ok {
		return provider.MiniProgramConfig{}, false
	}
	return p.MiniProgram()
}
func (g guarded) MiniLaunch(r provider.MiniLaunchRequest) (provider.CreateResult, error) {
	if !g.allowLive {
		return provider.CreateResult{}, ErrLiveDisabled
	}
	p, ok := g.Provider.(provider.MiniLaunchProvider)
	if !ok {
		return provider.CreateResult{}, provider.ErrUnsupported
	}
	return p.MiniLaunch(r)
}
