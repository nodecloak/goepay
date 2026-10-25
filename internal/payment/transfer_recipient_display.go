package payment

import "fmt"

// TransferRecipientDisplay contains only fields safe for the console list and
// export. It is authenticated against the original intent and never looks up a
// merchant's current recipient or the channel's current payment type.
type TransferRecipientDisplay struct {
	Type          string
	AccountMasked string
	NameMasked    string
}

func (s *Store) TransferRecipientDisplay(id, amount int64, cipher []byte) (TransferRecipientDisplay, error) {
	var out TransferRecipientDisplay
	if id <= 0 || amount <= 0 {
		return out, ErrInvalid
	}
	if len(cipher) == 0 {
		return out, nil
	} // Historical reservation without a provider intent.
	r, e := s.decryptTransfer(id, cipher)
	if e != nil {
		return out, ErrConflict
	}
	if r.TransferNo != fmt.Sprintf("TF%d", id) || r.Amount != amount || !validTransferRequest(r) {
		return out, ErrConflict
	}
	// Reuse the settlement masking policy: no name characters, at most the last
	// four account runes, and no suffix at all for short accounts.
	view := recipientView("", 0, SettlementRecipientInput{Account: r.Account, Name: r.Name})
	return TransferRecipientDisplay{Type: r.Type, AccountMasked: view.AccountMasked, NameMasked: view.NameMasked}, nil
}
