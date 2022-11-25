package main

import (
	"fmt"

	"github.com/rs/zerolog"

	voipttt "github.com/n9v9/voip-ttt"
)

type application struct {
	agi              *voipttt.AGI
	hasReadVariables bool
}

func newApplication(log zerolog.Logger) *application {
	return &application{
		agi:              voipttt.NewAGI(log),
		hasReadVariables: false,
	}
}

func (aa *application) promptVerificationCode() (voipttt.VerificationCode, error) {
	if err := aa.readVariables(); err != nil {
		return 0, fmt.Errorf("agi read variables: %w", err)
	}

	digit, err := aa.agi.ReadDigit()
	if err != nil {
		return 0, fmt.Errorf("agi read digit: %w", err)
	}

	return voipttt.VerificationCode(digit), nil
}

func (aa *application) promptPhoneNumber() (voipttt.PhoneNumber, error) {
	if err := aa.readVariables(); err != nil {
		return "", fmt.Errorf("agi read variables: %w", err)
	}
	return voipttt.PhoneNumber(aa.agi.Get("agi_callerid")), nil
}

func (aa *application) promptDigit() (int, error) {
	if err := aa.readVariables(); err != nil {
		return 0, fmt.Errorf("agi read variables: %w", err)
	}

	return aa.agi.ReadDigit()
}

func (aa *application) readVariables() error {
	if aa.hasReadVariables {
		return nil
	}
	aa.hasReadVariables = true
	if err := aa.agi.ReadVariables(); err != nil {
		return fmt.Errorf("agi read variables: %w", err)
	}
	return nil
}
