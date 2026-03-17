package svgreader

import (
	"fmt"
	"strconv"
	"strings"
)

// PathCommand represents a single SVG path command with its arguments.
type PathCommand struct {
	Cmd  byte      // Command letter: M, L, H, V, C, S, Q, T, A, Z (upper=absolute, lower=relative)
	Args []float64 // Coordinate arguments
}

// argsPerCommand returns the number of arguments expected for each command.
func argsPerCommand(cmd byte) int {
	switch cmd | 0x20 { // lowercase
	case 'z':
		return 0
	case 'h', 'v':
		return 1
	case 'm', 'l', 't':
		return 2
	case 's', 'q':
		return 4
	case 'c':
		return 6
	case 'a':
		return 7
	default:
		return -1
	}
}

// ParsePathData parses an SVG path data string (the "d" attribute) into a
// sequence of path commands.
//
// Supported commands: M, L, H, V, C, S, Q, T, A, Z and their relative
// (lowercase) variants. Implicit command repetition is handled: extra
// coordinate pairs after M are treated as L, and similarly for other commands.
func ParsePathData(d string) ([]PathCommand, error) {
	p := pathParser{input: d}
	return p.parse()
}

type pathParser struct {
	input string
	pos   int
}

func (p *pathParser) parse() ([]PathCommand, error) {
	var cmds []PathCommand
	var lastCmd byte

	for {
		p.skipWhitespaceAndCommas()
		if p.pos >= len(p.input) {
			break
		}

		ch := p.input[p.pos]

		var cmd byte
		if isPathCommand(ch) {
			cmd = ch
			p.pos++
		} else if lastCmd != 0 {
			// Implicit repetition
			switch lastCmd {
			case 'M':
				cmd = 'L'
			case 'm':
				cmd = 'l'
			default:
				cmd = lastCmd
			}
		} else {
			return nil, fmt.Errorf("svgreader: expected command at position %d, got %q", p.pos, ch)
		}

		nargs := argsPerCommand(cmd)
		if nargs < 0 {
			return nil, fmt.Errorf("svgreader: unknown path command %q", cmd)
		}

		if nargs == 0 {
			cmds = append(cmds, PathCommand{Cmd: cmd})
			lastCmd = cmd
			continue
		}

		// Read argument groups (command may repeat with multiple arg sets)
		for {
			args, err := p.readArgs(cmd, nargs)
			if err != nil {
				return nil, err
			}
			cmds = append(cmds, PathCommand{Cmd: cmd, Args: args})
			lastCmd = cmd

			// After first M, implicit commands become L
			if cmd == 'M' {
				cmd = 'L'
			} else if cmd == 'm' {
				cmd = 'l'
			}

			// Check if more numbers follow (implicit repetition)
			p.skipWhitespaceAndCommas()
			if p.pos >= len(p.input) {
				break
			}
			ch := p.input[p.pos]
			if isPathCommand(ch) {
				break
			}
			if !isNumberStart(ch) {
				break
			}
		}
	}

	return cmds, nil
}

// readArgs reads nargs numbers for the given command. For arc commands (A/a),
// arguments 3 and 4 are flag values (0 or 1) that may be packed without
// separators.
func (p *pathParser) readArgs(cmd byte, nargs int) ([]float64, error) {
	args := make([]float64, nargs)
	isArc := cmd == 'A' || cmd == 'a'

	for i := range nargs {
		p.skipWhitespaceAndCommas()

		if isArc && (i == 3 || i == 4) {
			// Arc flags: single digit 0 or 1, possibly without separator
			f, err := p.readFlag()
			if err != nil {
				return nil, fmt.Errorf("svgreader: arc flag at position %d: %w", p.pos, err)
			}
			args[i] = f
		} else {
			n, err := p.readNumber()
			if err != nil {
				return nil, fmt.Errorf("svgreader: argument %d for %q at position %d: %w", i, cmd, p.pos, err)
			}
			args[i] = n
		}
	}
	return args, nil
}

func (p *pathParser) readFlag() (float64, error) {
	if p.pos >= len(p.input) {
		return 0, fmt.Errorf("unexpected end of path data")
	}
	ch := p.input[p.pos]
	if ch == '0' || ch == '1' {
		p.pos++
		return float64(ch - '0'), nil
	}
	return 0, fmt.Errorf("expected flag (0 or 1), got %q", ch)
}

func (p *pathParser) readNumber() (float64, error) {
	if p.pos >= len(p.input) {
		return 0, fmt.Errorf("unexpected end of path data")
	}

	start := p.pos

	// Optional sign
	if p.pos < len(p.input) && (p.input[p.pos] == '-' || p.input[p.pos] == '+') {
		p.pos++
	}

	// Integer part
	hadDigit := false
	for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
		p.pos++
		hadDigit = true
	}

	// Decimal part
	if p.pos < len(p.input) && p.input[p.pos] == '.' {
		p.pos++
		for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
			p.pos++
			hadDigit = true
		}
	}

	if !hadDigit {
		return 0, fmt.Errorf("expected number at position %d", start)
	}

	// Exponent
	if p.pos < len(p.input) && (p.input[p.pos] == 'e' || p.input[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.input) && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
			p.pos++
		}
		for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
			p.pos++
		}
	}

	f, err := strconv.ParseFloat(p.input[start:p.pos], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q: %w", p.input[start:p.pos], err)
	}
	return f, nil
}

func (p *pathParser) skipWhitespaceAndCommas() {
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == ',' {
			p.pos++
		} else {
			break
		}
	}
}

func isPathCommand(ch byte) bool {
	switch ch {
	case 'M', 'm', 'L', 'l', 'H', 'h', 'V', 'v',
		'C', 'c', 'S', 's', 'Q', 'q', 'T', 't',
		'A', 'a', 'Z', 'z':
		return true
	}
	return false
}

func isNumberStart(ch byte) bool {
	return ch == '-' || ch == '+' || ch == '.' || (ch >= '0' && ch <= '9')
}

// ResolveToAbsolute converts all relative commands to absolute coordinates.
// This simplifies rendering since PDF only uses absolute coordinates.
func ResolveToAbsolute(cmds []PathCommand) []PathCommand {
	var result []PathCommand
	var cx, cy float64 // current point
	var sx, sy float64 // start of current subpath

	for _, c := range cmds {
		isRel := c.Cmd >= 'a' && c.Cmd <= 'z'
		absCmd := c.Cmd
		if isRel {
			absCmd = c.Cmd - 32 // to uppercase
		}

		args := make([]float64, len(c.Args))
		copy(args, c.Args)

		switch absCmd {
		case 'M':
			if isRel {
				args[0] += cx
				args[1] += cy
			}
			cx, cy = args[0], args[1]
			sx, sy = cx, cy
		case 'L', 'T':
			if isRel {
				args[0] += cx
				args[1] += cy
			}
			cx, cy = args[0], args[1]
		case 'H':
			if isRel {
				args[0] += cx
			}
			cx = args[0]
		case 'V':
			if isRel {
				args[0] += cy
			}
			cy = args[0]
		case 'C':
			if isRel {
				for i := 0; i < 6; i += 2 {
					args[i] += cx
					args[i+1] += cy
				}
			}
			cx, cy = args[4], args[5]
		case 'S', 'Q':
			if isRel {
				for i := 0; i < 4; i += 2 {
					args[i] += cx
					args[i+1] += cy
				}
			}
			cx, cy = args[len(args)-2], args[len(args)-1]
		case 'A':
			if isRel {
				args[5] += cx
				args[6] += cy
			}
			cx, cy = args[5], args[6]
		case 'Z':
			cx, cy = sx, sy
		}

		result = append(result, PathCommand{Cmd: absCmd, Args: args})
	}

	return result
}

// ExpandShorthands expands S/T shorthand commands into full C/Q commands by
// computing the reflected control point from the previous command.
func ExpandShorthands(cmds []PathCommand) []PathCommand {
	var result []PathCommand
	var cx, cy float64               // current point
	var lastCtrlX, lastCtrlY float64 // last control point for reflection
	var lastCmd byte

	for _, c := range cmds {
		switch c.Cmd {
		case 'S':
			// Smooth cubic: reflect last C control point
			var rx, ry float64
			if lastCmd == 'C' || lastCmd == 'S' {
				rx = 2*cx - lastCtrlX
				ry = 2*cy - lastCtrlY
			} else {
				rx, ry = cx, cy
			}
			result = append(result, PathCommand{
				Cmd:  'C',
				Args: []float64{rx, ry, c.Args[0], c.Args[1], c.Args[2], c.Args[3]},
			})
			lastCtrlX, lastCtrlY = c.Args[0], c.Args[1]
			cx, cy = c.Args[2], c.Args[3]
		case 'T':
			// Smooth quadratic: reflect last Q control point
			var rx, ry float64
			if lastCmd == 'Q' || lastCmd == 'T' {
				rx = 2*cx - lastCtrlX
				ry = 2*cy - lastCtrlY
			} else {
				rx, ry = cx, cy
			}
			result = append(result, PathCommand{
				Cmd:  'Q',
				Args: []float64{rx, ry, c.Args[0], c.Args[1]},
			})
			lastCtrlX, lastCtrlY = rx, ry
			cx, cy = c.Args[0], c.Args[1]
		case 'C':
			result = append(result, c)
			lastCtrlX, lastCtrlY = c.Args[2], c.Args[3]
			cx, cy = c.Args[4], c.Args[5]
		case 'Q':
			result = append(result, c)
			lastCtrlX, lastCtrlY = c.Args[0], c.Args[1]
			cx, cy = c.Args[2], c.Args[3]
		case 'M':
			result = append(result, c)
			cx, cy = c.Args[0], c.Args[1]
			lastCtrlX, lastCtrlY = cx, cy
		case 'L', 'H', 'V':
			result = append(result, c)
			switch c.Cmd {
			case 'L':
				cx, cy = c.Args[0], c.Args[1]
			case 'H':
				cx = c.Args[0]
			case 'V':
				cy = c.Args[0]
			}
			lastCtrlX, lastCtrlY = cx, cy
		default:
			result = append(result, c)
			if c.Cmd == 'A' && len(c.Args) >= 7 {
				cx, cy = c.Args[5], c.Args[6]
			}
			lastCtrlX, lastCtrlY = cx, cy
		}
		lastCmd = c.Cmd
	}
	return result
}

// QuadToCubic converts a quadratic Bézier (Q) to a cubic Bézier (C).
// PDF only supports cubic Bézier curves.
func QuadToCubic(qx0, qy0, qx1, qy1, qx2, qy2 float64) (cx1, cy1, cx2, cy2 float64) {
	// CP1 = Q0 + 2/3 * (Q1 - Q0)
	cx1 = qx0 + 2.0/3.0*(qx1-qx0)
	cy1 = qy0 + 2.0/3.0*(qy1-qy0)
	// CP2 = Q2 + 2/3 * (Q1 - Q2)
	cx2 = qx2 + 2.0/3.0*(qx1-qx2)
	cy2 = qy2 + 2.0/3.0*(qy1-qy2)
	return
}

// String returns the SVG path data string representation of the commands.
func PathCommandsToString(cmds []PathCommand) string {
	var buf strings.Builder
	for _, c := range cmds {
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteByte(c.Cmd)
		for i, a := range c.Args {
			if i > 0 {
				buf.WriteByte(' ')
			} else {
				buf.WriteByte(' ')
			}
			buf.WriteString(strconv.FormatFloat(a, 'f', -1, 64))
		}
	}
	return buf.String()
}
