package voipttt

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moby/moby/pkg/namesgenerator"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// errCodeNotExist is a sentinel error representing the scenario that an entered code
// is not managed by the webSocketManager.
var errCodeNotExist = errors.New("the given codes does not exist")

type webSocketMessage string

const (
	messageSendCode            webSocketMessage = "SEND_CODE"
	messageSendWaitForOpponent webSocketMessage = "WAIT_FOR_OPPONENT"
	messageOpponentReady       webSocketMessage = "OPPONENT_READY"
	messageTurnInfo            webSocketMessage = "TURN_INFO"
	messageGameDone            webSocketMessage = "GAME_DONE"
)

type webSocketData struct {
	Type webSocketMessage `json:"type"`
	Data any              `json:"data"`
}

type dataVerificationCode struct {
	Code            VerificationCode `json:"code"`
	CallPhoneNumber PhoneNumber      `json:"callPhoneNumber"`
}

type dataWaitForOpponent struct {
	GameRoomName      string      `json:"gameRoomName"`
	PlayerPhoneNumber PhoneNumber `json:"playerPhoneNumber"`
}

type dataOpponentReady struct {
	OpponentPhoneNumber AnonymizedPhoneNumber `json:"opponentPhoneNumber"`
	PlayerHasFirstTurn  bool                  `json:"playerHasFirstTurn"`
}

type dataTurnInfo struct {
	SelectedDigit int  `json:"selectedDigit"`
	IsPlayer      bool `json:"isPlayer"`
}

type dataGameDone struct {
	HasWinner      bool `json:"hasWinner"`
	IsPlayerWinner bool `json:"isPlayerWinner"`
}

type webSocketClient struct {
	conn          *websocket.Conn
	connMu        *sync.Mutex     // Used to serialize concurrent write access.
	incomingAudio *websocket.Conn // Incoming connection from asterisk-audio-fork.
	phoneNumber   PhoneNumber
	getDigitURL   WebhookURL
	heartbeatURL  WebhookURL
	gameDoneURL   WebhookURL
	gameStartURL  WebhookURL
	log           zerolog.Logger
}

// sendCode sends the verification code to the client.
func (wsc *webSocketClient) sendCode(code VerificationCode, callPhoneNumber PhoneNumber) error {
	return wsc.sendData(webSocketData{
		Type: messageSendCode,
		Data: &dataVerificationCode{
			Code:            code,
			CallPhoneNumber: callPhoneNumber,
		},
	})
}

// sendWaitForOpponent notifies the client that they are ready but the Server
// is waiting to find an opponent for them.
func (wsc *webSocketClient) sendWaitForOpponent(gameRoomName string) error {
	return wsc.sendData(webSocketData{
		Type: messageSendWaitForOpponent,
		Data: &dataWaitForOpponent{
			GameRoomName:      gameRoomName,
			PlayerPhoneNumber: wsc.phoneNumber,
		},
	})
}

// sendOpponentReady notifies the client that an opponent has been found.
// It sends the opponent's phone number as well as whether the player we send it to
// has the first turn.
func (wsc *webSocketClient) sendOpponentReady(phoneNumber PhoneNumber, playerHasFirstTurn bool) error {
	return wsc.sendData(webSocketData{
		Type: messageOpponentReady,
		Data: &dataOpponentReady{
			OpponentPhoneNumber: phoneNumber.Anonymized(),
			PlayerHasFirstTurn:  playerHasFirstTurn,
		},
	})
}

// sendTurnInfo sends information about the selected digit and who selected it.
func (wsc *webSocketClient) sendTurnInfo(selectedDigit int, isPlayer bool) error {
	return wsc.sendData(webSocketData{
		Type: messageTurnInfo,
		Data: &dataTurnInfo{
			SelectedDigit: selectedDigit,
			IsPlayer:      isPlayer,
		},
	})
}

// sendGameDone notifies the client that the game has ended and how it has ended.
func (wsc *webSocketClient) sendGameDone(hasWinner, isPlayerWinner bool) error {
	return wsc.sendData(webSocketData{
		Type: messageGameDone,
		Data: &dataGameDone{
			HasWinner:      hasWinner,
			IsPlayerWinner: isPlayerWinner,
		},
	})
}

// sendData is the generic send method and should only be called by higher level send methods.
func (wsc *webSocketClient) sendData(data webSocketData) error {
	wsc.connMu.Lock()
	defer wsc.connMu.Unlock()

	wsc.log.Info().
		Str("data_type", string(data.Type)).
		Interface("data", data).
		Msg("Send data to client")
	if err := wsc.conn.WriteJSON(data); err != nil {
		return fmt.Errorf(
			"send data of type %q to client %s: %w",
			data.Type,
			wsc.conn.RemoteAddr(),
			err,
		)
	}
	return nil
}

// sendAudio sends raw PCM audio data as a web socket binary frame to the client.
func (wsc *webSocketClient) sendAudio(data []byte) error {
	wsc.connMu.Lock()
	defer wsc.connMu.Unlock()
	if err := wsc.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("send audio to client %s: %v", wsc.conn.RemoteAddr(), err)
	}
	return nil
}

// webSocketManager manages all web socket connections to play the game.
// Each method is concurrency safe.
type webSocketManager struct {
	codeCounter atomic.Uint64

	pendingAudioConns   map[PhoneNumber]*websocket.Conn
	pendingAudioConnsMu *sync.Mutex

	// Mapping from a verification code to a client. Contains all clients that
	// have requested a code, but have not yet verified it.
	waitingForCode   map[VerificationCode]*webSocketClient
	waitingForCodeMu *sync.Mutex

	// Clients that have verified their code and are now waiting
	// to be matched with an opponent.
	lookingForMatch chan *webSocketClient

	// Phone number that is sent to the client, so the client knows which number to call.
	callPhoneNumber PhoneNumber
}

// newManager matches two web socket connections so that they can
// play against each other.
func newManager(callPhoneNumber PhoneNumber) *webSocketManager {
	return &webSocketManager{
		codeCounter:         atomic.Uint64{},
		pendingAudioConns:   map[PhoneNumber]*websocket.Conn{},
		pendingAudioConnsMu: new(sync.Mutex),
		waitingForCode:      map[VerificationCode]*webSocketClient{},
		waitingForCodeMu:    new(sync.Mutex),
		lookingForMatch:     make(chan *webSocketClient),
		callPhoneNumber:     callPhoneNumber,
	}
}

func (wsm *webSocketManager) registerAudioConnection(phoneNumber PhoneNumber, conn *websocket.Conn) {
	wsm.pendingAudioConnsMu.Lock()
	defer wsm.pendingAudioConnsMu.Unlock()
	wsm.pendingAudioConns[phoneNumber] = conn
}

func (wsm *webSocketManager) removeAudioConnection(phoneNumber PhoneNumber) {
	wsm.pendingAudioConnsMu.Lock()
	defer wsm.pendingAudioConnsMu.Unlock()
	delete(wsm.pendingAudioConns, phoneNumber)
}

// handleClient takes over the communication with the client and handles signalling,
// matching them with another client and playing the game.
func (wsm *webSocketManager) handleClient(conn *websocket.Conn) {
	client := &webSocketClient{
		conn:   conn,
		connMu: new(sync.Mutex),
		log:    log.Logger.With().Str("websocket_addr", conn.RemoteAddr().String()).Logger(),
	}

	client.log.Info().Msg("Handle new client")

	code := VerificationCode(wsm.codeCounter.Add(1))
	if err := client.sendCode(code, wsm.callPhoneNumber); err != nil {
		client.log.Err(err).Uint64("code", uint64(code)).Msg("Failed to send code to client")
		_ = client.conn.Close()
		return
	}

	wsm.waitingForCodeMu.Lock()
	wsm.waitingForCode[code] = client
	wsm.waitingForCodeMu.Unlock()
}

// verifyCode notifies the webSocketManager that the client has called the displayed
// phone number and entered the displayed code. It then sets up the given
// webhooks which are used to notify the calling client of events like prompting
// for a new digit or notifying that the game is done.
//
// If the code exists, the frontend will be notified.
//
// If the given code does not exist, maybe because of a timeout, then errCodeNotExist is returned.
func (wsm *webSocketManager) verifyCode(
	code VerificationCode,
	clientPhoneNumber PhoneNumber,
	selectDigitURL WebhookURL,
	heartbeatURL WebhookURL,
	gameDoneURL WebhookURL,
	gameStartURL WebhookURL,
) error {
	wsm.waitingForCodeMu.Lock()
	defer wsm.waitingForCodeMu.Unlock()

	client, ok := wsm.waitingForCode[code]
	if !ok {
		return errCodeNotExist
	}

	client.phoneNumber = clientPhoneNumber
	client.getDigitURL = selectDigitURL
	client.heartbeatURL = heartbeatURL
	client.gameDoneURL = gameDoneURL
	client.gameStartURL = gameStartURL

	client.log.Info().Uint64("code", uint64(code)).Msg("Client verified code")

	delete(wsm.waitingForCode, code)
	wsm.lookingForMatch <- client

	return nil
}

// runClientMatcher receives clients over the given channel and matches two clients so that they
// can play a game against each other.
func (wsm *webSocketManager) runClientMatcher(ctx context.Context) {
	log.Logger.Info().Msg("Started web socket client matcher")
	defer log.Logger.Info().Msg("Stopped web socket client matcher")

	sendWaitMessage := func(client **webSocketClient, roomName string) {
		(*client).log.Info().Msg("Looking for opponent to match with client")

		// TODO: If this blocks then the whole method blocks, should be maybe in an extra goroutine.
		if err := (*client).sendWaitForOpponent(roomName); err != nil {
			(*client).log.Err(err).Msg("Failed to send waiting for opponent notification")
			_ = (*client).conn.Close()
			// This sets either first or second to nil, effectively continuing the search
			// for a matching client.
			*client = nil
		}
	}

	var first, second *webSocketClient

	getRandomRoomName := func() string {
		name := strings.Fields(strings.ReplaceAll(namesgenerator.GetRandomName(0), "_", " "))
		adjective := strings.ToUpper(string(name[0][0])) + name[0][1:]
		person := strings.ToTitle(string(name[1][0])) + name[1][1:]
		return fmt.Sprintf("%s %s", adjective, person)
	}

	rand.Seed(time.Now().Unix())
	roomName := getRandomRoomName()

	for {
		select {
		case <-ctx.Done():
			return
		case client, ok := <-wsm.lookingForMatch:
			if !ok {
				return
			}
			if first == nil {
				first = client
				sendWaitMessage(&first, roomName)
			} else if second == nil {
				second = client
				sendWaitMessage(&second, roomName)
			}
			// Matched two clients.
			// Start their game and wait for the next two players.
			if first != nil && second != nil {
				gameLogger := log.Logger.With().
					Str("first_addr", first.conn.RemoteAddr().String()).
					Str("second_addr", second.conn.RemoteAddr().String()).
					Logger()

				gameLogger.Info().Msg("Matched clients")

				g := game{
					playerOne: first,
					playerTwo: second,
					log:       gameLogger,
				}

				var wg sync.WaitGroup
				wg.Add(2)
				wsm.startAudioStream(ctx, &wg, g.playerOne)
				wsm.startAudioStream(ctx, &wg, g.playerTwo)
				wg.Wait()

				go func() {
					g.run()
					wsm.removeAudioConnection(g.playerOne.phoneNumber)
					wsm.removeAudioConnection(g.playerTwo.phoneNumber)
				}()

				first = nil
				second = nil
				roomName = getRandomRoomName()
			}
		}
	}
}

func (wsm *webSocketManager) startAudioStream(
	ctx context.Context,
	wg *sync.WaitGroup,
	client *webSocketClient,
) {
	defer wg.Done()

	if _, err := http.Get(string(client.gameStartURL)); err != nil {
		client.log.Err(err).
			Str("webhook", string(client.gameStartURL)).
			Str("http_method", "GET").
			Msg("Failed HTTP request to game start webhook")
	}

	for {
		wsm.pendingAudioConnsMu.Lock()
		audioConn, ok := wsm.pendingAudioConns[client.phoneNumber]
		wsm.pendingAudioConnsMu.Unlock()

		if ok {
			client.incomingAudio = audioConn
			client.log = client.log.With().
				Str("incoming_audio", audioConn.RemoteAddr().String()).
				Logger()
			client.log.Info().Msg("Matched client web socket with incoming audio web socket")
			break
		}

		// TODO: Use sync.Cond for signalling instead of polling.
		client.log.Info().Msg("Did not find audio web socket connection. Sleeping the trying again.")

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Millisecond * 200):
		}
	}
}
