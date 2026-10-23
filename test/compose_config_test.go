package test_test

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// The integration stack is built by docker compose on CI, where a build
// block whose dockerfile key is missing defaults to "Dockerfile" in the
// context directory. Container definitions here follow the podman
// convention ("Containerfile"), which podman accepts implicitly but
// docker compose does not — the mismatch only surfaces on the nightly
// main run, after merge. This test fails on any build block whose
// resolved file does not exist, in every environment that runs it.

type composeBuild struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

type composeService struct {
	Build *composeBuild `yaml:"build"`
}

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

func TestComposeBuildFilesResolve(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docker-compose.integration.yml"))
	if err != nil {
		t.Fatalf("read compose file: %v", err)
	}
	var c composeFile
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parse compose file: %v", err)
	}
	if len(c.Services) == 0 {
		t.Fatal("no services parsed — compose file schema drifted from docker compose v2")
	}
	for name, svc := range c.Services {
		if svc.Build == nil || svc.Build.Context == "" {
			continue
		}
		dockerfile := svc.Build.Dockerfile
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}
		path := filepath.Join("..", svc.Build.Context, dockerfile)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("service %q: build file %q does not exist (docker compose defaults the filename to Dockerfile; a Containerfile must be named via the dockerfile key)", name, path)
		}
	}
}
