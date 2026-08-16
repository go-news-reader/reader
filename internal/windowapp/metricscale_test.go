package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-widgets/toolkit"
)

// The reader's scale and the toolkit's are one number.
//
// This test used to render a frame with the toolkit told nothing and again with
// it told 2, and compare them: 0.17% of pixels differed, in one band, which was
// the evidence that connecting window.Run to SetMetricScale did not double-scale
// anything (go-widgets/window#49).
//
// It stopped measuring that the day Scene.SetScale began setting the global
// itself. Both arms of the comparison now set it to the same value before
// drawing, so the answer is 0.00% BY CONSTRUCTION -- a test that cannot fail,
// sitting in the suite looking like one that can. It passed for the wrong
// reason, which is the failure mode this whole suite spends its time hunting
// elsewhere.
//
// What is worth guarding now is the invariant that replaced it: the scene's
// scale and the toolkit's are the same number, set in one place. Delete that
// line in Scene.SetScale and every scale-aware widget lays out at half size on a
// HiDPI screen while the reader's own fonts do not -- and this fails.
func TestSceneScaleAndToolkitScaleAreOneNumber(t *testing.T) {
	defer toolkit.SetMetricScale(1)

	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 800, Height: 600})
	a.SetRefreshHook(func() {})
	h := New(a)

	for _, scale := range []float64{1, 2, 1.5, 3} {
		toolkit.SetMetricScale(99) // a value neither side would arrive at by luck
		h.Resize(800, 600, scale)
		if got := toolkit.MetricScale(); got != a.Scene().Scale {
			t.Errorf("after a resize at scale %v the scene is at %v and the toolkit at %v: "+
				"scale-aware widgets will lay out at a different size from the reader's own text",
				scale, a.Scene().Scale, got)
		}
	}
}
