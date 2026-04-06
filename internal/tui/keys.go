package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up           key.Binding
	Down         key.Binding
	Edit         key.Binding
	Drop         key.Binding
	Combine      key.Binding
	Undo         key.Binding
	Reset        key.Binding
	Apply        key.Binding
	Quit         key.Binding
	Confirm      key.Binding
	Cancel       key.Binding
	Tab          key.Binding
	ShiftTab     key.Binding
	Select       key.Binding
	BatchEdit    key.Binding
	SwitchBranch key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Edit, k.Drop, k.Combine, k.Undo, k.Select, k.BatchEdit, k.SwitchBranch, k.Apply, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down},
		{k.Edit, k.Drop, k.Combine, k.Undo, k.Select},
		{k.BatchEdit, k.Reset},
		{k.SwitchBranch, k.Apply},
		{k.Quit},
	}
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:           key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:         key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Edit:         key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Drop:         key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "drop")),
		Combine:      key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fold")),
		Undo:         key.NewBinding(key.WithKeys("u", "ctrl+z"), key.WithHelp("u", "undo")),
		Reset:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reset")),
		Apply:        key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "apply")),
		Quit:         key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Confirm:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		Cancel:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Tab:          key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
		ShiftTab:     key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev")),
		Select:       key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select")),
		BatchEdit:    key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "batch edit")),
		SwitchBranch: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "switch branch")),
	}
}
