package gib

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// FileEntry represents a single file to add to a layer.
type FileEntry struct {
	// SourcePath is the local filesystem path of the file.
	SourcePath string
	// DestinationPath is the absolute path in the container.
	DestinationPath string
	// Permissions are the file permissions (e.g., 0644).
	Permissions fs.FileMode
	// ModificationTime is the modification timestamp in milliseconds since epoch.
	ModificationTime int64
	// Ownership is the user:group ownership string (e.g., "0:0").
	Ownership string
}

// FileEntriesLayer represents a named layer consisting of file entries.
type FileEntriesLayer struct {
	// Name is the layer name.
	Name string
	// Entries are the file entries in this layer.
	Entries []FileEntry
}

// FileEntriesLayerBuilder builds a FileEntriesLayer.
type FileEntriesLayerBuilder struct {
	name    string
	entries []FileEntry
	err     error
}

// NewFileEntriesLayerBuilder creates a new FileEntriesLayerBuilder.
func NewFileEntriesLayerBuilder() *FileEntriesLayerBuilder {
	return &FileEntriesLayerBuilder{}
}

// SetName sets the layer name.
func (b *FileEntriesLayerBuilder) SetName(name string) *FileEntriesLayerBuilder {
	b.name = name
	return b
}

// AddEntry adds a file entry with default permissions (0644).
func (b *FileEntriesLayerBuilder) AddEntry(src, dest string) *FileEntriesLayerBuilder {
	return b.AddEntryWithPermissions(src, dest, 0644)
}

// AddEntryWithPermissions adds a file entry with the given permissions.
func (b *FileEntriesLayerBuilder) AddEntryWithPermissions(src, dest string, perm fs.FileMode) *FileEntriesLayerBuilder {
	b.entries = append(b.entries, FileEntry{
		SourcePath:       src,
		DestinationPath:  dest,
		Permissions:      perm,
		ModificationTime: 1000, // epoch + 1 second in millis
		Ownership:        "0:0",
	})
	return b
}

// AddEntryRecursive adds all files from a directory recursively with default permissions.
func (b *FileEntriesLayerBuilder) AddEntryRecursive(srcDir, destDir string) *FileEntriesLayerBuilder {
	return b.AddEntryRecursiveWithPermissions(srcDir, destDir, 0644, 0755)
}

// AddEntryRecursiveWithPermissions adds all files from a directory recursively.
func (b *FileEntriesLayerBuilder) AddEntryRecursiveWithPermissions(srcDir, destDir string, filePerm, dirPerm fs.FileMode) *FileEntriesLayerBuilder {
	if b.err != nil {
		return b
	}

	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(destDir, rel)
		// Normalize to forward slashes for container paths
		destPath = filepath.ToSlash(destPath)

		perm := filePerm
		if d.IsDir() {
			perm = dirPerm
		}

		b.entries = append(b.entries, FileEntry{
			SourcePath:       path,
			DestinationPath:  destPath,
			Permissions:      perm,
			ModificationTime: 1000,
			Ownership:        "0:0",
		})
		return nil
	})
	if err != nil {
		b.err = err
	}
	return b
}

// Build creates the FileEntriesLayer.
func (b *FileEntriesLayerBuilder) Build() (FileEntriesLayer, error) {
	if b.err != nil {
		return FileEntriesLayer{}, b.err
	}

	// Validate entries
	for _, e := range b.entries {
		if e.DestinationPath == "" {
			return FileEntriesLayer{}, fmt.Errorf("destination path must not be empty")
		}
		if !filepath.IsAbs(e.DestinationPath) {
			return FileEntriesLayer{}, fmt.Errorf("destination path must be absolute: %s", e.DestinationPath)
		}
		if e.SourcePath != "" {
			if _, err := os.Stat(e.SourcePath); err != nil {
				return FileEntriesLayer{}, fmt.Errorf("source path %s: %w", e.SourcePath, err)
			}
		}
	}

	// Sort entries by destination path for reproducibility
	sorted := make([]FileEntry, len(b.entries))
	copy(sorted, b.entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].DestinationPath < sorted[j].DestinationPath
	})

	return FileEntriesLayer{
		Name:    b.name,
		Entries: sorted,
	}, nil
}
