package gib

import (
	"fmt"
	"strconv"
	"strings"
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

// ParsePort parses a port string like "8080", "8080/tcp", or "8080/udp".
func ParsePort(s string) (Port, error) {
	parts := strings.SplitN(s, "/", 2)
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return Port{}, fmt.Errorf("invalid port number %q: %w", parts[0], err)
	}
	if n < 1 || n > 65535 {
		return Port{}, fmt.Errorf("port number %d out of range (1-65535)", n)
	}
	proto := "tcp"
	if len(parts) == 2 {
		proto = strings.ToLower(parts[1])
		if proto != "tcp" && proto != "udp" {
			return Port{}, fmt.Errorf("unsupported protocol %q (must be tcp or udp)", proto)
		}
	}
	return Port{Number: n, Protocol: proto}, nil
}
