package main

import (
	"log"

	"github.com/douglarek/melo/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
