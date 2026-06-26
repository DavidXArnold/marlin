package config

import (
	"fmt"
	"strings"
)

const maxInheritDepth = 16

// ResolveModel loads slug from dirs and merges any inherited parent config.
// Child values win over parent values; zero/empty child values fall through
// to the parent. Circular references and missing parents are errors.
func ResolveModel(slug string, dirs ...string) (*ModelConfig, error) {
	return resolveChain(slug, dirs, nil)
}

func resolveChain(slug string, dirs []string, visited []string) (*ModelConfig, error) {
	for _, s := range visited {
		if s == slug {
			return nil, fmt.Errorf("circular inheritance: %s", strings.Join(append(visited, slug), " → "))
		}
	}
	if len(visited) >= maxInheritDepth {
		return nil, fmt.Errorf("inheritance chain too deep (max %d)", maxInheritDepth)
	}

	path, err := FindModelPath(slug, dirs...)
	if err != nil {
		return nil, err
	}
	child, err := LoadModel(path)
	if err != nil {
		return nil, err
	}

	if child.Model.Extends == "" {
		return child, nil
	}

	parent, err := resolveChain(child.Model.Extends, dirs, append(visited, slug))
	if err != nil {
		return nil, fmt.Errorf("resolving parent %q for %q: %w", child.Model.Extends, slug, err)
	}

	return mergeModelConfigs(parent, child), nil
}

func mergeModelConfigs(parent, child *ModelConfig) *ModelConfig {
	return &ModelConfig{
		Model: mergeModelMeta(parent.Model, child.Model),
		Serve: mergeServeConfig(parent.Serve, child.Serve),
	}
}

func mergeModelMeta(parent, child ModelMeta) ModelMeta {
	result := parent
	if child.Type != "" {
		result.Type = child.Type
	}
	if child.ID != "" {
		result.ID = child.ID
	}
	if child.Image != "" {
		result.Image = child.Image
	}
	if child.Registry != "" {
		result.Registry = child.Registry
	}
	if child.Status != "" {
		result.Status = child.Status
	}
	if child.Notes != "" {
		result.Notes = child.Notes
	}
	// Extends and Abstract are always taken from the child.
	result.Extends = child.Extends
	result.Abstract = child.Abstract
	return result
}

func mergeServeConfig(parent, child ServeConfig) ServeConfig {
	result := parent
	if child.Quantization != "" {
		result.Quantization = child.Quantization
	}
	if child.ToolCallParser != "" {
		result.ToolCallParser = child.ToolCallParser
	}
	if len(child.ServedModelName) > 0 {
		result.ServedModelName = child.ServedModelName
	}
	if child.GPUMemoryUtilization > 0 {
		result.GPUMemoryUtilization = child.GPUMemoryUtilization
	}
	if child.MaxModelLen > 0 {
		result.MaxModelLen = child.MaxModelLen
	}
	if len(child.ExtraFlags) > 0 {
		result.ExtraFlags = child.ExtraFlags
	}
	if len(child.ExtraEnv) > 0 {
		result.ExtraEnv = child.ExtraEnv
	}
	if len(child.ExtraVolumes) > 0 {
		result.ExtraVolumes = child.ExtraVolumes
	}
	if child.TrustRemoteCode {
		result.TrustRemoteCode = true
	}
	return result
}
