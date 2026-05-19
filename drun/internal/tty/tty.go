package tty

import (
	"errors"
	"os"

	"golang.org/x/term"
)

var ErrNotTerminal = errors.New("not a terminal")

type Size struct {
	Rows uint16
	Cols uint16
}

type TTY struct {
	In       *os.File
	Out      *os.File
	oldState *term.State
}

func New() *TTY {
	return &TTY{
		In:  os.Stdin,
		Out: os.Stdout,
	}
}

func NewFromFiles(in, out *os.File) *TTY {
	return &TTY{In: in, Out: out}
}

func (t *TTY) IsTerminal() bool {
	return term.IsTerminal(int(t.In.Fd()))
}

func (t *TTY) Size() (*Size, error) {
	if !t.IsTerminal() {
		return &Size{}, nil
	}

	width, height, err := term.GetSize(int(t.Out.Fd()))
	if err != nil {
		return nil, err
	}

	return &Size{
		Rows: uint16(height),
		Cols: uint16(width),
	}, nil
}

func (t *TTY) SetRaw() error {
	if !t.IsTerminal() {
		return ErrNotTerminal
	}

	state, err := term.MakeRaw(int(t.In.Fd()))
	if err != nil {
		return err
	}

	if t.oldState == nil {
		t.oldState = state
	}

	return nil
}

func (t *TTY) Restore() error {
	if t.oldState == nil {
		return nil
	}

	err := term.Restore(int(t.In.Fd()), t.oldState)
	if err != nil {
		return err
	}

	t.oldState = nil
	return nil
}

func IsTTY() bool {
	return New().IsTerminal()
}

func GetSize() (*Size, error) {
	return New().Size()
}
