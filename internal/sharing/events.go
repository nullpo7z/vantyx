package sharing

// Event types pushed over SSE so the SPA can react to room changes
// without polling. internal/httpapi/sse.go marshals these into JSON
// payloads.
const (
	EventParticipantJoined     = "participant_joined"
	EventParticipantLeft       = "participant_left"
	EventWriteRequestPending   = "write_request_pending"
	EventWriteRequestDecided   = "write_request_decided"
	EventWriteTokenTransferred = "write_token_transferred"
	EventInvitationRevoked     = "invitation_revoked"
	EventInvitationCreated     = "invitation_created"
	EventInvitationReceived    = "invitation_received"
	EventInvitationUpdated     = "invitation_updated"
	EventInvitationConsumed    = "invitation_consumed"
)

// Event is the common payload published by the broker. Individual
// event kinds use Extra for type-specific fields.
type Event struct {
	Type      string                 `json:"type"`
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id,omitempty"`
	Username  string                 `json:"username,omitempty"`
	Extra     map[string]interface{} `json:"extra,omitempty"`
}
