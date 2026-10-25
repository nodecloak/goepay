package simulation

import (
	"context"
	"fmt"
	provider "goepay/internal/adapters/payment"
	"time"
)

// Transfer exercises only local ledger effects; there is no network client.
func (c *client) TransferTypes() []string { return []string{c.kind} }
func (c *client) Transfer(_ context.Context, in provider.TransferRequest) (provider.TransferResult, error) {
	if in.Type != c.kind || in.TransferNo == "" || in.Account == "" || in.Amount <= 0 || in.BankCode != "" {
		return provider.TransferResult{}, ErrSimulation
	}
	c.env.mu.Lock()
	defer c.env.mu.Unlock()
	key := fmt.Sprintf("%d:%s", c.id, in.TransferNo)
	if old, ok := c.env.transfers[key]; ok {
		if old.Request != in {
			return provider.TransferResult{}, ErrSimulation
		}
		return old.Result, nil
	}
	result := provider.TransferResult{TransferNo: in.TransferNo, ProviderTransferNo: "sim-" + in.TransferNo, State: "SUCCESS", PayDate: time.Now().UTC().Format(time.RFC3339), Amount: in.Amount}
	c.env.transfers[key] = simTransfer{in, result}
	return result, nil
}
func (c *client) QueryTransfer(_ context.Context, in provider.TransferRequest) (provider.TransferResult, error) {
	c.env.mu.Lock()
	defer c.env.mu.Unlock()
	old, ok := c.env.transfers[fmt.Sprintf("%d:%s", c.id, in.TransferNo)]
	if !ok || old.Request != in {
		return provider.TransferResult{}, ErrSimulation
	}
	return old.Result, nil
}
