package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func selectedActionFlag(cmd *cobra.Command, names ...string) (string, error) {
	var selected []string
	for _, name := range names {
		if !cmd.Flags().Changed(name) {
			continue
		}
		v, err := cmd.Flags().GetBool(name)
		if err != nil {
			return "", err
		}
		if v {
			selected = append(selected, "--"+name)
		}
	}
	if len(selected) > 1 {
		return "", fmt.Errorf("choose one action flag: %s", strings.Join(selected, ", "))
	}
	if len(selected) == 0 {
		return "", nil
	}
	return strings.TrimPrefix(selected[0], "--"), nil
}

func showActionHelp(cmd *cobra.Command, args []string, actions ...string) error {
	if len(args) > 0 {
		return fmt.Errorf("expected action flag: --%s", strings.Join(actions, "|--"))
	}
	return cmd.Help()
}
