package buildfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jbadeau/gib"
)

// ConvertOptions carries authentication and registry options for the base image.
type ConvertOptions struct {
	FromUsername            string
	FromPassword            string
	FromCredentialHelper    string
	AllowInsecureRegistries bool
}

// Convert transforms a BuildFileSpec into a ContainerBuilder.
func Convert(spec *BuildFileSpec, contextDir string, opts *ConvertOptions) (*gib.ContainerBuilder, error) {
	var builder *gib.ContainerBuilder

	// Base image
	if spec.From != nil && spec.From.Image != "" {
		var sourceOpts []gib.ImageSourceOption
		if opts != nil {
			if opts.FromUsername != "" && opts.FromPassword != "" {
				sourceOpts = append(sourceOpts, gib.WithSourceCredentials(opts.FromUsername, opts.FromPassword))
			} else if opts.FromCredentialHelper != "" {
				sourceOpts = append(sourceOpts, gib.WithSourceCredentialHelper(opts.FromCredentialHelper))
			}
			if opts.AllowInsecureRegistries {
				sourceOpts = append(sourceOpts, gib.WithSourceInsecure())
			}
		}
		builder = gib.From(spec.From.Image, sourceOpts...)
	} else {
		builder = gib.FromScratch()
	}

	// Platforms
	if spec.From != nil {
		for _, p := range spec.From.Platforms {
			builder.AddPlatform(p.Architecture, p.OS)
		}
	}

	// Creation time
	if spec.CreationTime != "" {
		millis, err := parseTimestamp(spec.CreationTime)
		if err != nil {
			return nil, fmt.Errorf("invalid creationTime: %w", err)
		}
		builder.SetCreationTime(millis)
	}

	// Format
	if spec.Format != "" {
		builder.SetFormat(gib.ParseImageFormat(spec.Format))
	}

	// Environment
	if len(spec.Environment) > 0 {
		builder.SetEnvironment(spec.Environment)
	}

	// Labels
	if len(spec.Labels) > 0 {
		builder.SetLabels(spec.Labels)
	}

	// Volumes
	for _, v := range spec.Volumes {
		builder.AddVolume(v)
	}

	// Exposed ports
	for _, portStr := range spec.ExposedPorts {
		p, err := gib.ParsePort(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", portStr, err)
		}
		builder.AddExposedPort(p)
	}

	// User
	if spec.User != "" {
		builder.SetUser(spec.User)
	}

	// Working directory
	if spec.WorkingDirectory != "" {
		builder.SetWorkingDirectory(spec.WorkingDirectory)
	}

	// Entrypoint
	if spec.Entrypoint != nil {
		builder.SetEntrypoint(spec.Entrypoint...)
	}

	// Cmd
	if spec.Cmd != nil {
		builder.SetProgramArguments(spec.Cmd...)
	}

	// Layers
	if spec.Layers != nil {
		globalProps := defaultProperties()
		if spec.Layers.Properties != nil {
			globalProps = mergeProperties(globalProps, spec.Layers.Properties)
		}

		for _, entry := range spec.Layers.Entries {
			layerProps := globalProps
			if entry.Properties != nil {
				layerProps = mergeProperties(layerProps, entry.Properties)
			}

			layer, err := buildLayer(entry, layerProps, contextDir)
			if err != nil {
				return nil, fmt.Errorf("building layer %q: %w", entry.Name, err)
			}
			builder.AddFileEntriesLayer(layer)
		}
	}

	return builder, nil
}

func buildLayer(entry LayerEntrySpec, layerProps resolvedProperties, contextDir string) (gib.FileEntriesLayer, error) {
	var entries []gib.FileEntry

	for _, copyDir := range entry.Files {
		props := layerProps
		if copyDir.Properties != nil {
			props = mergeProperties(props, copyDir.Properties)
		}

		srcPath := filepath.Join(contextDir, copyDir.Src)

		fi, err := os.Stat(srcPath)
		if err != nil {
			return gib.FileEntriesLayer{}, fmt.Errorf("source %q: %w", copyDir.Src, err)
		}

		if !fi.IsDir() {
			// Single file copy
			e := gib.FileEntry{
				SourcePath:       srcPath,
				DestinationPath:  copyDir.Dest,
				Permissions:      props.filePermissions,
				ModificationTime: props.timestamp,
				Ownership:        props.user + ":" + props.group,
			}
			entries = append(entries, e)
		} else {
			// Directory copy with globs
			files, err := matchFiles(srcPath, copyDir.Includes, copyDir.Excludes)
			if err != nil {
				return gib.FileEntriesLayer{}, fmt.Errorf("matching files in %q: %w", copyDir.Src, err)
			}

			// Also collect directories for proper dir entries
			dirs := make(map[string]bool)
			for _, f := range files {
				rel, _ := filepath.Rel(srcPath, f)
				rel = filepath.ToSlash(rel)
				// Add all parent dirs
				dir := filepath.ToSlash(filepath.Dir(rel))
				for dir != "." && dir != "/" {
					dirs[dir] = true
					dir = filepath.ToSlash(filepath.Dir(dir))
				}
			}

			// Add directory entries
			for d := range dirs {
				destDir := filepath.ToSlash(filepath.Join(copyDir.Dest, d))
				dirSrc := filepath.Join(srcPath, filepath.FromSlash(d))
				entries = append(entries, gib.FileEntry{
					SourcePath:       dirSrc,
					DestinationPath:  destDir + "/",
					Permissions:      props.directoryPermissions,
					ModificationTime: props.timestamp,
					Ownership:        props.user + ":" + props.group,
				})
			}

			// Add file entries
			for _, f := range files {
				rel, _ := filepath.Rel(srcPath, f)
				dest := filepath.ToSlash(filepath.Join(copyDir.Dest, filepath.ToSlash(rel)))
				entries = append(entries, gib.FileEntry{
					SourcePath:       f,
					DestinationPath:  dest,
					Permissions:      props.filePermissions,
					ModificationTime: props.timestamp,
					Ownership:        props.user + ":" + props.group,
				})
			}
		}
	}

	return gib.FileEntriesLayer{
		Name:    entry.Name,
		Entries: entries,
	}, nil
}

type resolvedProperties struct {
	filePermissions      fs.FileMode
	directoryPermissions fs.FileMode
	user                 string
	group                string
	timestamp            int64
}

func defaultProperties() resolvedProperties {
	return resolvedProperties{
		filePermissions:      0644,
		directoryPermissions: 0755,
		user:                 "0",
		group:                "0",
		timestamp:            1000, // epoch + 1s in millis
	}
}

func mergeProperties(base resolvedProperties, override *FilePropertiesSpec) resolvedProperties {
	if override.FilePermissions != "" {
		if perm, err := strconv.ParseUint(override.FilePermissions, 8, 32); err == nil {
			base.filePermissions = fs.FileMode(perm)
		}
	}
	if override.DirectoryPermissions != "" {
		if perm, err := strconv.ParseUint(override.DirectoryPermissions, 8, 32); err == nil {
			base.directoryPermissions = fs.FileMode(perm)
		}
	}
	if override.User != "" {
		base.user = override.User
	}
	if override.Group != "" {
		base.group = override.Group
	}
	if override.Timestamp != "" {
		if ts, err := parseTimestamp(override.Timestamp); err == nil {
			base.timestamp = ts
		}
	}
	return base
}

// parseTimestamp parses a timestamp as millis or ISO 8601.
func parseTimestamp(s string) (int64, error) {
	// Try as millis first
	if millis, err := strconv.ParseInt(s, 10, 64); err == nil {
		return millis, nil
	}

	// Try as ISO 8601
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t.UnixMilli(), nil
		}
	}

	return 0, fmt.Errorf("cannot parse timestamp %q: expected millis or ISO 8601", s)
}
