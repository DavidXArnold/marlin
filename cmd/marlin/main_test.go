package main

import (
	"os"
	"testing"
)

func TestMainCommandHelp(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"marlin", "--help"}
	defer func() { os.Args = oldArgs }()

	main()
}
