package tiktokenbpe

import (
	"testing"

	"github.com/pkoukk/tiktoken-go"
)

func TestEmbeddedLoader_KnownEncodings(t *testing.T) {
	for _, name := range []string{"cl100k_base", "o200k_base", "p50k_base", "r50k_base"} {
		enc, err := tiktoken.GetEncoding(name)
		if err != nil {
			t.Fatalf("GetEncoding(%s) failed: %v", name, err)
		}
		if enc == nil {
			t.Fatalf("expected encoding for %s, got nil", name)
		}
	}
}

func TestEmbeddedLoader_KnownModels(t *testing.T) {
	for _, model := range []string{"gpt-4", "gpt-4o", "gpt-3.5-turbo"} {
		enc, err := tiktoken.EncodingForModel(model)
		if err != nil {
			t.Fatalf("EncodingForModel(%s) failed: %v", model, err)
		}
		if enc == nil {
			t.Fatalf("expected encoding for %s, got nil", model)
		}
	}
}
