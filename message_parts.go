package main

import (
	"fmt"
)

// MessagePartsManager manages message parts for SSE streaming,
// ensuring chronological order is maintained.
// Note: This is designed for single-goroutine use per instance.
// If sharing across goroutines, add synchronization.
type MessagePartsManager struct {
	parts map[string][]MessagePartData // messageID -> ordered parts
}

// NewMessagePartsManager creates a new message parts manager
func NewMessagePartsManager() *MessagePartsManager {
	return &MessagePartsManager{
		parts: make(map[string][]MessagePartData),
	}
}

// UpdatePart updates or appends a message part, maintaining order
func (m *MessagePartsManager) UpdatePart(messageID, partID string, part MessagePartData) error {
	// Validate inputs
	if messageID == "" {
		return fmt.Errorf("messageID cannot be empty")
	}
	if partID == "" {
		return fmt.Errorf("partID cannot be empty")
	}

	// Set the partID in the part
	part.PartID = partID

	// Get or create the parts slice for this message
	msgParts, exists := m.parts[messageID]
	if !exists {
		// First part for this message
		m.parts[messageID] = []MessagePartData{part}
		return nil
	}

	// Find existing part with same ID
	for i, existingPart := range msgParts {
		if existingPart.PartID == partID {
			// Update existing part at same position
			m.parts[messageID][i] = part
			return nil
		}
	}

	// New part - append to maintain chronological order
	m.parts[messageID] = append(msgParts, part)
	return nil
}

// GetParts returns a copy of all parts for a message in order
func (m *MessagePartsManager) GetParts(messageID string) []MessagePartData {
	parts, exists := m.parts[messageID]
	if !exists {
		return nil
	}

	// Return a copy to prevent external modification
	result := make([]MessagePartData, len(parts))
	copy(result, parts)
	return result
}

// Clear removes all parts for a message
func (m *MessagePartsManager) Clear(messageID string) {
	delete(m.parts, messageID)
}

// ValidateAndExtractMessagePart validates and extracts message part data from SSE event
func ValidateAndExtractMessagePart(event map[string]interface{}, sessionID string) (messageID, partID string, partData map[string]interface{}, err error) {
	// Check event type
	eventType, ok := event["type"].(string)
	if !ok || eventType != "message.part.updated" {
		return "", "", nil, fmt.Errorf("not a message.part.updated event")
	}

	// Extract properties
	props, ok := event["properties"].(map[string]interface{})
	if !ok {
		return "", "", nil, fmt.Errorf("missing or invalid properties")
	}

	// Extract part data
	part, ok := props["part"].(map[string]interface{})
	if !ok {
		return "", "", nil, fmt.Errorf("missing or invalid part data")
	}

	// Check session ID
	partSessionID, _ := part["sessionID"].(string)
	if partSessionID != sessionID {
		return "", "", nil, fmt.Errorf("session mismatch")
	}

	// Extract and validate message ID
	messageID, ok = part["messageID"].(string)
	if !ok || messageID == "" {
		return "", "", nil, fmt.Errorf("invalid or missing messageID")
	}

	// Extract and validate part ID
	partID, ok = part["id"].(string)
	if !ok || partID == "" {
		return "", "", nil, fmt.Errorf("invalid or missing partID")
	}

	return messageID, partID, part, nil
}