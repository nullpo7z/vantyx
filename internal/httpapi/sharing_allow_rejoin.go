package httpapi

import "github.com/nullpo7z/vantyx/internal/sharing"

// kickedUserIDs is a nil-safe accessor for the room's rejoin block list,
// exposed on the participants list so the owner's UI can show who was
// removed. The only way to lift the block is for the owner to issue a
// new *named* invitation to that user (Room.Unkick in the create-
// invitation handlers); there is deliberately no separate "allow
// rejoin" endpoint (F-2).
func kickedUserIDs(room *sharing.Room) []string {
	if room == nil {
		return []string{}
	}
	return room.KickedUserIDs()
}
