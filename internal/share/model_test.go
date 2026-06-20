package share

import (
	"database/sql"
	"testing"
	"time"
)

func TestIsExpired(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Second).Format(time.RFC3339Nano)
	future := now.Add(time.Second).Format(time.RFC3339Nano)
	if !IsExpired(sql.NullString{String: past, Valid: true}, now) {
		t.Fatalf("past should be expired")
	}
	if IsExpired(sql.NullString{String: future, Valid: true}, now) {
		t.Fatalf("future should be active")
	}
	if IsExpired(sql.NullString{}, now) {
		t.Fatalf("null expiry should be active")
	}
}
