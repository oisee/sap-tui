package main

// The key model the interactive session acts on. tcell decodes the real
// terminal (see tcell.go) and maps its events onto these; screenState.handleKey
// and session.handleKey consume them, independent of the backend.

type keyKind int

const (
	keyRune keyKind = iota
	keyEnter
	keyTab
	keyBackTab
	keyBackspace
	keyDelete
	keyLeft
	keyRight
	keyUp
	keyDown
	keyHome
	keyEnd
	keyCtrlHome
	keyCtrlEnd
	keyPgUp
	keyPgDn
	keyEsc
	keyCtrlC
	keyCtrlO
	keyCtrlP
	keyFunc // F1..F24, number in n
)

type key struct {
	kind keyKind
	r    rune
	n    int
}
