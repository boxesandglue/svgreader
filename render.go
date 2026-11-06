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

// ShadingRegistrar is implemented by callers (e.g. boxesandglue) to make a
// gradient available as a PDF Pattern resource. RegisterShading is invoked
// once per gradient fill encountered during rendering; it returns the
// resource name (without leading slash) that the renderer then writes as
// "/<name> scn" into the content stream. The same gradient may be
// registered multiple times — implementations may dedupe, but the renderer
// does not require it.
type ShadingRegistrar interface {
	RegisterShading(req ShadingRequest) string
}

// ShadingRequest is the data the registrar needs to materialise a PDF
// shading-pattern object. CTM is the renderer's current transformation
// matrix at the point of fill; gradients use it together with their own
// GradientTransform to build the pattern-to-page mapping.
type ShadingRequest struct {
	Gradient *LinearGradient
	CTM      Matrix
}

// OpacityRegistrar is implemented by callers to make transparency available.
// A constant alpha is a graphics state, and the opacity of a group needs a
// Form XObject with a transparency group, which the renderer cannot write on
// its own. All names come without a leading slash.
//
// BeginGroup and EndGroup nest. What RegisterAlpha and a ShadingRegistrar get
// between them belongs to the resources of that group. In a group the CTM of a
// ShadingRequest is relative to the group: a pattern matrix maps to the
// coordinates of the form that uses the pattern.
type OpacityRegistrar interface {
	// RegisterAlpha returns the name of a graphics state with these alpha
	// values for filling and stroking, both in 0..1. An empty name paints
	// opaque.
	RegisterAlpha(fill, stroke float64) string
	// BeginGroup starts a group. On false there is no EndGroup and the
	// content is painted without the group, which is what a host does for a
	// format that forbids transparency.
	BeginGroup() bool
	// EndGroup ends a group and returns the name of the form for its
	// content. bbox is lower left x, lower left y, upper right x, upper
	// right y in the coordinates of the content.
	EndGroup(content string, bbox [4]float64) string
}

// RenderOptions controls PDF rendering.
type RenderOptions struct {
	// Width and Height specify the output size in PDF points.
	// If zero, the SVG's own dimensions are used (1 SVG user unit = 1 PDF point).
	Width, Height float64

	// TextRenderer handles <text> elements. If nil, text is skipped.
	TextRenderer TextRenderer

	// ShadingRegistrar, if non-nil, receives gradient fills and returns the
	// page-resource name the renderer should use in the content stream.
	// When nil, gradient fills degrade to the first stop's flat color (or
	// to "no fill" when the gradient has no stops).
	ShadingRegistrar ShadingRegistrar

	// OpacityRegistrar, if non-nil, receives the alpha values and the groups
	// of the drawing. When nil, everything is painted opaque.
	OpacityRegistrar OpacityRegistrar
}

// RenderPDF renders the SVG document as a PDF content stream.
// The width and height specify the desired output size in PDF points.
// If both are zero, the SVG dimensions are used as-is.
func (doc *Document) RenderPDF(opts RenderOptions) string {
	r := &renderer{
		doc:         doc,
		opts:        opts,
		buf:         &strings.Builder{},
		formInverse: Identity(),
		elemOpacity: 1,
	}
	return r.render()
}

// renderer holds state during PDF rendering.
type renderer struct {
	doc  *Document
	opts RenderOptions
	buf  *strings.Builder
	// ctmStack mirrors the PDF graphics state stack. Each pushQ() copies
	// the top onto the stack; popQ() drops the top. The top is always
	// the current CTM. Needed because PDF Pattern matrices map to the
	// default coordinate space (page coords), so a gradient fill applied
	// to a path with a nested transform must bake the surrounding CTM
	// into the pattern matrix.
	ctmStack []Matrix
	// formInverse takes a CTM to the coordinates of the current form.
	formInverse Matrix
	// viewport is the visible area in the coordinates of the root: the
	// BBox of a form must not cut off anything that can be seen.
	viewport [4]float64
	// elemOpacity is the opacity of the current shape if a constant alpha
	// can stand in for it.
	elemOpacity float64
}

// pushQ saves the graphics state and duplicates the current CTM onto the
// stack so subsequent transforms accumulate into a new top.
func (r *renderer) pushQ() {
	r.emit("q")
	var top Matrix
	if n := len(r.ctmStack); n > 0 {
		top = r.ctmStack[n-1]
	} else {
		top = Identity()
	}
	r.ctmStack = append(r.ctmStack, top)
}

// popQ restores the graphics state and drops the matching CTM frame.
func (r *renderer) popQ() {
	r.emit("Q")
	if n := len(r.ctmStack); n > 0 {
		r.ctmStack = r.ctmStack[:n-1]
	}
}

// applyTransform emits a cm operator and folds the transform into the
// current CTM frame. SVG-spec composition order: child = parent × local.
func (r *renderer) applyTransform(m Matrix) {
	r.emit(m.PDFOperator())
	if n := len(r.ctmStack); n > 0 {
		r.ctmStack[n-1] = r.ctmStack[n-1].Multiply(m)
	}
}

// currentCTM returns the active CTM (identity if the stack is empty).
func (r *renderer) currentCTM() Matrix {
	if n := len(r.ctmStack); n > 0 {
		return r.ctmStack[n-1]
	}
	return Identity()
}

// defaultStyle returns the SVG default style (black fill, no stroke).
func defaultStyle() resolvedStyle {
	return resolvedStyle{
		fill:             Color{0, 0, 0, false}, // black
		fillSet:          true,
		stroke:           Color{IsNone: true},
		strokeWidth:      1,
		fillOpacity:      1,
		strokeOpacity:    1,
		fillColorAlpha:   1,
		strokeColorAlpha: 1,
		opacity:          1,
		linecap:          0, // butt
		linejoin:         0, // miter
		fillRule:         "nonzero",
	}
}

// resolvedStyle holds computed style values for rendering.
type resolvedStyle struct {
	fill          Color
	fillSet       bool
	fillGradient  *LinearGradient // set when fill="url(#id)" resolves
	stroke        Color
	strokeSet     bool
	strokeWidth   float64
	fillOpacity   float64
	strokeOpacity float64
	// fillColorAlpha and strokeColorAlpha are the alpha values of the paints
	// themselves, as in rgba(). They go with the paint: a new fill resets
	// the first one, while fill-opacity stays.
	fillColorAlpha   float64
	strokeColorAlpha float64
	opacity          float64 // not inherited, see mergeStyle
	linecap          int     // 0=butt, 1=round, 2=square
	linejoin         int     // 0=miter, 1=round, 2=bevel
	dashArray        []float64
	dashOffset       float64
	fillRule         string
}

// hasFillPaint reports whether the style produces any fill (flat color or
// gradient). Drives both applyStyle (which operator to emit) and
// emitPaintOp (whether the path should be filled at all).
//
// fill-opacity=0 makes the element invisible: a common SVG idiom for
// hit-target paths under an opaque graphic. Treating it as no-fill here
// keeps such paths out of the PDF, with or without an OpacityRegistrar.
func (s resolvedStyle) hasFillPaint() bool {
	if s.fillAlpha() == 0 {
		return false
	}
	return s.fillGradient != nil || !s.fill.IsNone
}

// hasStrokePaint is hasFillPaint for the stroke.
func (s resolvedStyle) hasStrokePaint() bool {
	return s.strokeAlpha() != 0 && !s.stroke.IsNone
}

func (s resolvedStyle) fillAlpha() float64 { return s.fillOpacity * s.fillColorAlpha }

func (s resolvedStyle) strokeAlpha() float64 { return s.strokeOpacity * s.strokeColorAlpha }

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

	// An SVG without any size is drawn unclipped.
	r.viewport = [4]float64{-16383, -16383, 16383, 16383}
	if width > 0 && height > 0 {
		r.viewport = [4]float64{0, -height, width, 0}
	}

	// Save graphics state
	r.pushQ()

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
		r.applyTransform(Matrix{sx, 0, 0, -sy, -vb.MinX * sx, vb.MinY * sy})
	} else {
		// No viewBox: just flip Y
		r.applyTransform(Matrix{1, 0, 0, -1, 0, 0})
	}

	style := defaultStyle()
	for _, elem := range r.doc.Elements {
		r.renderElement(elem, style)
	}

	// Restore graphics state
	r.popQ()

	return r.buf.String()
}

// elementStyle returns the style attributes of elem.
func elementStyle(elem Element) StyleAttrs {
	switch e := elem.(type) {
	case Group:
		return e.Style
	case Path:
		return e.Style
	case Rect:
		return e.Style
	case Circle:
		return e.Style
	case Ellipse:
		return e.Style
	case Line:
		return e.Style
	case Polyline:
		return e.Style
	case Polygon:
		return e.Style
	case Text:
		return e.Style
	}
	return StyleAttrs{}
}

// renderElement draws elem with its opacity. Opacity applies to an element as
// a whole. For a single paint a constant alpha gives that result, but children
// that overlap, or a stroke that overlaps its fill, have to be composited first
// and need a group.
func (r *renderer) renderElement(elem Element, inherited resolvedStyle) {
	style := mergeStyle(inherited, elementStyle(elem), r.doc.Defs)
	if style.opacity == 0 {
		return
	}
	if style.opacity == 1 {
		r.drawElement(elem, inherited)
		return
	}
	_, isGroup := elem.(Group)
	if isGroup || (style.hasFillPaint() && style.hasStrokePaint()) {
		r.drawGroup(style.opacity, func() { r.drawElement(elem, inherited) })
		return
	}
	r.elemOpacity = style.opacity
	r.drawElement(elem, inherited)
	r.elemOpacity = 1
}

// drawGroup paints what draw writes as one object with the given opacity.
func (r *renderer) drawGroup(opacity float64, draw func()) {
	reg := r.opts.OpacityRegistrar
	if reg == nil {
		draw()
		return
	}
	ctm := r.currentCTM()
	inverse, ok := ctm.Inverse()
	if !ok {
		return // a transformation to nothing
	}
	if !reg.BeginGroup() {
		draw()
		return
	}
	outerBuf, outerInverse := r.buf, r.formInverse
	r.buf, r.formInverse = &strings.Builder{}, inverse
	draw()
	content := r.buf.String()
	r.buf, r.formInverse = outerBuf, outerInverse

	name := reg.EndGroup(content, r.viewportIn(inverse))
	if name == "" {
		return
	}
	r.pushQ()
	r.applyAlpha(opacity, 1)
	r.emitf("/%s Do", name)
	r.popQ()
}

// viewportIn returns the bounding box of the viewport after inverse.
func (r *renderer) viewportIn(inverse Matrix) [4]float64 {
	x0, y0 := inverse.TransformPoint(r.viewport[0], r.viewport[1])
	box := [4]float64{x0, y0, x0, y0}
	for _, corner := range [][2]float64{
		{r.viewport[2], r.viewport[1]},
		{r.viewport[2], r.viewport[3]},
		{r.viewport[0], r.viewport[3]},
	} {
		x, y := inverse.TransformPoint(corner[0], corner[1])
		box[0], box[1] = min(box[0], x), min(box[1], y)
		box[2], box[3] = max(box[2], x), max(box[3], y)
	}
	return box
}

// applyAlpha sets the alpha values for what is painted until the next Q.
func (r *renderer) applyAlpha(fill, stroke float64) {
	reg := r.opts.OpacityRegistrar
	if reg == nil || (fill == 1 && stroke == 1) {
		return
	}
	if name := reg.RegisterAlpha(fill, stroke); name != "" {
		r.emitf("/%s gs", name)
	}
}

func (r *renderer) drawElement(elem Element, inherited resolvedStyle) {
	switch e := elem.(type) {
	case Group:
		r.pushQ()
		if e.Transform != "" {
			r.applyTransform(ParseTransform(e.Transform))
		}
		style := mergeStyle(inherited, e.Style, r.doc.Defs)
		for _, child := range e.Children {
			r.renderElement(child, style)
		}
		r.popQ()

	case Path:
		r.pushQ()
		if e.Transform != "" {
			r.applyTransform(ParseTransform(e.Transform))
		}
		style := mergeStyle(inherited, e.Style, r.doc.Defs)
		r.applyStyle(style)
		r.renderPath(e.D, style)
		r.popQ()

	case Rect:
		r.pushQ()
		if e.Transform != "" {
			r.applyTransform(ParseTransform(e.Transform))
		}
		style := mergeStyle(inherited, e.Style, r.doc.Defs)
		r.applyStyle(style)
		r.renderRect(e, style)
		r.popQ()

	case Circle:
		r.pushQ()
		if e.Transform != "" {
			r.applyTransform(ParseTransform(e.Transform))
		}
		style := mergeStyle(inherited, e.Style, r.doc.Defs)
		r.applyStyle(style)
		r.renderEllipse(e.Cx, e.Cy, e.R, e.R, style)
		r.popQ()

	case Ellipse:
		r.pushQ()
		if e.Transform != "" {
			r.applyTransform(ParseTransform(e.Transform))
		}
		style := mergeStyle(inherited, e.Style, r.doc.Defs)
		r.applyStyle(style)
		r.renderEllipse(e.Cx, e.Cy, e.Rx, e.Ry, style)
		r.popQ()

	case Line:
		r.pushQ()
		if e.Transform != "" {
			r.applyTransform(ParseTransform(e.Transform))
		}
		style := mergeStyle(inherited, e.Style, r.doc.Defs)
		r.applyStyle(style)
		r.emitf("%s %s m %s %s l", fmtF(e.X1), fmtF(e.Y1), fmtF(e.X2), fmtF(e.Y2))
		r.emitPaintOp(style)
		r.popQ()

	case Polyline:
		r.pushQ()
		if e.Transform != "" {
			r.applyTransform(ParseTransform(e.Transform))
		}
		style := mergeStyle(inherited, e.Style, r.doc.Defs)
		r.applyStyle(style)
		r.renderPolyPoints(e.Points, false)
		r.emitPaintOp(style)
		r.popQ()

	case Polygon:
		r.pushQ()
		if e.Transform != "" {
			r.applyTransform(ParseTransform(e.Transform))
		}
		style := mergeStyle(inherited, e.Style, r.doc.Defs)
		r.applyStyle(style)
		r.renderPolyPoints(e.Points, true)
		r.emitPaintOp(style)
		r.popQ()

	case Text:
		if r.opts.TextRenderer == nil {
			return
		}
		r.pushQ()
		if e.Transform != "" {
			r.applyTransform(ParseTransform(e.Transform))
		}
		style := mergeStyle(inherited, e.Style, r.doc.Defs)
		fillColor := style.fill
		if style.fill.IsNone {
			fillColor = Color{0, 0, 0, false}
		}
		alpha := style.fillAlpha() * r.elemOpacity
		if alpha == 0 {
			r.popQ()
			return
		}
		r.applyAlpha(alpha, 1)
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
		r.popQ()
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
	fillAlpha, strokeAlpha := 1.0, 1.0
	if style.hasFillPaint() {
		fillAlpha = style.fillAlpha() * r.elemOpacity
	}
	if style.hasStrokePaint() {
		strokeAlpha = style.strokeAlpha() * r.elemOpacity
	}
	r.applyAlpha(fillAlpha, strokeAlpha)
	if style.hasFillPaint() {
		switch {
		case style.fillGradient != nil:
			// Pattern-fill path: register the gradient with the host
			// (which allocates an indirect PDF object + page-resource
			// name) and switch into the Pattern color space.
			// /<name> scn binds the pattern as the non-stroking color;
			// subsequent f / f* operate against it just like a flat
			// color.
			emitted := false
			if reg := r.opts.ShadingRegistrar; reg != nil {
				name := reg.RegisterShading(ShadingRequest{
					Gradient: style.fillGradient,
					CTM:      r.formInverse.Multiply(r.currentCTM()),
				})
				if name != "" {
					r.emit("/Pattern cs")
					r.emitf("/%s scn", name)
					emitted = true
				}
			}
			// No registrar (or it declined) → degrade to first stop's
			// color. Better than dropping the fill silently; preserves
			// the original behaviour for callers that don't care about
			// gradients.
			if !emitted && len(style.fillGradient.Stops) > 0 {
				r.emit(style.fillGradient.Stops[0].Color.PDFNonstroking())
			}
		case !style.fill.IsNone:
			r.emit(style.fill.PDFNonstroking())
		}
	}
	if style.hasStrokePaint() {
		r.emit(style.stroke.PDFStroking())
		r.emitf("%s w", fmtF(style.strokeWidth))
		r.emitf("%d J", style.linecap)
		r.emitf("%d j", style.linejoin)
		if len(style.dashArray) > 0 {
			parts := make([]string, 0, len(style.dashArray))
			for _, v := range style.dashArray {
				parts = append(parts, fmtF(v))
			}
			r.emitf("[%s] %s d", strings.Join(parts, " "), fmtF(style.dashOffset))
		} else {
			r.emit("[] 0 d")
		}
	}
}

// emitPaintOp emits the appropriate PDF paint operator based on fill/stroke.
func (r *renderer) emitPaintOp(style resolvedStyle) {
	hasFill := style.hasFillPaint()
	hasStroke := style.hasStrokePaint()

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
func mergeStyle(parent resolvedStyle, attrs StyleAttrs, defs map[string]*LinearGradient) resolvedStyle {
	s := parent
	// Unlike fill-opacity, opacity is not inherited.
	s.opacity = 1

	if attrs.Fill != "" {
		paint := resolvePaint(attrs.Fill)
		switch paint.Kind {
		case PaintGradient:
			if g, ok := defs[paint.GradientRef]; ok {
				s.fillGradient = g
				// Clear any inherited flat fill so applyStyle goes through
				// the gradient branch.
				s.fill = Color{IsNone: true}
				s.fillColorAlpha = 1
				s.fillSet = true
			}
			// Unresolved gradient ref: keep the inherited fill rather than
			// silently flipping to black — matches Firefox behaviour for
			// dangling url() refs.
		case PaintColor:
			s.fill = paint.Color
			s.fillColorAlpha = paint.Alpha
			s.fillGradient = nil
			s.fillSet = true
		case PaintNone:
			s.fill = Color{IsNone: true}
			s.fillColorAlpha = 1
			s.fillGradient = nil
			s.fillSet = true
		}
	}
	if attrs.Stroke != "" {
		c, alpha, err := parseColorAlpha(attrs.Stroke)
		if err == nil {
			s.stroke = c
			s.strokeColorAlpha = alpha
			s.strokeSet = true
		}
	}
	if attrs.StrokeWidth != "" {
		if w, err := strconv.ParseFloat(attrs.StrokeWidth, 64); err == nil {
			s.strokeWidth = w
		}
	}
	if v, ok := parseOpacity(attrs.FillOpacity); ok {
		s.fillOpacity = v
	}
	if v, ok := parseOpacity(attrs.StrokeOpacity); ok {
		s.strokeOpacity = v
	}
	if v, ok := parseOpacity(attrs.Opacity); ok {
		s.opacity = v
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
	if attrs.StrokeDasharray != "" {
		s.dashArray = parseDashArray(attrs.StrokeDasharray)
		s.dashOffset = 0
	}
	if attrs.StrokeDashoffset != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(attrs.StrokeDashoffset), 64); err == nil {
			s.dashOffset = v
		}
	}
	if attrs.FillRule != "" {
		s.fillRule = attrs.FillRule
	}
	return s
}

// parseDashArray parses an SVG stroke-dasharray attribute into a float64 slice
// suitable for the PDF d operator. Returns nil for "none", empty input, or any
// unparseable / negative value (meaning: solid stroke per SVG spec). Odd-length
// arrays are doubled (SVG 1.1 §11.4).
func parseDashArray(s string) []float64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "none") {
		return nil
	}
	s = strings.ReplaceAll(s, ",", " ")
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	vals := make([]float64, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil || v < 0 {
			return nil
		}
		vals = append(vals, v)
	}
	if len(vals)%2 != 0 {
		vals = append(vals, vals...)
	}
	return vals
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
