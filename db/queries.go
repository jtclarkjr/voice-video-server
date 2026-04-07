package db

import (
	"context"
	"time"
)

type User struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Room struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type RoomParticipant struct {
	ID       string     `json:"id"`
	RoomID   string     `json:"roomId"`
	UserID   string     `json:"userId"`
	JoinedAt time.Time  `json:"joinedAt"`
	LeftAt   *time.Time `json:"leftAt,omitempty"`
}

// CreateUser inserts a new user and returns it.
func CreateUser(ctx context.Context, displayName string) (User, error) {
	var u User
	err := Pool.QueryRow(ctx,
		`INSERT INTO users (display_name) VALUES ($1) RETURNING id, display_name, created_at`,
		displayName,
	).Scan(&u.ID, &u.DisplayName, &u.CreatedAt)
	return u, err
}

// GetOrCreateRoom returns an existing room by name or creates a new one.
func GetOrCreateRoom(ctx context.Context, name string) (Room, error) {
	var r Room
	err := Pool.QueryRow(ctx,
		`INSERT INTO rooms (name) VALUES ($1)
		 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id, name, created_at`,
		name,
	).Scan(&r.ID, &r.Name, &r.CreatedAt)
	return r, err
}

// JoinRoom records a participant joining a room.
func JoinRoom(ctx context.Context, roomID, userID string) (RoomParticipant, error) {
	var rp RoomParticipant
	err := Pool.QueryRow(ctx,
		`INSERT INTO room_participants (room_id, user_id) VALUES ($1, $2)
		 RETURNING id, room_id, user_id, joined_at, left_at`,
		roomID, userID,
	).Scan(&rp.ID, &rp.RoomID, &rp.UserID, &rp.JoinedAt, &rp.LeftAt)
	return rp, err
}

// LeaveRoom sets the left_at timestamp for active participation records.
func LeaveRoom(ctx context.Context, roomID, userID string) error {
	_, err := Pool.Exec(ctx,
		`UPDATE room_participants SET left_at = now()
		 WHERE room_id = $1 AND user_id = $2 AND left_at IS NULL`,
		roomID, userID,
	)
	return err
}

// GetRoomByName looks up a room by its name.
func GetRoomByName(ctx context.Context, name string) (Room, error) {
	var r Room
	err := Pool.QueryRow(ctx,
		`SELECT id, name, created_at FROM rooms WHERE name = $1`,
		name,
	).Scan(&r.ID, &r.Name, &r.CreatedAt)
	return r, err
}

// DeleteRoom removes a room by name. Cascading deletes clean up participants.
func DeleteRoom(ctx context.Context, name string) error {
	_, err := Pool.Exec(ctx, `DELETE FROM rooms WHERE name = $1`, name)
	return err
}

// GetActiveParticipants returns users currently in a room (left_at IS NULL).
func GetActiveParticipants(ctx context.Context, roomID string) ([]User, error) {
	rows, err := Pool.Query(ctx,
		`SELECT u.id, u.display_name, u.created_at
		 FROM users u
		 JOIN room_participants rp ON rp.user_id = u.id
		 WHERE rp.room_id = $1 AND rp.left_at IS NULL`,
		roomID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
