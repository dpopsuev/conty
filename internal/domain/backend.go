package domain

type BackendInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Capabilities string `json:"capabilities,omitempty"` // human-readable capability list
}
