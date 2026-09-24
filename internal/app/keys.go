package app

import "github.com/charmbracelet/bubbles/key"

// KeyMap splits bindings by where they apply. While the composer has focus,
// letters are text: "q" must type a q, not quit. So Quit and Switch only act
// outside the composer, and the bindings that work everywhere use keys that
// cannot be typed as text.
type KeyMap struct {
	// Anywhere, including the composer.
	NextPane  key.Binding
	PrevPane  key.Binding
	ForceQuit key.Binding
	Cancel    key.Binding

	// Outside the composer only.
	Quit   key.Binding
	Switch key.Binding

	// In the provider list only.
	Add  key.Binding
	Edit key.Binding
}

var Keys = KeyMap{
	NextPane:  key.NewBinding(key.WithKeys("tab")),
	PrevPane:  key.NewBinding(key.WithKeys("shift+tab")),
	ForceQuit: key.NewBinding(key.WithKeys("ctrl+c")),
	Cancel:    key.NewBinding(key.WithKeys("esc")),
	Quit:      key.NewBinding(key.WithKeys("q")),
	Switch:    key.NewBinding(key.WithKeys("s")),
	Add:       key.NewBinding(key.WithKeys("a")),
	Edit:      key.NewBinding(key.WithKeys("e")),
}
