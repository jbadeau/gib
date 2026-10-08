package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// mirror is one entry of registryMirrors in Jib's global configuration.
type mirror struct {
	Registry string
	Mirrors  []string
}

const configFAQ = "https://github.com/GoogleContainerTools/jib/blob/master/docs/faq.md#where-is-the-global-jib-configuration-file-and-how-i-can-configure-it"

// registryMirrors are the registry mirrors of Jib's global
// configuration file, config.json in Jib's configuration directory,
// refused as Jib refuses them when an entry lacks its registry or its
// mirrors.
func registryMirrors() ([]mirror, error) {
	file := filepath.Join(configDir(), "config.json")
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var cfg struct{ RegistryMirrors []mirror }
	if err == nil {
		err = json.Unmarshal(data, &cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("Failed to create, open, or parse global Jib config file; see %s to fix or you may need to delete %s: %w", configFAQ, file, err) //nolint:staticcheck // Jib's message
	}
	for _, m := range cfg.RegistryMirrors {
		switch {
		case m.Registry == "":
			return nil, fmt.Errorf("'registryMirrors.registry' property is missing; see %s to fix or you may need to delete %s", configFAQ, file)
		case len(m.Mirrors) == 0:
			return nil, fmt.Errorf("'registryMirrors.mirrors' property is missing; see %s to fix or you may need to delete %s", configFAQ, file)
		}
	}
	return cfg.RegistryMirrors, nil
}

// configDir is Jib's configuration directory, as its XdgDirectories
// finds it.
func configDir() string {
	home, _ := os.UserHomeDir()
	xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	switch runtime.GOOS {
	case "linux":
		if xdg != "" {
			return filepath.Join(xdg, "google-cloud-tools-java", "jib")
		}
		return filepath.Join(home, ".config", "google-cloud-tools-java", "jib")
	case "windows":
		sub := filepath.Join("Google", "Jib", "Config")
		if xdg != "" {
			return filepath.Join(xdg, sub)
		}
		if l := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); l != "" {
			if _, err := os.Stat(l); err == nil {
				return filepath.Join(l, sub)
			}
		}
		return filepath.Join(home, ".config", sub)
	}
	if xdg != "" {
		return filepath.Join(xdg, "Google", "Jib")
	}
	lib := filepath.Join(home, "Library", "Preferences")
	if _, err := os.Stat(lib); err != nil {
		return filepath.Join(home, ".config", "Google", "Jib")
	}
	return filepath.Join(lib, "Google", "Jib")
}
