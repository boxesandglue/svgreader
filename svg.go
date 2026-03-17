// Package svgreader parses SVG documents and renders them as PDF content streams.
//
// The package supports a practical subset of SVG: paths, basic shapes (rect,
// circle, ellipse, line, polyline, polygon), groups with transforms, and
// fill/stroke styling. Text elements are parsed and can be rendered using an
// external text shaper (e.g. textshape).
//
// Usage:
//
//	doc, err := svgreader.Parse(reader)
//	pdfStream := doc.RenderPDF(widthPt, heightPt)
package svgreader

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Document represents a parsed SVG document.
type Document struct {
	Width      float64   // Document width in SVG user units
	Height     float64   // Document height in SVG user units
	ViewBox    ViewBox   // ViewBox specification
	Elements   []Element // Child elements in document order
	FontFamily string    // Default font-family from the <svg> element
}

// ViewBox defines the SVG coordinate system.
type ViewBox struct {
	MinX, MinY    float64
	Width, Height float64
}

// Element is the interface implemented by all SVG elements.
type Element interface {
	elementTag() string
}

// Group represents an SVG <g> element.
type Group struct {
	Style     StyleAttrs
	Transform string
	Children  []Element
}

func (Group) elementTag() string { return "g" }

// Path represents an SVG <path> element.
type Path struct {
	D         string
	Style     StyleAttrs
	Transform string
}

func (Path) elementTag() string { return "path" }

// Rect represents an SVG <rect> element.
type Rect struct {
	X, Y          float64
	Width, Height float64
	Rx, Ry        float64
	Style         StyleAttrs
	Transform     string
}

func (Rect) elementTag() string { return "rect" }

// Circle represents an SVG <circle> element.
type Circle struct {
	Cx, Cy    float64
	R         float64
	Style     StyleAttrs
	Transform string
}

func (Circle) elementTag() string { return "circle" }

// Ellipse represents an SVG <ellipse> element.
type Ellipse struct {
	Cx, Cy    float64
	Rx, Ry    float64
	Style     StyleAttrs
	Transform string
}

func (Ellipse) elementTag() string { return "ellipse" }

// Line represents an SVG <line> element.
type Line struct {
	X1, Y1    float64
	X2, Y2    float64
	Style     StyleAttrs
	Transform string
}

func (Line) elementTag() string { return "line" }

// Polyline represents an SVG <polyline> element.
type Polyline struct {
	Points    []Point
	Style     StyleAttrs
	Transform string
}

func (Polyline) elementTag() string { return "polyline" }

// Polygon represents an SVG <polygon> element.
type Polygon struct {
	Points    []Point
	Style     StyleAttrs
	Transform string
}

func (Polygon) elementTag() string { return "polygon" }

// Text represents an SVG <text> element.
// Text rendering requires an external shaper (e.g. textshape).
type Text struct {
	X, Y       float64
	Content    string
	FontFamily string
	FontSize   float64
	FontWeight string
	FontStyle  string
	TextAnchor string // "start", "middle", "end"
	Style      StyleAttrs
	Transform  string
}

func (Text) elementTag() string { return "text" }

// Point is a 2D coordinate.
type Point struct {
	X, Y float64
}

// StyleAttrs holds SVG presentation attributes.
type StyleAttrs struct {
	Fill             string
	FillOpacity      string
	Stroke           string
	StrokeWidth      string
	StrokeOpacity    string
	StrokeLinecap    string
	StrokeLinejoin   string
	StrokeDasharray  string
	StrokeDashoffset string
	Opacity          string
	FillRule         string
	ClipPath         string
}

// Parse parses an SVG document from a reader.
func Parse(r io.Reader) (*Document, error) {
	dec := xml.NewDecoder(r)

	// Find the <svg> start element
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("svgreader: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "svg" {
			return parseSVG(dec, se)
		}
	}
}

func parseSVG(dec *xml.Decoder, start xml.StartElement) (*Document, error) {
	doc := &Document{}

	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "width":
			doc.Width = parseDimension(attr.Value)
		case "height":
			doc.Height = parseDimension(attr.Value)
		case "viewBox":
			doc.ViewBox = parseViewBox(attr.Value)
		case "font-family":
			doc.FontFamily = attr.Value
		}
	}

	// If no viewBox, derive from width/height
	if doc.ViewBox.Width == 0 && doc.Width > 0 {
		doc.ViewBox = ViewBox{Width: doc.Width, Height: doc.Height}
	}
	// If no width/height, derive from viewBox
	if doc.Width == 0 && doc.ViewBox.Width > 0 {
		doc.Width = doc.ViewBox.Width
		doc.Height = doc.ViewBox.Height
	}

	elements, err := parseChildren(dec)
	if err != nil {
		return nil, err
	}
	doc.Elements = elements
	return doc, nil
}

func parseChildren(dec *xml.Decoder) ([]Element, error) {
	var elements []Element

	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("svgreader: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			elem, err := parseElement(dec, t)
			if err != nil {
				return nil, err
			}
			if elem != nil {
				elements = append(elements, elem)
			}
		case xml.EndElement:
			return elements, nil
		}
	}
}

func parseElement(dec *xml.Decoder, start xml.StartElement) (Element, error) {
	attrs := attrMap(start.Attr)
	style := extractStyle(attrs)
	transform := attrs["transform"]

	switch start.Name.Local {
	case "g":
		children, err := parseChildren(dec)
		if err != nil {
			return nil, err
		}
		return Group{Style: style, Transform: transform, Children: children}, nil

	case "path":
		dec.Skip()
		return Path{D: attrs["d"], Style: style, Transform: transform}, nil

	case "rect":
		dec.Skip()
		return Rect{
			X: attrFloat(attrs, "x"), Y: attrFloat(attrs, "y"),
			Width: attrFloat(attrs, "width"), Height: attrFloat(attrs, "height"),
			Rx: attrFloat(attrs, "rx"), Ry: attrFloat(attrs, "ry"),
			Style: style, Transform: transform,
		}, nil

	case "circle":
		dec.Skip()
		return Circle{
			Cx: attrFloat(attrs, "cx"), Cy: attrFloat(attrs, "cy"),
			R:     attrFloat(attrs, "r"),
			Style: style, Transform: transform,
		}, nil

	case "ellipse":
		dec.Skip()
		return Ellipse{
			Cx: attrFloat(attrs, "cx"), Cy: attrFloat(attrs, "cy"),
			Rx: attrFloat(attrs, "rx"), Ry: attrFloat(attrs, "ry"),
			Style: style, Transform: transform,
		}, nil

	case "line":
		dec.Skip()
		return Line{
			X1: attrFloat(attrs, "x1"), Y1: attrFloat(attrs, "y1"),
			X2: attrFloat(attrs, "x2"), Y2: attrFloat(attrs, "y2"),
			Style: style, Transform: transform,
		}, nil

	case "polyline":
		dec.Skip()
		return Polyline{
			Points: parsePoints(attrs["points"]),
			Style:  style, Transform: transform,
		}, nil

	case "polygon":
		dec.Skip()
		return Polygon{
			Points: parsePoints(attrs["points"]),
			Style:  style, Transform: transform,
		}, nil

	case "text":
		content, err := parseTextContent(dec)
		if err != nil {
			return nil, err
		}
		return Text{
			X: attrFloat(attrs, "x"), Y: attrFloat(attrs, "y"),
			Content:    content,
			FontFamily: firstOf(attrs["font-family"], styleValue(attrs, "font-family")),
			FontSize:   parseDimension(firstOf(attrs["font-size"], styleValue(attrs, "font-size"), "12")),
			FontWeight: firstOf(attrs["font-weight"], styleValue(attrs, "font-weight")),
			FontStyle:  firstOf(attrs["font-style"], styleValue(attrs, "font-style")),
			TextAnchor: firstOf(attrs["text-anchor"], styleValue(attrs, "text-anchor")),
			Style:      style,
			Transform:  transform,
		}, nil

	default:
		// Skip unknown elements
		dec.Skip()
		return nil, nil
	}
}

func parseTextContent(dec *xml.Decoder) (string, error) {
	var buf strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			buf.Write(t)
		case xml.EndElement:
			return strings.TrimSpace(buf.String()), nil
		case xml.StartElement:
			// Skip nested elements like <tspan> for now,
			// but collect their text content
			inner, err := parseTextContent(dec)
			if err != nil {
				return "", err
			}
			buf.WriteString(inner)
		}
	}
}

// extractStyle reads SVG presentation attributes, with the style attribute
// taking precedence over individual attributes (CSS specificity).
func extractStyle(attrs map[string]string) StyleAttrs {
	s := StyleAttrs{
		Fill:             attrs["fill"],
		FillOpacity:      attrs["fill-opacity"],
		Stroke:           attrs["stroke"],
		StrokeWidth:      attrs["stroke-width"],
		StrokeOpacity:    attrs["stroke-opacity"],
		StrokeLinecap:    attrs["stroke-linecap"],
		StrokeLinejoin:   attrs["stroke-linejoin"],
		StrokeDasharray:  attrs["stroke-dasharray"],
		StrokeDashoffset: attrs["stroke-dashoffset"],
		Opacity:          attrs["opacity"],
		FillRule:         attrs["fill-rule"],
		ClipPath:         attrs["clip-path"],
	}

	// The style attribute overrides individual attributes
	if cssStyle, ok := attrs["style"]; ok {
		parseInlineCSS(cssStyle, &s)
	}
	return s
}

func parseInlineCSS(css string, s *StyleAttrs) {
	for _, decl := range strings.Split(css, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		parts := strings.SplitN(decl, ":", 2)
		if len(parts) != 2 {
			continue
		}
		prop := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch prop {
		case "fill":
			s.Fill = val
		case "fill-opacity":
			s.FillOpacity = val
		case "stroke":
			s.Stroke = val
		case "stroke-width":
			s.StrokeWidth = val
		case "stroke-opacity":
			s.StrokeOpacity = val
		case "stroke-linecap":
			s.StrokeLinecap = val
		case "stroke-linejoin":
			s.StrokeLinejoin = val
		case "stroke-dasharray":
			s.StrokeDasharray = val
		case "stroke-dashoffset":
			s.StrokeDashoffset = val
		case "opacity":
			s.Opacity = val
		case "fill-rule":
			s.FillRule = val
		}
	}
}

// styleValue extracts a property from the inline style attribute.
func styleValue(attrs map[string]string, prop string) string {
	css, ok := attrs["style"]
	if !ok {
		return ""
	}
	for _, decl := range strings.Split(css, ";") {
		parts := strings.SplitN(strings.TrimSpace(decl), ":", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == prop {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

// Helper functions

func attrMap(attrs []xml.Attr) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[a.Name.Local] = a.Value
	}
	return m
}

func attrFloat(attrs map[string]string, name string) float64 {
	v, ok := attrs[name]
	if !ok {
		return 0
	}
	return parseDimension(v)
}

// parseDimension parses a CSS/SVG dimension value like "100", "100px", "72pt".
// For now, treats all values as user units (px). Unit conversion can be added.
func parseDimension(s string) float64 {
	s = strings.TrimSpace(s)
	// Strip known unit suffixes
	for _, suffix := range []string{"px", "pt", "mm", "cm", "in", "em", "ex", "%"} {
		if strings.HasSuffix(s, suffix) {
			s = strings.TrimSuffix(s, suffix)
			// TODO: apply unit conversion factor
			break
		}
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func parseViewBox(s string) ViewBox {
	fields := strings.Fields(strings.ReplaceAll(s, ",", " "))
	if len(fields) < 4 {
		return ViewBox{}
	}
	vb := ViewBox{}
	vb.MinX, _ = strconv.ParseFloat(fields[0], 64)
	vb.MinY, _ = strconv.ParseFloat(fields[1], 64)
	vb.Width, _ = strconv.ParseFloat(fields[2], 64)
	vb.Height, _ = strconv.ParseFloat(fields[3], 64)
	return vb
}

func parsePoints(s string) []Point {
	s = strings.ReplaceAll(s, ",", " ")
	fields := strings.Fields(s)
	var pts []Point
	for i := 0; i+1 < len(fields); i += 2 {
		x, _ := strconv.ParseFloat(fields[i], 64)
		y, _ := strconv.ParseFloat(fields[i+1], 64)
		pts = append(pts, Point{x, y})
	}
	return pts
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
