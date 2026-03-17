package svgreader

import (
	"strings"
	"testing"
)

func TestParsePathData(t *testing.T) {
	tests := []struct {
		name string
		d    string
		want int // expected number of commands
	}{
		{"moveto lineto", "M 10 20 L 30 40", 2},
		{"implicit lineto", "M 10 20 30 40 50 60", 3}, // M + 2x implicit L
		{"cubic", "M 0 0 C 10 20 30 40 50 60", 2},
		{"close", "M 0 0 L 10 0 L 10 10 Z", 4},
		{"relative", "m 10 20 l 5 5", 2},
		{"no spaces", "M10,20L30,40", 2},
		{"negative", "M10-20L-30 40", 2},
		{"decimal", "M.5.5L1.5 2.5", 2},
		{"hv", "M 0 0 H 10 V 20", 3},
		{"quadratic", "M 0 0 Q 10 20 30 0", 2},
		{"smooth cubic", "M 0 0 C 10 20 30 40 50 60 S 70 80 90 100", 3},
		{"arc", "M 10 80 A 25 25 0 0 1 50 50", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds, err := ParsePathData(tt.d)
			if err != nil {
				t.Fatalf("ParsePathData(%q) error: %v", tt.d, err)
			}
			if len(cmds) != tt.want {
				t.Errorf("ParsePathData(%q) got %d commands, want %d", tt.d, len(cmds), tt.want)
				for i, c := range cmds {
					t.Logf("  [%d] %c %v", i, c.Cmd, c.Args)
				}
			}
		})
	}
}

func TestParsePathDataArcFlags(t *testing.T) {
	// Arc flags can be packed without separators: "0 0 1" or "001"
	cmds, err := ParsePathData("M 10 80 A 25 25 0 0 1 50 50")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 {
		t.Fatalf("got %d cmds, want 2", len(cmds))
	}
	arc := cmds[1]
	if arc.Cmd != 'A' {
		t.Fatalf("got cmd %c, want A", arc.Cmd)
	}
	// flags at index 3 and 4
	if arc.Args[3] != 0 || arc.Args[4] != 1 {
		t.Errorf("arc flags: got %v %v, want 0 1", arc.Args[3], arc.Args[4])
	}
}

func TestResolveToAbsolute(t *testing.T) {
	cmds, _ := ParsePathData("m 10 20 l 5 5 l 10 0")
	abs := ResolveToAbsolute(cmds)

	// m 10 20 → M 10 20
	// l 5 5  → L 15 25
	// l 10 0 → L 25 25
	if abs[1].Args[0] != 15 || abs[1].Args[1] != 25 {
		t.Errorf("first l: got (%v,%v), want (15,25)", abs[1].Args[0], abs[1].Args[1])
	}
	if abs[2].Args[0] != 25 || abs[2].Args[1] != 25 {
		t.Errorf("second l: got (%v,%v), want (25,25)", abs[2].Args[0], abs[2].Args[1])
	}
}

func TestParseColor(t *testing.T) {
	tests := []struct {
		input   string
		r, g, b float64
		none    bool
	}{
		{"none", 0, 0, 0, true},
		{"#ff0000", 1, 0, 0, false},
		{"#f00", 1, 0, 0, false},
		{"red", 1, 0, 0, false},
		{"rgb(0, 128, 255)", 0, 128.0 / 255, 1, false},
		{"rgb(50%, 0%, 100%)", 0.5, 0, 1, false},
		{"black", 0, 0, 0, false},
		{"white", 1, 1, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			c, err := ParseColor(tt.input)
			if err != nil {
				t.Fatalf("ParseColor(%q) error: %v", tt.input, err)
			}
			if c.IsNone != tt.none {
				t.Errorf("IsNone: got %v, want %v", c.IsNone, tt.none)
			}
			if !tt.none {
				if !closeEnough(c.R, tt.r) || !closeEnough(c.G, tt.g) || !closeEnough(c.B, tt.b) {
					t.Errorf("got (%v,%v,%v), want (%v,%v,%v)", c.R, c.G, c.B, tt.r, tt.g, tt.b)
				}
			}
		})
	}
}

func TestParseTransform(t *testing.T) {
	m := ParseTransform("translate(10, 20)")
	if m[4] != 10 || m[5] != 20 {
		t.Errorf("translate: got e=%v f=%v, want 10 20", m[4], m[5])
	}

	m = ParseTransform("scale(2)")
	if m[0] != 2 || m[3] != 2 {
		t.Errorf("scale: got a=%v d=%v, want 2 2", m[0], m[3])
	}

	m = ParseTransform("scale(2, 3)")
	if m[0] != 2 || m[3] != 3 {
		t.Errorf("scale: got a=%v d=%v, want 2 3", m[0], m[3])
	}
}

func TestParseSVG(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100">
		<g fill="red" transform="translate(10,10)">
			<path d="M 0 0 L 50 50" stroke="blue" stroke-width="2"/>
			<rect x="10" y="10" width="30" height="20" fill="green"/>
		</g>
		<circle cx="50" cy="50" r="25" fill="none" stroke="black"/>
	</svg>`

	doc, err := Parse(strings.NewReader(svg))
	if err != nil {
		t.Fatal(err)
	}

	if doc.Width != 100 || doc.Height != 100 {
		t.Errorf("dimensions: got %vx%v, want 100x100", doc.Width, doc.Height)
	}

	if len(doc.Elements) != 2 {
		t.Fatalf("got %d elements, want 2 (g + circle)", len(doc.Elements))
	}

	g, ok := doc.Elements[0].(Group)
	if !ok {
		t.Fatal("first element is not a Group")
	}
	if g.Style.Fill != "red" {
		t.Errorf("group fill: got %q, want red", g.Style.Fill)
	}
	if len(g.Children) != 2 {
		t.Errorf("group children: got %d, want 2", len(g.Children))
	}

	_, ok = doc.Elements[1].(Circle)
	if !ok {
		t.Fatal("second element is not a Circle")
	}
}

func TestRenderPDF(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<path d="M 10 10 L 90 10 L 90 90 Z" fill="red" stroke="blue" stroke-width="2"/>
	</svg>`

	doc, err := Parse(strings.NewReader(svg))
	if err != nil {
		t.Fatal(err)
	}

	pdf := doc.RenderPDF(RenderOptions{Width: 100, Height: 100})

	// Should contain basic PDF operators
	if !strings.Contains(pdf, "q") {
		t.Error("missing q (save state)")
	}
	if !strings.Contains(pdf, "Q") {
		t.Error("missing Q (restore state)")
	}
	if !strings.Contains(pdf, "m") {
		t.Error("missing m (moveto)")
	}
	if !strings.Contains(pdf, "l") {
		t.Error("missing l (lineto)")
	}
	if !strings.Contains(pdf, "h") {
		t.Error("missing h (closepath)")
	}
	if !strings.Contains(pdf, "B") {
		t.Error("missing B (fill+stroke)")
	}

	t.Logf("PDF content stream:\n%s", pdf)
}

func closeEnough(a, b float64) bool {
	if a == b {
		return true
	}
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.002
}
