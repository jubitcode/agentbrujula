package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"drun/internal/pathmap"
)

// PathMapResult represents the result of a path mapping operation.
type PathMapResult struct {
	Container string `json:"container"`
	Host      string `json:"host"`
}

func NewPathmapCommand(appCtx *AppContext) *cobra.Command {
	var (
		reverse    bool
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "pathmap <path>",
		Short: "Translate paths between container and host",
		Long: `Translate file paths between the container and host filesystem.

By default, converts a container path to a host path.
Use --reverse to convert a host path to a container path.`,
		Example: `  drun pathmap /workspace/main.go       # Container to host
  drun pathmap --reverse ~/project/main.go  # Host to container
  drun pathmap --json /workspace/main.go    # JSON output`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPathmap(appCtx, args[0], reverse, jsonOutput)
		},
	}

	cmd.Flags().BoolVarP(&reverse, "reverse", "r", false, "Convert host path to container path")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

func runPathmap(appCtx *AppContext, path string, reverse bool, jsonOutput bool) error {
	if appCtx.Config == nil {
		return fmt.Errorf("no .drun.yaml config found in current directory")
	}

	// Build mounts from config
	mounts := make([]pathmap.Mount, 0, len(appCtx.Config.Mounts))
	for _, m := range appCtx.Config.Mounts {
		mounts = append(mounts, pathmap.Mount{
			Source: m.Source,
			Target: m.Target,
		})
	}

	if len(mounts) == 0 {
		return fmt.Errorf("no mounts configured")
	}

	mapper := pathmap.NewMapper(mounts, pathmap.WithWorkdir(appCtx.Config.Workdir))

	var result PathMapResult
	var err error

	if reverse {
		// Host to container
		containerPath, err := mapper.ToContainer(path)
		if err != nil {
			return fmt.Errorf("path not in any mount: %s", path)
		}
		result = PathMapResult{
			Host:      path,
			Container: containerPath,
		}
	} else {
		// Container to host
		hostPath, err := mapper.ToHost(path)
		if err != nil {
			return fmt.Errorf("path not in any mount: %s", path)
		}
		result = PathMapResult{
			Container: path,
			Host:      hostPath,
		}
	}

	if err != nil {
		return err
	}

	if jsonOutput {
		output, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(output))
	} else {
		if reverse {
			fmt.Println(result.Container)
		} else {
			fmt.Println(result.Host)
		}
	}

	return nil
}
