package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/discord"
)

func discordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discord",
		Short: "Manage Discord chat transport",
	}
	register := &cobra.Command{
		Use:   "register",
		Short: "Register the /onibi Discord slash command",
		RunE:  runDiscordRegister,
	}
	register.Flags().String("app-id", "", "Discord application id (default ONIBI_DISCORD_APPLICATION_ID or bot application)")
	register.Flags().String("guild-id", "", "Discord guild id for fast guild-scoped registration")
	register.Flags().Bool("json", false, "print JSON")
	cmd.AddCommand(register)
	return cmd
}

func runDiscordRegister(cmd *cobra.Command, _ []string) error {
	token := strings.TrimSpace(os.Getenv("ONIBI_DISCORD_TOKEN"))
	if token == "" {
		return fmt.Errorf("ONIBI_DISCORD_TOKEN required")
	}
	appID, _ := cmd.Flags().GetString("app-id")
	if strings.TrimSpace(appID) == "" {
		appID = strings.TrimSpace(os.Getenv("ONIBI_DISCORD_APPLICATION_ID"))
	}
	guildID, _ := cmd.Flags().GetString("guild-id")
	if strings.TrimSpace(guildID) == "" {
		guildID = strings.TrimSpace(os.Getenv("ONIBI_DISCORD_GUILD_ID"))
	}
	got, err := discord.New(token).RegisterOnibiCommand(cmd.Context(), appID, guildID)
	if err != nil {
		return err
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(got)
	}
	scope := "global"
	if strings.TrimSpace(guildID) != "" {
		scope = "guild " + strings.TrimSpace(guildID)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "registered /%s (%s)\n", got.Name, scope)
	return nil
}
