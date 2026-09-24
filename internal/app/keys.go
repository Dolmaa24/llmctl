package app

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	NextPane key.Binding
	PrevPane key.Binding
	Switch   key.Binding
	Quit     key.Binding
}

var Keys = KeyMap{
	NextPane: key.NewBinding(key.WithKeys("tab")),
	PrevPane: key.NewBinding(key.WithKeys("shift+tab")),
	Switch:   key.NewBinding(key.WithKeys("s")),
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c")),
}
