package svgreader

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// TextRenderer is implemented by external text shapers (e.g. textshape) to
// render SVG <text> elements into PDF operators. If nil, text elements are
// skipped.
type TextRenderer interface {
	// RenderText returns PDF operators to render text at the given position.
	// fontSize is in PDF points. textAnchor is "start", "middle", or "end".
	// The returned string is inserted into the content stream as-is.
	RenderText(text string, x, y, fontSize float64, fontFamily, fontWeight, fontStyle, textAnchor string, fill Color) string
}

// RenderOptions controls PDF rendering.
type RenderOptions struct {
	// Width and Height specify the output size in PDF points.
	// If zero, the SVG's own dimensions are used (1 SVG user unit = 1 PDF point).
	Width, Height float64

	// TextRenderer handles <text> elements. If nil, text is skipped.
	TextRenderer TextRenderer
}

// RenderPDF renders the SVG document as a PDF content stream.
// The width and height specify the desired output size in PDF points.
// If both are zero, the SVG dimensions are used as-is.
func (doc *Document) RenderPDF(opts RenderOptions) string {
	r := &renderer{
		doc:  doc,
		opts: opts,
	}
	return r.render()
}

// renderer holds state during PDF rendering.
type renderer struct {
	doc  *Document
	opts RenderOptions
	buf  strings.Builder
}

// defaultStyle returns the SVG default style (black fill, no stroke).
func defaultStyle() resolvedStyle {
	return resolvedStyle{
		fill:        Color{0, 0, 0, false}, // black
		fillSet:     true,
		stroke:      Color{IsNone: true},
		strokeWidth: 1,
		linecap:     0, // butt
		linejoin:    0, // miter
		fillRule:    "nonzero",
	}
}

// resolvedStyle holds computed style values for rendering.
type resolvedStyle struct {
	fill          Color
	fillSet       bool
	stroke        Color
	strokeSet     bool
	strokeWidth   float64
	fillOpacity   float64
	strokeOpacity float64
	opacity       float64
	linecap       int // 0=butt, 1=round, 2=square
	linejoin      int // 0=miter, 1=round, 2=bevel
	fillRule      string
}

func (r *renderer) render() string {
	width := r.opts.Width
	height := r.opts.Height
	if width == 0 {
		width = r.doc.Width
	}
	if height == 0 {
		height = r.doc.Height
	}

	vb := r.doc.ViewBox

	// Save graphics state
	r.emit("q")

	// Coordinate transform: SVG (Y-down) → Rule-local (Y-up, origin at top)
	//
	// The Rule rendering context places origin at the top-left of the node,
	// with Y increasing upward. SVG has origin at top-left with Y increasing
	// downward. So we flip Y without an upward offset:
	//   SVG (0, 0)          → local (0, 0)       = top of rule
	//   SVG (0, viewBoxH)   → local (0, -height)  = bottom of rule
	if vb.Width > 0 && vb.Height > 0 {
		sx := width / vb.Width
		sy := height / vb.Height
		m := Matrix{sx, 0, 0, -sy, -vb.MinX * sx, vb.MinY * sy}
		r.emit(m.PDFOperator())
	} else {
		// No viewBox: just flip Y
		m := Matrix{1, 0, 0, -1, 0, 0}
		r.emit(m.PDFOperator())
	}

	style := defaultStyle()
	for _, elem := range r.doc.Elements {
		r.renderElement(elem, style)
	}

	// Restore graphics state
	r.emit("Q")

	return r.buf.String()
}

func (r *renderer) renderElement(elem Element, inherited resolvedStyle) {
	switch e := elem.(type) {
	case Group:
		r.emit("q")
		if e.Transform != "" {
			m := ParseTransform(e.Transform)
			r.emit(m.PDFOperator())
		}
		style := mergeStyle(inherited, e.Style)
		for _, child := range e.Children {
			r.renderElement(child, style)
		}
		r.emit("Q")

	case Path:
		r.emit("q")
		if e.Transform != "" {
			m := ParseTransform(e.Transform)
			r.emit(m.PDFOperator())
		}
		style := mergeStyle(inherited, e.Style)
		r.applyStyle(style)
		r.renderPath(e.D, style)
		r.emit("Q")

	case Rect:
		r.emit("q")
		if e.Transform != "" {
			m := ParseTransform(e.Transform)
			r.emit(m.PDFOperator())
		}
		style := mergeStyle(inherited, e.Style)
		r.applyStyle(style)
		r.renderRect(e, style)
		r.emit("Q")

	case Circle:
		r.emit("q")
		if e.Transform != "" {
			m := ParseTransform(e.Transform)
			r.emit(m.PDFOperator())
		}
		style := mergeStyle(inherited, e.Style)
		r.applyStyle(style)
		r.renderEllipse(e.Cx, e.Cy, e.R, e.R, style)
		r.emit("Q")

	case Ellipse:
		r.emit("q")
		if e.Transform != "" {
			m := ParseTransform(e.Transform)
			r.emit(m.PDFOperator())
		}
		style := mergeStyle(inherited, e.Style)
		r.applyStyle(style)
		r.renderEllipse(e.Cx, e.Cy, e.Rx, e.Ry, style)
		r.emit("Q")

	case Line:
		r.emit("q")
		if e.Transform != "" {
			m := ParseTransform(e.Transform)
			r.emit(m.PDFOperator())
		}
		style := mergeStyle(inherited, e.Style)
		r.applyStyle(style)
		r.emitf("%s %s m %s %s l", fmtF(e.X1), fmtF(e.Y1), fmtF(e.X2), fmtF(e.Y2))
		r.emitPaintOp(style)
		r.emit("Q")

	case Polyline:
		r.emit("q")
		if e.Transform != "" {
			m := ParseTransform(e.Transform)
			r.emit(m.PDFOperator())
		}
		style := mergeStyle(inherited, e.Style)
		r.applyStyle(style)
		r.renderPolyPoints(e.Points, false)
		r.emitPaintOp(style)
		r.emit("Q")

	case Polygon:
		r.emit("q")
		if e.Transform != "" {
			m := ParseTransform(e.Transform)
			r.emit(m.PDFOperator())
		}
		style := mergeStyle(inherited, e.Style)
		r.applyStyle(style)
		r.renderPolyPoints(e.Points, true)
		r.emitPaintOp(style)
		r.emit("Q")

	case Text:
		if r.opts.TextRenderer == nil {
			return
		}
		r.emit("q")
		if e.Transform != "" {
			m := ParseTransform(e.Transform)
			r.emit(m.PDFOperator())
		}
		style := mergeStyle(inherited, e.Style)
		fillColor := style.fill
		if style.fill.IsNone {
			fillColor = Color{0, 0, 0, false}
		}
		fontFamily := e.FontFamily
		if fontFamily == "" {
			fontFamily = r.doc.FontFamily
		}
		s := r.opts.TextRenderer.RenderText(
			e.Content, e.X, e.Y, e.FontSize,
			fontFamily, e.FontWeight, e.FontStyle, e.TextAnchor,
			fillColor,
		)
		if s != "" {
			r.emit(s)
		}
		r.emit("Q")
	}
}

func (r *renderer) renderPath(d string, style resolvedStyle) {
	cmds, err := ParsePathData(d)
	if err != nil {
		return
	}

	cmds = ResolveToAbsolute(cmds)
	cmds = ExpandShorthands(cmds)

	var cx, cy float64 // current point for Q→C conversion

	for _, c := range cmds {
		switch c.Cmd {
		case 'M':
			r.emitf("%s %s m", fmtF(c.Args[0]), fmtF(c.Args[1]))
			cx, cy = c.Args[0], c.Args[1]
		case 'L':
			r.emitf("%s %s l", fmtF(c.Args[0]), fmtF(c.Args[1]))
			cx, cy = c.Args[0], c.Args[1]
		case 'H':
			r.emitf("%s %s l", fmtF(c.Args[0]), fmtF(cy))
			cx = c.Args[0]
		case 'V':
			r.emitf("%s %s l", fmtF(cx), fmtF(c.Args[0]))
			cy = c.Args[0]
		case 'C':
			r.emitf("%s %s %s %s %s %s c",
				fmtF(c.Args[0]), fmtF(c.Args[1]),
				fmtF(c.Args[2]), fmtF(c.Args[3]),
				fmtF(c.Args[4]), fmtF(c.Args[5]))
			cx, cy = c.Args[4], c.Args[5]
		case 'Q':
			// Convert quadratic to cubic for PDF
			cx1, cy1, cx2, cy2 := QuadToCubic(cx, cy, c.Args[0], c.Args[1], c.Args[2], c.Args[3])
			r.emitf("%s %s %s %s %s %s c",
				fmtF(cx1), fmtF(cy1), fmtF(cx2), fmtF(cy2),
				fmtF(c.Args[2]), fmtF(c.Args[3]))
			cx, cy = c.Args[2], c.Args[3]
		case 'A':
			curves := arcToCubic(cx, cy, c.Args[0], c.Args[1], c.Args[2], c.Args[3] != 0, c.Args[4] != 0, c.Args[5], c.Args[6])
			for _, crv := range curves {
				r.emitf("%s %s %s %s %s %s c",
					fmtF(crv[0]), fmtF(crv[1]),
					fmtF(crv[2]), fmtF(crv[3]),
					fmtF(crv[4]), fmtF(crv[5]))
			}
			cx, cy = c.Args[5], c.Args[6]
		case 'Z':
			r.emit("h")
		}
	}

	r.emitPaintOp(style)
}

func (r *renderer) renderRect(rect Rect, style resolvedStyle) {
	if rect.Rx > 0 || rect.Ry > 0 {
		// Rounded rectangle: build path with arcs
		rx := rect.Rx
		ry := rect.Ry
		if rx == 0 {
			rx = ry
		}
		if ry == 0 {
			ry = rx
		}
		// Clamp to half dimensions
		if rx > rect.Width/2 {
			rx = rect.Width / 2
		}
		if ry > rect.Height/2 {
			ry = rect.Height / 2
		}
		x, y, w, h := rect.X, rect.Y, rect.Width, rect.Height
		k := 0.5522847498 // Bézier kappa for quarter circle
		r.emitf("%s %s m", fmtF(x+rx), fmtF(y))
		r.emitf("%s %s l", fmtF(x+w-rx), fmtF(y))
		r.emitf("%s %s %s %s %s %s c", fmtF(x+w-rx+rx*k), fmtF(y), fmtF(x+w), fmtF(y+ry-ry*k), fmtF(x+w), fmtF(y+ry))
		r.emitf("%s %s l", fmtF(x+w), fmtF(y+h-ry))
		r.emitf("%s %s %s %s %s %s c", fmtF(x+w), fmtF(y+h-ry+ry*k), fmtF(x+w-rx+rx*k), fmtF(y+h), fmtF(x+w-rx), fmtF(y+h))
		r.emitf("%s %s l", fmtF(x+rx), fmtF(y+h))
		r.emitf("%s %s %s %s %s %s c", fmtF(x+rx-rx*k), fmtF(y+h), fmtF(x), fmtF(y+h-ry+ry*k), fmtF(x), fmtF(y+h-ry))
		r.emitf("%s %s l", fmtF(x), fmtF(y+ry))
		r.emitf("%s %s %s %s %s %s c", fmtF(x), fmtF(y+ry-ry*k), fmtF(x+rx-rx*k), fmtF(y), fmtF(x+rx), fmtF(y))
		r.emit("h")
	} else {
		r.emitf("%s %s %s %s re", fmtF(rect.X), fmtF(rect.Y), fmtF(rect.Width), fmtF(rect.Height))
	}
	r.emitPaintOp(style)
}

func (r *renderer) renderEllipse(cx, cy, rx, ry float64, style resolvedStyle) {
	// Approximate ellipse with 4 cubic Bézier curves
	k := 0.5522847498
	r.emitf("%s %s m", fmtF(cx+rx), fmtF(cy))
	r.emitf("%s %s %s %s %s %s c", fmtF(cx+rx), fmtF(cy+ry*k), fmtF(cx+rx*k), fmtF(cy+ry), fmtF(cx), fmtF(cy+ry))
	r.emitf("%s %s %s %s %s %s c", fmtF(cx-rx*k), fmtF(cy+ry), fmtF(cx-rx), fmtF(cy+ry*k), fmtF(cx-rx), fmtF(cy))
	r.emitf("%s %s %s %s %s %s c", fmtF(cx-rx), fmtF(cy-ry*k), fmtF(cx-rx*k), fmtF(cy-ry), fmtF(cx), fmtF(cy-ry))
	r.emitf("%s %s %s %s %s %s c", fmtF(cx+rx*k), fmtF(cy-ry), fmtF(cx+rx), fmtF(cy-ry*k), fmtF(cx+rx), fmtF(cy))
	r.emit("h")
	r.emitPaintOp(style)
}

func (r *renderer) renderPolyPoints(pts []Point, close bool) {
	if len(pts) == 0 {
		return
	}
	r.emitf("%s %s m", fmtF(pts[0].X), fmtF(pts[0].Y))
	for _, p := range pts[1:] {
		r.emitf("%s %s l", fmtF(p.X), fmtF(p.Y))
	}
	if close {
		r.emit("h")
	}
}

// applyStyle emits PDF operators for colors, line width, etc.
func (r *renderer) applyStyle(style resolvedStyle) {
	if !style.fill.IsNone {
		r.emit(style.fill.PDFNonstroking())
	}
	if !style.stroke.IsNone {
		r.emit(style.stroke.PDFStroking())
		r.emitf("%s w", fmtF(style.strokeWidth))
		r.emitf("%d J", style.linecap)
		r.emitf("%d j", style.linejoin)
	}
}

// emitPaintOp emits the appropriate PDF paint operator based on fill/stroke.
func (r *renderer) emitPaintOp(style resolvedStyle) {
	hasFill := !style.fill.IsNone
	hasStroke := !style.stroke.IsNone

	switch {
	case hasFill && hasStroke:
		if style.fillRule == "evenodd" {
			r.emit("B*")
		} else {
			r.emit("B")
		}
	case hasFill:
		if style.fillRule == "evenodd" {
			r.emit("f*")
		} else {
			r.emit("f")
		}
	case hasStroke:
		r.emit("S")
	default:
		r.emit("n")
	}
}

func (r *renderer) emit(s string) {
	if r.buf.Len() > 0 {
		r.buf.WriteByte('\n')
	}
	r.buf.WriteString(s)
}

func (r *renderer) emitf(format string, args ...any) {
	r.emit(fmt.Sprintf(format, args...))
}

// mergeStyle applies an element's style attributes on top of the inherited style.
func mergeStyle(parent resolvedStyle, attrs StyleAttrs) resolvedStyle {
	s := parent

	if attrs.Fill != "" {
		c, err := ParseColor(attrs.Fill)
		if err == nil {
			s.fill = c
			s.fillSet = true
		}
	}
	if attrs.Stroke != "" {
		c, err := ParseColor(attrs.Stroke)
		if err == nil {
			s.stroke = c
			s.strokeSet = true
		}
	}
	if attrs.StrokeWidth != "" {
		if w, err := strconv.ParseFloat(attrs.StrokeWidth, 64); err == nil {
			s.strokeWidth = w
		}
	}
	if attrs.FillOpacity != "" {
		if v, err := strconv.ParseFloat(attrs.FillOpacity, 64); err == nil {
			s.fillOpacity = v
		}
	}
	if attrs.StrokeOpacity != "" {
		if v, err := strconv.ParseFloat(attrs.StrokeOpacity, 64); err == nil {
			s.strokeOpacity = v
		}
	}
	if attrs.Opacity != "" {
		if v, err := strconv.ParseFloat(attrs.Opacity, 64); err == nil {
			s.opacity = v
		}
	}
	if attrs.StrokeLinecap != "" {
		switch attrs.StrokeLinecap {
		case "butt":
			s.linecap = 0
		case "round":
			s.linecap = 1
		case "square":
			s.linecap = 2
		}
	}
	if attrs.StrokeLinejoin != "" {
		switch attrs.StrokeLinejoin {
		case "miter":
			s.linejoin = 0
		case "round":
			s.linejoin = 1
		case "bevel":
			s.linejoin = 2
		}
	}
	if attrs.FillRule != "" {
		s.fillRule = attrs.FillRule
	}
	return s
}

// arcToCubic converts an SVG arc to a series of cubic Bézier curves.
// Returns a slice of [6]float64{cx1,cy1, cx2,cy2, x,y} for each curve segment.
func arcToCubic(x1, y1, rx, ry, phi float64, largeArc, sweep bool, x2, y2 float64) [][6]float64 {
	if rx == 0 || ry == 0 {
		return [][6]float64{{x1, y1, x2, y2, x2, y2}}
	}

	sinPhi := math.Sin(phi * math.Pi / 180)
	cosPhi := math.Cos(phi * math.Pi / 180)

	// Step 1: compute (x1', y1')
	dx := (x1 - x2) / 2
	dy := (y1 - y2) / 2
	x1p := cosPhi*dx + sinPhi*dy
	y1p := -sinPhi*dx + cosPhi*dy

	// Adjust radii
	rx = math.Abs(rx)
	ry = math.Abs(ry)
	lambda := (x1p*x1p)/(rx*rx) + (y1p*y1p)/(ry*ry)
	if lambda > 1 {
		s := math.Sqrt(lambda)
		rx *= s
		ry *= s
	}

	// Step 2: compute (cx', cy')
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	if den == 0 {
		return nil
	}
	sq := num / den
	if sq < 0 {
		sq = 0
	}
	sq = math.Sqrt(sq)
	if largeArc == sweep {
		sq = -sq
	}
	cxp := sq * rx * y1p / ry
	cyp := -sq * ry * x1p / rx

	// Step 3: compute (cx, cy) from (cx', cy')
	cx := cosPhi*cxp - sinPhi*cyp + (x1+x2)/2
	cy := sinPhi*cxp + cosPhi*cyp + (y1+y2)/2

	// Step 4: compute theta1 and dTheta
	theta1 := vectorAngle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dTheta := vectorAngle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)

	if !sweep && dTheta > 0 {
		dTheta -= 2 * math.Pi
	} else if sweep && dTheta < 0 {
		dTheta += 2 * math.Pi
	}

	// Split arc into segments of at most π/2
	segments := int(math.Ceil(math.Abs(dTheta) / (math.Pi / 2)))
	if segments == 0 {
		segments = 1
	}
	segAngle := dTheta / float64(segments)

	var curves [][6]float64
	for i := range segments {
		t1 := theta1 + float64(i)*segAngle
		t2 := t1 + segAngle
		curves = append(curves, arcSegmentToCubic(cx, cy, rx, ry, sinPhi, cosPhi, t1, t2))
	}
	return curves
}

func arcSegmentToCubic(cx, cy, rx, ry, sinPhi, cosPhi, t1, t2 float64) [6]float64 {
	alpha := math.Sin(t2-t1) * (math.Sqrt(4+3*math.Pow(math.Tan((t2-t1)/2), 2)) - 1) / 3

	sin1, cos1 := math.Sin(t1), math.Cos(t1)
	sin2, cos2 := math.Sin(t2), math.Cos(t2)

	// End point on the ellipse (before rotation)
	ex1 := rx * cos1
	ey1 := ry * sin1
	ex2 := rx * cos2
	ey2 := ry * sin2

	// Derivatives
	dx1 := -rx * sin1
	dy1 := ry * cos1
	dx2 := -rx * sin2
	dy2 := ry * cos2

	// Control points (before rotation)
	qx1 := ex1 + alpha*dx1
	qy1 := ey1 + alpha*dy1
	qx2 := ex2 - alpha*dx2
	qy2 := ey2 - alpha*dy2

	// Rotate and translate
	cp1x := cosPhi*qx1 - sinPhi*qy1 + cx
	cp1y := sinPhi*qx1 + cosPhi*qy1 + cy
	cp2x := cosPhi*qx2 - sinPhi*qy2 + cx
	cp2y := sinPhi*qx2 + cosPhi*qy2 + cy
	epx := cosPhi*ex2 - sinPhi*ey2 + cx
	epy := sinPhi*ex2 + cosPhi*ey2 + cy

	return [6]float64{cp1x, cp1y, cp2x, cp2y, epx, epy}
}

func vectorAngle(ux, uy, vx, vy float64) float64 {
	sign := 1.0
	if ux*vy-uy*vx < 0 {
		sign = -1
	}
	dot := ux*vx + uy*vy
	uLen := math.Sqrt(ux*ux + uy*uy)
	vLen := math.Sqrt(vx*vx + vy*vy)
	cos := dot / (uLen * vLen)
	// Clamp to [-1, 1] for numerical stability
	if cos < -1 {
		cos = -1
	}
	if cos > 1 {
		cos = 1
	}
	return sign * math.Acos(cos)
}
