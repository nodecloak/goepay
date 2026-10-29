// Package grouppolicy defines original group payment settings without HTTP,
// database or provider dependencies.
package grouppolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"goepay/internal/ledger"
	"io"
	"strconv"
)

var ErrInvalidInfo = errors.New("invalid group payment info")

type ChoiceKind string

const (
	Inherit     ChoiceKind = ""
	Disabled    ChoiceKind = "disabled"
	Random      ChoiceKind = "random"
	Subchannels ChoiceKind = "subchannels"
	RandomRoll  ChoiceKind = "random_roll"
	Sequential  ChoiceKind = "sequential"
	Channel     ChoiceKind = "channel"
	Roll        ChoiceKind = "roll"
	First       ChoiceKind = "first"
)

type Choice struct {
	kind ChoiceKind
	id   int64
}

func (c Choice) Kind() ChoiceKind { return c.kind }
func (c Choice) ID() int64        { return c.id }

type PaymentPolicy struct {
	retained         int64
	explicitRetained bool
	choice           Choice
	hasRate          bool
	rateText         string
	hasRouting       bool
	routeType        string
}

func (p PaymentPolicy) RetainedBPS() (int64, bool) { return p.retained, p.explicitRetained }
func (p PaymentPolicy) RoutingChoice() Choice      { return p.choice }

type Info map[string]PaymentPolicy

// CanonicalJSON converts route IDs to exact strings before a generic resource
// decoder can turn a JSON number into a floating-point value. Original rate
// text, absent fields and default sentinels retain their meanings.
func (info Info) CanonicalJSON() ([]byte, error) {
	if info == nil {
		return nil, ErrInvalidInfo
	}
	object := map[string]map[string]string{}
	for kind, policy := range info {
		fields := map[string]string{}
		if policy.hasRate {
			fields["rate"] = policy.rateText
		}
		if policy.hasRouting {
			id := "0"
			switch policy.choice.kind {
			case Disabled:
			case Random:
				id = "-1"
			case Subchannels:
				id = "-2"
			case RandomRoll:
				id = "-3"
			case Sequential:
				id = "-4"
			case First:
				id = "-5"
			case Channel, Roll:
				id = strconv.FormatInt(policy.choice.id, 10)
			default:
				return nil, ErrInvalidInfo
			}
			fields["channel"], fields["type"] = id, policy.routeType
		}
		object[kind] = fields
	}
	encoded, e := json.Marshal(object)
	if e != nil {
		return nil, ErrInvalidInfo
	}
	checked, e := ParseInfo(encoded)
	if e != nil || len(checked) != len(info) {
		return nil, ErrInvalidInfo
	}
	for kind, policy := range info {
		if checked[kind] != policy {
			return nil, ErrInvalidInfo
		}
	}
	return encoded, nil
}

// catalogKind reports whether kind is an original platform pay type (pre_type)
// or the internal sandbox. grouppolicy stays free of the payment adapter
// dependency, so the catalog is mirrored here; keep it in sync with
// adapter.PluginPayTypes.
func catalogKind(kind string) bool {
	switch kind {
	case "alipay", "wxpay", "qqpay", "bank", "jdpay", "paypal", "douyinpay", "ecny", "sandbox":
		return true
	}
	return false
}

func ParseInfo(raw []byte) (Info, error) {
	if len(raw) > 4096 {
		return nil, ErrInvalidInfo
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	start, e := d.Token()
	if e != nil || start != json.Delim('{') {
		return nil, ErrInvalidInfo
	}
	out := Info{}
	for d.More() {
		token, e := d.Token()
		kind, ok := token.(string)
		if e != nil || !ok || !catalogKind(kind) {
			return nil, ErrInvalidInfo
		}
		if _, exists := out[kind]; exists {
			return nil, ErrInvalidInfo
		}
		start, e = d.Token()
		if e != nil || start != json.Delim('{') {
			return nil, ErrInvalidInfo
		}
		policy := PaymentPolicy{}
		seen := map[string]bool{}
		var channel any
		routeType := ""
		for d.More() {
			key, e := d.Token()
			field, ok := key.(string)
			if e != nil || !ok || seen[field] {
				return nil, ErrInvalidInfo
			}
			seen[field] = true
			token, e = d.Token()
			if e != nil {
				return nil, ErrInvalidInfo
			}
			switch field {
			case "rate":
				text, ok := token.(string)
				if !ok {
					return nil, ErrInvalidInfo
				}
				policy.hasRate, policy.rateText = true, text
				if text != "" {
					value, e := ledger.Parse(text)
					if e != nil || value > 10000 {
						return nil, ErrInvalidInfo
					}
					policy.retained = value
					policy.explicitRetained = value > 0
				}
			case "channel":
				channel = token
			case "type":
				text, ok := token.(string)
				if !ok {
					return nil, ErrInvalidInfo
				}
				routeType = text
			default:
				return nil, ErrInvalidInfo
			}
		}
		end, e := d.Token()
		if e != nil || end != json.Delim('}') {
			return nil, ErrInvalidInfo
		}
		if seen["channel"] != seen["type"] {
			return nil, ErrInvalidInfo
		}
		if seen["channel"] {
			choice, e := parseChoice(channel, routeType)
			if e != nil {
				return nil, e
			}
			policy.choice = choice
			policy.hasRouting, policy.routeType = true, routeType
			if kind == "sandbox" && choice.kind != Inherit {
				return nil, ErrInvalidInfo
			}
		}
		out[kind] = policy
	}
	end, e := d.Token()
	if e != nil || end != json.Delim('}') {
		return nil, ErrInvalidInfo
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalidInfo
	}
	return out, nil
}

func parseChoice(raw any, routeType string) (Choice, error) {
	var text string
	switch value := raw.(type) {
	case string:
		text = value
	case json.Number:
		text = string(value)
	default:
		return Choice{}, ErrInvalidInfo
	}
	id, e := strconv.ParseInt(text, 10, 64)
	if e != nil || strconv.FormatInt(id, 10) != text {
		return Choice{}, ErrInvalidInfo
	}
	if id == 0 && (routeType == "" || routeType == "channel" || routeType == "roll") {
		return Choice{kind: Disabled}, nil
	}
	if id == -3 && routeType == "roll" {
		return Choice{kind: RandomRoll}, nil
	}
	if routeType == "" || routeType == "channel" {
		switch id {
		case -2:
			return Choice{kind: Subchannels}, nil
		case -1:
			return Choice{kind: Random}, nil
		case -4:
			return Choice{kind: Sequential}, nil
		case -5:
			return Choice{kind: First}, nil
		}
	}
	if id > 0 {
		switch routeType {
		case "channel":
			return Choice{kind: Channel, id: id}, nil
		case "roll":
			return Choice{kind: Roll, id: id}, nil
		}
	}
	return Choice{}, ErrInvalidInfo
}
