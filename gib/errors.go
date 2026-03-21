package gib

import "fmt"

// BuildError represents an error during the build process.
type BuildError struct {
	Message string
	Cause   error
}

func (e *BuildError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *BuildError) Unwrap() error {
	return e.Cause
}

// InvalidBuildFileError represents an error in the build file.
type InvalidBuildFileError struct {
	Message string
	Cause   error
}

func (e *InvalidBuildFileError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("invalid build file: %s: %v", e.Message, e.Cause)
	}
	return fmt.Sprintf("invalid build file: %s", e.Message)
}

func (e *InvalidBuildFileError) Unwrap() error {
	return e.Cause
}
