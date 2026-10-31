package goepay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

// Response contains top-level scalar wire fields after signature verification.
// Code zero acknowledges an API operation; it does not assert payment/refund
// completion. Consult status and query the original order when outcome is unknown.
type Response struct {
	Fields        map[string]string
	Code          int
	Authenticated bool
}

// APIError is a nonzero API code or non-200 HTTP response. An unsigned error is
// explicitly untrusted: inspect Authenticated before using its Message as a
// platform statement. The caller also receives Response to preserve original IDs.
type APIError struct {
	Code          int
	Message       string
	HTTPStatus    int
	Authenticated bool
}

func (e *APIError) Error() string {
	trust := "unverified"
	if e.Authenticated {
		trust = "signature-verified"
	}
	return fmt.Sprintf("goepay: %s API error (HTTP %d, code %d)", trust, e.HTTPStatus, e.Code)
}

// decodeFields rejects duplicate keys, trailing documents and nested values.
// The protocol excludes top-level objects/arrays from RSA signing. These five
// pay JSON APIs use scalars, so refusing nested fields avoids suggesting an
// unsigned subtree was authenticated. JSON numbers never pass through float64.
func decodeFields(body []byte) (map[string]string, int, error) {
	if !utf8.Valid(body) {
		return nil, 0, ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, 0, ErrProtocol
	}
	fields := map[string]string{}
	codeSeen := false
	code := 0
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, 0, ErrProtocol
		}
		if _, exists := fields[key]; exists {
			return nil, 0, ErrProtocol
		}
		var value any
		if err := d.Decode(&value); err != nil {
			return nil, 0, ErrProtocol
		}
		switch v := value.(type) {
		case string:
			fields[key] = v
		case json.Number:
			fields[key] = v.String()
		case bool:
			if v {
				fields[key] = "1"
			} else {
				fields[key] = ""
			}
		case nil:
			fields[key] = ""
		default:
			return nil, 0, ErrProtocol
		}
		if key == "code" {
			number, numeric := value.(json.Number)
			if !numeric {
				return nil, 0, ErrProtocol
			}
			code, err = strconv.Atoi(number.String())
			if err != nil {
				return nil, 0, ErrProtocol
			}
			codeSeen = true
		}
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || !codeSeen {
		return nil, 0, ErrProtocol
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, 0, ErrProtocol
	}
	return fields, code, nil
}
