package engine

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// OpenURL opens a web address in the player's browser: a link to the
// game's site, a bug tracker, a store page. Only http and https
// addresses are opened, so a link from untrusted data cannot run a
// program. It returns without waiting for the browser.
func OpenURL(url string) error {
	lower := strings.ToLower(url)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return fmt.Errorf("bunyip: OpenURL opens http and https addresses only, not %q", url)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("bunyip: open %s: %w", url, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
