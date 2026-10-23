package main

import (
	"fmt"
	"os"

	"github.com/example/mail-aggregator/internal/auth"
)

func main() {
	setup, err := auth.SetupHex()
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate OPAQUE setup:", err)
		os.Exit(1)
	}
	fmt.Println(setup)
}
