package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/built-for-devs/quiver/internal/apperr"
)

// confirm gates an immediate mutation. With --yes it proceeds. On an
// interactive terminal it prompts. Otherwise (scripts, CI, pipes) it refuses,
// so guarded actions never run silently.
func confirm(cmd *cobra.Command, yes bool, action string) error {
	if yes {
		return nil
	}
	if !isTerminal(cmd.InOrStdin()) || g.json {
		return apperr.Usage("refusing to %s without --yes (non-interactive)", action)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "About to %s. Continue? [y/N] ", action)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	return apperr.Usage("aborted")
}
