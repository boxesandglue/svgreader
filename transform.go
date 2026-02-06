package svgreader

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Matrix is a 2D affine transformation matrix in the form:
//
//	[a c e]
//	[b d f]
//	[0 0 1]
//
// This matches both SVG's matrix(a,b,c,d,e,f) and PDF's a b c d e f cm.
type Matrix [6]float64

// Identity returns the identity matrix.
func Identity() Matrix {
	return Matrix{1, 0, 0, 1, 0, 0}
}

// Multiply returns the product m × n.
func (m Matrix) Multiply(n Matrix) Matrix {
	return Matrix{
		m[0]*n[0] + m[2]*n[1],
		m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3],
		m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4],
		m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

// TransformPoint applies the matrix to a point.
func (m Matrix) TransformPoint(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

// Translate returns a translation matrix.
func Translate(tx, ty float64) Matrix {
	return Matrix{1, 0, 0, 1, tx, ty}
}

// Scale returns a scaling matrix.
func Scale(sx, sy float64) Matrix {
	return Matrix{sx, 0, 0, sy, 0, 0}
}

// Rotate returns a rotation matrix for angle in degrees.
func Rotate(angleDeg float64) Matrix {
	rad := angleDeg * math.Pi / 180
	cos := math.Cos(rad)
	sin := math.Sin(rad)
	return Matrix{cos, sin, -sin, cos, 0, 0}
}

// RotateAround returns a rotation matrix around point (cx, cy).
func RotateAround(angleDeg, cx, cy float64) Matrix {
	return Translate(cx, cy).Multiply(Rotate(angleDeg)).Multiply(Translate(-cx, -cy))
}

// SkewX returns a skewX matrix.
func SkewX(angleDeg float64) Matrix {
	return Matrix{1, 0, math.Tan(angleDeg * math.Pi / 180), 1, 0, 0}
}

// SkewY returns a skewY matrix.
func SkewY(angleDeg float64) Matrix {
	return Matrix{1, math.Tan(angleDeg * math.Pi / 180), 0, 1, 0, 0}
}

// PDFOperator returns the PDF content stream operator for this matrix.
func (m Matrix) PDFOperator() string {
	return fmt.Sprintf("%s %s %s %s %s %s cm",
		fmtF(m[0]), fmtF(m[1]), fmtF(m[2]), fmtF(m[3]), fmtF(m[4]), fmtF(m[5]))
}

// ParseTransform parses an SVG transform attribute value into a combined matrix.
// Supported: matrix(), translate(), scale(), rotate(), skewX(), skewY().
// Multiple transforms are applied left to right (as per SVG spec).
func ParseTransform(s string) Matrix {
	m := Identity()
	s = strings.TrimSpace(s)
	if s == "" {
		return m
	}

	for len(s) > 0 {
		s = strings.TrimSpace(s)
		if s == "" {
			break
		}

		// Find function name
		idx := strings.IndexByte(s, '(')
		if idx < 0 {
			break
		}
		fname := strings.TrimSpace(s[:idx])
		s = s[idx+1:]

		// Find closing paren
		end := strings.IndexByte(s, ')')
		if end < 0 {
			break
		}
		argsStr := s[:end]
		s = s[end+1:]

		// Skip optional comma/whitespace between transforms
		s = strings.TrimLeft(s, ", \t\n\r")

		args := parseTransformArgs(argsStr)

		switch fname {
		case "matrix":
			if len(args) >= 6 {
				m = m.Multiply(Matrix{args[0], args[1], args[2], args[3], args[4], args[5]})
			}
		case "translate":
			tx := 0.0
			ty := 0.0
			if len(args) >= 1 {
				tx = args[0]
			}
			if len(args) >= 2 {
				ty = args[1]
			}
			m = m.Multiply(Translate(tx, ty))
		case "scale":
			sx := 1.0
			sy := 1.0
			if len(args) >= 1 {
				sx = args[0]
				sy = sx // uniform scale if only one arg
			}
			if len(args) >= 2 {
				sy = args[1]
			}
			m = m.Multiply(Scale(sx, sy))
		case "rotate":
			if len(args) >= 3 {
				m = m.Multiply(RotateAround(args[0], args[1], args[2]))
			} else if len(args) >= 1 {
				m = m.Multiply(Rotate(args[0]))
			}
		case "skewX":
			if len(args) >= 1 {
				m = m.Multiply(SkewX(args[0]))
			}
		case "skewY":
			if len(args) >= 1 {
				m = m.Multiply(SkewY(args[0]))
			}
		}
	}

	return m
}

func parseTransformArgs(s string) []float64 {
	s = strings.ReplaceAll(s, ",", " ")
	fields := strings.Fields(s)
	args := make([]float64, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.ParseFloat(f, 64)
		if err == nil {
			args = append(args, v)
		}
	}
	return args
}

// fmtF formats a float64 for PDF output: max 4 decimal places, trailing zeros stripped.
func fmtF(f float64) string {
	s := strconv.FormatFloat(f, 'f', 4, 64)
	if strings.ContainsRune(s, '.') {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	// Avoid "-0"
	if s == "-0" {
		s = "0"
	}
	return s
}
