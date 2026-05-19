package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRootCommandNew(t *testing.T) {
	cmd := NewRootCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "drun", cmd.Use)
}

func TestRootCommandHasSubcommands(t *testing.T) {
	cmd := NewRootCommand()
	assert.True(t, cmd.HasSubCommands())
}

func TestRootCommandVersion(t *testing.T) {
	cmd := NewRootCommand()

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "drun")
}

func TestRootCommandHelp(t *testing.T) {
	cmd := NewRootCommand()

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	assert.NoError(t, err)
	output := buf.String()

	assert.Contains(t, output, "drun")
	assert.Contains(t, output, "run")
	assert.Contains(t, output, "build")
}

func TestRootCommandFlags(t *testing.T) {
	cmd := NewRootCommand()

	// Check global flags exist
	configFlag := cmd.PersistentFlags().Lookup("config")
	assert.NotNil(t, configFlag)

	verboseFlag := cmd.PersistentFlags().Lookup("verbose")
	assert.NotNil(t, verboseFlag)

	quietFlag := cmd.PersistentFlags().Lookup("quiet")
	assert.NotNil(t, quietFlag)
}

func TestGlobalOptionsNew(t *testing.T) {
	opts := NewGlobalOptions()
	assert.NotNil(t, opts)
}

func TestGlobalOptionsDefaults(t *testing.T) {
	opts := NewGlobalOptions()
	assert.Empty(t, opts.ConfigPath)
	assert.False(t, opts.Verbose)
	assert.False(t, opts.Quiet)
}

func TestAppContextNew(t *testing.T) {
	ctx := NewAppContext()
	assert.NotNil(t, ctx)
}

func TestAppContextWithOptions(t *testing.T) {
	opts := NewGlobalOptions()
	opts.Verbose = true

	ctx := NewAppContext().WithOptions(opts)
	assert.True(t, ctx.Options.Verbose)
}

func TestVersionString(t *testing.T) {
	version := GetVersion()
	assert.NotEmpty(t, version)
}

func TestBuildInfo(t *testing.T) {
	info := GetBuildInfo()
	assert.NotEmpty(t, info.Version)
}
