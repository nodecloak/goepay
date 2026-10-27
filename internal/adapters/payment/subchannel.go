package payment

// SubchannelField is an operator-declared logical input. Target names and parent
// credentials remain private to the factory; console clients cannot choose them.
type SubchannelField struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
}
type SubchannelSchema struct {
	ParentID    int64             `json:"parent_id,string"`
	Revision    int64             `json:"revision,string"`
	PayType     string            `json:"pay_type"`
	Fields      []SubchannelField `json:"fields"`
	Fingerprint string            `json:"fingerprint"`
}

// SubchannelExecution is built offline by the same trusted constructors as the
// parent. It is private execution material and must never enter a console DTO.
type SubchannelExecution struct {
	SchemaFingerprint string            `json:"-"`
	Payment           Provider          `json:"-"`
	Web               WebOAuthProvider  `json:"-"`
	Mini              MiniOAuthProvider `json:"-"`
}
type SubchannelFactory interface {
	Schema() SubchannelSchema
	Build(values map[string]string) (SubchannelExecution, error)
	Snapshot(values map[string]string) ([]byte, error)
	Restore(snapshot []byte) (SubchannelExecution, error)
}

// This optional capability consumes only an authenticated plaintext snapshot
// opened by Store. It is never a console input or a provider discovery API.
type SubchannelAliasFactory interface {
	SubchannelFactory
	DeriveParentSubchannel(parentID int64, authenticatedParentSnapshot []byte) (SubchannelFactory, error)
	ParentSubchannelOrigin() (int64, []byte)
}
