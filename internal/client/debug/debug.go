package debug

import (
	"fmt"
	"os"
)

var Enabled = false

// Log writes to debug.log only if debug mode is enabled
func Log(format string, args ...interface{}) {
	if !Enabled {
		return
	}
	f, err := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, format+"\n", args...)
}

