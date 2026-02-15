package server

// EnhancedMessagePart represents the full structure returned by OpenCode API (for testing)
type EnhancedMessagePart struct {
	ID        string         `json:"id"`
	MessageID string         `json:"messageID"`
	SessionID string         `json:"sessionID"`
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	CallID    string         `json:"callID,omitempty"`
	State     map[string]any `json:"state,omitempty"`
	Time      struct {
		Start int64 `json:"start,omitempty"`
		End   int64 `json:"end,omitempty"`
	} `json:"time,omitempty"`
}

// EnhancedMessageResponse represents the full message structure from OpenCode (for testing)
type EnhancedMessageResponse struct {
	Info struct {
		ID         string         `json:"id"`
		Role       string         `json:"role"`
		SessionID  string         `json:"sessionID"`
		Time       string         `json:"time"`
		Cost       map[string]any `json:"cost,omitempty"`
		Tokens     map[string]any `json:"tokens,omitempty"`
		ModelID    string         `json:"modelID,omitempty"`
		ProviderID string         `json:"providerID,omitempty"`
		System     string         `json:"system,omitempty"`
		Mode       string         `json:"mode,omitempty"`
		Path       string         `json:"path,omitempty"`
	} `json:"info"`
	Parts []EnhancedMessagePart `json:"parts"`
}
