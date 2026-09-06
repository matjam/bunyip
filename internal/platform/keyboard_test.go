package platform

import (
	"github.com/matjam/bunyip/input"
	"testing"
)

func TestTranslatedNamedKeyDoesNotUsePhysicalPosition(t *testing.T) {
	for text, want := range map[string]input.KeySymbol{"\r": "key:Enter", "\uf700": "key:Up", "\uf726": "key:F35", "": "", "\x01": ""} {
		got := keyDescription(input.KeyA, "", text, false)
		if got.Symbol != want {
			t.Errorf("translated %q at physical A = %q, want %q", text, got.Symbol, want)
		}
	}
}
