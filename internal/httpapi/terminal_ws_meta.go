package httpapi

import (
	"encoding/json"

	"github.com/gorilla/websocket"
)

// terminalWsMetaPrefix marks WebSocket text frames that carry protocol
// metadata for the SPA. They must never be written into the xterm buffer.
const terminalWsMetaPrefix = "vantyx:meta:"

func wrapTerminalWsMeta(jsonBody []byte) []byte {
	out := make([]byte, len(terminalWsMetaPrefix)+len(jsonBody))
	copy(out, terminalWsMetaPrefix)
	copy(out[len(terminalWsMetaPrefix):], jsonBody)
	return out
}

func writeTerminalWsMeta(conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, wrapTerminalWsMeta(b))
}
