package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	voipttt "github.com/n9v9/voip-ttt"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ch := make(chan os.Signal)
		signal.Notify(ch, os.Interrupt)
		<-ch
		log.Logger.Info().Msg("Shutdown of application requested")
		cancel()
	}()

	server := voipttt.NewServer(":8080", ":8081")
	server.Run(ctx, time.Second*5)
}
