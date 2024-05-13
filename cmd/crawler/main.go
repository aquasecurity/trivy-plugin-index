package main

import (
	"context"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"

	"github.com/go-playground/validator/v10"
	"github.com/hashicorp/go-getter"
	"golang.org/x/xerrors"
	"gopkg.in/yaml.v3"
)

// use a single instance of Validate, it caches struct info
var validate = validator.New(validator.WithRequiredStructEnabled())

type Index struct {
	Name       string `yaml:"name"`
	Repository string `yaml:"repository"`
}

// Plugin represents a plugin.
type Plugin struct {
	Name       string `yaml:"name" validate:"required"`
	Repository string `yaml:"repository" validate:"required"`
	Maintainer string `yaml:"maintainer" validate:"required"`
	Summary    string `yaml:"summary" validate:"required"`
	Output     bool   `yaml:"output"`
}

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	var plugins []Plugin
	err := filepath.WalkDir("plugins", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return xerrors.Errorf("failed to walk the directory: %w", err)
		} else if d.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return xerrors.Errorf("failed to open the file: %w", err)
		}
		defer f.Close()

		var index Index
		if err = yaml.NewDecoder(f).Decode(&index); err != nil {
			return xerrors.Errorf("failed to decode the file: %w", err)
		}

		plugin, err := download(ctx, index)
		if err != nil {
			return xerrors.Errorf("failed to download: %w", err)
		}

		// Copy the necessary fields
		plugins = append(plugins, Plugin{
			Name:       plugin.Name,
			Repository: plugin.Repository,
			Maintainer: plugin.Maintainer,
			Summary:    plugin.Summary,
			Output:     plugin.Output,
		})

		return nil
	})
	if err != nil {
		return xerrors.Errorf("walk dir error: %w", err)
	}

	indexPath := filepath.Join("site", "data", "index.yaml")
	if len(args) == 2 {
		indexPath = args[1]
	}

	f, err := os.Create(indexPath)
	if err != nil {
		return xerrors.Errorf("failed to create the file: %w", err)
	}
	defer f.Close()

	if err = yaml.NewEncoder(f).Encode(plugins); err != nil {
		return xerrors.Errorf("failed to encode the file: %w", err)
	}

	return nil
}

// Download downloads the configured source to the destination.
func download(ctx context.Context, index Index) (*Plugin, error) {
	log.Printf("Downloading the plugin '%s' from %s", index.Name, index.Repository)
	tmpDir, err := os.MkdirTemp("", "trivy-plugin-index-*")
	if err != nil {
		return nil, xerrors.Errorf("failed to create a temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dst := filepath.Join(tmpDir, index.Name)

	pwd, err := os.Getwd()
	if err != nil {
		return nil, xerrors.Errorf("failed to get the current working directory: %w", err)
	}

	// Build the client
	client := &getter.Client{
		Ctx:     ctx,
		Src:     index.Repository,
		Dst:     dst,
		Pwd:     pwd,
		Getters: maps.Clone(getter.Getters),
		Mode:    getter.ClientModeAny,
	}

	if err = client.Get(); err != nil {
		return nil, xerrors.Errorf("download error: %w", err)
	}

	filePath := filepath.Join(dst, "plugin.yaml")
	f, err := os.Open(filePath)
	if err != nil {
		return nil, xerrors.Errorf("failed to open the file: %w", err)
	}
	defer f.Close()

	var plugin Plugin
	if err = yaml.NewDecoder(f).Decode(&plugin); err != nil {
		return nil, xerrors.Errorf("failed to decode the file: %w", err)
	}

	// Validating the manifest fields
	if err = validate.Struct(plugin); err != nil {
		return nil, xerrors.Errorf("validation error: %w", err)
	}

	return &plugin, nil
}
