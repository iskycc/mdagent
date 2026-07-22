package debuglog

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

func TestEnabled_TrueValues(t *testing.T) {
	orig := os.Getenv("APP_DEBUG")
	defer os.Setenv("APP_DEBUG", orig)

	trueValues := []string{"1", "true", "TRUE", "yes", "YES", "on", "ON"}
	for _, v := range trueValues {
		if err := os.Setenv("APP_DEBUG", v); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		if !Enabled() {
			t.Fatalf("Enabled() = false for APP_DEBUG=%q", v)
		}
	}
}

func TestEnabled_FalseValues(t *testing.T) {
	orig := os.Getenv("APP_DEBUG")
	defer os.Setenv("APP_DEBUG", orig)

	falseValues := []string{"", "0", "false", "FALSE", "no", "NO", "off", "OFF", "maybe"}
	for _, v := range falseValues {
		if err := os.Setenv("APP_DEBUG", v); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		if Enabled() {
			t.Fatalf("Enabled() = true for APP_DEBUG=%q", v)
		}
	}
}

func TestPrintf_WhenEnabled(t *testing.T) {
	orig := os.Getenv("APP_DEBUG")
	if err := os.Setenv("APP_DEBUG", "1"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	defer os.Setenv("APP_DEBUG", orig)

	oldOutput := log.Writer()
	oldFlags := log.Flags()
	defer func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	}()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	Printf("hello %d %s", 42, "world")

	got := buf.String()
	want := "[app_debug] hello 42 world"
	if !strings.Contains(got, want) {
		t.Fatalf("Printf output = %q, want substring %q", got, want)
	}
}

func TestPrintf_WhenDisabled(t *testing.T) {
	orig := os.Getenv("APP_DEBUG")
	if err := os.Setenv("APP_DEBUG", "0"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	defer os.Setenv("APP_DEBUG", orig)

	oldOutput := log.Writer()
	oldFlags := log.Flags()
	defer func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	}()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	Printf("should not appear")

	if buf.Len() != 0 {
		t.Fatalf("Printf output = %q, expected empty", buf.String())
	}
}
