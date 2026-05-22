package settings

import (
	"testing"
)

func TestNewService(t *testing.T) {
	s := NewService(nil)
	if s == nil {
		t.Fatal("expected non-nil settings service")
	}
}
