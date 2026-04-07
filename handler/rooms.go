package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// HandleListRooms returns a JSON array of all active rooms with participant counts.
func HandleListRooms(w http.ResponseWriter, _ *http.Request) {
	rooms := manager.ListRooms()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rooms)
}

// HandleRoomEvents streams room list updates via Server-Sent Events.
func HandleRoomEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send current state immediately.
	initial := manager.ListRooms()
	data, _ := json.Marshal(initial)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	ch := manager.Subscribe()
	defer manager.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case rooms, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(rooms)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
