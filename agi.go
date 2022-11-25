package voipttt

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// AGI is used for the communication with asterisk by using AGI commands.
type AGI struct {
	scanner   *bufio.Scanner
	variables map[string]string
	log       zerolog.Logger
}

// NewAGI returns an initialized agi instance.
func NewAGI(log zerolog.Logger) *AGI {
	return &AGI{
		scanner:   bufio.NewScanner(os.Stdin),
		variables: map[string]string{},
		log:       log,
	}
}

// ReadVariables reads the variables that asterisks writes to stdin upon
// starting the AGI program.
func (a *AGI) ReadVariables() error {
	for a.scanner.Scan() {
		if err := a.scanner.Err(); err != nil {
			return fmt.Errorf("reading AGI parameters: %w", err)
		}
		text := a.scanner.Text()
		if text == "" {
			break
		}
		before, after, ok := strings.Cut(text, ": ")
		if !ok {
			return errors.New("unexpected format of AGI preamble")
		}
		a.variables[strings.TrimSpace(before)] = strings.TrimSpace(after)
	}
	return nil
}

// ReadDigit waits for user to enter a single digit.
func (a *AGI) ReadDigit() (int, error) {
	timeout := time.Second * 5

	var sb strings.Builder

	for {
		a.log.Debug().
			Int64("timeout_ms", timeout.Milliseconds()).
			Msg("Waiting for digit with timeout")

		fmt.Printf("WAIT FOR DIGIT %d\n", timeout.Milliseconds())

		a.scanner.Scan()
		if err := a.scanner.Err(); err != nil {
			return 0, fmt.Errorf("read stdin: %w", err)
		}
		text := a.scanner.Text()
		_, after, ok := strings.Cut(text, "=")
		if !ok {
			return 0, fmt.Errorf("received invalid value from asterisk: %s", text)
		}
		if after == "-1" {
			return 0, fmt.Errorf("asterisk reported a channel failure")
		}
		if after == "0" {
			continue
		}

		num, _ := strconv.ParseInt(after, 10, 32)
		digit := rune(num)

		if digit == '#' {
			digits, _ := strconv.Atoi(sb.String())
			a.log.Info().Int("digits", digits).Msg("Received final digits")
			return digits, nil
		}

		a.log.Debug().Str("digit", string(digit)).Msg("Received single digit")
		sb.WriteRune(digit)
	}
}

// StartAudioFork starts the asterisk-audio-fork extension which forks the audio stream
// and then connects to the given websocket server sending the stream there.
func (a *AGI) StartAudioFork(phoneNumber PhoneNumber) error {
	fmt.Printf(
		"EXEC AudioFork \"ws://localhost:8081/ws-audio?phoneNumber=\"%s\"\" \"r(0)\"\n",
		phoneNumber,
	)
	a.scanner.Scan()
	if err := a.scanner.Err(); err != nil {
		return fmt.Errorf("start asterisk audio fork: %w", err)
	}
	return nil
}

// Get returns the value for the given variable.
func (a *AGI) Get(variable string) string {
	return a.variables[variable]
}
