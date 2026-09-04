package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
)

// cliChallengeHandler implements bank.ChallengeHandler by prompting on the
// terminal: it writes any challenge graphic to a temp file for the user to
// scan, prints any hint text, and reads a typed TAN from stdin when needed.
type cliChallengeHandler struct{}

func (cliChallengeHandler) Handle(ctx context.Context, challenge bank.Challenge) (string, error) {
	if challenge.NeedsInput {
		fmt.Printf("TAN required: %s\n", challenge.Description)
	} else {
		fmt.Println(challenge.Description)
	}

	if len(challenge.Image) > 0 {
		f, err := os.CreateTemp("", "comdirect-phototan-*.png")
		if err != nil {
			return "", fmt.Errorf("write challenge graphic: %w", err)
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(challenge.Image); err != nil {
			f.Close()
			return "", fmt.Errorf("write challenge graphic: %w", err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("write challenge graphic: %w", err)
		}
		fmt.Printf("Graphic saved to %s — open it and scan with your photoTAN app/reader.\n", f.Name())
	}
	if challenge.Hint != "" {
		fmt.Printf("Challenge: %s\n", challenge.Hint)
	}

	if !challenge.NeedsInput {
		return "", nil
	}

	fmt.Print("Enter TAN: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line), err
}
