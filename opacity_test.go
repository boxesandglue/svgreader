package svgreader

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// fakeRegistrar records what the renderer asks for.
type fakeRegistrar struct {
	alpha    []string
	groups   []string
	boxes    [][4]float64
	depth    int
	refuse   bool
	shadings []ShadingRequest
	masks    []GradientMask
}

func (f *fakeRegistrar) RegisterAlpha(fill, stroke float64) string {
	name := fmt.Sprintf("A%g_%g", fill, stroke)
	f.alpha = append(f.alpha, name)
	return name
}

func (f *fakeRegistrar) BeginGroup() bool {
	if f.refuse {
		return false
	}
	f.depth++
	return true
}

func (f *fakeRegistrar) EndGroup(content string, bbox [4]float64) string {
	f.depth--
	f.groups = append(f.groups, content)
	f.boxes = append(f.boxes, bbox)
	return fmt.Sprintf("Fm%d", len(f.groups))
}

func (f *fakeRegistrar) RegisterGradientMask(mask GradientMask) string {
	f.masks = append(f.masks, mask)
	return "Mask"
}

func (f *fakeRegistrar) RegisterShading(req ShadingRequest) string {
	f.shadings = append(f.shadings, req)
	return "Sh1"
}

func renderSVG(t *testing.T, body string, reg *fakeRegistrar) string {
	t.Helper()
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100">` + body + `</svg>`
	doc, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	opts := RenderOptions{}
	if reg != nil {
		opts.OpacityRegistrar = reg
		opts.ShadingRegistrar = reg
	}
	return doc.RenderPDF(opts)
}

func TestConstantAlpha(t *testing.T) {
	testdata := []struct {
		name string
		body string
		want string
	}{
		{"fill-opacity", `<rect width="9" height="9" fill="#f00" fill-opacity="0.5"/>`, "A0.5_1"},
		{"percentage", `<rect width="9" height="9" fill="#f00" fill-opacity="50%"/>`, "A0.5_1"},
		{"stroke-opacity", `<rect width="9" height="9" fill="none" stroke="#f00" stroke-opacity="0.25"/>`, "A1_0.25"},
		{"opacity fill", `<rect width="9" height="9" fill="#f00" opacity="0.5"/>`, "A0.5_1"},
		{"opacity stroke", `<rect width="9" height="9" fill="none" stroke="#f00" opacity="0.5"/>`, "A1_0.5"},
		{"both", `<rect width="9" height="9" fill="#f00" fill-opacity="0.5" opacity="0.5"/>`, "A0.25_1"},
		{"rgba", `<rect width="9" height="9" fill="rgba(255,0,0,.5)"/>`, "A0.5_1"},
		{"rgb slash", `<rect width="9" height="9" fill="rgb(255 0 0 / 50%)"/>`, "A0.5_1"},
		{"hex", `<rect width="9" height="9" fill="#ff000000" fill-opacity="1"/>`, ""},
		{"stroke rgba", `<rect width="9" height="9" fill="none" stroke="rgba(0,0,0,.5)"/>`, "A1_0.5"},
		{"inherited fill-opacity", `<g fill-opacity="0.5"><rect width="9" height="9"/></g>`, "A0.5_1"},
		{"css style", `<rect width="9" height="9" style="fill-opacity:0.5"/>`, "A0.5_1"},
	}
	for _, td := range testdata {
		t.Run(td.name, func(t *testing.T) {
			reg := &fakeRegistrar{}
			out := renderSVG(t, td.body, reg)
			if td.want == "" {
				if !strings.Contains(out, " re\nn") {
					t.Errorf("an invisible paint must not be painted\n%s", out)
				}
				return
			}
			if len(reg.alpha) != 1 || reg.alpha[0] != td.want {
				t.Fatalf("alpha = %v, want %s", reg.alpha, td.want)
			}
			if !strings.Contains(out, "/"+td.want+" gs") {
				t.Errorf("no gs operator in\n%s", out)
			}
			if len(reg.groups) != 0 {
				t.Errorf("a single paint needs no group: %v", reg.groups)
			}
		})
	}
}

// TestPaintAlphaGoesWithThePaint: a new fill replaces the alpha value of the
// old one, unlike fill-opacity.
func TestPaintAlphaGoesWithThePaint(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, `<g fill="rgba(0,0,0,.5)"><rect width="9" height="9" fill="#f00"/></g>`, reg)
	if len(reg.alpha) != 0 {
		t.Errorf("alpha = %v, want none", reg.alpha)
	}
}

func TestGroupOpacity(t *testing.T) {
	reg := &fakeRegistrar{}
	out := renderSVG(t, `<g opacity="0.5"><rect width="9" height="9" fill="#f00"/><rect x="5" width="9" height="9" fill="#0f0"/></g>`, reg)
	if len(reg.groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(reg.groups))
	}
	if !strings.Contains(reg.groups[0], "1 0 0 rg") || !strings.Contains(reg.groups[0], "0 1 0 rg") {
		t.Errorf("the children are not in the group:\n%s", reg.groups[0])
	}
	if strings.Contains(out, " re") {
		t.Errorf("the children must not be in the outer stream:\n%s", out)
	}
	if !strings.Contains(out, "/A0.5_1 gs\n/Fm1 Do") {
		t.Errorf("no alpha for the group in\n%s", out)
	}
	// The children get no alpha of their own, opacity is not inherited.
	if len(reg.alpha) != 1 {
		t.Errorf("alpha = %v, want the one of the group", reg.alpha)
	}
}

// TestFillAndStrokeNeedAGroup: the stroke overlaps the fill, so a constant
// alpha would show the fill through the inner half of the stroke.
func TestFillAndStrokeNeedAGroup(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, `<rect width="9" height="9" fill="#f00" stroke="#00f" stroke-width="2" opacity="0.5"/>`, reg)
	if len(reg.groups) != 1 {
		t.Errorf("groups = %d, want 1", len(reg.groups))
	}
}

func TestNestedGroups(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, `<g opacity="0.5"><g opacity="0.25"><rect width="9" height="9"/></g></g>`, reg)
	if reg.depth != 0 {
		t.Fatalf("depth = %d after rendering", reg.depth)
	}
	if len(reg.groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(reg.groups))
	}
	if !strings.Contains(reg.groups[1], "/A0.25_1 gs\n/Fm1 Do") {
		t.Errorf("the outer group does not paint the inner one:\n%s", reg.groups[1])
	}
}

func TestOpacityZeroDrawsNothing(t *testing.T) {
	reg := &fakeRegistrar{}
	out := renderSVG(t, `<g opacity="0"><rect width="9" height="9"/></g><rect width="9" height="9" opacity="0"/>
		<rect width="9" height="9" fill="none" stroke="#000" stroke-opacity="0"/>`, reg)
	if strings.Contains(out, " re\nS") || strings.Contains(out, " re\nf") || len(reg.groups) != 0 {
		t.Errorf("something was drawn:\n%s", out)
	}
}

// TestWithoutRegistrar paints opaque, as a host does that cannot write
// transparency.
func TestWithoutRegistrar(t *testing.T) {
	out := renderSVG(t, `<g opacity="0.5"><rect width="9" height="9" fill="#f00" fill-opacity="0.5"/></g>`, nil)
	if strings.Contains(out, " gs") || strings.Contains(out, " Do") {
		t.Errorf("unexpected operators in\n%s", out)
	}
	if !strings.Contains(out, "1 0 0 rg") {
		t.Errorf("the shape is missing in\n%s", out)
	}
}

// TestRefusedGroup paints the content in place, without the group.
func TestRefusedGroup(t *testing.T) {
	reg := &fakeRegistrar{refuse: true}
	out := renderSVG(t, `<g opacity="0.5"><rect width="9" height="9" fill="#f00"/></g>`, reg)
	if !strings.Contains(out, "1 0 0 rg") || strings.Contains(out, " Do") {
		t.Errorf("the content is not in place:\n%s", out)
	}
}

// TestGroupBBox: the box covers the viewport in the coordinates of the group.
// Root flips y, with origin at the top of the rule.
func TestGroupBBox(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, `<g transform="translate(10,20)"><g opacity="0.5"><rect width="9" height="9"/></g></g>`, reg)
	if len(reg.boxes) != 1 {
		t.Fatalf("boxes = %d", len(reg.boxes))
	}
	want := [4]float64{-10, -20, 90, 80}
	for i, v := range reg.boxes[0] {
		if math.Abs(v-want[i]) > 1e-9 {
			t.Fatalf("bbox = %v, want %v", reg.boxes[0], want)
		}
	}
}

// TestSingularTransformDrawsNothing: scale(0) has no inverse for the box.
func TestSingularTransformDrawsNothing(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, `<g transform="scale(0)"><g opacity="0.5"><rect width="9" height="9"/></g></g>`, reg)
	if reg.depth != 0 || len(reg.groups) != 0 {
		t.Errorf("depth %d, groups %d", reg.depth, len(reg.groups))
	}
}

// TestShadingCTMInGroup: a pattern matrix maps to the coordinates of the form,
// so the transformation outside of the group must not be part of it.
func TestShadingCTMInGroup(t *testing.T) {
	const grad = `<defs><linearGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient></defs>`
	outside, inside := &fakeRegistrar{}, &fakeRegistrar{}
	renderSVG(t, grad+`<g transform="translate(10,20)"><rect width="9" height="9" fill="url(#g)"/></g>`, outside)
	renderSVG(t, grad+`<g transform="translate(10,20)"><g opacity="0.5" transform="translate(3,4)"><rect width="9" height="9" fill="url(#g)"/></g></g>`, inside)
	if len(outside.shadings) != 1 || len(inside.shadings) != 1 {
		t.Fatalf("shadings: %d, %d", len(outside.shadings), len(inside.shadings))
	}
	if got, want := outside.shadings[0].CTM, (Matrix{1, 0, 0, -1, 10, -20}); got != want {
		t.Errorf("CTM outside of a group = %v, want %v", got, want)
	}
	if got, want := inside.shadings[0].CTM, (Matrix{1, 0, 0, 1, 3, 4}); got != want {
		t.Errorf("CTM in a group = %v, want %v", got, want)
	}
}

func TestMatrixInverse(t *testing.T) {
	m := Matrix{2, 1, -1, 3, 5, 7}
	inv, ok := m.Inverse()
	if !ok {
		t.Fatal("no inverse")
	}
	got := m.Multiply(inv)
	for i, v := range Identity() {
		if math.Abs(got[i]-v) > 1e-12 {
			t.Fatalf("m × inverse = %v", got)
		}
	}
	if _, ok := (Matrix{1, 2, 2, 4, 0, 0}).Inverse(); ok {
		t.Error("a singular matrix has no inverse")
	}
}

func TestParseColorAlpha(t *testing.T) {
	testdata := []struct {
		in      string
		r       float64
		alpha   float64
		wantErr bool
	}{
		{"#f00", 1, 1, false},
		{"#f008", 1, 0x88 / 255.0, false},
		{"#ff0000", 1, 1, false},
		{"#ff000080", 1, 0x80 / 255.0, false},
		{"#12", 0, 1, true},
		{"#12345", 0, 1, true},
		{"rgb(255,0,0)", 1, 1, false},
		{"RGB(255,0,0)", 1, 1, false},
		{"rgb(100%, 0%, 0%)", 1, 1, false},
		{"rgb(255 0 0)", 1, 1, false},
		{"rgba(255,0,0,.5)", 1, 0.5, false},
		{"rgba(255, 0, 0, 50%)", 1, 0.5, false},
		{"rgb(255 0 0 / .5)", 1, 0.5, false},
		{"rgb(255 0 0 / 2)", 1, 1, false},
		{"rgb(255,0)", 0, 1, true},
		{"rgb(255 0 0 / x)", 0, 1, true},
		{"red", 1, 1, false},
	}
	for _, td := range testdata {
		c, alpha, err := parseColorAlpha(td.in)
		if (err != nil) != td.wantErr {
			t.Errorf("%s: err = %v", td.in, err)
			continue
		}
		if err == nil && (math.Abs(c.R-td.r) > 1e-9 || math.Abs(alpha-td.alpha) > 1e-9) {
			t.Errorf("%s: R %g alpha %g, want %g, %g", td.in, c.R, alpha, td.r, td.alpha)
		}
	}
}

const fadingGradient = `<defs><linearGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#f00" stop-opacity="0"/></linearGradient></defs>`

func TestStopOpacityUsesAMask(t *testing.T) {
	reg := &fakeRegistrar{}
	out := renderSVG(t, fadingGradient+`<g transform="translate(10,20)"><rect width="9" height="9" fill="url(#g)"/></g>`, reg)
	if len(reg.masks) != 1 {
		t.Fatalf("masks = %d, want 1", len(reg.masks))
	}
	if !strings.Contains(out, "/Mask gs\n/Pattern cs\n/Sh1 scn") {
		t.Errorf("the mask is not set in\n%s", out)
	}
	// The box is the viewport in the coordinates that set the mask.
	want := [4]float64{-10, -20, 90, 80}
	if reg.masks[0].BBox != want {
		t.Errorf("bbox = %v, want %v", reg.masks[0].BBox, want)
	}
}

// TestUniformStopOpacity: the same opacity everywhere needs no soft mask.
func TestUniformStopOpacity(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, `<defs><linearGradient id="g"><stop offset="0" stop-color="#f00" stop-opacity="0.5"/><stop offset="1" stop-color="#00f" style="stop-opacity:50%"/></linearGradient></defs>
		<rect width="9" height="9" fill="url(#g)"/>`, reg)
	if len(reg.masks) != 0 {
		t.Errorf("masks = %d, want none", len(reg.masks))
	}
	if len(reg.alpha) != 1 || reg.alpha[0] != "A0.5_1" {
		t.Errorf("alpha = %v", reg.alpha)
	}
}

// TestMaskTakesTheAlpha: MuPDF counts a constant alpha next to a soft mask
// twice, so the mask carries it and there is no second graphics state.
func TestMaskTakesTheAlpha(t *testing.T) {
	reg := &fakeRegistrar{}
	out := renderSVG(t, fadingGradient+`<rect width="9" height="9" fill="url(#g)" fill-opacity="0.5" opacity="0.5"/>`, reg)
	if len(reg.masks) != 1 || reg.masks[0].Opacity != 0.25 {
		t.Fatalf("masks = %+v, want one with opacity 0.25", reg.masks)
	}
	if len(reg.alpha) != 0 || !strings.Contains(out, "/Mask gs") {
		t.Errorf("alpha = %v in\n%s", reg.alpha, out)
	}
}

// TestStrokeKeepsItsAlpha: the stroke is no part of the mask.
func TestStrokeKeepsItsAlpha(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, fadingGradient+`<rect width="9" height="9" fill="url(#g)" fill-opacity="0.5" stroke="#00f" stroke-opacity="0.25"/>`, reg)
	if len(reg.alpha) != 1 || reg.alpha[0] != "A1_0.25" {
		t.Errorf("alpha = %v, want only the one of the stroke", reg.alpha)
	}
	if len(reg.masks) != 1 || reg.masks[0].Opacity != 0.5 {
		t.Errorf("masks = %+v", reg.masks)
	}
}

func TestInvisibleGradient(t *testing.T) {
	reg := &fakeRegistrar{}
	out := renderSVG(t, `<defs><linearGradient id="g"><stop offset="0" stop-color="transparent"/><stop offset="1" stop-color="rgba(0,0,0,0)"/></linearGradient></defs>
		<rect width="9" height="9" fill="url(#g)"/>`, reg)
	if strings.Contains(out, "scn") || len(reg.shadings) != 0 {
		t.Errorf("an invisible gradient is painted:\n%s", out)
	}
}

func TestStopColorAlpha(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, `<defs><linearGradient id="g"><stop offset="0" stop-color="rgba(255,0,0,.5)" stop-opacity="0.5"/><stop offset="1" stop-color="#f00"/></linearGradient></defs>
		<rect width="9" height="9" fill="url(#g)"/>`, reg)
	if len(reg.masks) != 1 {
		t.Fatalf("masks = %d, want 1", len(reg.masks))
	}
	if got := reg.masks[0].Gradient.Stops[0].StopOpacity; got != 0.25 {
		t.Errorf("opacity of the stop = %g, want 0.25", got)
	}
}

// TestMaskKeepsStrokeOut: a soft mask applies to all that is painted, so the
// stroke is painted on its own. A single B operator would fade it as well.
func TestMaskKeepsStrokeOut(t *testing.T) {
	reg := &fakeRegistrar{}
	out := renderSVG(t, fadingGradient+`<rect width="9" height="9" fill="url(#g)" stroke="#00f" stroke-width="2"/>`, reg)
	fill := strings.Index(out, "\nf\n")
	stroke := strings.Index(out, "\nS\n")
	mask := strings.Index(out, "/Mask gs")
	if strings.Contains(out, "\nB\n") || fill < 0 || stroke < fill || mask < 0 || mask > fill {
		t.Fatalf("fill and stroke are not separate:\n%s", out)
	}
	if end := strings.Index(out[fill:], "\nQ\n"); end < 0 || fill+end > stroke {
		t.Errorf("the stroke is inside of the mask:\n%s", out)
	}
}

// TestMaskInOpacityGroup: the mask belongs to the content of the group.
func TestMaskInOpacityGroup(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, fadingGradient+`<g opacity="0.5"><rect width="9" height="9" fill="url(#g)"/></g>`, reg)
	if len(reg.groups) != 1 || !strings.Contains(reg.groups[0], "/Mask gs") {
		t.Errorf("groups: %v", reg.groups)
	}
}

// TestWithoutMaskRegistrar paints the gradient opaque, as before.
func TestWithoutMaskRegistrar(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">` + fadingGradient +
		`<rect width="9" height="9" fill="url(#g)" stroke="#00f"/></svg>`
	doc, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	out := doc.RenderPDF(RenderOptions{ShadingRegistrar: &fakeRegistrar{}})
	if !strings.Contains(out, "\nB\n") {
		t.Errorf("without a registrar the shape is one operator:\n%s", out)
	}
}
