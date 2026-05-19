package presets

import (
	"embed"
	"fmt"
)

//go:embed *.yaml
var presetFS embed.FS

func Load(name string) ([]byte, error) {
	filename := name + ".yaml"
	data, err := presetFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("preset %q not found: %w", name, err)
	}
	return data, nil
}

func Available() []string {
	return []string{"go", "node", "python", "rust", "odin"}
}

func Exists(name string) bool {
	filename := name + ".yaml"
	_, err := presetFS.ReadFile(filename)
	return err == nil
}
