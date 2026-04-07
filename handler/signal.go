package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"voice-video-server/db"

	"github.com/gorilla/websocket"
	"github.com/pion/randutil"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// SignalMessage is the envelope for all signaling messages.
type SignalMessage struct {
	Type        string          `json:"type"`
	RoomID      string          `json:"roomId,omitempty"`
	DisplayName string          `json:"displayName,omitempty"`
	TargetID    string          `json:"targetId,omitempty"`
	FromID      string          `json:"fromId,omitempty"`
	SDP         json.RawMessage `json:"sdp,omitempty"`
	Candidate   json.RawMessage `json:"candidate,omitempty"`
	Peers       []PeerInfo      `json:"peers,omitempty"`
	UserID      string          `json:"userId,omitempty"`
	PeerID      string          `json:"peerId,omitempty"`
	Message     string          `json:"message,omitempty"`
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	sendBufLen = 256
)

// HandleSignal upgrades the HTTP connection to a WebSocket and handles
// signaling messages for room-based WebRTC peer connections.
func HandleSignal(w http.ResponseWriter, r *http.Request) {
	log.Printf("WebSocket upgrade request from %s", r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{
		ID:   generateID(),
		Conn: conn,
		Send: make(chan []byte, sendBufLen),
	}

	log.Printf("WebSocket connected: client %s from %s", client.ID, r.RemoteAddr)

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		if c.RoomID != "" {
			broadcastPeerLeft(c)
			roomDestroyed := manager.RemoveClient(c)
			manager.notifySubscribers()
			if roomDestroyed && db.Pool != nil {
				go func(roomName string) {
					_ = db.DeleteRoom(context.Background(), roomName)
				}(c.RoomID)
			}
		}
		c.Conn.Close()
	}()

	if err := c.Conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}
	c.Conn.SetPongHandler(func(string) error {
		return c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		var msg SignalMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			sendError(c, "invalid message format")
			continue
		}

		switch msg.Type {
		case "join":
			handleJoin(c, msg)
		case "leave":
			handleLeave(c)
		case "offer", "answer", "ice-candidate":
			handleRelay(c, msg)
		case "screen-share-start", "screen-share-stop":
			handleScreenShareBroadcast(c, msg)
		default:
			sendError(c, "unknown message type: "+msg.Type)
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if err := c.Conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if !ok {
				_ = c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.Conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func handleJoin(c *Client, msg SignalMessage) {
	if msg.RoomID == "" {
		sendError(c, "roomId is required")
		return
	}

	c.DisplayName = msg.DisplayName
	if c.DisplayName == "" {
		c.DisplayName = "Anonymous"
	}
	c.RoomID = msg.RoomID

	room := manager.GetOrCreateRoom(msg.RoomID)
	peers := room.GetPeerList(c.ID)
	room.AddClient(c)

	// Send joined confirmation to the new client
	sendJSON(c, SignalMessage{
		Type:   "joined",
		UserID: c.ID,
		RoomID: c.RoomID,
		Peers:  peers,
	})

	// Broadcast peer-joined to existing clients
	broadcast, _ := json.Marshal(SignalMessage{
		Type:        "peer-joined",
		PeerID:      c.ID,
		DisplayName: c.DisplayName,
	})
	room.Broadcast(broadcast, c.ID)

	manager.notifySubscribers()
	log.Printf("Client %s (%s) joined room %s (%d peers)", c.ID, c.DisplayName, c.RoomID, len(peers)+1)
}

func handleLeave(c *Client) {
	if c.RoomID == "" {
		return
	}
	broadcastPeerLeft(c)
	roomDestroyed := manager.RemoveClient(c)
	manager.notifySubscribers()
	if roomDestroyed && db.Pool != nil {
		go func(roomName string) {
			_ = db.DeleteRoom(context.Background(), roomName)
		}(c.RoomID)
	}
	log.Printf("Client %s (%s) left room %s", c.ID, c.DisplayName, c.RoomID)
	c.RoomID = ""
}

func handleRelay(c *Client, msg SignalMessage) {
	if c.RoomID == "" {
		sendError(c, "not in a room")
		return
	}
	if msg.TargetID == "" {
		sendError(c, "targetId is required")
		return
	}

	relay := SignalMessage{
		Type:      msg.Type,
		FromID:    c.ID,
		SDP:       msg.SDP,
		Candidate: msg.Candidate,
	}

	data, err := json.Marshal(relay)
	if err != nil {
		return
	}

	room := manager.GetRoom(c.RoomID)
	if room == nil {
		sendError(c, "room not found")
		return
	}
	if !room.SendTo(msg.TargetID, data) {
		sendError(c, "target peer not found")
	}
}

func handleScreenShareBroadcast(c *Client, msg SignalMessage) {
	if c.RoomID == "" {
		sendError(c, "not in a room")
		return
	}

	data, err := json.Marshal(SignalMessage{
		Type:   msg.Type,
		PeerID: c.ID,
	})
	if err != nil {
		return
	}

	room := manager.GetRoom(c.RoomID)
	if room != nil {
		room.Broadcast(data, c.ID)
	}
}

func broadcastPeerLeft(c *Client) {
	room := manager.GetRoom(c.RoomID)
	if room == nil {
		return
	}
	data, _ := json.Marshal(SignalMessage{
		Type:   "peer-left",
		PeerID: c.ID,
	})
	room.Broadcast(data, c.ID)
}

func sendJSON(c *Client, msg SignalMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case c.Send <- data:
	default:
	}
}

func sendError(c *Client, message string) {
	sendJSON(c, SignalMessage{
		Type:    "error",
		Message: message,
	})
}

const alphanumeric = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateID() string {
	id, err := randutil.GenerateCryptoRandomString(16, alphanumeric)
	if err != nil {
		log.Fatalf("failed to generate client ID: %v", err)
	}
	return id
}
