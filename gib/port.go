package gib

import (
	"fmt"
	"regexp"
	"strconv"
)

// Port represents a container port with optional protocol.
type Port struct {
	Number   int
	Protocol string // "tcp" or "udp"; defaults to "tcp"
}

// String returns the port in "number/protocol" format.
func (p Port) String() string {
	proto := p.Protocol
	if proto == "" {
		proto = "tcp"
	}
	return fmt.Sprintf("%d/%s", p.Number, proto)
}

var portPattern = regexp.MustCompile(`^([0-9]+)(?:-([0-9]+))?(?:/(tcp|udp))?$`)

// ParsePorts parses a port as Jib's Ports.parse does: a number or a
// range of two, "8000-8002", either optionally followed by "/tcp" or
// "/udp"; a range gives each port in it.
func ParsePorts(s string) ([]Port, error) {
	m := portPattern.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("Invalid port configuration: '%s'. Make sure the port is a single number or a range of two numbers separated with a '-', with or without protocol specified (e.g. '<portNum>/tcp' or '<portNum>/udp').", s) //nolint:staticcheck // Jib's message
	}
	lo, err := javaInt(m[1])
	if err != nil {
		return nil, err
	}
	hi := lo
	if m[2] != "" {
		if hi, err = javaInt(m[2]); err != nil {
			return nil, err
		}
	}
	if lo > hi {
		return nil, fmt.Errorf("Invalid port range '%s'; smaller number must come first.", s) //nolint:staticcheck // Jib's message
	}
	if lo < 1 || hi > 65535 {
		return nil, fmt.Errorf("Port number '%s' is out of usual range (1-65535).", s) //nolint:staticcheck // Jib's message
	}
	proto := "tcp"
	if m[3] != "" {
		proto = m[3]
	}
	var out []Port
	for n := lo; n <= hi; n++ {
		out = append(out, Port{Number: n, Protocol: proto})
	}
	return out, nil
}

// javaInt is Integer.parseInt of decimal digits.
func javaInt(s string) (int, error) {
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("For input string: \"%s\"", s) //nolint:staticcheck // Java's message
	}
	return int(n), nil
}
