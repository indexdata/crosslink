package main

import (
	"fmt"
	"os"

	"github.com/indexdata/crosslink/illmock/app"
)

var exit = os.Exit

func main() {
	var app app.MockApp
	err := app.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		exit(1)
	}
}
