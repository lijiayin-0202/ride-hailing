package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
)

// configSources 只加载实际运行配置，避免把 local.yaml.example 当作 YAML 配置解析。
func configSources(path string) ([]config.Source, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read config path: %w", err)
	}
	if !info.IsDir() {
		return []config.Source{file.NewSource(path)}, nil
	}

	baseConfig := filepath.Join(path, "config.yaml")
	if _, err = os.Stat(baseConfig); err != nil {
		return nil, fmt.Errorf("read base config: %w", err)
	}
	sources := []config.Source{file.NewSource(baseConfig)}
	localConfig := filepath.Join(path, "local.yaml")
	if _, err = os.Stat(localConfig); err == nil {
		sources = append(sources, file.NewSource(localConfig))
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read local config: %w", err)
	}
	return sources, nil
}
