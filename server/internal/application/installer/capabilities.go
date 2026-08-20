package installer

import "encoding/json"

type PlatformCapability struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type ToolCapability struct {
	ID        string `json:"id"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type Capabilities struct {
	Platform PlatformCapability `json:"platform"`
	Tools    []ToolCapability   `json:"tools"`
}

// String returns a stable, credential-free representation suitable for tests
// and structured diagnostics.
func (c Capabilities) String() string {
	encoded, _ := json.Marshal(c)
	return string(encoded)
}
