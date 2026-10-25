package merchantnotice

import "context"

// Sender describes a configured account, without exposing credentials. The
// store must key the scope and recipient and commit its source claim first.
type Sender interface {
	NoticeTransport() Transport
	NoticeScope() string
	ValidateNoticeRecipient(string) error
	SendMerchantNotice(context.Context, string, Message) error
}

// App-scoped OpenIDs must remain bound to the original public application,
// independently of a factory's credential/configuration selection.
type AccountBoundSender interface{ NoticeAccountID() string }
