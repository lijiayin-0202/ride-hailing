package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-kratos/kratos/v2/config"
)

func TestConfigSourcesSkipsExampleFiles(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, filepath.Join(dir, "config.yaml"), "data:\n  database:\n    driver: postgres\n")
	writeConfigFile(t, filepath.Join(dir, "local.yaml"), "data:\n  database:\n    source: postgresql://localhost/test\n")
	writeConfigFile(t, filepath.Join(dir, "local.yaml.example"), "not-a-supported-format: true\n")

	sources, err := configSources(dir)
	if err != nil {
		t.Fatalf("configSources() error = %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("configSources() returned %d sources, want 2", len(sources))
	}
	loaded := config.New(config.WithSource(sources...))
	t.Cleanup(func() { _ = loaded.Close() })
	if err = loaded.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestConfigSourcesAcceptsSingleConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFile(t, path, "server: {}\n")
	sources, err := configSources(path)
	if err != nil {
		t.Fatalf("configSources() error = %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("configSources() returned %d sources, want 1", len(sources))
	}
}

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}
