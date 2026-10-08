package gib

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"syscall"
)

// InsecureRegistryError is a registry gib could not reach securely
// while insecure registries are not allowed, Jib's
// InsecureRegistryException.
type InsecureRegistryError struct {
	URL   string
	Cause error
}

func (e *InsecureRegistryError) Error() string {
	return "Failed to verify the server at " + e.URL + " because only secure connections are allowed."
}

func (e *InsecureRegistryError) Unwrap() error { return e.Cause }

// CredentialsNotSentError is a registry that asked for credentials gib
// held back because the connection was plain HTTP, Jib's
// RegistryCredentialsNotSentException.
type CredentialsNotSentError struct{ Registry, Repository string }

func (e *CredentialsNotSentError) Error() string {
	return "Required credentials for " + e.Registry + "/" + e.Repository + " were not sent because the connection was over HTTP"
}

// HTTPTrace is how much of each HTTP exchange is traced, Jib's
// --http-trace levels.
type HTTPTrace int

const (
	TraceOff HTTPTrace = iota
	TraceConfig
	TraceAll
)

// failover is how gib reaches a registry, as Jib's FailoverHttpClient
// does: HTTPS first, always. Only when insecure registries are allowed
// does a server that cannot be verified get HTTPS without verification,
// then plain HTTP, and a server refusing port 443 get port 80. A
// registry's first answer decides how every later request reaches it.
// Over plain HTTP, Authorization is held back unless credentials may be
// sent over HTTP.
type failover struct {
	host       string
	repository string
	insecure   bool
	sendAuth   bool
	log        LogHandler
	trace      HTTPTrace
	traceTo    io.Writer

	secure, unverified http.RoundTripper

	mu      sync.Mutex
	decided map[string]*decision
}

type mode int

const (
	viaHTTPS mode = iota
	viaUnverifiedHTTPS
	viaHTTP
)

type decision struct {
	once sync.Mutex
	done bool
	mode mode
}

func newFailover(host, repository string, s registrySettings) *failover {
	secure := http.DefaultTransport.(*http.Transport).Clone()
	unverified := secure.Clone()
	unverified.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // only with --allow-insecure-registries, as Jib
	return &failover{
		host:       host,
		repository: repository,
		insecure:   s.allowInsecure,
		sendAuth:   s.sendCredentialsOverHTTP,
		log:        s.log,
		trace:      s.trace,
		traceTo:    s.traceTo,
		secure:     secure,
		unverified: unverified,
		decided:    map[string]*decision{},
	}
}

func (f *failover) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	if u.Host == f.host && u.Scheme == "http" {
		// go-containerregistry tries local registries over HTTP by itself;
		// Jib always tries HTTPS first.
		u.Scheme = "https"
	}
	if u.Scheme != "https" {
		if !f.insecure {
			return nil, &InsecureRegistryError{URL: u.String(), Cause: errors.New("insecure HTTP connection not allowed: " + u.String())}
		}
		return f.send(req, &u, f.secure)
	}

	f.mu.Lock()
	d, ok := f.decided[u.Host]
	if !ok {
		d = &decision{}
		f.decided[u.Host] = d
	}
	f.mu.Unlock()

	d.once.Lock()
	if d.done {
		d.once.Unlock()
		switch d.mode {
		case viaHTTP:
			return f.send(req, toHTTP(&u), f.secure)
		case viaUnverifiedHTTPS:
			return f.send(req, &u, f.unverified)
		}
		return f.send(req, &u, f.secure)
	}
	defer d.once.Unlock()

	resp, err := f.first(req, &u, d)
	if err == nil {
		d.done = true
	}
	return resp, err
}

// first is a registry's first request, which decides how it is reached.
func (f *failover) first(req *http.Request, u *url.URL, d *decision) (*http.Response, error) {
	resp, err := f.send(req, u, f.secure)
	switch {
	case err == nil:
		d.mode = viaHTTPS
		return resp, nil
	case isTLS(err):
		if !f.insecure {
			return nil, &InsecureRegistryError{URL: u.String(), Cause: err}
		}
		f.log.log(LevelWarn, "Cannot verify server at %s. Attempting again with no TLS verification.", u)
		if resp, err := f.send(req, u, f.unverified); err == nil || !isTLS(err) {
			d.mode = viaUnverifiedHTTPS
			return resp, err
		}
		f.log.log(LevelWarn, "Failed to connect to %s over HTTPS. Attempting again with HTTP.", u)
		d.mode = viaHTTP
		return f.send(req, toHTTP(u), f.secure)
	case errors.Is(err, syscall.ECONNREFUSED) && f.insecure && u.Port() == "":
		f.log.log(LevelWarn, "Failed to connect to %s over HTTPS. Attempting again with HTTP.", u)
		d.mode = viaHTTP
		return f.send(req, toHTTP(u), f.secure)
	}
	return nil, err
}

// send sends req to u, with its Authorization held back over plain HTTP
// unless credentials may be sent over HTTP.
func (f *failover) send(req *http.Request, u *url.URL, t http.RoundTripper) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.URL = u
	r.Host = ""
	if req.Body != nil && req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		r.Body = body
	}
	cleared := false
	if u.Scheme == "http" && !f.sendAuth && r.Header.Get("Authorization") != "" {
		r.Header.Del("Authorization")
		cleared = true
	}
	f.traceRequest(r)
	resp, err := t.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	f.traceResponse(resp)
	if cleared && resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		return nil, &CredentialsNotSentError{Registry: f.host, Repository: f.repository}
	}
	return resp, nil
}

func toHTTP(u *url.URL) *url.URL {
	h := *u
	h.Scheme = "http"
	return &h
}

// isTLS reports whether err is a failure to set up TLS, what Java calls
// an SSLException.
func isTLS(err error) bool {
	var (
		header    tls.RecordHeaderError
		verify    *tls.CertificateVerificationError
		authority x509.UnknownAuthorityError
		hostname  x509.HostnameError
		invalid   x509.CertificateInvalidError
		alert     tls.AlertError
	)
	return errors.As(err, &header) || errors.As(err, &verify) || errors.As(err, &authority) ||
		errors.As(err, &hostname) || errors.As(err, &invalid) || errors.As(err, &alert) ||
		strings.Contains(err.Error(), "tls: ")
}

func (f *failover) traceRequest(r *http.Request) {
	if f.trace == TraceOff || f.traceTo == nil {
		return
	}
	var b strings.Builder
	b.WriteString("-------------- REQUEST  --------------\n")
	fmt.Fprintf(&b, "%s %s\n", r.Method, r.URL)
	writeHeaders(&b, r.Header)
	_, _ = io.WriteString(f.traceTo, b.String())
}

func (f *failover) traceResponse(resp *http.Response) {
	if f.trace == TraceOff || f.traceTo == nil {
		return
	}
	var b strings.Builder
	b.WriteString("-------------- RESPONSE --------------\n")
	fmt.Fprintf(&b, "%s %s\n", resp.Proto, resp.Status)
	writeHeaders(&b, resp.Header)
	_, _ = io.WriteString(f.traceTo, b.String())
}

func writeHeaders(b *strings.Builder, h http.Header) {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range h[k] {
			if k == "Authorization" {
				v = "<Not Logged>"
			}
			fmt.Fprintf(b, "%s: %s\n", k, v)
		}
	}
}
