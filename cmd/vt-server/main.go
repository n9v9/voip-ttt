package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	voipttt "github.com/n9v9/voip-ttt"
)

var callPhoneNumber string

func main() {
	rootCmd().Execute()
}

func rootCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "vt-server",
		Short: "vt-server serves the frontend and manages all connections from web clients and vt-clients",
		Run: func(cmd *cobra.Command, args []string) {
			run()
		},
	}

	cmd.Flags().StringVar(
		&callPhoneNumber,
		"call-phone-number",
		"",
		"Phone number that is displayed on the website",
	)

	cmd.MarkFlagRequired("call-phone-number")

	return cmd
}

func run() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		<-ch
		log.Logger.Info().Msg("Shutdown of application requested")
		cancel()
	}()

	server := voipttt.NewServer(voipttt.PhoneNumber(callPhoneNumber), ":8080", ":8081")
	server.Run(ctx, time.Second*5)
}
