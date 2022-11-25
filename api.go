package voipttt

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
)

//go:embed frontend
var frontend embed.FS

// RoutePrivateAPIRegister is the route used by the private API to register
// clients, confirm their verification code and setup webhooks for further communication.
const RoutePrivateAPIRegister = "/register"

// privateAPI is a REST API that is used by internal services
// to register a calling client to create a connection between the clients
// web socket connections and its phone number.
type privateAPI struct {
	mux       *chi.Mux
	wsManager *webSocketManager
}

// newPrivate returns an initialized API instance.
func newPrivate(wsManager *webSocketManager) *privateAPI {
	api := &privateAPI{
		mux:       chi.NewMux(),
		wsManager: wsManager,
	}
	api.routes()
	return api
}

// routes hooks up all handlers with their respective routes.
func (pa *privateAPI) routes() {
	RegisterHTTPMiddleware(pa.mux)
	pa.mux.Post(RoutePrivateAPIRegister, pa.registerClient())
	pa.mux.Get("/ws-audio", pa.handleAudioStream())
}

// registerClient tries to verify a previously generated verification code.
// The handler expects the verification code and the calling client's phone
// number as URL parameters.
func (pa *privateAPI) registerClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RegisterClientRequest

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			hlog.FromRequest(r).Err(err).Msg("Failed to decode JSON body")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()

		if err := pa.wsManager.verifyCode(
			req.VerificationCode,
			req.ClientPhoneNumber,
			req.SelectDigitURL,
			req.HeartbeatURL,
			req.GameDoneURL,
			req.GameStartURL,
		); err != nil {
			hlog.FromRequest(r).Err(err).
				Uint64("verification_code", uint64(req.VerificationCode)).
				Msg("Verification code does not exist")
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func (pa *privateAPI) handleAudioStream() http.HandlerFunc {
	var upgrade websocket.Upgrader
	return func(w http.ResponseWriter, r *http.Request) {
		l := hlog.FromRequest(r).With().Str("addr", r.RemoteAddr).Logger()

		phoneNumber := strings.TrimSpace(r.URL.Query().Get("phoneNumber"))
		if phoneNumber == "" {
			l.Warn().Msg("Missing query parameter `phoneNumber` for web socket audio connection")
			return
		}

		conn, err := upgrade.Upgrade(w, r, nil)
		if err != nil {
			l.Err(err).Msg("Failed to accept audio stream web socket client")
			return
		}

		l.Info().Msg("Register incoming audio stream on web socket")
		pa.wsManager.registerAudioConnection(PhoneNumber(phoneNumber), conn)
	}
}

func (pa *privateAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pa.mux.ServeHTTP(w, r)
}

// publicAPI is the public, user facing API that is used to retrieve the
// static files and to establish a web socket connection.
type publicAPI struct {
	mux         *chi.Mux
	staticFiles fs.FS
	upgrader    websocket.Upgrader
	wsManager   *webSocketManager
}

// newPublic returns an initialized API instance.
func newPublic(wsManager *webSocketManager) *publicAPI {
	staticFiles, _ := fs.Sub(frontend, "frontend")
	api := &publicAPI{
		mux:         chi.NewMux(),
		staticFiles: staticFiles,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		wsManager: wsManager,
	}
	api.routes()
	return api
}

// routes hooks up all handlers with their respective routes.
func (pa *publicAPI) routes() {
	RegisterHTTPMiddleware(pa.mux)
	pa.mux.Get("/", pa.handleStaticFiles())
	pa.mux.Get("/js/*", pa.handleStaticFiles())
	pa.mux.Get("/css/*", pa.handleStaticFiles())
	pa.mux.Get("/ws", pa.handleWebSocket())
}

// handleStaticFiles serves the static HTML and CSS files.
func (pa *publicAPI) handleStaticFiles() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.FileServer(http.FS(pa.staticFiles)).ServeHTTP(w, r)
	}
}

// handleWebSocket upgrades an HTTP connection to a web socket connection
// that is used to communicate with the client throughout the game's lifetime.
func (pa *publicAPI) handleWebSocket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := pa.upgrader.Upgrade(w, r, nil)
		if err != nil {
			hlog.FromRequest(r).Err(err).Msg("Failed to upgrade to web socket connection for data stream")
			return
		}
		pa.wsManager.handleClient(conn)
	}
}

func (pa *publicAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pa.mux.ServeHTTP(w, r)
}

func RegisterHTTPMiddleware(mux *chi.Mux) {
	mux.Use(hlog.NewHandler(log.Logger))
	mux.Use(hlog.RemoteAddrHandler("addr"))
	mux.Use(hlog.MethodHandler("http_method"))
	mux.Use(hlog.URLHandler("url"))

	mux.Use(func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l := hlog.FromRequest(r)
			l.Info().Msg("New request")
			start := time.Now()
			handler.ServeHTTP(w, r)
			l.Info().
				TimeDiff("response_time_ms", time.Now(), start).
				Msg("Served request")
		})
	})
}
