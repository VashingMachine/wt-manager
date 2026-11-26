package terminal

import (
	"fmt"

	"github.com/VashingMachine/wt-manager/internal/shell"
)

func terminalTabScript(appName string, command string) string {
	commandLiteral := shell.AppleScriptString(command)
	appLiteral := shell.AppleScriptString(appName)
	return fmt.Sprintf(`set commandText to %s
tell application %s to activate
delay 0.1
tell application "System Events"
	tell process %s
		keystroke "t" using command down
		delay 0.2
		keystroke commandText
		key code 36
	end tell
end tell`, commandLiteral, appLiteral, appLiteral)
}
