package timeutil

import "testing"

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
