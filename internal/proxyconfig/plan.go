package proxyconfig

import (
	"context"
	"errors"
	"strconv"
)

// Plan is the resolved 中转代理 delivery decision: the non-secret settings plus
// the decrypted secrets, validated so an incompletely-configured proxy is a
// definite error rather than a silent direct send.
type Plan struct {
	Mode                               int // 0 direct, 1 proxy server, 2 proxy API
	Type, Server, Port, User, Password string
	APIURL, APIKey                     string
}

var ErrIncomplete = errors.New("中转代理 enabled but not fully configured")

// Plan reads the proxy settings from backoffice_records and the secrets from
// this store. Mode 0 (or unset) returns an empty plan. Mode 1 requires a
// server, port and protocol; mode 2 requires an API URL and key. A missing
// required piece returns ErrIncomplete so the caller fails closed.
func (s *Store) Plan(ctx context.Context) (Plan, error) {
	if s.pool == nil {
		return Plan{}, ErrUnavailable
	}
	rows, e := s.pool.Query(ctx, "SELECT body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('proxy','proxy_server','proxy_port','proxy_user','proxy_type','proxy_apiurl') ORDER BY id")
	if e != nil {
		return Plan{}, e
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var k, v string
		if e = rows.Scan(&k, &v); e != nil {
			return Plan{}, e
		}
		values[k] = v
	}
	if e = rows.Err(); e != nil {
		return Plan{}, e
	}
	mode, _ := strconv.Atoi(values["proxy"])
	plan := Plan{Mode: mode, Type: values["proxy_type"], Server: values["proxy_server"], Port: values["proxy_port"], User: values["proxy_user"], APIURL: values["proxy_apiurl"]}
	if mode != 1 && mode != 2 {
		return Plan{Mode: 0}, nil
	}
	secrets, e := s.Resolve(ctx)
	if e != nil {
		return Plan{}, e
	}
	plan.Password, plan.APIKey = secrets.Password, secrets.APIKey
	switch mode {
	case 1:
		switch plan.Type {
		case "http", "https", "sock4", "sock5", "sock5h":
		default:
			return Plan{}, ErrIncomplete
		}
		n, err := strconv.Atoi(plan.Port)
		if plan.Server == "" || err != nil || n < 1 || n > 65535 {
			return Plan{}, ErrIncomplete
		}
	case 2:
		if plan.APIURL == "" || plan.APIKey == "" {
			return Plan{}, ErrIncomplete
		}
	}
	return plan, nil
}
