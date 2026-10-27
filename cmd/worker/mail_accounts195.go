package main

import (
	"context"
	"goepay/internal/mailaccount195"
)

type mailAccountResolver195 interface {
	ResolveSMTP(context.Context) (*mailaccount195.BoundSMTP, bool, error)
}
