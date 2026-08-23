// Copyright (c) 2026 the go-news-reader authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package window

import (
	"reflect"
	"testing"

	"github.com/go-widgets/toolkit"
)

// damagingHandler is a Handler that also reports damage, so a test can prove
// bind routes it onto the surface.
type damagingHandler struct {
	recorder
	rects []toolkit.Rect
}

func (d *damagingHandler) DamageRects() []toolkit.Rect { return d.rects }

// A handler that reports damage has its DamageRects wired onto Surface.Damage,
// so the surface presents incrementally.
func TestBindWiresDamageForADamager(t *testing.T) {
	h := &damagingHandler{rects: []toolkit.Rect{{X: 1, Y: 2, W: 3, H: 4}}}
	surf := bind(toolkit.NewSurface(nil), h, func() float64 { return 1 }, &appearancePump{})
	if surf.Damage == nil {
		t.Fatal("bind did not wire Surface.Damage for a damager handler")
	}
	if got := surf.Damage(); !reflect.DeepEqual(got, h.rects) {
		t.Fatalf("Surface.Damage() = %v, want the handler's %v", got, h.rects)
	}
}

// A handler that cannot report damage leaves Surface.Damage unset, keeping the
// whole-surface present path unchanged.
func TestBindLeavesDamageUnsetForPlainHandler(t *testing.T) {
	surf := bind(toolkit.NewSurface(nil), &recorder{}, func() float64 { return 1 }, &appearancePump{})
	if surf.Damage != nil {
		t.Fatal("a handler without DamageRects should leave Surface.Damage nil")
	}
}
