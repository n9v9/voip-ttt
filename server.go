package voipttt

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Server manages the api.publicAPI and api.privateAPI instances.
type Server struct {
	publicAPI      *publicAPI
	publicAPIAddr  string
	privateAPI     *privateAPI
	privateAPIAddr string
	wsManager      *webSocketManager
}

// NewServer returns an initialized instance whose APIs will
// listen on the given addresses.
func NewServer(callPhoneNumber PhoneNumber, publicAPIAddr, privateAPIAddr string) *Server {
	wsManager := newManager(callPhoneNumber)
	return &Server{
		publicAPI:      newPublic(wsManager),
		privateAPI:     newPrivate(wsManager),
		publicAPIAddr:  publicAPIAddr,
		privateAPIAddr: privateAPIAddr,
		wsManager:      wsManager,
	}
}

// Run starts the APIs.
// shutdownTimeout specifies the timeout to wait when shutting down the APIs
// in case the context is cancelled.
func (s *Server) Run(ctx context.Context, shutdownTimeout time.Duration) {
	publicServer := http.Server{
		Addr:    s.publicAPIAddr,
		Handler: s.publicAPI,
	}
	privateServer := http.Server{
		Addr:    s.privateAPIAddr,
		Handler: s.privateAPI,
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()

		log.Info().Msg("Shutdown public and private APIs")
		ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		shutdown := func(server *http.Server, api string) {
			l := log.With().Str("api_type", api).Logger()
			if err := server.Shutdown(ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					l.Warn().Msg("Forceful shutdown of API because set timeout was reached")
				} else if !errors.Is(err, http.ErrServerClosed) {
					l.Err(err).Msg("Forceful shutdown of API failed")
				}
			} else {
				l.Info().Msg("Shutdown of API successful")
			}
		}

		shutdown(&publicServer, "public")
		shutdown(&privateServer, "private")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.wsManager.runClientMatcher(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		l := log.With().Str("addr", s.publicAPIAddr).Logger()
		l.Info().Msg("Start public API")
		if err := publicServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			l.Err(err).Msg("Failed to run public API")
		}
	}()

	l := log.With().Str("addr", s.privateAPIAddr).Logger()
	l.Info().Msg("Start private API")
	if err := privateServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		l.Err(err).Msg("Failed to run private API")
	}

	wg.Wait()
}
