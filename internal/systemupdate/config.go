package systemupdate

import "encoding/hex"

// FromEnvironment is deployment-only trust configuration. Console requests
// cannot supply paths, URLs, verification keys, or runtime build identity.
func FromEnvironment(getenv func(string) string) (*Service, error) {
	c := Config{ManifestPath: getenv("GOEPAY_UPDATE_MANIFEST_FILE"), PackagePath: getenv("GOEPAY_UPDATE_PACKAGE_FILE"), StagingDir: getenv("GOEPAY_UPDATE_STAGING_DIR")}
	raw := getenv("GOEPAY_UPDATE_PUBLIC_KEY")
	if raw != "" {
		if len(raw) != 64 {
			return nil, ErrConfig
		}
		key, e := hex.DecodeString(raw)
		if e != nil {
			return nil, ErrConfig
		}
		c.PublicKey = key
	}
	return New(c)
}
