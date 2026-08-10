package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/kaypoh/internal/api"
)

func newAPICommand(runtime *runtime) *cobra.Command {
	command := &cobra.Command{Use: "api", Short: "Serve the local versioned HTTP API"}
	var address string
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Serve the API at the configured loopback address",
		RunE: func(command *cobra.Command, _ []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			cfg := service.Config()
			if address != "" {
				cfg.API.Address = address
			}
			server, err := api.New(service, cfg)
			if err != nil {
				return err
			}
			go func() {
				<-command.Context().Done()
				shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(shutdownContext)
			}()
			return server.Serve()
		},
	}
	serve.Flags().StringVar(&address, "address", "", "listen address override; loopback unless config enables remote access")
	command.AddCommand(serve)
	return command
}
