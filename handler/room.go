package handler

import (
	"sync"

	"github.com/gorilla/websocket"
)

// Client represents a connected WebSocket client in a room.
type Client struct {
	ID          string
	DisplayName string
	RoomID      string
	Conn        *websocket.Conn
	Send        chan []byte
}

// PeerInfo is the public view of a client sent in signaling messages.
type PeerInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// Room holds the set of clients in a single call room.
type Room struct {
	ID      string
	Clients map[string]*Client
	mu      sync.RWMutex
}

// RoomManager tracks all active rooms.
type RoomManager struct {
	rooms       map[string]*Room
	subscribers map[chan []RoomInfo]struct{}
	mu          sync.RWMutex
}

var manager = &RoomManager{
	rooms:       make(map[string]*Room),
	subscribers: make(map[chan []RoomInfo]struct{}),
}

// Subscribe returns a channel that receives room list snapshots on every change.
func (rm *RoomManager) Subscribe() chan []RoomInfo {
	ch := make(chan []RoomInfo, 8)
	rm.mu.Lock()
	rm.subscribers[ch] = struct{}{}
	rm.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (rm *RoomManager) Unsubscribe(ch chan []RoomInfo) {
	rm.mu.Lock()
	delete(rm.subscribers, ch)
	rm.mu.Unlock()
	close(ch)
}

// notifySubscribers sends the current room list to all subscribers.
// Must be called while rm.mu is NOT held (it acquires the lock itself).
func (rm *RoomManager) notifySubscribers() {
	snapshot := rm.ListRooms()
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	for ch := range rm.subscribers {
		select {
		case ch <- snapshot:
		default:
			// Drop if subscriber is slow
		}
	}
}

func (rm *RoomManager) GetOrCreateRoom(roomID string) *Room {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	room, ok := rm.rooms[roomID]
	if !ok {
		room = &Room{
			ID:      roomID,
			Clients: make(map[string]*Client),
		}
		rm.rooms[roomID] = room
	}
	return room
}

// RemoveClient removes a client from its room and deletes the room if empty.
// Returns true if the room was destroyed (last client left).
func (rm *RoomManager) RemoveClient(client *Client) bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	room, ok := rm.rooms[client.RoomID]
	if !ok {
		return false
	}

	room.RemoveClient(client.ID)

	if len(room.Clients) == 0 {
		delete(rm.rooms, client.RoomID)
		return true
	}
	return false
}

// GetRoom returns the room with the given ID, or nil if it does not exist.
func (rm *RoomManager) GetRoom(roomID string) *Room {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.rooms[roomID]
}

// RoomInfo is a snapshot of a room for the listing endpoint.
type RoomInfo struct {
	ID               string `json:"id"`
	ParticipantCount int    `json:"participantCount"`
}

// ListRooms returns a snapshot of all active rooms with participant counts.
func (rm *RoomManager) ListRooms() []RoomInfo {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	rooms := make([]RoomInfo, 0, len(rm.rooms))
	for id, room := range rm.rooms {
		room.mu.RLock()
		count := len(room.Clients)
		room.mu.RUnlock()
		rooms = append(rooms, RoomInfo{ID: id, ParticipantCount: count})
	}
	return rooms
}

func (r *Room) AddClient(client *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Clients[client.ID] = client
}

func (r *Room) RemoveClient(clientID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Clients, clientID)
}

func (r *Room) Broadcast(message []byte, excludeID string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, client := range r.Clients {
		if id == excludeID {
			continue
		}
		select {
		case client.Send <- message:
		default:
			// Drop message if client buffer is full
		}
	}
}

func (r *Room) SendTo(targetID string, message []byte) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	client, ok := r.Clients[targetID]
	if !ok {
		return false
	}
	select {
	case client.Send <- message:
		return true
	default:
		return false
	}
}

func (r *Room) GetPeerList(excludeID string) []PeerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	peers := make([]PeerInfo, 0, len(r.Clients))
	for id, client := range r.Clients {
		if id == excludeID {
			continue
		}
		peers = append(peers, PeerInfo{
			ID:          client.ID,
			DisplayName: client.DisplayName,
		})
	}
	return peers
}
