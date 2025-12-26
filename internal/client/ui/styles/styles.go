package styles

import "github.com/charmbracelet/lipgloss"

var (
	PrimaryColor   = lipgloss.Color("#7C3AED")
	SecondaryColor = lipgloss.Color("#10B981") // Green for self
	BgColor        = lipgloss.Color("#1F2937")
	MutedColor     = lipgloss.Color("#9CA3AF")
	ErrorColor     = lipgloss.Color("#EF4444")
	ActiveBorder   = lipgloss.Color("#F59E0B") // Amber for focus

	// App container
	AppStyle = lipgloss.NewStyle().Padding(1, 2)

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(PrimaryColor).
			Padding(0, 1)

	ProfileStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#34D399")). // Emerald
			Bold(true)

	// Utils
	MutedStyle = lipgloss.NewStyle().
			Foreground(MutedColor)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ErrorColor).
			Bold(true)

	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(PrimaryColor).
			Padding(1, 2)

	// Sidebar styles
	SidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(PrimaryColor).
			Padding(0, 1).
			MarginRight(1)

	SelectedItemStyle = lipgloss.NewStyle().
				Foreground(SecondaryColor).
				Bold(true).
				PaddingLeft(1).
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(SecondaryColor)

	UnselectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(2) // Match indentation of selected items

	// Chat styles
	ChatWindowStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(PrimaryColor)

	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(MutedColor).
			Padding(0, 1)

	FooterStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(MutedColor).
			Padding(0, 1)

	// Message Bubbles
	OwnMessageStyle = lipgloss.NewStyle().
			Foreground(SecondaryColor)
	OtherMessageStyle = lipgloss.NewStyle().
				Foreground(PrimaryColor)

	AsciiArt = `
  ██████╗██╗     ██████╗ ███████╗███╗   ███╗███████╗ ██████╗ 
 ██╔════╝██║     ██╔══██╗╚══███╔╝████╗ ████║██╔════╝██╔════╝ 
 ██║     ██║     ██║  ██║  ███╔╝ ██╔████╔██║███████╗██║  ███╗
 ██║     ██║     ██║  ██║ ███╔╝  ██║╚██╔╝██║╚════██║██║   ██║
 ╚██████╗███████╗██████╔╝███████╗██║ ╚═╝ ██║███████║╚██████╔╝
  ╚═════╝╚══════╝╚═════╝ ╚══════╝╚═╝     ╚═╝╚══════╝ ╚═════╝ 
`
)
