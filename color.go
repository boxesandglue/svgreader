package svgreader

import (
	"fmt"
	"strconv"
	"strings"
)

// Color represents an SVG color with RGB components (0–1 range).
type Color struct {
	R, G, B float64
	IsNone  bool // true for "none" (transparent)
}

var colorNone = Color{IsNone: true}

// ParseColor parses an SVG color value.
// Supported formats: "none", "#rgb", "#rrggbb", "rgb(r,g,b)", named colors.
func ParseColor(s string) (Color, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "none" || s == "transparent" {
		return colorNone, nil
	}

	if s[0] == '#' {
		return parseHexColor(s)
	}

	if strings.HasPrefix(s, "rgb(") || strings.HasPrefix(s, "RGB(") {
		return parseRGBFunc(s)
	}

	if c, ok := namedColors[strings.ToLower(s)]; ok {
		return c, nil
	}

	return colorNone, fmt.Errorf("svgreader: unsupported color %q", s)
}

func parseHexColor(s string) (Color, error) {
	s = s[1:] // strip #

	switch len(s) {
	case 3:
		r, _ := strconv.ParseUint(string(s[0])+string(s[0]), 16, 8)
		g, _ := strconv.ParseUint(string(s[1])+string(s[1]), 16, 8)
		b, _ := strconv.ParseUint(string(s[2])+string(s[2]), 16, 8)
		return Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255}, nil
	case 6:
		r, _ := strconv.ParseUint(s[0:2], 16, 8)
		g, _ := strconv.ParseUint(s[2:4], 16, 8)
		b, _ := strconv.ParseUint(s[4:6], 16, 8)
		return Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255}, nil
	default:
		return colorNone, fmt.Errorf("svgreader: invalid hex color #%s", s)
	}
}

func parseRGBFunc(s string) (Color, error) {
	s = strings.TrimPrefix(s, "rgb(")
	s = strings.TrimPrefix(s, "RGB(")
	s = strings.TrimSuffix(s, ")")
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return colorNone, fmt.Errorf("svgreader: invalid rgb() color")
	}

	vals := [3]float64{}
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasSuffix(p, "%") {
			v, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
			if err != nil {
				return colorNone, err
			}
			vals[i] = v / 100
		} else {
			v, err := strconv.ParseFloat(p, 64)
			if err != nil {
				return colorNone, err
			}
			vals[i] = v / 255
		}
	}
	return Color{R: vals[0], G: vals[1], B: vals[2]}, nil
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
