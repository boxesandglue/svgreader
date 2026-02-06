# svgreader

A pure Go SVG-to-PDF renderer. Parses SVG documents and produces PDF content streams — no external tools (like Inkscape or librsvg) needed.

> **Status:** Beta. The library is actively used in [boxes and glue](https://github.com/boxesandglue/boxesandglue) for embedding SVG graphics in PDF documents. The API may still change.

## Features

- **Basic shapes**: `<rect>`, `<circle>`, `<ellipse>`, `<line>`, `<polyline>`, `<polygon>` (including rounded corners)
- **Paths**: All SVG path commands (M, L, H, V, C, S, Q, T, A, Z), with arc-to-cubic conversion
- **Groups**: `<g>` with style inheritance
- **Transforms**: `translate()`, `rotate()`, `scale()`, `matrix()`, `skewX()`, `skewY()`
- **Styling**: `fill`, `stroke`, `stroke-width`, `stroke-linecap`, `stroke-linejoin`, `fill-rule`, `opacity`; inline CSS via the `style` attribute
- **Colors**: Hex (`#rgb`, `#rrggbb`), `rgb()` function, 148 named CSS colors
- **Text**: `<text>` elements via a pluggable `TextRenderer` interface for external font shaping
- **Aspect ratio**: Automatic scaling from viewBox to output size, with aspect-ratio preservation

## Installation

```bash
go get github.com/boxesandglue/svgreader
```

## Example

```go
package main

import (
    "fmt"
    "strings"

    "github.com/boxesandglue/svgreader"
)

const mySVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100">
  <rect x="10" y="10" width="180" height="80" rx="8"
        fill="#eef" stroke="#336" stroke-width="2"/>
  <circle cx="100" cy="50" r="30" fill="none" stroke="red"/>
</svg>`

func main() {
    doc, err := svgreader.Parse(strings.NewReader(mySVG))
    if err != nil {
        panic(err)
    }

    fmt.Printf("SVG size: %.0f x %.0f\n", doc.Width, doc.Height)

    stream := doc.RenderPDF(svgreader.RenderOptions{
        Width:  200,
        Height: 100,
    })
    fmt.Println(stream)
}
```

### Using with boxes and glue

When used inside [boxes and glue](https://github.com/boxesandglue/boxesandglue), SVG nodes integrate directly into the page layout:

```go
svgDoc, _ := svgreader.Parse(file)
svgNode := f.Doc.CreateSVGNodeFromDocument(svgDoc, bag.MustSP("80mm"), 0)
p.OutputAt(bag.MustSP("2cm"), bag.MustSP("26cm"), node.Vpack(svgNode))
```

For SVGs with `<text>` elements, pass an `SVGTextRenderer` to enable HarfBuzz-based font shaping:

```go
tr := frontend.NewSVGTextRenderer(f)
tr.DefaultFamily = myFontFamily
svgNode := f.Doc.CreateSVGNodeFromDocument(svgDoc, bag.MustSP("80mm"), 0, tr)
```

## Documentation

- [SVG graphics (Go API)](https://boxesandglue.dev/goapi/frontend/svg/) — Using svgreader from Go
- [SVG graphics (Lua CLI)](https://boxesandglue.dev/cli/#svg-graphics) — Using svgreader from glu

## Text rendering

SVG `<text>` elements require a `TextRenderer` implementation to produce PDF text operators. Without one, text elements are silently skipped.

```go
type TextRenderer interface {
    RenderText(text string, x, y, fontSize float64,
        fontFamily, fontWeight, fontStyle string,
        fill Color) string
}
```

The `RenderText` method receives position, font properties, and fill color. It returns PDF operators (e.g. `BT ... ET`) that are inserted into the content stream.

## Package structure

```
svg.go        SVG parser (Document, Element types, Parse)
pathdata.go   SVG path data parser (M, L, C, A, ... commands)
render.go     PDF content stream renderer
color.go      SVG color parser (hex, rgb(), named colors)
transform.go  SVG transform parser and matrix math
```

## License

Modified BSD License — see [License.md](License.md)
