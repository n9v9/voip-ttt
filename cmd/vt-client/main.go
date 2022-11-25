package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	voipttt "github.com/n9v9/voip-ttt"
)

const (
	webhookSelectDigitURL = "/digit"
	webhookHeartbeatURL   = "/heartbeat"
	webhookGameDoneURL    = "/done"
	webhookGameStart      = "/start"
)

var (
	addr       string
	serverAddr string
)

func main() {
	rootCmd().Execute()
}

func rootCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "vt-server",
		Short: "vt-server is enables you to play Tic-Tac-Toe over VoIP",
		Run: func(cmd *cobra.Command, args []string) {
			log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
			if err := run(); err != nil {
				log.Logger.Err(err).Msg("Client program failed")
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVar(
		&addr,
		"addr",
		"",
		"Address and port to listen on to receive webhooks",
	)
	cmd.Flags().StringVar(
		&serverAddr,
		"server-addr",
		"",
		"Address and port of the game server",
	)

	cmd.MarkFlagRequired("addr")
	cmd.MarkFlagRequired("server-addr")

	return cmd
}

func run() error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen tcp: %w", err)
	}
	defer listener.Close()

	// Important because are most likely using ":0" to get an available port
	// from the OS. This line then gets the concrete port we got from the OS.
	addr = listener.Addr().String()
	l := log.Logger.With().Str("listen_addr", addr).Logger()

	app := newApplication(l)

	l.Info().Msg("Waiting for verification code")
	verificationCode, err := app.promptVerificationCode()
	if err != nil {
		return fmt.Errorf("prompt verification code: %w", err)
	}

	l.Info().Msg("Waiting for phone number")
	phoneNumber, err := app.promptPhoneNumber()
	if err != nil {
		return fmt.Errorf("prompt phone number: %w", err)
	}

	mux := chi.NewMux()
	voipttt.RegisterHTTPMiddleware(mux)
	mux.Get(webhookSelectDigitURL, handleSelectDigitWebhook(app))
	mux.Get(webhookGameStart, handleGameStartWebhook(app, phoneNumber))
	mux.Get(webhookGameDoneURL, handleGameDoneWebhook())
	mux.Get(webhookHeartbeatURL, handleHeartbeatWebhook())

	l.Info().Msg("Calling server to register application")
	if err := register(verificationCode, phoneNumber); err != nil {
		return fmt.Errorf("register application: %w", err)
	}
	l.Info().Msg("Application is registered")

	return http.Serve(listener, mux)
}

func handleSelectDigitWebhook(app *application) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := hlog.FromRequest(r)
		l.Info().Msg("Received get digit request")

		var (
			digit int
			err   error
		)

		for {
			digit, err = app.promptDigit()
			if err != nil {
				l.Err(err).Msg("Failed to get digit")
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if digit < 1 || digit > 9 {
				l.Warn().Int("digit", digit).Msg("Invalid digit, digit must be between 1 and 9")
				continue
			}
			break
		}

		data := voipttt.ReceiveDigitRequest{Digit: digit}
		if err := json.NewEncoder(w).Encode(&data); err != nil {
			l.Err(err).
				Int("digit", digit).
				Interface("data", data).
				Msg("Failed to respond with selected digit")
			os.Exit(1)
		}
	}
}

func handleGameStartWebhook(app *application, phoneNumber voipttt.PhoneNumber) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := hlog.FromRequest(r)
		l.Info().Msg("Received game start notification. Start audio fork.")

		if err := app.agi.StartAudioFork(phoneNumber); err != nil {
			l.Err(err).Msg("Failed to start audio fork")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		return
	}
}

func handleGameDoneWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hlog.FromRequest(r).Info().Msg("Received game done notification")
		w.WriteHeader(http.StatusOK)
		os.Exit(0)
	}
}

func handleHeartbeatWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hlog.FromRequest(r).Info().Msg("Received heartbeat check")
		w.WriteHeader(http.StatusOK)
	}
}

// register executes the registration process to verify the given code and hook up
// the necessary callbacks.
func register(verificationCode voipttt.VerificationCode, phoneNumber voipttt.PhoneNumber) error {
	url := fmt.Sprintf("http://%s%s", serverAddr, voipttt.RoutePrivateAPIRegister)
	data := voipttt.RegisterClientRequest{
		VerificationCode:  verificationCode,
		ClientPhoneNumber: phoneNumber,
		SelectDigitURL:    voipttt.WebhookURL(fmt.Sprintf("http://%s%s", addr, webhookSelectDigitURL)),
		HeartbeatURL:      voipttt.WebhookURL(fmt.Sprintf("http://%s%s", addr, webhookHeartbeatURL)),
		GameDoneURL:       voipttt.WebhookURL(fmt.Sprintf("http://%s%s", addr, webhookGameDoneURL)),
		GameStartURL:      voipttt.WebhookURL(fmt.Sprintf("http://%s%s", addr, webhookGameStart)),
	}

	body, err := json.Marshal(&data)
	if err != nil {
		return fmt.Errorf("marshal register JSON: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send POST request: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected HTTP status code 200 OK, but got %d", resp.StatusCode)
	}
	return nil
}
