package main

import (
	"context"
	"log"
	"os"

	"github.com/Pratyush-who/WhatsDown/internal/cli"
)

func main() {
	if err := cli.New().ExecuteContext(context.Background()); err != nil {
		log.SetFlags(0)
		log.SetPrefix("pstw: ")
		log.Println(err)
		os.Exit(1)
	}
}
