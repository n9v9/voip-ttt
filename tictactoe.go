package voipttt

import "math/rand"

type player int

const (
	playerNone player = 0 // The field is free.
	playerOne  player = 1
	playerTwo  player = 2
)

// ticTacToe represents an active game of Tic-Tac-Toe between
// two players.
type ticTacToe struct {
	fields [9]player
}

// selectField selects the given field for the given player.
// The field number is zero based.
// It is the callers responsibility to check before this call, that
// the game is not done yet.
func (t *ticTacToe) selectField(field int, isPlayerOne bool) bool {
	if t.fields[field] == playerNone {
		if isPlayerOne {
			t.fields[field] = playerOne
		} else {
			t.fields[field] = playerTwo
		}
		return true
	}
	return false
}

// selectRandomField makes a random selection for the given player
// returning the selected, zero based field.
// It is the callers responsibility to check before this call, that
// the game is not done yet.
func (t *ticTacToe) selectRandomField(isPlayerOne bool) int {
	var free []int
	for i, v := range t.fields {
		if v == playerNone {
			free = append(free, i)
		}
	}
	field := free[rand.Intn(len(free))]
	if isPlayerOne {
		t.fields[field] = playerOne
	} else {
		t.fields[field] = playerTwo
	}
	return field
}

// done returns whether the game is over, either because
// of a winning combination or of a draw.
func (t *ticTacToe) done() bool {
	if _, _, ok := t.hasWinner(); ok {
		return true
	}
	for _, v := range t.fields {
		if v == playerNone {
			return false
		}
	}
	return true
}

// hasWinner returns whether the game has a winner.
// If it does, the winning player and the winning combination
// are set accordingly. The winning combination is zero based.
// The returned player is one of the above defined constants.
func (t *ticTacToe) hasWinner() (winner player, fields [3]byte, ok bool) {
	check := func(player player) ([3]byte, bool) {
		if t.fields[0] == player && t.fields[1] == player && t.fields[2] == player {
			return [...]byte{1, 2, 3}, true
		}
		if t.fields[3] == player && t.fields[4] == player && t.fields[5] == player {
			return [...]byte{4, 5, 6}, true
		}
		if t.fields[6] == player && t.fields[7] == player && t.fields[8] == player {
			return [...]byte{7, 8, 9}, true
		}
		if t.fields[0] == player && t.fields[3] == player && t.fields[6] == player {
			return [...]byte{1, 4, 7}, true
		}
		if t.fields[1] == player && t.fields[4] == player && t.fields[7] == player {
			return [...]byte{2, 5, 8}, true
		}
		if t.fields[2] == player && t.fields[5] == player && t.fields[8] == player {
			return [...]byte{3, 6, 9}, true
		}
		if t.fields[0] == player && t.fields[4] == player && t.fields[8] == player {
			return [...]byte{1, 5, 9}, true
		}
		if t.fields[2] == player && t.fields[4] == player && t.fields[6] == player {
			return [...]byte{3, 5, 7}, true
		}
		return [3]byte{}, false
	}
	if fields, ok := check(playerOne); ok {
		return playerOne, fields, true
	}
	if fields, ok := check(playerTwo); ok {
		return playerTwo, fields, true
	}
	return 0, [3]byte{}, false
}
