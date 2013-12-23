package main

import (
	"os"
)

func main() {
	s := CreateServer()

	if err := s.runListeningLoop(); err != nil {
		os.Exit(1)
	}
}
