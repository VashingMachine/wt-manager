package tui

import "github.com/charmbracelet/lipgloss"

var (
	appStyle         = lipgloss.NewStyle().Padding(1, 2)
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	subtleStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	mutedStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	successStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	infoStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	magentaStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("171"))
	purpleStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	errorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("204"))
	cardStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	focusStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(0, 1)
	headerCellStyle  = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(lipgloss.Color("252"))
	tableCellStyle   = lipgloss.NewStyle().Padding(0, 1)
	selectedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Bold(true)
	blurredRowStyle  = lipgloss.NewStyle().Background(lipgloss.Color("237"))
	armedRowStyle    = lipgloss.NewStyle().Background(lipgloss.Color("52")).Bold(true)
	statusStyle      = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("252"))
	statusErrorStyle = statusStyle.Foreground(lipgloss.Color("204"))
	statusOKStyle    = statusStyle.Foreground(lipgloss.Color("42"))
	modalStyle       = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("62")).Padding(1, 2).Width(64)

	badgeWorktreesStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("81"))
	badgePRRadarStyle   = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("171"))
	badgeTicketsStyle   = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("42"))
	chipStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("237")).Padding(0, 1)
	skeletonStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	keyHintKeyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)

	spinnerWorktreeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	spinnerPRStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("171"))
	spinnerAgentStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	spinnerMutationStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("204"))
)
