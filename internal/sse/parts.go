package sse

import (
	"fmt"

	"opencode-chat/internal/views"
)

// MessagePartsManager manages message parts for SSE streaming,
// ensuring chronological order is maintained.
// Note: This is designed for single-goroutine use per instance.
// If sharing across goroutines, add synchronization.
type MessagePartsManager struct {
	parts map[string][]views.MessagePartData // messageID -> ordered parts
}

// NewMessagePartsManager creates a new message parts manager.
func NewMessagePartsManager() *MessagePartsManager {
	return &MessagePartsManager{
		parts: make(map[string][]views.MessagePartData),
	}
}

// UpdatePart updates or appends a message part, maintaining order.
func (m *MessagePartsManager) UpdatePart(messageID, partID string, part views.MessagePartData) error {
	if messageID == "" {
		return fmt.Errorf("messageID cannot be empty")
	}
	if partID == "" {
		return fmt.Errorf("partID cannot be empty")
	}

	part.PartID = partID

	msgParts, exists := m.parts[messageID]
	if !exists {
		m.parts[messageID] = []views.MessagePartData{part}
		return nil
	}

	for i, existingPart := range msgParts {
		if existingPart.PartID == partID {
			m.parts[messageID][i] = part
			return nil
		}
	}

	m.parts[messageID] = append(msgParts, part)
	return nil
}

// GetParts returns a copy of all parts for a message in order.
func (m *MessagePartsManager) GetParts(messageID string) []views.MessagePartData {
	parts, exists := m.parts[messageID]
	if !exists {
		return nil
	}

	result := make([]views.MessagePartData, len(parts))
	copy(result, parts)
	return result
}

// ValidateAndExtractMessagePart validates and extracts message part data from SSE event.
func ValidateAndExtractMessagePart(event map[string]any, sessionID string) (messageID, partID string, partData map[string]any, err error) {
	eventType, ok := event["type"].(string)
	if !ok || eventType != "message.part.updated" {
		return "", "", nil, fmt.Errorf("not a message.part.updated event")
	}

	props, ok := event["properties"].(map[string]any)
	if !ok {
		return "", "", nil, fmt.Errorf("missing or invalid properties")
	}

	part, ok := props["part"].(map[string]any)
	if !ok {
		return "", "", nil, fmt.Errorf("missing or invalid part data")
	}

	partSessionID, _ := part["sessionID"].(string)
	if partSessionID != sessionID {
		return "", "", nil, fmt.Errorf("session mismatch")
	}

	messageID, ok = part["messageID"].(string)
	if !ok || messageID == "" {
		return "", "", nil, fmt.Errorf("invalid or missing messageID")
	}

	partID, ok = part["id"].(string)
	if !ok || partID == "" {
		return "", "", nil, fmt.Errorf("invalid or missing partID")
	}

	return messageID, partID, part, nil
}
