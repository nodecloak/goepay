package payment

import "context"

func (g ProviderFlow) upstreamSubject(ctx context.Context, o Order, channelID int64) (string, error) {
	if g.SubjectResolver == nil {
		return o.Name, nil
	}
	return g.SubjectResolver(ctx, o, channelID)
}
