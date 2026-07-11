//go:build !darwin

package window

import (
	"errors"
	"testing"
)

// stubHandler is a no-op Handler used to exercise the stub Run signature.
type stubHandler struct{}

func (stubHandler) Frame() ([]byte, int, int, bool) { return nil, 0, 0, false }
func (stubHandler) Resize(int, int, float64)         {}
func (stubHandler) MouseDown(int, int)               {}
func (stubHandler) Scroll(int)                        {}
func (stubHandler) Key(string, rune)                  {}

func TestRunUnsupported(t *testing.T) {
	err := Run(Config{Title: "x", Width: 100, Height: 80}, stubHandler{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Run = %v, want ErrUnsupported", err)
	}
}
