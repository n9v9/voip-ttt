package voipttt

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// game represents an ongoing game between two clients.
type game struct {
	playerOne *webSocketClient
	playerTwo *webSocketClient
	log       zerolog.Logger
}

// sendOpponentReadyMessage notifies the given web socket, that
// it's opponent is ready and the game can transition to the playing state.
func (g *game) sendOpponentReadyMessage(
	client *webSocketClient,
	opponentPhoneNumber PhoneNumber,
	hasFirstTurn bool,
) bool {
	if err := client.sendOpponentReady(opponentPhoneNumber, hasFirstTurn); err != nil {
		// log.Printf(
		// 	"Failed to notify client %s that opponent is ready: %v\n",
		// 	client.conn.RemoteAddr(),
		// 	err,
		// )
		return false
	}
	return true
}

// sendTurnInfo sends information about the latest turn to the given client.
// isPlayer determines whether the client is the one who executed the turn.
func (g *game) sendTurnInfo(client *webSocketClient, selectedDigit int, isPlayer bool) bool {
	if err := client.sendTurnInfo(selectedDigit, isPlayer); err != nil {
		client.log.Err(err).Msg("Failed to send turn info")
		return false
	}
	return true
}

func (g *game) sendGameDone(client *webSocketClient, hasWinner, isPlayerWinner bool) {
	if err := client.sendGameDone(hasWinner, isPlayerWinner); err != nil {
		client.log.Err(err).Msg("Failed to send game done info")
	}
}

func (g *game) getDigitFromClient(client *webSocketClient) (int, bool) {
	l := client.log.With().
		Str("webhook", string(client.getDigitURL)).
		Str("http_method", "GET").
		Logger()

	resp, err := http.Get(string(client.getDigitURL))
	if err != nil {
		l.Err(err).Msg("Failed HTTP request to get digit from webhook")
		return 0, false
	}
	defer resp.Body.Close()

	var data ReceiveDigitRequest
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		l.Err(err).Msg("Failed to decode JSON response from webhook")
		return 0, false
	}

	return data.Digit, true
}

func (g *game) callGameDoneWebHook() {
	send := func(client *webSocketClient) {
		if _, err := http.Get(string(client.gameDoneURL)); err != nil {
			client.log.Err(err).
				Str("webhook", string(client.gameDoneURL)).
				Str("http_method", "GET").
				Msg("Failed HTTP request to game done webhook")
		}
	}
	send(g.playerOne)
	send(g.playerTwo)
}

// runGame runs the game between the two web socket clients.
func (g *game) run() {
	start := time.Now()

	go g.copyAudioStream(g.playerOne, g.playerTwo)
	go g.copyAudioStream(g.playerTwo, g.playerOne)

	defer func() {
		g.callGameDoneWebHook()

		_ = g.playerOne.incomingAudio.Close()
		_ = g.playerTwo.incomingAudio.Close()

		_ = g.playerOne.conn.Close()
		_ = g.playerTwo.conn.Close()

		g.log.Info().TimeDiff("game_duration", time.Now(), start).Msg("Closed connections to clients")
	}()

	isFirstsTurn := rand.Intn(2) == 1

	if !g.sendOpponentReadyMessage(g.playerOne, g.playerTwo.phoneNumber, isFirstsTurn) ||
		!g.sendOpponentReadyMessage(g.playerTwo, g.playerOne.phoneNumber, !isFirstsTurn) {
		return
	}

	var game ticTacToe

	for !game.done() {
		var client *webSocketClient

		if isFirstsTurn {
			client = g.playerOne
		} else {
			client = g.playerTwo
		}

		digit, ok := g.getDigitFromClient(client)
		if !ok {
			return
		}

		// Normalize digit from 1-9 to expected field index 0-8.
		if !game.selectField(digit-1, isFirstsTurn) {
			digit = game.selectRandomField(isFirstsTurn) + 1
		}

		g.log.Info().
			Str("current_turn_addr", client.conn.RemoteAddr().String()).
			Int("digit", digit).
			Msg("Client selected digit")

		if !g.sendTurnInfo(g.playerOne, digit, isFirstsTurn) || !g.sendTurnInfo(
			g.playerTwo,
			digit,
			!isFirstsTurn,
		) {
			return
		}

		isFirstsTurn = !isFirstsTurn
	}

	winner, _, ok := game.hasWinner()
	if !ok {
		g.sendGameDone(g.playerOne, false, false)
		g.sendGameDone(g.playerTwo, false, false)
	} else {
		g.sendGameDone(g.playerOne, true, winner == playerOne)
		g.sendGameDone(g.playerTwo, true, winner == playerTwo)
	}
}

func (g *game) copyAudioStream(from, to *webSocketClient) {
	// Sample logs because streaming audio is called many times per second.
	sampleLogTo := to.log.Sample(&zerolog.BurstSampler{
		Burst:  1,
		Period: time.Second * 2,
	})
	var total uint64
	for {
		typ, data, err := from.incomingAudio.ReadMessage()
		if err != nil {
			from.log.Err(err).Msg("Failed to read audio from web socket")
			break
		}
		if typ != websocket.BinaryMessage {
			from.log.Warn().Int("msg_type", typ).Msg("Received unexpected web socket message type")
			continue
		}
		total += uint64(len(data))
		if err := to.sendAudio(data); err != nil {
			to.log.Err(err).Msg("Failed to stream audio to client")
			break
		}
		sampleLogTo.Info().
			Int("bytes", len(data)).
			Uint64("total_bytes", total).
			Msg("Send PCM audio to client")
	}
}
