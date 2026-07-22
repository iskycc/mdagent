package timeutil

import (
	"strings"
	"testing"
	"time"
)

func TestNowUsesBeijingLocation(t *testing.T) {
	_, offset := Now().Zone()
	if offset != 8*60*60 {
		t.Fatalf("expected +08:00 offset, got %d", offset)
	}
}

func TestDateTime(t *testing.T) {
	if len(Date()) != len("2006-01-02") {
		t.Fatalf("unexpected date: %s", Date())
	}
	if len(DateTime()) != len("2006-01-02 15:04:05") {
		t.Fatalf("unexpected datetime: %s", DateTime())
	}
}

func TestBeijingLocation(t *testing.T) {
	loc := BeijingLocation()
	if loc == nil {
		t.Fatal("BeijingLocation returned nil")
	}
	now := time.Now().In(loc)
	_, offset := now.Zone()
	if offset != 8*60*60 {
		t.Fatalf("expected +08:00 offset, got %d", offset)
	}
}

func TestFormatMinute(t *testing.T) {
	utc := time.Date(2024, 1, 15, 6, 30, 45, 0, time.UTC)
	got := FormatMinute(utc)
	want := "2024-01-15 14:30"
	if got != want {
		t.Fatalf("FormatMinute(%v) = %q, want %q", utc, got, want)
	}
}

func TestLoadLocation_Fallback(t *testing.T) {
	loc := loadLocation("No/Such/Zone")
	_, offset := time.Now().In(loc).Zone()
	if offset != 8*60*60 {
		t.Fatalf("fallback offset = %d, want +08:00", offset)
	}
}

func TestFormatMinute_NilLocation(t *testing.T) {
	// Ensure FormatMinute handles a time with no location by relying on
	// In(beijingLocation).
	utc := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	got := FormatMinute(utc)
	want := "2024-06-01 08:00"
	if got != want {
		t.Fatalf("FormatMinute(%v) = %q, want %q", utc, got, want)
	}
}

func TestNow_Date_DateTime(t *testing.T) {
	before := time.Now().In(beijingLocation)
	n := Now()
	after := time.Now().In(beijingLocation)

	if n.Before(before) || n.After(after) {
		t.Fatalf("Now() = %v, not in range [%v, %v]", n, before, after)
	}

	d := Date()
	if !strings.HasPrefix(n.Format("2006-01-02"), d) {
		t.Fatalf("Date() = %s, expected prefix of %s", d, n.Format("2006-01-02"))
	}

	dt := DateTime()
	if len(dt) != len("2006-01-02 15:04:05") {
		t.Fatalf("DateTime() returned unexpected format: %s", dt)
	}
}
