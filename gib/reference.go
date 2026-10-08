package gib

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
)

const (
	dockerHub     = "registry-1.docker.io"
	defaultTag    = "latest"
	libraryPrefix = "library/"
)

var (
	registryComponent   = `(?:[a-zA-Z\d]|(?:[a-zA-Z\d][a-zA-Z\d-]*[a-zA-Z\d]))`
	registryPattern     = registryComponent + `(?:\.` + registryComponent + `)*(?::\d+)?`
	repositoryComponent = `[a-z\d]+(?:(?:[_.]|__|-+)[a-z\d]+)*`
	repositoryPattern   = `(?:` + repositoryComponent + `/)*` + repositoryComponent
	tagPattern          = `[\w][\w.-]{0,127}`
	digestPattern       = `sha256:[a-f0-9]{64}`

	referenceRE = regexp.MustCompile(`^(?:(` + registryPattern + `)/)?(` + repositoryPattern + `)(?::(` + tagPattern + `))?(?:@(` + digestPattern + `))?$`)
	tagRE       = regexp.MustCompile(`^` + tagPattern + `$`)
	digestRE    = regexp.MustCompile(`^` + digestPattern + `$`)
)

// InvalidReferenceError is an image reference Jib's ImageReference does
// not parse.
type InvalidReferenceError struct{ Reference string }

func (e *InvalidReferenceError) Error() string {
	return "Invalid image reference: " + e.Reference
}

// reference is an image reference as Jib's ImageReference parses and
// prints it: Docker Hub's registry is registry-1.docker.io, its
// single-component repositories are under library/, and a reference with
// neither tag nor digest is tagged latest.
type reference struct {
	registry, repository, tag, digest string
}

func parseReference(s string) (reference, error) {
	m := referenceRE.FindStringSubmatch(s)
	if m == nil {
		return reference{}, &InvalidReferenceError{s}
	}
	registry, repository, tag, digest := m[1], m[2], m[3], m[4]
	if registry == "" {
		registry = dockerHub
	}
	if !strings.ContainsAny(registry, ".:") && registry != "localhost" {
		repository = registry + "/" + repository
		registry = dockerHub
	}
	if registry == dockerHub && !strings.Contains(repository, "/") {
		repository = libraryPrefix + repository
	}
	if tag == "" && digest == "" {
		tag = defaultTag
	}
	if registry == "docker.io" {
		registry = dockerHub
	}
	return reference{registry: registry, repository: repository, tag: tag, digest: digest}, nil
}

// qualifier is the reference's digest, or its tag when it has none.
func (r reference) qualifier() string {
	if r.digest != "" {
		return r.digest
	}
	return r.tag
}

// withQualifier is the reference with q as its digest, when q is one,
// or as its tag.
func (r reference) withQualifier(q string) reference {
	if digestRE.MatchString(q) {
		r.digest = q
		return r
	}
	r.tag = q
	return r
}

func (r reference) base() string {
	switch {
	case r.registry != dockerHub:
		return r.registry + "/" + r.repository
	case strings.HasPrefix(r.repository, libraryPrefix):
		return strings.TrimPrefix(r.repository, libraryPrefix)
	default:
		return r.repository
	}
}

// String is the reference as Jib prints it: without the latest tag.
func (r reference) String() string {
	s := r.base()
	if r.tag != "" && r.tag != defaultTag {
		s += ":" + r.tag
	}
	if r.digest != "" {
		s += "@" + r.digest
	}
	return s
}

// withQualifierString is the reference with its one qualifier: its
// digest, or its tag.
func (r reference) withQualifierString() string {
	if r.digest != "" {
		return r.base() + "@" + r.digest
	}
	return r.base() + ":" + r.tag
}

// name is the reference as go-containerregistry names it: by its
// digest when it has one.
func (r reference) name() (name.Reference, error) {
	repo, err := r.repoName()
	if err != nil {
		return nil, err
	}
	if r.digest != "" {
		return repo.Digest(r.digest), nil
	}
	return repo.Tag(r.tag), nil
}

// repoName is the reference's repository as go-containerregistry names
// it, its name as Jib validates it rather than as go-containerregistry
// would, which refuses a repository of one character.
func (r reference) repoName() (name.Repository, error) {
	reg, err := name.NewRegistry(r.registry)
	if err != nil {
		return name.Repository{}, err
	}
	return reg.Repo(r.repository), nil
}

// validTag reports whether tag is one Jib accepts as an additional tag.
func validTag(tag string) error {
	if !tagRE.MatchString(tag) {
		return fmt.Errorf("invalid tag '%s'", tag)
	}
	return nil
}
