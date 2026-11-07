package svgreader

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"
)

const userSpaceOnUse = "userSpaceOnUse"

// Gradient is the parsed form of an SVG <linearGradient> or <radialGradient>
// element. spreadMethod and xlink:href are not read.
//
// With GradientUnits "objectBoundingBox", the default of SVG, the coordinates
// are fractions of the bounding box of the shape they fill. With
// "userSpaceOnUse" they are user units, and a percentage is already resolved
// against the viewport.
type Gradient struct {
	ID string
	// Radial tells a <radialGradient> from a <linearGradient>.
	Radial bool
	// X1, Y1, X2, Y2 is the axis of a linear gradient.
	X1, Y1, X2, Y2 float64
	// CX, CY, R is the end circle of a radial gradient, offset 1.
	CX, CY, R float64
	// FX, FY, FR is the start circle of a radial gradient, offset 0.
	FX, FY, FR        float64
	GradientUnits     string
	GradientTransform Matrix // Identity if absent
	Stops             []GradientStop
}

// Shading returns the PDF shading type of the gradient, 2 (axial) or 3
// (radial), and its /Coords.
func (g *Gradient) Shading() (int, []float64) {
	if g.Radial {
		return 3, []float64{g.FX, g.FY, g.FR, g.CX, g.CY, g.R}
	}
	return 2, []float64{g.X1, g.Y1, g.X2, g.Y2}
}

// GradientStop represents a single <stop> inside a gradient.
type GradientStop struct {
	// unknownColor is a stop-color that cannot be read, the stop is black.
	unknownColor string
	Offset       float64 // 0..1
	Color        Color
	StopOpacity  float64 // 1 = opaque
}

// uniformOpacity returns the opacity of the stops if it is the same for all of
// them, so that a constant alpha can stand in for a soft mask.
func (g *Gradient) uniformOpacity() (float64, bool) {
	if len(g.Stops) == 0 {
		return 1, true
	}
	opacity := g.Stops[0].StopOpacity
	for _, stop := range g.Stops[1:] {
		if stop.StopOpacity != opacity {
			return 0, false
		}
	}
	return opacity, true
}

// PaintKind discriminates a paint between "no fill", a flat color, and a
// gradient reference. Distinguishing the three at parse time keeps the
// renderer's mergeStyle free of url(#…) string inspection.
type PaintKind int

const (
	PaintNone PaintKind = iota
	PaintColor
	PaintGradient
	// PaintInherit is the value inherit.
	PaintInherit
	// PaintUnknown is a value that cannot be read. As in a browser it counts
	// as not given, so the paint is inherited.
	PaintUnknown
)

// Paint is a resolved SVG paint value: either none, a uniform color, or a
// reference to a gradient in the document's Defs map.
type Paint struct {
	Kind        PaintKind
	Color       Color
	Alpha       float64 // alpha value of Color when Kind == PaintColor
	GradientRef string  // id of the gradient when Kind == PaintGradient
}

// resolvePaint converts an SVG paint string ("none", "#rgb", "url(#id)", a
// named color, etc.) into a Paint.
func resolvePaint(s string) Paint {
	s = strings.TrimSpace(s)
	if s == "" || s == "none" || s == "transparent" {
		return Paint{Kind: PaintNone}
	}
	if strings.EqualFold(s, "inherit") {
		return Paint{Kind: PaintInherit}
	}
	if strings.HasPrefix(s, "url(") {
		id := strings.TrimSuffix(strings.TrimPrefix(s, "url("), ")")
		id = strings.TrimSpace(id)
		id = strings.TrimPrefix(id, "#")
		if id == "" {
			return Paint{Kind: PaintNone}
		}
		return Paint{Kind: PaintGradient, GradientRef: id}
	}
	c, alpha, err := parseColorAlpha(s)
	if err != nil {
		return Paint{Kind: PaintUnknown}
	}
	if c.IsNone {
		return Paint{Kind: PaintNone}
	}
	return Paint{Kind: PaintColor, Color: c, Alpha: alpha}
}

// parseDefs consumes a <defs> element's children, extracting gradient
// definitions into the document's Defs map. Non-gradient defs (filters,
// clipPaths, …) are skipped — they can be added later without disturbing
// this path.
func parseDefs(dec *xml.Decoder, doc *Document) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "linearGradient", "radialGradient":
				g, err := parseGradient(dec, t, doc)
				if err != nil {
					return err
				}
				if g != nil && g.ID != "" {
					doc.Defs[g.ID] = g
				}
			default:
				if err := dec.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			return nil
		}
	}
}

// parseGradient reads a single <linearGradient> or <radialGradient> element
// including its <stop> children. Returns nil if the element has no usable
// content (no id, no stops).
func parseGradient(dec *xml.Decoder, start xml.StartElement, doc *Document) (*Gradient, error) {
	attrs := attrMap(start.Attr)
	g := &Gradient{
		ID:                attrs["id"],
		Radial:            start.Name.Local == "radialGradient",
		GradientUnits:     "objectBoundingBox",
		GradientTransform: Identity(),
	}
	if v, ok := attrs["gradientUnits"]; ok {
		g.GradientUnits = v
	}
	if v, ok := attrs["gradientTransform"]; ok {
		g.GradientTransform = ParseTransform(v)
	}

	// A percentage in user space refers to the viewport, the radius to its
	// normalized diagonal.
	width, height := doc.ViewBox.Width, doc.ViewBox.Height
	diagonal := math.Sqrt((width*width + height*height) / 2)
	length := func(name, def string, reference float64) float64 {
		v, ok := attrs[name]
		if !ok {
			v = def
		}
		return g.parseLength(v, reference)
	}
	if g.Radial {
		g.CX = length("cx", "50%", width)
		g.CY = length("cy", "50%", height)
		g.R = length("r", "50%", diagonal)
		g.FX = g.CX
		if _, ok := attrs["fx"]; ok {
			g.FX = length("fx", "", width)
		}
		g.FY = g.CY
		if _, ok := attrs["fy"]; ok {
			g.FY = length("fy", "", height)
		}
		g.FR = length("fr", "0%", diagonal)
	} else {
		g.X1 = length("x1", "0%", width)
		g.Y1 = length("y1", "0%", height)
		g.X2 = length("x2", "100%", width)
		g.Y2 = length("y2", "0%", height)
	}

	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "stop" {
				if stop, ok := parseStop(t); ok {
					g.Stops = append(g.Stops, stop)
				}
				if err := dec.Skip(); err != nil {
					return nil, err
				}
			} else {
				if err := dec.Skip(); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			if g.ID == "" || len(g.Stops) == 0 {
				return nil, nil
			}
			g.flattenIfEmpty()
			return g, nil
		}
	}
}

// parseLength reads a coordinate of the gradient. reference is what a
// percentage refers to in user space.
func (g *Gradient) parseLength(s string, reference float64) float64 {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, "%") {
		return parseDimension(s)
	}
	v := parseDimension(s) / 100
	if g.GradientUnits == userSpaceOnUse {
		return v * reference
	}
	return v
}

// flattenIfEmpty handles a gradient without any extent, which SVG paints in the
// last stop. PDF would paint nothing, so it becomes a constant gradient with a
// size.
func (g *Gradient) flattenIfEmpty() {
	empty := !g.Radial && g.X1 == g.X2 && g.Y1 == g.Y2
	if g.Radial {
		empty = g.R <= 0
	}
	if !empty {
		return
	}
	g.Stops = g.Stops[len(g.Stops)-1:]
	g.X2, g.R = g.X1+1, 1
	g.FX, g.FY, g.FR = g.CX, g.CY, 0
}

func parseStop(start xml.StartElement) (GradientStop, bool) {
	stop := GradientStop{StopOpacity: 1}
	var stopColorAttr, styleAttr string
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "offset":
			stop.Offset = parseOffsetValue(a.Value)
		case "stop-color":
			stopColorAttr = a.Value
		case "stop-opacity":
			if v, ok := parseOpacity(a.Value); ok {
				stop.StopOpacity = v
			}
		case "style":
			styleAttr = a.Value
		}
	}
	// style="stop-color:#…;stop-opacity:…" overrides individual attrs
	// (matches extractStyle's precedence rule).
	if styleAttr != "" {
		for _, decl := range strings.Split(styleAttr, ";") {
			parts := strings.SplitN(strings.TrimSpace(decl), ":", 2)
			if len(parts) != 2 {
				continue
			}
			prop := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			switch prop {
			case "stop-color":
				stopColorAttr = val
			case "stop-opacity":
				if v, ok := parseOpacity(val); ok {
					stop.StopOpacity = v
				}
			}
		}
	}
	if stopColorAttr == "" {
		stopColorAttr = "#000000"
	}
	c, alpha, err := parseColorAlpha(stopColorAttr)
	if err != nil {
		// As in a browser the value counts as not given, which is black.
		// A gradient has no way to use a color of the document.
		stop.unknownColor = stopColorAttr
		c, alpha = Color{}, 1
	}
	if strings.EqualFold(strings.TrimSpace(stopColorAttr), "transparent") {
		alpha = 0
	}
	stop.Color = c
	stop.StopOpacity *= alpha
	return stop, true
}

// parseOffsetValue accepts "0.3", "30%", "30 %". Clamped to [0, 1].
func parseOffsetValue(s string) float64 {
	s = strings.TrimSpace(s)
	pct := false
	if strings.HasSuffix(s, "%") {
		s = strings.TrimSuffix(s, "%")
		pct = true
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	if pct {
		v /= 100
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
