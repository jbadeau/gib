package gib

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
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

// guard is what go-containerregistry's transport lacks of Jib's rules
// for reaching a registry. Insecure registries allowed, go-containerregistry
// tries HTTPS, without verifying the certificate, then plain HTTP, as
// Jib's FailoverHttpClient does. Not allowed, guard refuses plain HTTP,
// which go-containerregistry would otherwise try on a local registry, and
// a certificate that cannot be verified. Over plain HTTP, guard holds
// back Authorization unless credentials may be sent over HTTP.
type guard struct {
	registry, repository string
	settings             registrySettings
	next                 http.RoundTripper
}

func newGuard(ref reference, s registrySettings) http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if s.allowInsecure {
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // only with insecure registries allowed, as Jib
	}
	return &guard{registry: ref.registry, repository: ref.repository, settings: s, next: t}
}

func (g *guard) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "http" && !g.settings.allowInsecure {
		u := *req.URL
		u.Scheme = "https"
		return nil, &InsecureRegistryError{URL: u.String(), Cause: errors.New("insecure HTTP connection not allowed: " + req.URL.String())}
	}
	cleared := false
	if req.URL.Scheme == "http" && !g.settings.sendCredentialsOverHTTP && req.Header.Get("Authorization") != "" {
		req = req.Clone(req.Context())
		req.Header.Del("Authorization")
		cleared = true
	}
	g.traceRequest(req)
	resp, err := g.next.RoundTrip(req)
	if err != nil {
		if req.URL.Scheme == "https" && !g.settings.allowInsecure && isTLS(err) {
			return nil, &InsecureRegistryError{URL: req.URL.String(), Cause: err}
		}
		return nil, err
	}
	g.traceResponse(resp)
	if cleared && resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		return nil, &CredentialsNotSentError{Registry: g.registry, Repository: g.repository}
	}
	return resp, nil
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
		errors.As(err, &hostname) || errors.As(err, &invalid) || errors.As(err, &alert)
}

func (g *guard) traceRequest(r *http.Request) {
	g.trace("-------------- REQUEST  --------------\n"+r.Method+" "+r.URL.String(), r.Header)
}

func (g *guard) traceResponse(resp *http.Response) {
	g.trace("-------------- RESPONSE --------------\n"+resp.Proto+" "+resp.Status, resp.Header)
}

func (g *guard) trace(head string, h http.Header) {
	if g.settings.trace == TraceOff || g.settings.traceTo == nil {
		return
	}
	var b strings.Builder
	b.WriteString(head + "\n")
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
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	_, _ = io.WriteString(g.settings.traceTo, b.String())
}
