package buildfile

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	requiredAPIVersion = "jib/v1alpha1"
	requiredKind       = "BuildFile"
)

// Parse reads and parses a build file from the given path.
func Parse(path string, params map[string]string) (*BuildFileSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading build file: %w", err)
	}
	return ParseBytes(data, params)
}

// ParseBytes parses a build file from bytes.
func ParseBytes(data []byte, params map[string]string) (*BuildFileSpec, error) {
	// Substitute template parameters
	content := string(data)
	content, err := substituteParams(content, params)
	if err != nil {
		return nil, err
	}

	var spec BuildFileSpec
	if err := yaml.Unmarshal([]byte(content), &spec); err != nil {
		return nil, fmt.Errorf("parsing build file YAML: %w", err)
	}

	if err := validate(&spec); err != nil {
		return nil, err
	}

	return &spec, nil
}

func validate(spec *BuildFileSpec) error {
	if spec.APIVersion == "" {
		return &ValidationError{Field: "apiVersion", Message: "is required"}
	}
	if strings.TrimSpace(spec.APIVersion) == "" {
		return &ValidationError{Field: "apiVersion", Message: "cannot be an empty string"}
	}
	if spec.APIVersion != requiredAPIVersion {
		return &ValidationError{Field: "apiVersion", Message: fmt.Sprintf("must be %q, got %q", requiredAPIVersion, spec.APIVersion)}
	}
	if spec.Kind == "" {
		return &ValidationError{Field: "kind", Message: "is required"}
	}
	if strings.TrimSpace(spec.Kind) == "" {
		return &ValidationError{Field: "kind", Message: "cannot be an empty string"}
	}
	if spec.Kind != requiredKind {
		return &ValidationError{Field: "kind", Message: fmt.Sprintf("must be %q, got %q", requiredKind, spec.Kind)}
	}

	// Validate no empty string values in scalar fields
	if spec.CreationTime != "" && strings.TrimSpace(spec.CreationTime) == "" {
		return &ValidationError{Field: "creationTime", Message: "cannot be an empty string"}
	}
	if spec.Format != "" && strings.TrimSpace(spec.Format) == "" {
		return &ValidationError{Field: "format", Message: "cannot be an empty string"}
	}
	if spec.User != "" && strings.TrimSpace(spec.User) == "" {
		return &ValidationError{Field: "user", Message: "cannot be an empty string"}
	}
	if spec.WorkingDirectory != "" && strings.TrimSpace(spec.WorkingDirectory) == "" {
		return &ValidationError{Field: "workingDirectory", Message: "cannot be an empty string"}
	}

	// Validate no empty string entries in string slices
	if err := validateStringSlice(spec.Volumes, "volumes"); err != nil {
		return err
	}
	if err := validateStringSlice(spec.ExposedPorts, "exposedPorts"); err != nil {
		return err
	}
	if err := validateStringSlice(spec.Entrypoint, "entrypoint"); err != nil {
		return err
	}
	if err := validateStringSlice(spec.Cmd, "cmd"); err != nil {
		return err
	}

	// Validate no empty keys or values in maps
	if err := validateStringMap(spec.Environment, "environment"); err != nil {
		return err
	}
	if err := validateStringMap(spec.Labels, "labels"); err != nil {
		return err
	}

	// Validate file properties
	if spec.Layers != nil {
		if spec.Layers.Properties != nil {
			if err := validateFileProperties(spec.Layers.Properties, "layers.properties"); err != nil {
				return err
			}
		}
		for i, entry := range spec.Layers.Entries {
			if entry.Properties != nil {
				prefix := fmt.Sprintf("layers.entries[%d].properties", i)
				if err := validateFileProperties(entry.Properties, prefix); err != nil {
					return err
				}
			}
			for j, f := range entry.Files {
				if f.Properties != nil {
					prefix := fmt.Sprintf("layers.entries[%d].files[%d].properties", i, j)
					if err := validateFileProperties(f.Properties, prefix); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func validateStringSlice(slice []string, field string) error {
	for _, s := range slice {
		if strings.TrimSpace(s) == "" {
			return &ValidationError{Field: field, Message: "cannot contain empty strings"}
		}
	}
	return nil
}

func validateStringMap(m map[string]string, field string) error {
	for k, v := range m {
		if strings.TrimSpace(k) == "" {
			return &ValidationError{Field: field, Message: "cannot contain empty keys"}
		}
		if strings.TrimSpace(v) == "" {
			return &ValidationError{Field: field, Message: "cannot contain empty values"}
		}
	}
	return nil
}

func validateFileProperties(props *FilePropertiesSpec, prefix string) error {
	if props.FilePermissions != "" {
		if err := validateOctalPermissions(props.FilePermissions, prefix+".filePermissions"); err != nil {
			return err
		}
	}
	if props.DirectoryPermissions != "" {
		if err := validateOctalPermissions(props.DirectoryPermissions, prefix+".directoryPermissions"); err != nil {
			return err
		}
	}
	return nil
}

func validateOctalPermissions(perm, field string) error {
	if len(perm) != 3 {
		return &ValidationError{Field: field, Message: "must be a 3-digit octal number (000-777)"}
	}
	val, err := strconv.ParseUint(perm, 8, 32)
	if err != nil || val > 0777 {
		return &ValidationError{Field: field, Message: "must be a 3-digit octal number (000-777)"}
	}
	return nil
}

// substituteParams replaces $${paramName} with values from the params map.
// Escaped $$$${...} produces a literal $${...}.
func substituteParams(content string, params map[string]string) (string, error) {
	if len(params) == 0 && !strings.Contains(content, "$${") {
		return content, nil
	}

	var result strings.Builder
	i := 0
	for i < len(content) {
		// Look for $${
		idx := strings.Index(content[i:], "$${")
		if idx == -1 {
			result.WriteString(content[i:])
			break
		}

		// Write everything before the match
		result.WriteString(content[i : i+idx])

		// Check for escaped $$$${
		if idx > 0 && content[i+idx-1] == '$' {
			// This is an escaped $$$${...} -> produce literal $${...}
			// Remove the extra $ we already wrote
			s := result.String()
			result.Reset()
			result.WriteString(s[:len(s)-1]) // remove trailing $
			result.WriteString("$${")
			i = i + idx + 3
			// Find closing }
			end := strings.Index(content[i:], "}")
			if end == -1 {
				return "", fmt.Errorf("unclosed template parameter at position %d", i)
			}
			result.WriteString(content[i : i+end+1])
			i = i + end + 1
			continue
		}

		// Find closing }
		paramStart := i + idx + 3
		end := strings.Index(content[paramStart:], "}")
		if end == -1 {
			return "", fmt.Errorf("unclosed template parameter at position %d", i+idx)
		}

		paramName := content[paramStart : paramStart+end]
		value, ok := params[paramName]
		if !ok {
			return "", fmt.Errorf("missing required template parameter %q", paramName)
		}

		result.WriteString(value)
		i = paramStart + end + 1
	}

	return result.String(), nil
}

// ValidationError represents a build file validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("build file validation: %s %s", e.Field, e.Message)
}
