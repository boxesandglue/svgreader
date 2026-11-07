package svgreader

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Color represents an SVG color with RGB components (0–1 range).
type Color struct {
	R, G, B float64
	IsNone  bool // true for "none" (transparent)
}

var colorNone = Color{IsNone: true}

// ParseColor parses an SVG color value. An alpha value in s is parsed and
// dropped, see parseColorAlpha.
// Supported formats: "none", "#rgb", "#rgba", "#rrggbb", "#rrggbbaa",
// "rgb(r,g,b)", "rgba(r,g,b,a)", "rgb(r g b / a)", "hsl(h,s,l)",
// "hsla(h,s,l,a)", "hsl(h s l / a)", named colors.
func ParseColor(s string) (Color, error) {
	c, _, err := parseColorAlpha(s)
	return c, err
}

// parseColorAlpha is ParseColor with the alpha value of the color, 1 for a
// color without one. Color has no field for it: its positional literals are
// part of the API, and only painting needs the value.
func parseColorAlpha(s string) (Color, float64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "none" || s == "transparent" {
		return colorNone, 1, nil
	}

	if s[0] == '#' {
		return parseHexColor(s)
	}

	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "rgb(") || strings.HasPrefix(lower, "rgba(") {
		return parseRGBFunc(lower)
	}
	if strings.HasPrefix(lower, "hsl(") || strings.HasPrefix(lower, "hsla(") {
		return parseHSLFunc(lower)
	}

	if c, ok := namedColors[lower]; ok {
		return c, 1, nil
	}

	return colorNone, 1, fmt.Errorf("svgreader: unsupported color %q", s)
}

func parseHexColor(s string) (Color, float64, error) {
	s = s[1:] // strip #

	// A short form doubles each digit.
	if len(s) == 3 || len(s) == 4 {
		var long strings.Builder
		for i := range len(s) {
			long.WriteByte(s[i])
			long.WriteByte(s[i])
		}
		s = long.String()
	}
	if len(s) != 6 && len(s) != 8 {
		return colorNone, 1, fmt.Errorf("svgreader: invalid hex color #%s", s)
	}
	r, _ := strconv.ParseUint(s[0:2], 16, 8)
	g, _ := strconv.ParseUint(s[2:4], 16, 8)
	b, _ := strconv.ParseUint(s[4:6], 16, 8)
	alpha := 1.0
	if len(s) == 8 {
		a, _ := strconv.ParseUint(s[6:8], 16, 8)
		alpha = float64(a) / 255
	}
	return Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255}, alpha, nil
}

// colorFuncArguments splits the commas and the spaces of a color function into
// its three components and the alpha value, 1 without one.
func colorFuncArguments(s string) ([]string, float64, error) {
	_, s, _ = strings.Cut(s, "(")
	s = strings.TrimSuffix(s, ")")
	s, alphaText, _ := strings.Cut(s, "/")
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	if len(parts) == 4 && alphaText == "" {
		alphaText = parts[3]
		parts = parts[:3]
	}
	if len(parts) != 3 {
		return nil, 1, fmt.Errorf("svgreader: invalid color function")
	}
	alpha := 1.0
	if alphaText != "" {
		a, ok := parseOpacity(alphaText)
		if !ok {
			return nil, 1, fmt.Errorf("svgreader: invalid alpha value %q", alphaText)
		}
		alpha = a
	}
	return parts, alpha, nil
}

// parseHue reads an angle with the units of CSS, a number means degrees.
func parseHue(s string) (float64, error) {
	scale := 1.0
	for _, unit := range []struct {
		suffix string
		scale  float64
	}{{"deg", 1}, {"grad", 0.9}, {"rad", 180 / math.Pi}, {"turn", 360}} {
		if bare, ok := strings.CutSuffix(s, unit.suffix); ok {
			s, scale = bare, unit.scale
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	v = math.Mod(v*scale, 360)
	if v < 0 {
		v += 360
	}
	return v, nil
}

// parseHSLFunc parses hsl() and hsla(), the two functions are aliases in CSS.
// See CSS Color 4 section 7.1 for the conversion.
func parseHSLFunc(s string) (Color, float64, error) {
	parts, alpha, err := colorFuncArguments(s)
	if err != nil {
		return colorNone, 1, err
	}
	hue, err := parseHue(parts[0])
	if err != nil {
		return colorNone, 1, err
	}
	saturation, err := strconv.ParseFloat(strings.TrimSuffix(parts[1], "%"), 64)
	if err != nil {
		return colorNone, 1, err
	}
	lightness, err := strconv.ParseFloat(strings.TrimSuffix(parts[2], "%"), 64)
	if err != nil {
		return colorNone, 1, err
	}
	saturation = max(0, min(saturation/100, 1))
	lightness = max(0, min(lightness/100, 1))
	channel := func(n float64) float64 {
		k := math.Mod(n+hue/30, 12)
		a := saturation * min(lightness, 1-lightness)
		return lightness - a*max(-1, min(k-3, 9-k, 1))
	}
	return Color{R: channel(0), G: channel(8), B: channel(4)}, alpha, nil
}

// parseRGBFunc parses rgb() and rgba(), the two functions are aliases in CSS.
func parseRGBFunc(s string) (Color, float64, error) {
	parts, alpha, err := colorFuncArguments(s)
	if err != nil {
		return colorNone, 1, err
	}

	vals := [3]float64{}
	for i, p := range parts {
		if strings.HasSuffix(p, "%") {
			v, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
			if err != nil {
				return colorNone, 1, err
			}
			vals[i] = v / 100
		} else {
			v, err := strconv.ParseFloat(p, 64)
			if err != nil {
				return colorNone, 1, err
			}
			vals[i] = v / 255
		}
	}
	return Color{R: vals[0], G: vals[1], B: vals[2]}, alpha, nil
}

// parseOpacity parses a number or a percentage and clamps it to 0..1, as CSS
// does for an alpha value.
func parseOpacity(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	scale := 1.0
	if strings.HasSuffix(s, "%") {
		s = strings.TrimSuffix(s, "%")
		scale = 100
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return max(0, min(1, v/scale)), true
}

// PDFStroking returns the PDF operator to set this color as the stroking color.
func (c Color) PDFStroking() string {
	if c.IsNone {
		return ""
	}
	return fmt.Sprintf("%s %s %s RG", fmtF(c.R), fmtF(c.G), fmtF(c.B))
}

// PDFNonstroking returns the PDF operator to set this color as the fill color.
func (c Color) PDFNonstroking() string {
	if c.IsNone {
		return ""
	}
	return fmt.Sprintf("%s %s %s rg", fmtF(c.R), fmtF(c.G), fmtF(c.B))
}

// SVG named colors (CSS Color Level 3).
var namedColors = map[string]Color{
	"black":        {0, 0, 0, false},
	"white":        {1, 1, 1, false},
	"red":          {1, 0, 0, false},
	"green":        {0, 128.0 / 255, 0, false},
	"blue":         {0, 0, 1, false},
	"yellow":       {1, 1, 0, false},
	"cyan":         {0, 1, 1, false},
	"magenta":      {1, 0, 1, false},
	"orange":       {1, 165.0 / 255, 0, false},
	"purple":       {128.0 / 255, 0, 128.0 / 255, false},
	"gray":         {128.0 / 255, 128.0 / 255, 128.0 / 255, false},
	"grey":         {128.0 / 255, 128.0 / 255, 128.0 / 255, false},
	"silver":       {192.0 / 255, 192.0 / 255, 192.0 / 255, false},
	"maroon":       {128.0 / 255, 0, 0, false},
	"olive":        {128.0 / 255, 128.0 / 255, 0, false},
	"lime":         {0, 1, 0, false},
	"aqua":         {0, 1, 1, false},
	"teal":         {0, 128.0 / 255, 128.0 / 255, false},
	"navy":         {0, 0, 128.0 / 255, false},
	"fuchsia":      {1, 0, 1, false},
	"darkred":      {139.0 / 255, 0, 0, false},
	"darkgreen":    {0, 100.0 / 255, 0, false},
	"darkblue":     {0, 0, 139.0 / 255, false},
	"lightgray":    {211.0 / 255, 211.0 / 255, 211.0 / 255, false},
	"lightgrey":    {211.0 / 255, 211.0 / 255, 211.0 / 255, false},
	"dimgray":      {105.0 / 255, 105.0 / 255, 105.0 / 255, false},
	"dimgrey":      {105.0 / 255, 105.0 / 255, 105.0 / 255, false},
	"darkgray":     {169.0 / 255, 169.0 / 255, 169.0 / 255, false},
	"darkgrey":     {169.0 / 255, 169.0 / 255, 169.0 / 255, false},
	"brown":        {165.0 / 255, 42.0 / 255, 42.0 / 255, false},
	"crimson":      {220.0 / 255, 20.0 / 255, 60.0 / 255, false},
	"coral":        {1, 127.0 / 255, 80.0 / 255, false},
	"gold":         {1, 215.0 / 255, 0, false},
	"indigo":       {75.0 / 255, 0, 130.0 / 255, false},
	"ivory":        {1, 1, 240.0 / 255, false},
	"khaki":        {240.0 / 255, 230.0 / 255, 140.0 / 255, false},
	"lavender":     {230.0 / 255, 230.0 / 255, 250.0 / 255, false},
	"linen":        {250.0 / 255, 240.0 / 255, 230.0 / 255, false},
	"pink":         {1, 192.0 / 255, 203.0 / 255, false},
	"plum":         {221.0 / 255, 160.0 / 255, 221.0 / 255, false},
	"salmon":       {250.0 / 255, 128.0 / 255, 114.0 / 255, false},
	"sienna":       {160.0 / 255, 82.0 / 255, 45.0 / 255, false},
	"tan":          {210.0 / 255, 180.0 / 255, 140.0 / 255, false},
	"tomato":       {1, 99.0 / 255, 71.0 / 255, false},
	"turquoise":    {64.0 / 255, 224.0 / 255, 208.0 / 255, false},
	"violet":       {238.0 / 255, 130.0 / 255, 238.0 / 255, false},
	"wheat":        {245.0 / 255, 222.0 / 255, 179.0 / 255, false},
	"currentcolor": {0, 0, 0, false}, // default
}
