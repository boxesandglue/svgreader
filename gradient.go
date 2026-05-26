package svgreader

import (
	"encoding/xml"
	"strconv"
	"strings"
)

// LinearGradient is the parsed form of an SVG <linearGradient> element.
// Only the subset needed for PDF Shading Type 2 (axial shading) is captured;
// spreadMethod, xlink:href, and percentage coordinates against
// objectBoundingBox are out of scope for the first iteration.
type LinearGradient struct {
	ID                string
	X1, Y1, X2, Y2    float64
	GradientUnits     string // "userSpaceOnUse" (default for this implementation)
	GradientTransform Matrix // Identity if absent
	Stops             []GradientStop
}

// GradientStop represents a single <stop> inside a gradient.
type GradientStop struct {
	Offset      float64 // 0..1
	Color       Color
	StopOpacity float64 // 1 = opaque
}

// PaintKind discriminates a paint between "no fill", a flat color, and a
// gradient reference. Distinguishing the three at parse time keeps the
// renderer's mergeStyle free of url(#…) string inspection.
type PaintKind int

const (
	PaintNone PaintKind = iota
	PaintColor
	PaintGradient
)

// Paint is a resolved SVG paint value: either none, a uniform color, or a
// reference to a gradient in the document's Defs map.
type Paint struct {
	Kind        PaintKind
	Color       Color
	GradientRef string // id of the gradient when Kind == PaintGradient
}

// resolvePaint converts an SVG paint string ("none", "#rgb", "url(#id)", a
// named color, etc.) into a Paint. Unparseable values become PaintNone.
func resolvePaint(s string) Paint {
	s = strings.TrimSpace(s)
	if s == "" || s == "none" || s == "transparent" {
		return Paint{Kind: PaintNone}
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
	c, err := ParseColor(s)
	if err != nil || c.IsNone {
		return Paint{Kind: PaintNone}
	}
	return Paint{Kind: PaintColor, Color: c}
}

// parseDefs consumes a <defs> element's children, extracting gradient
// definitions into the document's Defs map. Non-gradient defs (filters,
// clipPaths, …) are skipped — they can be added later without disturbing
// this path.
func parseDefs(dec *xml.Decoder, defs map[string]*LinearGradient) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "linearGradient":
				g, err := parseLinearGradient(dec, t)
				if err != nil {
					return err
				}
				if g != nil && g.ID != "" {
					defs[g.ID] = g
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

// parseLinearGradient reads a single <linearGradient> element including its
// <stop> children. Returns nil if the element has no usable content (no id,
// no stops).
func parseLinearGradient(dec *xml.Decoder, start xml.StartElement) (*LinearGradient, error) {
	g := &LinearGradient{
		// SVG defaults for linearGradient coordinates are 0%,0%,100%,0% in
		// objectBoundingBox space. We treat absent x2 as 1 so the default
		// makes sense once objectBoundingBox is supported; userSpaceOnUse
		// SVGs always supply all four explicitly anyway.
		X2:                1,
		GradientUnits:     "objectBoundingBox",
		GradientTransform: Identity(),
	}
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "id":
			g.ID = a.Value
		case "x1":
			g.X1 = parseDimension(a.Value)
		case "y1":
			g.Y1 = parseDimension(a.Value)
		case "x2":
			g.X2 = parseDimension(a.Value)
		case "y2":
			g.Y2 = parseDimension(a.Value)
		case "gradientUnits":
			g.GradientUnits = a.Value
		case "gradientTransform":
			g.GradientTransform = ParseTransform(a.Value)
		}
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
			return g, nil
		}
	}
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
			if v, err := strconv.ParseFloat(strings.TrimSpace(a.Value), 64); err == nil {
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
				if v, err := strconv.ParseFloat(val, 64); err == nil {
					stop.StopOpacity = v
				}
			}
		}
	}
	if stopColorAttr == "" {
		stopColorAttr = "#000000"
	}
	c, err := ParseColor(stopColorAttr)
	if err != nil {
		return GradientStop{}, false
	}
	stop.Color = c
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
