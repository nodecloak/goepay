package payment

// ParentChannelExecution is trusted process-local material. It never appears
// in console DTOs; account versions are sealed by the payment Store.
type ParentChannelExecution struct {
	Payment Provider          `json:"-"`
	Web     WebOAuthProvider  `json:"-"`
	Mini    MiniOAuthProvider `json:"-"`
	// PaymentSelection restores a complete SDK from this authenticated account
	// snapshot; it never selects credentials from request metadata.
	PaymentSelection    func(string, bool, int64) (Provider, string, error) `json:"-"`
	PaymentAllowed      func(string, int64) bool                            `json:"-"`
	PaymentFreshAllowed func(string) bool                                   `json:"-"`
}

// Trusted startup records are produced only after Store authentication. They
// contain process-local plaintext and must never be serialized to a console.
type ParentStartupAccount struct {
	ChannelID, SourceID int64
	PayType             string
	Snapshot            []byte
}

// ParentChannelField describes one constructor-consumed configuration slot.
// Certificate fields contain PEM bytes, never paths on the server filesystem.
type ParentChannelField struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Secret    bool   `json:"secret"`
	PEM       bool   `json:"pem"`
	// Ported plugins describe their inputs with the original labels.
	Label   string                     `json:"label,omitempty"`
	Note    string                     `json:"note,omitempty"`
	Options []ParentChannelFieldOption `json:"options,omitempty"`
}

type ParentChannelFieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type ParentChannelValueEdit struct {
	Operation string
	Value     *string
}
type ParentChannelAccountChange struct {
	AppWXMP          *int64
	AppWXA           *int64
	ExpectedBindings string
	AppTypes         *[]string
	Mode             *int
	Config           map[string]ParentChannelValueEdit
	Certificates     map[string]ParentChannelValueEdit
}
type ParentChannelFieldState struct {
	ParentChannelField
	Present bool   `json:"present"`
	Value   string `json:"value,omitempty"`
}
type ParentChannelAccountView struct {
	AppWXMP            int64                     `json:"appwxmp,string,omitempty"`
	AppWXA             int64                     `json:"appwxa,string,omitempty"`
	WeixinBindingState *ParentWeixinBindingState `json:"weixin_binding_state,omitempty"`
	Plugin             string                    `json:"plugin"`
	AppTypes           []string                  `json:"app_types"`
	Mode               int                       `json:"mode"`
	Fields             []ParentChannelFieldState `json:"fields"`
}

type ParentWeixinBindingSummary struct {
	ID       int64  `json:"id,string"`
	Revision int64  `json:"revision,string"`
	AppID    string `json:"appid"`
	Current  bool   `json:"current"`
}
type ParentWeixinBindingState struct {
	Web  *ParentWeixinBindingSummary `json:"web,omitempty"`
	Mini *ParentWeixinBindingSummary `json:"mini,omitempty"`
}

type ParentWeixinOption struct {
	ID       int64  `json:"id,string"`
	Revision int64  `json:"revision,string"`
	Kind     string `json:"kind"`
	AppID    string `json:"appid"`
}
type ParentWeixinCatalog struct {
	Fingerprint string               `json:"fingerprint"`
	Items       []ParentWeixinOption `json:"items"`
}

// ParentAccountFactory is supplied only by trusted application configuration.
// Raw snapshots are plaintext within the process; persistence must AEAD seal
// them, and console code uses Describe rather than returning raw bytes.
type ParentAccountFactory interface {
	OriginID() int64
	TemplateCommitment() []byte
	PayType() string
	Fields() []ParentChannelField
	NewSnapshot(int64, ParentChannelAccountChange) (ParentChannelExecution, []byte, error)
	BootstrapSnapshot(int64) (ParentChannelExecution, []byte, error)
	ChangeSnapshot(int64, []byte, ParentChannelAccountChange) (ParentChannelExecution, []byte, error)
	CopySnapshot(int64, int64, []byte, ParentChannelAccountChange) (ParentChannelExecution, []byte, error)
	Restore(int64, []byte) (ParentChannelExecution, error)
	Describe(int64, []byte) (ParentChannelAccountView, error)
}
