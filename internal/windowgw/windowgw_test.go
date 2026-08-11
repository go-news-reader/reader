package windowgw

import (
	"errors"
	"image/color"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	gwwindow "github.com/go-widgets/window"

	rwindow "github.com/go-news-reader/reader/internal/window"
)

// fakeHandler records every call the adapter makes, so the translation from the
// go-widgets windowing model to the reader's native-input vocabulary is proven
// without an app, a scene render or a window.
type fakeHandler struct {
	buf     []byte
	fw, fh  int
	changed bool

	resizes    []resizeCall
	mouseDowns [][2]int
	mouseMoves [][2]int
	mouseUps   [][2]int
	scrolls    []int
	keys       []keyCall
	shortcuts  []shortcutCall
	els        []rwindow.A11yElement

	appr    *rwindow.SystemAppearance
	clip    rwindow.SystemClipboard
	clipSet bool
}

type resizeCall struct {
	w, h  int
	scale float64
}
type keyCall struct {
	name string
	r    rune
}
type shortcutCall struct {
	r          rune
	ctrl, meta bool
}

func (f *fakeHandler) Frame() ([]byte, int, int, bool) { return f.buf, f.fw, f.fh, f.changed }
func (f *fakeHandler) Resize(w, h int, scale float64) {
	f.resizes = append(f.resizes, resizeCall{w, h, scale})
}
func (f *fakeHandler) MouseDown(x, y int)      { f.mouseDowns = append(f.mouseDowns, [2]int{x, y}) }
func (f *fakeHandler) MouseMove(x, y int)      { f.mouseMoves = append(f.mouseMoves, [2]int{x, y}) }
func (f *fakeHandler) MouseUp(x, y int)        { f.mouseUps = append(f.mouseUps, [2]int{x, y}) }
func (f *fakeHandler) Scroll(dy int)           { f.scrolls = append(f.scrolls, dy) }
func (f *fakeHandler) Key(name string, r rune) { f.keys = append(f.keys, keyCall{name, r}) }
func (f *fakeHandler) Shortcut(r rune, c, m bool) {
	f.shortcuts = append(f.shortcuts, shortcutCall{r, c, m})
}
func (f *fakeHandler) A11yElements() []rwindow.A11yElement { return f.els }
func (f *fakeHandler) SystemAppearance(a rwindow.SystemAppearance) {
	cp := a
	f.appr = &cp
}
func (f *fakeHandler) SetSystemClipboard(c rwindow.SystemClipboard) { f.clip, f.clipSet = c, true }

// The fake must satisfy the same interface the production handler does.
var _ Reader = (*fakeHandler)(nil)

// --- rendering -------------------------------------------------------------

// opaqueGradient builds a w×h RGBA buffer whose every pixel is fully opaque and
// distinct, so a faithful blit can be asserted byte-for-byte.
func opaqueGradient(w, h int) []byte {
	b := make([]byte, w*h*4)
	for i := 0; i < w*h; i++ {
		b[i*4+0] = byte(i)
		b[i*4+1] = byte(i >> 8)
		b[i*4+2] = byte(i * 7)
		b[i*4+3] = 0xFF
	}
	return b
}

func TestDrawBlitsFramebufferIdentically(t *testing.T) {
	const w, h = 6, 4
	src := opaqueGradient(w, h)
	s := &surface{h: &fakeHandler{buf: src, fw: w, fh: h, changed: true}}
	s.SetBounds(toolkit.Rect{X: 0, Y: 0, W: w, H: h})

	dst := make([]byte, w*h*4)
	p := painter.NewPixelPainter(dst, w, h)
	s.Draw(p, toolkit.DefaultDark())

	for i := range src {
		if dst[i] != src[i] {
			t.Fatalf("blit differs at byte %d: got %d want %d", i, dst[i], src[i])
		}
	}
}

func TestDrawBlitsAtOffsetBounds(t *testing.T) {
	const w, h = 3, 2
	src := opaqueGradient(w, h)
	s := &surface{h: &fakeHandler{buf: src, fw: w, fh: h}}
	s.SetBounds(toolkit.Rect{X: 2, Y: 1, W: w, H: h})

	const cw, ch = 8, 8
	dst := make([]byte, cw*ch*4)
	p := painter.NewPixelPainter(dst, cw, ch)
	s.Draw(p, nil)

	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			so := (j*w + i) * 4
			do := ((j+1)*cw + (i + 2)) * 4
			for k := 0; k < 4; k++ {
				if dst[do+k] != src[so+k] {
					t.Fatalf("pixel (%d,%d) byte %d: got %d want %d", i, j, k, dst[do+k], src[so+k])
				}
			}
		}
	}
	// A pixel outside the blit target must be untouched.
	if dst[0] != 0 {
		t.Fatalf("pixel outside bounds was painted: %d", dst[0])
	}
}

// noImagePainter is a painter.Painter that does NOT implement painter.ImagePainter,
// so Draw must show nothing rather than reach past the painter.
type noImagePainter struct{ w, h int }

func (noImagePainter) FillRect(painter.Rect, painter.RGBA)                  {}
func (noImagePainter) StrokeRect(painter.Rect, painter.RGBA, int)           {}
func (noImagePainter) FillRoundRect(painter.Rect, int, painter.RGBA)        {}
func (noImagePainter) StrokeRoundRect(painter.Rect, int, painter.RGBA, int) {}
func (noImagePainter) PutPixel(int, int, painter.RGBA)                      {}
func (noImagePainter) Text(int, int, string, painter.RGBA)                  {}
func (p noImagePainter) Size() (int, int)                                   { return p.w, p.h }

func TestDrawWithoutImagePainterShowsNothing(t *testing.T) {
	s := &surface{h: &fakeHandler{buf: opaqueGradient(2, 2), fw: 2, fh: 2}}
	s.SetBounds(toolkit.Rect{W: 2, H: 2})
	s.Draw(noImagePainter{2, 2}, nil) // must not panic and must draw nothing
}

func TestDrawGuardsAgainstBadFrame(t *testing.T) {
	dst := make([]byte, 4*4*4)
	cases := map[string]*fakeHandler{
		"zero width":   {buf: opaqueGradient(1, 1), fw: 0, fh: 1},
		"zero height":  {buf: opaqueGradient(1, 1), fw: 1, fh: 0},
		"short buffer": {buf: make([]byte, 3), fw: 2, fh: 2},
		"nil buffer":   {buf: nil, fw: 2, fh: 2},
	}
	for name, fh := range cases {
		t.Run(name, func(t *testing.T) {
			s := &surface{h: fh}
			s.SetBounds(toolkit.Rect{W: 4, H: 4})
			for i := range dst {
				dst[i] = 0
			}
			p := painter.NewPixelPainter(dst, 4, 4)
			s.Draw(p, nil)
			for i, b := range dst {
				if b != 0 {
					t.Fatalf("guard %q painted at byte %d", name, i)
				}
			}
		})
	}
}

// --- sizing ----------------------------------------------------------------

func TestSetBoundsResizesOnlyOnChange(t *testing.T) {
	fh := &fakeHandler{}
	s := &surface{h: fh, backing: 2}
	s.SetBounds(toolkit.Rect{W: 100, H: 50})
	s.SetBounds(toolkit.Rect{W: 100, H: 50}) // same size: no second resize
	s.SetBounds(toolkit.Rect{W: 120, H: 60}) // changed: resize again
	if len(fh.resizes) != 2 {
		t.Fatalf("resizes = %d, want 2 (%+v)", len(fh.resizes), fh.resizes)
	}
	if fh.resizes[0] != (resizeCall{100, 50, 2}) {
		t.Fatalf("first resize = %+v", fh.resizes[0])
	}
	if fh.resizes[1] != (resizeCall{120, 60, 2}) {
		t.Fatalf("second resize = %+v", fh.resizes[1])
	}
}

// --- input routing ---------------------------------------------------------

func TestOnEventTranslatesCoordinatesAndRoutes(t *testing.T) {
	fh := &fakeHandler{}
	s := &surface{h: fh}
	s.SetBounds(toolkit.Rect{X: 10, Y: 20, W: 100, H: 100})

	s.OnEvent(toolkit.Event{Kind: toolkit.EventClick, X: 15, Y: 25})
	s.OnEvent(toolkit.Event{Kind: toolkit.EventMouseMove, X: 16, Y: 26})
	s.OnEvent(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 17, Y: 27})
	s.OnEvent(toolkit.Event{Kind: toolkit.EventMouseUp, X: 18, Y: 28})

	if want := [][2]int{{5, 5}}; !eq2(fh.mouseDowns, want) {
		t.Fatalf("mouseDowns = %v, want %v", fh.mouseDowns, want)
	}
	if want := [][2]int{{6, 6}, {7, 7}}; !eq2(fh.mouseMoves, want) {
		t.Fatalf("mouseMoves = %v, want %v", fh.mouseMoves, want)
	}
	if want := [][2]int{{8, 8}}; !eq2(fh.mouseUps, want) {
		t.Fatalf("mouseUps = %v, want %v", fh.mouseUps, want)
	}
}

func TestOnEventScrollMovesThenScrolls(t *testing.T) {
	fh := &fakeHandler{}
	s := &surface{h: fh}
	s.SetBounds(toolkit.Rect{W: 100, H: 100})
	s.OnEvent(toolkit.Event{Kind: toolkit.EventScroll, X: 3, Y: 4, Delta: 2})
	if want := [][2]int{{3, 4}}; !eq2(fh.mouseMoves, want) {
		t.Fatalf("scroll did not route pointer first: %v", fh.mouseMoves)
	}
	if len(fh.scrolls) != 1 || fh.scrolls[0] != 2*scrollPixelsPerRow {
		t.Fatalf("scrolls = %v, want [%d]", fh.scrolls, 2*scrollPixelsPerRow)
	}
}

func TestOnEventNamedKeys(t *testing.T) {
	cases := map[string]string{
		"Backspace":  "Backspace",
		"Escape":     "Escape",
		"Enter":      "Enter",
		"ArrowUp":    "Up",
		"ArrowDown":  "Down",
		"ArrowLeft":  "Left",
		"ArrowRight": "Right",
	}
	for code, want := range cases {
		fh := &fakeHandler{}
		s := &surface{h: fh}
		s.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: code})
		if len(fh.keys) != 1 || fh.keys[0] != (keyCall{want, 0}) {
			t.Fatalf("key %q → %v, want [{%s 0}]", code, fh.keys, want)
		}
	}
}

func TestOnEventUnmappedNamedKeyDropped(t *testing.T) {
	fh := &fakeHandler{}
	s := &surface{h: fh}
	s.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Tab"})
	s.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Home"})
	if len(fh.keys) != 0 {
		t.Fatalf("unmapped named key delivered: %v", fh.keys)
	}
}

func TestOnEventCharTypesRune(t *testing.T) {
	fh := &fakeHandler{}
	s := &surface{h: fh}
	s.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: "a"})
	if len(fh.keys) != 1 || fh.keys[0] != (keyCall{"", 'a'}) {
		t.Fatalf("char → %v, want [{\"\" a}]", fh.keys)
	}
	if len(fh.shortcuts) != 0 {
		t.Fatalf("plain char became a shortcut: %v", fh.shortcuts)
	}
}

func TestOnEventCharWithModifierIsShortcut(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ctrl, meta bool
	}{
		{"ctrl", true, false},
		{"meta", false, true},
		{"both", true, true},
	} {
		fh := &fakeHandler{}
		s := &surface{h: fh}
		s.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: "c", Ctrl: tc.ctrl, Meta: tc.meta})
		if len(fh.shortcuts) != 1 || fh.shortcuts[0] != (shortcutCall{'c', tc.ctrl, tc.meta}) {
			t.Fatalf("%s: shortcuts = %v", tc.name, fh.shortcuts)
		}
		if len(fh.keys) != 0 {
			t.Fatalf("%s: modifier chord leaked into Key: %v", tc.name, fh.keys)
		}
	}
}

func TestOnEventEmptyCharIgnored(t *testing.T) {
	fh := &fakeHandler{}
	s := &surface{h: fh}
	s.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: ""})
	if len(fh.keys) != 0 || len(fh.shortcuts) != 0 {
		t.Fatalf("empty char produced input: keys=%v shortcuts=%v", fh.keys, fh.shortcuts)
	}
}

func TestOnEventUnhandledKindIsNoop(t *testing.T) {
	fh := &fakeHandler{}
	s := &surface{h: fh}
	s.OnEvent(toolkit.Event{Kind: toolkit.EventKeyUp, Code: "a"})
	if len(fh.keys)+len(fh.shortcuts)+len(fh.mouseDowns) != 0 {
		t.Fatal("EventKeyUp should route nothing")
	}
}

// --- accessibility ---------------------------------------------------------

func TestSurfaceIsPresentationWithProxyChildren(t *testing.T) {
	fh := &fakeHandler{els: []rwindow.A11yElement{
		{Role: "button", Name: "Settings", Value: "", X: 4, Y: 5, W: 20, H: 10},
		{Role: "searchbox", Name: "Search", Value: "go", X: 0, Y: 0, W: 100, H: 8},
	}}
	s := &surface{h: fh}
	s.SetBounds(toolkit.Rect{X: 10, Y: 20, W: 200, H: 200})

	if s.A11y().Role != toolkit.RolePresentation {
		t.Fatalf("surface role = %q, want presentation", s.A11y().Role)
	}

	// WalkA11y is exactly what every go-widgets/window platform bridge calls:
	// it must skip the presentation surface and surface the reader's elements
	// as real nodes at their absolute rects.
	nodes := toolkit.WalkA11y(s)
	if len(nodes) != 2 {
		t.Fatalf("WalkA11y returned %d nodes, want 2: %+v", len(nodes), nodes)
	}
	if nodes[0].Role != toolkit.RoleButton || nodes[0].Name != "Settings" {
		t.Fatalf("node 0 = %+v", nodes[0].A11yInfo)
	}
	// Element rect (4,5) offset by the surface origin (10,20) → (14,25).
	if got := nodes[0].Rect; got != (toolkit.Rect{X: 14, Y: 25, W: 20, H: 10}) {
		t.Fatalf("node 0 rect = %+v, want {14 25 20 10}", got)
	}
	if nodes[1].Role != toolkit.RoleSearchbox || nodes[1].Value != "go" {
		t.Fatalf("node 1 = %+v", nodes[1].A11yInfo)
	}
}

func TestChildrenNilWhenNoElements(t *testing.T) {
	s := &surface{h: &fakeHandler{}}
	if got := s.Children(); got != nil {
		t.Fatalf("Children with no elements = %v, want nil", got)
	}
}

// TestSurfaceConstructor proves the exported constructor yields a live widget
// wired to the reader: it blits its framebuffer and resizes it with the given
// backing scale, which is exactly what gwwindow.Backend.Run drives.
func TestSurfaceConstructor(t *testing.T) {
	const w, h = 4, 3
	src := opaqueGradient(w, h)
	fh := &fakeHandler{buf: src, fw: w, fh: h}
	root := Surface(fh, 2)

	root.SetBounds(toolkit.Rect{W: w, H: h})
	if len(fh.resizes) != 1 || fh.resizes[0] != (resizeCall{w, h, 2}) {
		t.Fatalf("constructor did not resize with backing scale: %+v", fh.resizes)
	}
	dst := make([]byte, w*h*4)
	root.Draw(painter.NewPixelPainter(dst, w, h), nil)
	for i := range src {
		if dst[i] != src[i] {
			t.Fatalf("constructor widget blit differs at byte %d", i)
		}
	}
}

// --- Present helpers -------------------------------------------------------

type fakeBackend struct{ w, h int }

func (fakeBackend) Run(toolkit.Widget) error { return nil }
func (fakeBackend) Close() error             { return nil }
func (b fakeBackend) Size() (int, int)       { return b.w, b.h }
func (fakeBackend) String() string           { return "fakeBackend" }

var _ gwwindow.Backend = fakeBackend{}

// capBackend adds the optional appearance + clipboard capabilities.
type capBackend struct {
	fakeBackend
	appr    gwwindow.Appearance
	font    []byte
	fontErr error
	clip    string
}

func (b *capBackend) Appearance() gwwindow.Appearance { return b.appr }
func (b *capBackend) SystemFontTTF() ([]byte, error)  { return b.font, b.fontErr }
func (b *capBackend) ClipboardText() string           { return b.clip }
func (b *capBackend) SetClipboardText(text string)    { b.clip = text }

func TestBackingScale(t *testing.T) {
	if got := BackingScale(fakeBackend{w: 2000}, 1000); got != 2 {
		t.Fatalf("backingScale retina = %v, want 2", got)
	}
	if got := BackingScale(fakeBackend{w: 1000}, 1000); got != 1 {
		t.Fatalf("backingScale 1x = %v, want 1", got)
	}
	if got := BackingScale(fakeBackend{w: 0}, 1000); got != 1 {
		t.Fatalf("backingScale no device size = %v, want 1", got)
	}
	if got := BackingScale(fakeBackend{w: 2000}, 0); got != 1 {
		t.Fatalf("backingScale no logical width = %v, want 1", got)
	}
}

func TestWireCapabilitiesFull(t *testing.T) {
	fh := &fakeHandler{}
	be := &capBackend{
		appr: gwwindow.Appearance{Dark: true, Accent: color.RGBA{R: 1, G: 2, B: 3, A: 255}, HasAccent: true},
		font: []byte("ttf-bytes"),
	}
	WireCapabilities(be, fh)

	if fh.appr == nil {
		t.Fatal("appearance not forwarded")
	}
	if !fh.appr.Dark || !fh.appr.HasAccent || fh.appr.Accent != (color.RGBA{R: 1, G: 2, B: 3, A: 255}) {
		t.Fatalf("appearance mapped wrong: %+v", *fh.appr)
	}
	if string(fh.appr.FontTTF) != "ttf-bytes" {
		t.Fatalf("font not forwarded: %q", fh.appr.FontTTF)
	}
	if !fh.clipSet {
		t.Fatal("system clipboard not installed")
	}
	// The installed clipboard IS the backend, so it reaches the real pasteboard.
	fh.clip.SetClipboardText("x")
	if fh.clip.ClipboardText() != "x" {
		t.Fatalf("installed clipboard does not round-trip")
	}
}

func TestWireCapabilitiesFontError(t *testing.T) {
	fh := &fakeHandler{}
	be := &capBackend{fontErr: errors.New("no font")}
	WireCapabilities(be, fh)
	if fh.appr == nil {
		t.Fatal("appearance not forwarded")
	}
	if fh.appr.FontTTF != nil {
		t.Fatalf("font error should leave FontTTF nil, got %q", fh.appr.FontTTF)
	}
}

func TestWireCapabilitiesNone(t *testing.T) {
	fh := &fakeHandler{}
	WireCapabilities(fakeBackend{}, fh) // implements neither capability
	if fh.appr != nil {
		t.Fatalf("appearance forwarded by a backend without the capability: %+v", *fh.appr)
	}
	if fh.clipSet {
		t.Fatal("clipboard installed by a backend without the capability")
	}
}

func TestFirstRune(t *testing.T) {
	if got := firstRune(""); got != 0 {
		t.Fatalf("firstRune(\"\") = %d, want 0", got)
	}
	if got := firstRune("é"); got != 'é' {
		t.Fatalf("firstRune = %c, want é", got)
	}
}

// eq2 compares two slices of [2]int.
func eq2(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
