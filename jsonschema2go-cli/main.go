package main

import (
	"context"
	"flag"
	"log"

	"github.com/ns1/jsonschema2go"
)

func main() {
	var verbose bool
	flag.BoolVar(&verbose, "v", false, "enable verbose output")
	flag.Parse()
	files := []string{}
	for _, arg := range flag.Args() {
		if verbose {
			log.Printf("processing file: %s", arg)
		}
		files = append(files, arg)
	}
	if len(files) == 0 {
		log.Fatal("no input files specified")
	}
	options := []jsonschema2go.Option{}
	option := jsonschema2go.Debug(verbose)
	options = append(options, option)
	err := jsonschema2go.Generate(context.Background(), files, options...)
	if err != nil {
		log.Fatal(err)
	}
}
