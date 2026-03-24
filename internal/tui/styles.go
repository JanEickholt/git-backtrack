package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Background(lipgloss.Color("0")).Padding(0, 1)
	statusStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	selectedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	editStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	selectedBgStyle = lipgloss.NewStyle().Background(lipgloss.Color("20"))
	errorStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	successStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	labelStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
)
