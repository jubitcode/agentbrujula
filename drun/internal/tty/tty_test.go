package tty

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTTY(t *testing.T) {
	tty := New()
	// Result depends on test environment (CI vs interactive)
	result := tty.IsTerminal()
	t.Logf("IsTerminal: %v", result)
	// Just ensure it doesn't panic and returns a boolean
	assert.IsType(t, true, result)
}

func TestIsTTYWithPipe(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()

	tty := NewFromFiles(r, w)
	assert.False(t, tty.IsTerminal())
}

func TestIsTTYWithFile(t *testing.T) {
	f, err := os.CreateTemp("", "tty-test")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	defer f.Close()

	tty := NewFromFiles(f, f)
	assert.False(t, tty.IsTerminal())
}

func TestGetSize(t *testing.T) {
	tty := New()
	if !tty.IsTerminal() {
		t.Skip("Not running in terminal")
	}

	size, err := tty.Size()
	require.NoError(t, err)
	assert.Greater(t, size.Rows, uint16(0))
	assert.Greater(t, size.Cols, uint16(0))
}

func TestGetSizeNonTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()

	tty := NewFromFiles(r, w)
	size, err := tty.Size()
	// Either returns error or zero size
	if err == nil {
		assert.Equal(t, uint16(0), size.Rows)
		assert.Equal(t, uint16(0), size.Cols)
	}
}

func TestRawMode(t *testing.T) {
	tty := New()
	if !tty.IsTerminal() {
		t.Skip("Not running in terminal")
	}

	err := tty.SetRaw()
	require.NoError(t, err)

	err = tty.Restore()
	require.NoError(t, err)
}

func TestRawModeNonTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()

	tty := NewFromFiles(r, w)
	err = tty.SetRaw()
	assert.Error(t, err)
}

func TestRestoreWithoutRaw(t *testing.T) {
	tty := New()
	// Restore without SetRaw should be safe
	err := tty.Restore()
	assert.NoError(t, err)
}

func TestDoubleRaw(t *testing.T) {
	tty := New()
	if !tty.IsTerminal() {
		t.Skip("Not running in terminal")
	}

	err := tty.SetRaw()
	require.NoError(t, err)

	// Second SetRaw should be safe
	err = tty.SetRaw()
	assert.NoError(t, err)

	// Restore should still work
	err = tty.Restore()
	assert.NoError(t, err)
}

func TestDoubleRestore(t *testing.T) {
	tty := New()
	if !tty.IsTerminal() {
		t.Skip("Not running in terminal")
	}

	_ = tty.SetRaw()
	_ = tty.Restore()

	// Second restore should be safe
	err := tty.Restore()
	assert.NoError(t, err)
}

func TestInOut(t *testing.T) {
	tty := New()
	assert.Equal(t, os.Stdin, tty.In)
	assert.Equal(t, os.Stdout, tty.Out)
}

func TestCustomFiles(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()

	tty := NewFromFiles(r, w)
	assert.Equal(t, r, tty.In)
	assert.Equal(t, w, tty.Out)
}

func TestConvenienceFunctions(t *testing.T) {
	// IsTTY should not panic
	_ = IsTTY()

	// GetSize may error but should not panic
	_, _ = GetSize()
}

func TestSizeStruct(t *testing.T) {
	size := Size{
		Rows: 24,
		Cols: 80,
	}
	assert.Equal(t, uint16(24), size.Rows)
	assert.Equal(t, uint16(80), size.Cols)
}
