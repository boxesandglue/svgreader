package svgreader

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseDashArray(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []float64
	}{
		{"empty", "", nil},
		{"none", "none", nil},
		{"none-mixed-case", "None", nil},
		{"single value doubled", "5", []float64{5, 5}},
		{"pair", "5 10", []float64{5, 10}},
		{"comma separated", "5,10", []float64{5, 10}},
		{"comma+space", "5, 10, 2, 3", []float64{5, 10, 2, 3}},
		{"odd length tripled", "1 2 3", []float64{1, 2, 3, 1, 2, 3}},
		{"decimal", "1.5 0.5", []float64{1.5, 0.5}},
		{"negative rejected", "5 -2", nil},
		{"garbage rejected", "5 foo 3", nil},
		{"extra whitespace", "  5   10  ", []float64{5, 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDashArray(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseDashArray(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestDashArrayEmitsSetDash renders an inline SVG with stroke-dasharray and
// confirms the PDF d operator is emitted.
func TestDashArrayEmitsSetDash(t *testing.T) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100">
<line x1="0" y1="50" x2="100" y2="50" stroke="black" stroke-width="2" stroke-dasharray="5 3"/>
</svg>`
	doc, err := Parse(strings.NewReader(svg))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out := doc.RenderPDF(RenderOptions{})
	if !strings.Contains(out, "[5 3] 0 d") {
		t.Errorf("expected dashed pattern [5 3] 0 d in output, got:\n%s", out)
	}
}

// TestDashArrayNoneEmitsSolidReset confirms a fresh stroke without dasharray
// resets the dash pattern.
func TestDashArrayNoneEmitsSolidReset(t *testing.T) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100">
<line x1="0" y1="50" x2="100" y2="50" stroke="black"/>
</svg>`
	doc, err := Parse(strings.NewReader(svg))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out := doc.RenderPDF(RenderOptions{})
	if !strings.Contains(out, "[] 0 d") {
		t.Errorf("expected solid reset [] 0 d for non-dashed stroke, got:\n%s", out)
	}
}
