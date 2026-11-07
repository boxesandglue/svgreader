package svgreader

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

const stops = `<stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/>`

func parseGradientFor(t *testing.T, gradient string) *Gradient {
	t.Helper()
	src := `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="400" viewBox="10 20 200 100"><defs>` + gradient + `</defs></svg>`
	doc, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	g, ok := doc.Defs["g"]
	if !ok {
		t.Fatalf("no gradient in %s", gradient)
	}
	return g
}

func TestGradientCoordinates(t *testing.T) {
	diagonal := math.Sqrt((200*200 + 100*100) / 2.0)
	testdata := []struct {
		name        string
		gradient    string
		shadingType int
		want        []float64
	}{
		{"linear defaults", `<linearGradient id="g">` + stops + `</linearGradient>`, 2, []float64{0, 0, 1, 0}},
		{"linear percent in the box", `<linearGradient id="g" x1="10%" y2="100%">` + stops + `</linearGradient>`, 2, []float64{0.1, 0, 1, 1}},
		{"linear percent in user space", `<linearGradient id="g" gradientUnits="userSpaceOnUse" x1="10%" y2="100%">` + stops + `</linearGradient>`, 2, []float64{20, 0, 200, 100}},
		{"linear numbers", `<linearGradient id="g" gradientUnits="userSpaceOnUse" x1="1" y1="2" x2="3" y2="4">` + stops + `</linearGradient>`, 2, []float64{1, 2, 3, 4}},
		{"radial defaults", `<radialGradient id="g">` + stops + `</radialGradient>`, 3, []float64{.5, .5, 0, .5, .5, .5}},
		{"radial default focus follows the center", `<radialGradient id="g" cx="0.2" cy="0.3" r="0.4">` + stops + `</radialGradient>`, 3, []float64{.2, .3, 0, .2, .3, .4}},
		{"radial focus", `<radialGradient id="g" fx="0.1" fy="0.2" fr="0.05">` + stops + `</radialGradient>`, 3, []float64{.1, .2, .05, .5, .5, .5}},
		{"radial numbers", `<radialGradient id="g" gradientUnits="userSpaceOnUse" cx="50" cy="60" r="70">` + stops + `</radialGradient>`, 3, []float64{50, 60, 0, 50, 60, 70}},
		{"radial percent in user space", `<radialGradient id="g" gradientUnits="userSpaceOnUse">` + stops + `</radialGradient>`, 3, []float64{100, 50, 0, 100, 50, diagonal / 2}},
	}
	for _, td := range testdata {
		t.Run(td.name, func(t *testing.T) {
			shadingType, coords := parseGradientFor(t, td.gradient).Shading()
			if shadingType != td.shadingType {
				t.Errorf("shading type = %d, want %d", shadingType, td.shadingType)
			}
			if len(coords) != len(td.want) {
				t.Fatalf("coords = %v, want %v", coords, td.want)
			}
			for i := range coords {
				if math.Abs(coords[i]-td.want[i]) > 1e-9 {
					t.Fatalf("coords = %v, want %v", coords, td.want)
				}
			}
		})
	}
}

// TestGradientWithoutExtent: SVG paints the last stop, where a PDF shading
// would paint nothing at all.
func TestGradientWithoutExtent(t *testing.T) {
	testdata := []string{
		`<radialGradient id="g" r="0">` + stops + `</radialGradient>`,
		`<radialGradient id="g" r="-1">` + stops + `</radialGradient>`,
		`<linearGradient id="g" x1="0.5" x2="0.5">` + stops + `</linearGradient>`,
	}
	for _, src := range testdata {
		g := parseGradientFor(t, src)
		if len(g.Stops) != 1 || g.Stops[0].Offset != 1 {
			t.Errorf("%s: stops = %v, want the last one", src, g.Stops)
		}
		_, coords := g.Shading()
		if g.Radial && coords[5] <= 0 {
			t.Errorf("%s: radius in %v", src, coords)
		}
		if !g.Radial && coords[0] == coords[2] {
			t.Errorf("%s: no extent in %v", src, coords)
		}
	}
}

func TestGradientInFlow(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, `<radialGradient id="g">`+stops+`</radialGradient><rect width="9" height="9" fill="url(#g)"/>`, reg)
	if len(reg.shadings) != 1 || !reg.shadings[0].Gradient.Radial {
		t.Errorf("shadings = %v", reg.shadings)
	}
}

// TestBoundingBoxUnits: the box of the shape goes between the CTM and the
// gradient.
func TestBoundingBoxUnits(t *testing.T) {
	const grad = `<defs><radialGradient id="g">` + stops + `</radialGradient></defs>`
	testdata := []struct {
		name  string
		shape string
		want  Matrix // in user space, y up in PDF is part of the root CTM
	}{
		{"rect", `<rect x="10" y="20" width="30" height="40" fill="url(#g)"/>`, Matrix{30, 0, 0, 40, 10, 20}},
		{"circle", `<circle cx="50" cy="50" r="10" fill="url(#g)"/>`, Matrix{20, 0, 0, 20, 40, 40}},
		{"ellipse", `<ellipse cx="50" cy="50" rx="10" ry="20" fill="url(#g)"/>`, Matrix{20, 0, 0, 40, 40, 30}},
		{"polygon", `<polygon points="10,10 30,10 20,50" fill="url(#g)"/>`, Matrix{20, 0, 0, 40, 10, 10}},
		{"path", `<path d="M10 10 H30 V50 Z" fill="url(#g)"/>`, Matrix{20, 0, 0, 40, 10, 10}},
		// The control points reach y = 60, the curve itself only 52.5.
		{"curve", `<path d="M10 30 C10 60 30 60 30 30 Z" fill="url(#g)"/>`, Matrix{20, 0, 0, 22.5, 10, 30}},
		{"inherited", `<g fill="url(#g)"><rect x="10" y="20" width="30" height="40"/></g>`, Matrix{30, 0, 0, 40, 10, 20}},
	}
	for _, td := range testdata {
		t.Run(td.name, func(t *testing.T) {
			reg := &fakeRegistrar{}
			renderSVG(t, grad+td.shape, reg)
			if len(reg.shadings) != 1 {
				t.Fatalf("shadings = %v", reg.shadings)
			}
			want := (Matrix{1, 0, 0, -1, 0, 0}).Multiply(td.want)
			for i, v := range reg.shadings[0].CTM {
				if math.Abs(v-want[i]) > 1e-6 {
					t.Fatalf("CTM = %v, want %v", reg.shadings[0].CTM, want)
				}
			}
		})
	}
}

func TestUserSpaceUnitsIgnoreTheShape(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, `<defs><radialGradient id="g" gradientUnits="userSpaceOnUse" r="5">`+stops+`</radialGradient></defs>
		<rect x="10" y="20" width="30" height="40" fill="url(#g)"/>`, reg)
	if got, want := reg.shadings[0].CTM, (Matrix{1, 0, 0, -1, 0, 0}); got != want {
		t.Errorf("CTM = %v, want %v", got, want)
	}
}

// TestBoundingBoxWithoutArea: a shape without area has no units for the
// gradient, so SVG paints no fill.
func TestBoundingBoxWithoutArea(t *testing.T) {
	reg := &fakeRegistrar{}
	out := renderSVG(t, `<defs><radialGradient id="g">`+stops+`</radialGradient></defs>
		<rect x="10" y="20" width="30" height="0" fill="url(#g)"/>`, reg)
	if len(reg.shadings) != 0 {
		t.Errorf("shadings = %v", reg.shadings)
	}
	if strings.Contains(out, " f\n") || strings.HasSuffix(out, "\nf") {
		t.Errorf("a shape without area is filled:\n%s", out)
	}
}

func TestMaskTakesTheBoundingBox(t *testing.T) {
	reg := &fakeRegistrar{}
	renderSVG(t, `<defs><radialGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#f00" stop-opacity="0"/></radialGradient></defs>
		<circle cx="50" cy="50" r="10" fill="url(#g)"/>`, reg)
	if len(reg.masks) != 1 {
		t.Fatalf("masks = %v", reg.masks)
	}
	if got, want := reg.masks[0].Matrix, (Matrix{20, 0, 0, 20, 40, 40}); got != want {
		t.Errorf("mask matrix = %v, want %v", got, want)
	}
}

// TestUnknownGradientPaintsNothing: the fill of the parent does not stand in
// for a reference without a target.
func TestUnknownGradientPaintsNothing(t *testing.T) {
	testdata := []string{
		`<rect width="9" height="9" fill="url(#nope)"/>`,
		`<g fill="#f00"><rect width="9" height="9" fill="url(#nope)"/></g>`,
		`<g fill="url(#nope)"><rect width="9" height="9"/></g>`,
	}
	for _, body := range testdata {
		out := renderSVG(t, body, &fakeRegistrar{})
		for _, line := range strings.Split(out, "\n") {
			if line == "f" || strings.HasSuffix(line, " rg") {
				t.Errorf("%s is painted:\n%s", body, out)
			}
		}
	}
}

func TestPaintWarnings(t *testing.T) {
	const grad = `<defs><linearGradient id="g">` + stops + `</linearGradient></defs>`
	testdata := []struct {
		name string
		body string
		want []string
	}{
		{"known gradient", grad + `<rect width="9" height="9" fill="url(#g)"/>`, nil},
		{"unknown gradient once", `<rect width="9" height="9" fill="url(#nope)"/><rect width="9" height="9" fill="url(#nope)"/>`,
			[]string{"SVG: fill refers to an unknown gradient: nope"}},
		{"stroke", grad + `<rect width="9" height="9" stroke="url(#g)"/>`,
			[]string{"SVG: a gradient is not supported for a stroke: g"}},
		{"text", grad + `<text fill="url(#g)">x</text>`,
			[]string{"SVG: a gradient is not supported for text: g"}},
	}
	for _, td := range testdata {
		t.Run(td.name, func(t *testing.T) {
			doc, err := Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">` + td.body + `</svg>`))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			var got []string
			doc.RenderPDF(RenderOptions{
				TextRenderer: nopText{},
				Warn:         func(message string) { got = append(got, message) },
			})
			if !reflect.DeepEqual(got, td.want) {
				t.Errorf("warnings = %q, want %q", got, td.want)
			}
		})
	}
}

type nopText struct{}

func (nopText) RenderText(string, float64, float64, float64, string, string, string, string, Color) string {
	return ""
}

// TestPathOperators keeps the output of paths as it was before the operators
// became a list.
func TestPathOperators(t *testing.T) {
	doc, err := Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100">
		<path d="M1 2 H3 V4 L5 6 Q7 8 9 10 C1 2 3 4 5 6 Z"/></svg>`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out := doc.RenderPDF(RenderOptions{})
	for _, want := range []string{"1 2 m", "3 2 l", "3 4 l", "5 6 l", "1 2 3 4 5 6 c", "\nh"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
}
