package models

import "html/template"

type Provider struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Models map[string]Model `json:"models"`
}

type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProvidersResponse struct {
	Providers []Provider        `json:"providers"`
	Default   map[string]string `json:"default"`
}

type ModelOption struct {
	Value string // provider/model format
	Label string // Display name
}

type SessionResponse struct {
	ID string `json:"id"`
}

type MessageInfo struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	SessionID  string `json:"sessionID,omitempty"`
	ProviderID string `json:"providerID,omitempty"`
	ModelID    string `json:"modelID,omitempty"`
	Time       struct {
		Created int64 `json:"created,omitempty"`
		Updated int64 `json:"updated,omitempty"`
	} `json:"time,omitempty"`
}

type MessagePart struct {
	ID        string         `json:"id,omitempty"`
	MessageID string         `json:"messageID,omitempty"`
	SessionID string         `json:"sessionID,omitempty"`
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

type MessageResponse struct {
	Info  MessageInfo   `json:"info"`
	Parts []MessagePart `json:"parts"`
}

// FileNode represents a file or directory from OpenCode API
type FileNode struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Absolute string `json:"absolute"`
	Type     string `json:"type"` // "file" or "directory"
	Ignored  bool   `json:"ignored"`
}

// FileContent represents file content from OpenCode API
type FileContent struct {
	Content string `json:"content"`
}

// CodeTabData holds data for the code tab template
type CodeTabData struct {
	Files     []FileNode `json:"files"`
	FileCount int        `json:"fileCount"`
	LineCount int        `json:"lineCount"`
}

// MacChromeData holds data for the shared Mac OS chrome component
type MacChromeData struct {
	Title        string        `json:"title"`
	LeftContent  template.HTML `json:"leftContent"`
	RightContent template.HTML `json:"rightContent"`
	MainContent  template.HTML `json:"mainContent"`
}

// QuestionOption represents a single answer choice from OpenCode's question tool.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// QuestionInfo represents a single question with its options.
type QuestionInfo struct {
	Question string           `json:"question"`
	Header   string           `json:"header"`
	Options  []QuestionOption `json:"options"`
	Multiple bool             `json:"multiple,omitempty"`
}

// QuestionRequest represents a question.asked event from OpenCode.
type QuestionRequest struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionID"`
	Questions []QuestionInfo `json:"questions"`
}

// AuthConfig represents the authentication configuration for different providers
// Based on the structure found in ~/.local/share/opencode/auth.json
type AuthConfig struct {
	Type    string `json:"type"`              // "api" or "oauth"
	Key     string `json:"key,omitempty"`     // API key for "api" type
	Refresh string `json:"refresh,omitempty"` // Refresh token for "oauth" type
	Access  string `json:"access,omitempty"`  // Access token for "oauth" type
	Expires int64  `json:"expires,omitempty"` // Expiration timestamp for "oauth" type
}
