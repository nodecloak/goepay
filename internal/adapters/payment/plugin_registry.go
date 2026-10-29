package payment

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Ported epay_release payment plugins (epay_release/plugins/<name>/) register
// a PluginSpec from an init function. A channel built from a plugin serves ONE
// pay type, chosen by the channel's config "pay_type"; the provider's ID()
// returns that pay type, so routing, ledgers, limits and statistics key on the
// pay type exactly as they do for the native alipay/wxpay adapters.
//
// Build must validate the channel offline: no network, no file system. All
// later calls go through the guarded wrapper in channelconfig, which keeps the
// operator's live-write switch in force.

// PluginInput is one channel configuration slot, mirroring the original
// plugin's $info['inputs'] entry.
type PluginInput struct {
	Key, Label, Note string
	// Secret values are write-only in the console.
	Secret bool
	// PEM inputs are certificate/key text stored under "certificates".
	PEM      bool
	Required bool
	// Options makes the input a select; the original 'select' type.
	Options []PluginOption
	// Textarea keeps multi-line plain text (non-PEM) inputs readable.
	Textarea bool
}

type PluginOption struct{ Value, Label string }

// PluginChannel is the trusted, already-decrypted channel handed to Build.
type PluginChannel struct {
	ID           int64
	Plugin       string
	PayType      string
	AppTypes     []string
	Config       map[string]string
	Certificates map[string]string
	HTTPClient   *http.Client
}

type PluginSpec struct {
	Name, Title, Author, Link string
	// Pay types the original plugin declares in $info['types'].
	Types []string
	// Transfer types from $info['transtypes'] (empty when unsupported).
	TransferTypes []string
	Inputs        []PluginInput
	// Original $info['select'] product options (app types); empty when none.
	AppTypes []PluginOption
	Note     string
	Build    func(PluginChannel) (Provider, error)
}

var (
	pluginMu       sync.RWMutex
	pluginRegistry = map[string]PluginSpec{}
	pluginNameRE   = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)
	pluginKeyRE    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
)

// PluginPayTypes are the pay types the original platform knows (pre_type).
var PluginPayTypes = []string{"alipay", "wxpay", "qqpay", "bank", "jdpay", "paypal", "douyinpay", "ecny"}

// nativePlugins are served by the dedicated adapters, never by the registry.
var nativePlugins = map[string]bool{"alipay": true, "wxpay": true}

// RegisterPlugin is called from init functions only; misuse panics at start.
func RegisterPlugin(spec PluginSpec) {
	if !pluginNameRE.MatchString(spec.Name) || nativePlugins[spec.Name] {
		panic("payment: invalid plugin name " + spec.Name)
	}
	if spec.Build == nil || spec.Title == "" || len(spec.Types) == 0 {
		panic("payment: incomplete plugin spec " + spec.Name)
	}
	for _, t := range spec.Types {
		if !knownPayType(t) {
			panic("payment: plugin " + spec.Name + " declares unknown pay type " + t)
		}
	}
	seen := map[string]bool{"pay_type": true}
	for _, in := range spec.Inputs {
		if !pluginKeyRE.MatchString(in.Key) || seen[in.Key] || in.Label == "" {
			panic("payment: plugin " + spec.Name + " has an invalid or duplicate input " + in.Key)
		}
		seen[in.Key] = true
	}
	pluginMu.Lock()
	defer pluginMu.Unlock()
	if _, dup := pluginRegistry[spec.Name]; dup {
		panic("payment: duplicate plugin " + spec.Name)
	}
	pluginRegistry[spec.Name] = spec
}

func knownPayType(t string) bool {
	for _, v := range PluginPayTypes {
		if v == t {
			return true
		}
	}
	return false
}

// CatalogPayType reports whether t is one of the original platform's pay types
// (pre_type). Generic collection/accounting sites use it to accept the full
// ported catalog instead of only the native alipay/wxpay adapters.
func CatalogPayType(t string) bool { return knownPayType(t) }

// LookupPlugin returns a registered (ported) plugin.
func LookupPlugin(name string) (PluginSpec, bool) {
	pluginMu.RLock()
	defer pluginMu.RUnlock()
	s, ok := pluginRegistry[name]
	return s, ok
}

// RegisteredPlugins lists ported plugins by name.
func RegisteredPlugins() []PluginSpec {
	pluginMu.RLock()
	defer pluginMu.RUnlock()
	out := make([]PluginSpec, 0, len(pluginRegistry))
	for _, s := range pluginRegistry {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Serves reports whether the plugin declares the pay type.
func (s PluginSpec) Serves(payType string) bool {
	for _, t := range s.Types {
		if t == payType {
			return true
		}
	}
	return false
}

// Input returns the configuration slot with the given key.
func (s PluginSpec) Input(key string) (PluginInput, bool) {
	for _, in := range s.Inputs {
		if in.Key == key {
			return in, true
		}
	}
	return PluginInput{}, false
}

// BuildPluginChannel validates the generic parts of a plugin channel and
// calls the plugin's constructor.
func BuildPluginChannel(c PluginChannel) (Provider, error) {
	spec, ok := LookupPlugin(c.Plugin)
	if !ok {
		return nil, ErrUnsupported
	}
	if !spec.Serves(c.PayType) {
		return nil, fmt.Errorf("%w: plugin %s does not serve pay type %q", ErrInvalid, c.Plugin, c.PayType)
	}
	if c.Config == nil {
		c.Config = map[string]string{}
	}
	for key, value := range c.Config {
		if key == "pay_type" {
			continue
		}
		in, known := spec.Input(key)
		if !known || in.PEM || strings.ContainsRune(value, 0) || len(value) > 131072 {
			return nil, ErrInvalid
		}
		if len(in.Options) > 0 && value != "" && !optionAllowed(in.Options, value) {
			return nil, ErrInvalid
		}
	}
	for key, value := range c.Certificates {
		in, known := spec.Input(key)
		if !known || !in.PEM || strings.ContainsRune(value, 0) || len(value) > 131072 {
			return nil, ErrInvalid
		}
	}
	for _, in := range spec.Inputs {
		if !in.Required {
			continue
		}
		v := c.Config[in.Key]
		if in.PEM {
			v = c.Certificates[in.Key]
		}
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("%w: %s requires %s", ErrInvalid, c.Plugin, in.Key)
		}
	}
	for _, t := range c.AppTypes {
		if len(spec.AppTypes) == 0 || !optionAllowed(spec.AppTypes, t) {
			return nil, ErrInvalid
		}
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{}
	}
	return spec.Build(c)
}

func optionAllowed(options []PluginOption, value string) bool {
	for _, o := range options {
		if o.Value == value {
			return true
		}
	}
	return false
}
