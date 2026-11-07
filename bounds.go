package svgreader

import "math"

// bounds is a bounding box in user space. Unlike the bounding box of an SVG
// viewer it does not know about strokes, and it does not need to: gradients
// refer to the geometry alone.
type bounds struct {
	x0, y0, x1, y1 float64
	set            bool
}

func (b *bounds) add(x, y float64) {
	if !b.set {
		*b = bounds{x, y, x, y, true}
		return
	}
	b.x0, b.y0 = min(b.x0, x), min(b.y0, y)
	b.x1, b.y1 = max(b.x1, x), max(b.y1, y)
}

// addCubic adds the end point of a Bézier curve from the current point (x0, y0)
// and its extrema, which can lie well inside the control points.
func (b *bounds) addCubic(x0, y0 float64, c []float64) {
	b.add(c[4], c[5])
	for _, t := range cubicExtrema(x0, c[0], c[2], c[4]) {
		b.add(cubicAt(x0, c[0], c[2], c[4], t), cubicAt(y0, c[1], c[3], c[5], t))
	}
	for _, t := range cubicExtrema(y0, c[1], c[3], c[5]) {
		b.add(cubicAt(x0, c[0], c[2], c[4], t), cubicAt(y0, c[1], c[3], c[5], t))
	}
}

// empty reports whether the box has no area.
func (b bounds) empty() bool { return b.x1 <= b.x0 || b.y1 <= b.y0 }

// matrix takes the unit square to the box, which is what objectBoundingBox
// units mean.
func (b bounds) matrix() Matrix {
	return Matrix{b.x1 - b.x0, 0, 0, b.y1 - b.y0, b.x0, b.y0}
}

func cubicAt(p0, p1, p2, p3, t float64) float64 {
	s := 1 - t
	return s*s*s*p0 + 3*s*s*t*p1 + 3*s*t*t*p2 + t*t*t*p3
}

// cubicExtrema returns the parameters in (0, 1) where the derivative of the
// curve is zero.
func cubicExtrema(p0, p1, p2, p3 float64) []float64 {
	a := 3 * (-p0 + 3*p1 - 3*p2 + p3)
	b := 6 * (p0 - 2*p1 + p2)
	c := 3 * (p1 - p0)
	var roots []float64
	if math.Abs(a) < 1e-12 {
		if b != 0 {
			roots = append(roots, -c/b)
		}
	} else if d := b*b - 4*a*c; d >= 0 {
		d = math.Sqrt(d)
		roots = append(roots, (-b+d)/(2*a), (-b-d)/(2*a))
	}
	inside := roots[:0]
	for _, t := range roots {
		if t > 0 && t < 1 {
			inside = append(inside, t)
		}
	}
	return inside
}

// elementBounds returns the bounding box of a shape. There is none for a group,
// for text, whose extent only the text renderer knows, and for a shape without
// any points.
func elementBounds(elem Element) (bounds, bool) {
	var b bounds
	switch e := elem.(type) {
	case Path:
		ops, err := pathOperators(e.D)
		if err != nil {
			return b, false
		}
		var x, y float64
		for _, op := range ops {
			switch op.name {
			case 'm', 'l':
				x, y = op.args[0], op.args[1]
				b.add(x, y)
			case 'c':
				b.add(x, y)
				b.addCubic(x, y, op.args)
				x, y = op.args[4], op.args[5]
			}
		}
	case Rect:
		b.add(e.X, e.Y)
		b.add(e.X+e.Width, e.Y+e.Height)
	case Circle:
		b.add(e.Cx-e.R, e.Cy-e.R)
		b.add(e.Cx+e.R, e.Cy+e.R)
	case Ellipse:
		b.add(e.Cx-e.Rx, e.Cy-e.Ry)
		b.add(e.Cx+e.Rx, e.Cy+e.Ry)
	case Line:
		b.add(e.X1, e.Y1)
		b.add(e.X2, e.Y2)
	case Polyline:
		for _, p := range e.Points {
			b.add(p.X, p.Y)
		}
	case Polygon:
		for _, p := range e.Points {
			b.add(p.X, p.Y)
		}
	}
	return b, b.set
}
