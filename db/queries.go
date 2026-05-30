package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
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

const (
	TranslationSessionStatusPending   = "pending"
	TranslationSessionStatusConnected = "connected"
	TranslationSessionStatusFailed    = "failed"
	TranslationSessionStatusEnded     = "ended"
)

type TranslationSession struct {
	ID             string     `json:"id"`
	SupabaseUserID string     `json:"supabaseUserId"`
	Lang           string     `json:"lang"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"createdAt"`
	ExpiresAt      time.Time  `json:"expiresAt"`
	ConnectedAt    *time.Time `json:"connectedAt,omitempty"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
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

func CreateTranslationSession(ctx context.Context, userID, lang string, expiresAt time.Time) (TranslationSession, error) {
	var s TranslationSession
	err := Pool.QueryRow(ctx,
		`INSERT INTO translation_sessions (supabase_user_id, lang, expires_at)
		 VALUES ($1, $2, $3)
		 RETURNING id, supabase_user_id, lang, status, created_at, expires_at, connected_at, ended_at`,
		userID,
		lang,
		expiresAt,
	).Scan(
		&s.ID,
		&s.SupabaseUserID,
		&s.Lang,
		&s.Status,
		&s.CreatedAt,
		&s.ExpiresAt,
		&s.ConnectedAt,
		&s.EndedAt,
	)
	return s, err
}

func GetTranslationSessionForUser(ctx context.Context, id, userID string) (TranslationSession, error) {
	var s TranslationSession
	err := Pool.QueryRow(ctx,
		`SELECT id, supabase_user_id, lang, status, created_at, expires_at, connected_at, ended_at
		 FROM translation_sessions
		 WHERE id = $1 AND supabase_user_id = $2`,
		id,
		userID,
	).Scan(
		&s.ID,
		&s.SupabaseUserID,
		&s.Lang,
		&s.Status,
		&s.CreatedAt,
		&s.ExpiresAt,
		&s.ConnectedAt,
		&s.EndedAt,
	)
	return s, err
}

func MarkTranslationSessionConnected(ctx context.Context, id string) error {
	tag, err := Pool.Exec(ctx,
		`UPDATE translation_sessions
		 SET status = $2, connected_at = COALESCE(connected_at, now())
		 WHERE id = $1 AND status = $3`,
		id,
		TranslationSessionStatusConnected,
		TranslationSessionStatusPending,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func MarkTranslationSessionFailed(ctx context.Context, id string) error {
	tag, err := Pool.Exec(ctx,
		`UPDATE translation_sessions
		 SET status = $2, ended_at = COALESCE(ended_at, now())
		 WHERE id = $1`,
		id,
		TranslationSessionStatusFailed,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func MarkTranslationSessionEndedForUser(ctx context.Context, id, userID string) error {
	tag, err := Pool.Exec(ctx,
		`UPDATE translation_sessions
		 SET status = $3, ended_at = COALESCE(ended_at, now())
		 WHERE id = $1 AND supabase_user_id = $2`,
		id,
		userID,
		TranslationSessionStatusEnded,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
