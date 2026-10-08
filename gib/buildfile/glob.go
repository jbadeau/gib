package buildfile

import (
	"fmt"
	"regexp"
	"strings"
)

// pathMatcher is the PathMatcher Jib makes of an includes or excludes
// pattern: Java's "glob:" syntax, matched against a whole path as the
// directory walk spells it. A pattern ending in a slash matches what is
// beneath it, as Jib appends "**".
type pathMatcher struct{ re *regexp.Regexp }

func newPathMatcher(glob string) (pathMatcher, error) {
	if strings.HasSuffix(glob, "/") || strings.HasSuffix(glob, `\`) {
		glob += "**"
	}
	expr, err := globToRegex(glob)
	if err != nil {
		return pathMatcher{}, err
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return pathMatcher{}, fmt.Errorf("glob %q: %w", glob, err)
	}
	return pathMatcher{re: re}, nil
}

func (m pathMatcher) matches(path string) bool { return m.re.MatchString(path) }

// anyChar is any character, line terminators included, which a name
// may hold.
const anyChar = `(?s:.)`

// globToRegex translates a glob as the JDK's sun.nio.fs.Globs does for
// Unix: '*' within a name, "**" across names, '?' one character of a
// name, "[...]" a class never matching '/', "{a,b}" alternatives, and
// '\' escaping the next character.
func globToRegex(glob string) (string, error) {
	r := []rune(glob)
	next := func(i int) rune {
		if i < len(r) {
			return r[i]
		}
		return 0
	}
	var b strings.Builder
	b.WriteString("^")
	inGroup := false
	for i := 0; i < len(r); {
		c := r[i]
		i++
		switch c {
		case '\\':
			if i == len(r) {
				return "", fmt.Errorf("glob %q: no character to escape", glob)
			}
			b.WriteString(regexp.QuoteMeta(string(r[i])))
			i++
		case '/':
			b.WriteRune(c)
		case '[':
			class, end, err := globClass(glob, r, i)
			if err != nil {
				return "", err
			}
			b.WriteString(class)
			i = end
		case '{':
			if inGroup {
				return "", fmt.Errorf("glob %q: cannot nest groups", glob)
			}
			b.WriteString("(?:(?:")
			inGroup = true
		case '}':
			if inGroup {
				b.WriteString("))")
				inGroup = false
			} else {
				b.WriteRune('}')
			}
		case ',':
			if inGroup {
				b.WriteString(")|(?:")
			} else {
				b.WriteRune(',')
			}
		case '*':
			if next(i) == '*' {
				b.WriteString(anyChar + "*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	if inGroup {
		return "", fmt.Errorf("glob %q: missing '}'", glob)
	}
	b.WriteString("$")
	return b.String(), nil
}

// globClass translates the class opening at r[i-1]: Java's "[[^/]&&[...]]",
// which never matches '/', written without the intersection RE2 lacks.
// It returns the class and the index after its ']'.
func globClass(glob string, r []rune, i int) (string, int, error) {
	next := func(i int) rune {
		if i < len(r) {
			return r[i]
		}
		return 0
	}
	type span struct{ lo, hi rune }
	var spans []span
	negate := false
	if next(i) == '^' {
		spans = append(spans, span{'^', '^'})
		i++
	} else {
		if next(i) == '!' {
			negate = true
			i++
		}
		if next(i) == '-' {
			spans = append(spans, span{'-', '-'})
			i++
		}
	}
	closed := false
	hasStart := false
	var last rune
	for i < len(r) {
		c := r[i]
		i++
		if c == ']' {
			closed = true
			break
		}
		if c == '/' {
			return "", 0, fmt.Errorf("glob %q: explicit 'name separator' in class", glob)
		}
		if c == '-' {
			if !hasStart {
				return "", 0, fmt.Errorf("glob %q: invalid range", glob)
			}
			hi := next(i)
			i++
			if hi == 0 || hi == ']' {
				// Java writes the '-' and stops the class here.
				spans = append(spans, span{'-', '-'})
				closed = hi == ']'
				break
			}
			if hi < last {
				return "", 0, fmt.Errorf("glob %q: invalid range", glob)
			}
			spans[len(spans)-1] = span{last, hi}
			hasStart = false
			continue
		}
		spans = append(spans, span{c, c})
		hasStart, last = true, c
	}
	if !closed {
		return "", 0, fmt.Errorf("glob %q: missing ']'", glob)
	}
	var b strings.Builder
	b.WriteByte('[')
	if negate {
		b.WriteString("^/")
	}
	lit := func(c rune) string { return regexp.QuoteMeta(string(c)) }
	for _, sp := range spans {
		parts := []span{sp}
		if !negate {
			// A positive class never matches '/': split a range around it.
			parts = []span{{sp.lo, min(sp.hi, '/'-1)}, {max(sp.lo, '/'+1), sp.hi}}
		}
		for _, p := range parts {
			switch {
			case p.lo > p.hi:
			case p.lo == p.hi:
				b.WriteString(lit(p.lo))
			default:
				b.WriteString(lit(p.lo) + "-" + lit(p.hi))
			}
		}
	}
	b.WriteByte(']')
	return b.String(), i, nil
}
