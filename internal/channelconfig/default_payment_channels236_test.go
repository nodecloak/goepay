package channelconfig

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func TestDefaultPaymentChannels236DisabledAndCanonicalSources(t *testing.T) {
	p := ParentStartupConfiguration{origins: []Channel{{ID: 7}, {ID: 2}, {ID: 11}}}
	for _, tc := range []struct {
		name, raw string
		want      []int64
	}{
		{"unset", "", []int64{}},
		{"explicit disabled", "[]", []int64{}},
		{"one registered", "[7]", []int64{7}},
		{"canonical order", "[11,2,7]", []int64{2, 7, 11}},
		{"JSON whitespace", " \n[ 7, 2 ]\t", []int64{2, 7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.DefaultPaymentChannels(tc.raw)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("default collection sources = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	if !reflect.DeepEqual(p.origins, []Channel{{ID: 7}, {ID: 2}, {ID: 11}}) {
		t.Fatal("parsing reordered the trusted startup catalogue")
	}
}

func TestDefaultPaymentChannels236StandaloneUsesProviderRegistryKeys(t *testing.T) {
	// Registry membership is the pure parser boundary. It neither inspects nor
	// calls the provider objects; construction/runtime capability checks follow.
	providers := map[int64]adapter.Provider{7: nil, 2: nil}
	got, err := DefaultPaymentChannels("[7,2]", providers)
	if err != nil || !reflect.DeepEqual(got, []int64{2, 7}) {
		t.Fatal("standalone registered sources not canonical", got, err)
	}
	for _, raw := range []string{"[8]", "[2,8]", "null", "[2,2]", "[2.0]", "[\"2\"]"} {
		t.Run(raw, func(t *testing.T) {
			if got, err := DefaultPaymentChannels(raw, providers); !errors.Is(err, ErrImport) || got != nil {
				t.Fatal("standalone parser returned invalid or partial authority", got, err)
			}
		})
	}
	if got, err := DefaultPaymentChannels("[7]", nil); !errors.Is(err, ErrImport) || got != nil {
		t.Fatal("standalone parser accepted an unregistered source", got, err)
	}
	if got, err := DefaultPaymentChannels("", nil); err != nil || got == nil || len(got) != 0 {
		t.Fatal("unset standalone defaults should disable collection fallback", got, err)
	}
}

func TestDefaultPaymentChannels236RejectsUnregisteredOrAmbiguousAuthority(t *testing.T) {
	p := ParentStartupConfiguration{origins: []Channel{{ID: 7}, {ID: 2}}}
	for _, raw := range []string{
		"null", "{}", "7", "\"[7]\"", "true", " ",
		"[\"7\"]", "[7.0]", "[7e0]", "[true]", "[null]", "[[]]", "[{}]",
		"[0]", "[-7]", "[8]", "[7,8]", "[7,7]", "[2,7,2]",
		"[9223372036854775808]", "[7,]", "[7", "[7] []", "[7] null", "[7]\x00",
	} {
		t.Run(raw, func(t *testing.T) {
			got, err := p.DefaultPaymentChannels(raw)
			if !errors.Is(err, ErrImport) || got != nil {
				t.Fatalf("invalid defaults granted %v with error %v", got, err)
			}
		})
	}
	if got, err := (ParentStartupConfiguration{}).DefaultPaymentChannels("[7]"); !errors.Is(err, ErrImport) || got != nil {
		t.Fatal("unregistered empty catalogue granted a default source")
	}
}

func TestDefaultPaymentChannels236BoundsWithoutPartialGrants(t *testing.T) {
	var p ParentStartupConfiguration
	var values []string
	for id := int64(1); id <= 65; id++ {
		p.origins = append(p.origins, Channel{ID: id})
		values = append(values, strconv.FormatInt(id, 10))
	}
	withinCount := "[" + strings.Join(values[:64], ",") + "]"
	got, err := p.DefaultPaymentChannels(withinCount)
	if err != nil || len(got) != 64 || got[0] != 1 || got[63] != 64 {
		t.Fatal("bounded registered collection source list rejected", got, err)
	}
	withinBytes := "[" + strings.Repeat(" ", (64<<10)-2) + "]"
	if got, err := p.DefaultPaymentChannels(withinBytes); err != nil || got == nil || len(got) != 0 {
		t.Fatal("bounded empty list rejected", got, err)
	}
	for _, raw := range []string{
		"[" + strings.Join(values, ",") + "]",
		withinBytes + " ",
	} {
		if got, err := p.DefaultPaymentChannels(raw); !errors.Is(err, ErrImport) || got != nil {
			t.Fatal("unbounded defaults returned partial authority", len(got), err)
		}
	}
}
