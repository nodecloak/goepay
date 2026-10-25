package server

import (
	"errors"
	"goepay/internal/payment"
	"strconv"
)

const privateTransferRecipientColumn = "private_recipient_cipher"
const transferListDisplayColumns = ",t.created_at,COALESCE(p.provider_transfer_no,'') AS provider_transfer_no,COALESCE(p.pay_date,'') AS pay_date,''::text AS type,''::text AS account_masked,''::text AS name_masked,p.recipient_cipher AS " + privateTransferRecipientColumn

var errTransferListProjection = errors.New("original transfer display cannot be verified")

func transferPublicColumns(columns []string) []string {
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		if column != privateTransferRecipientColumn {
			out = append(out, column)
		}
	}
	return out
}

// The private value is removed before either JSON or CSV serialization. CSV
// buffers the complete verified export before writing response headers.
func (s *Server) projectTransferListValues(columns []string, values []any) ([]any, error) {
	if len(columns) != len(values) {
		return nil, errTransferListProjection
	}
	indexes := map[string]int{}
	for i, column := range columns {
		if _, exists := indexes[column]; exists {
			return nil, errTransferListProjection
		}
		indexes[column] = i
	}
	for _, key := range []string{"id", "amount", "type", "account_masked", "name_masked", privateTransferRecipientColumn} {
		if _, ok := indexes[key]; !ok {
			return nil, errTransferListProjection
		}
	}
	positive := func(key string) (int64, error) {
		raw, ok := values[indexes[key]].(string)
		if !ok {
			return 0, errTransferListProjection
		}
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || n <= 0 || strconv.FormatInt(n, 10) != raw {
			return 0, errTransferListProjection
		}
		return n, nil
	}
	id, e := positive("id")
	if e != nil {
		return nil, e
	}
	amount, e := positive("amount")
	if e != nil {
		return nil, e
	}
	var cipher []byte
	if raw := values[indexes[privateTransferRecipientColumn]]; raw != nil {
		var ok bool
		cipher, ok = raw.([]byte)
		if !ok {
			return nil, errTransferListProjection
		}
	}
	var display payment.TransferRecipientDisplay
	if len(cipher) > 0 {
		if s.Store == nil {
			return nil, errTransferListProjection
		}
		display, e = s.Store.TransferRecipientDisplay(id, amount, cipher)
		if e != nil {
			return nil, errTransferListProjection
		}
	}
	out := make([]any, 0, len(values)-1)
	for i, column := range columns {
		switch column {
		case privateTransferRecipientColumn:
			continue
		case "type":
			out = append(out, display.Type)
		case "account_masked":
			out = append(out, display.AccountMasked)
		case "name_masked":
			out = append(out, display.NameMasked)
		default:
			out = append(out, values[i])
		}
	}
	return out, nil
}
