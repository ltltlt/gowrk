package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/alinz/gowrk"
)

const usagestr = `
Usage: gowrk (-url <url>|-file <file>) [options]

gowork Options:
	-concurrent <value>  tnumber of concurrent connections (default 1)
	-request <value>     number of total requests (default 1)
	-unique              atatch timestamp to each request to prevent caching
	-dump <file>         dump all data request into tsv file
	-file <file>         file of requests
`

func usage() {
	fmt.Fprintf(os.Stderr, "%s\n", usagestr)
	os.Exit(0)
}

var url = flag.String("url", "", "full qualified url(include query param)")
var concurrent = flag.Int("concurrent", 1, "number of concurrent connections")
var request = flag.Int("request", 1, "number of total requests")
var unique = flag.Bool("unique", false, "attach timestamp to each request to prevent caching")
var dump = flag.String("dump", "", "dump all result data into tsv file")
var file = flag.String("file", "", "request file")

func main() {
	log.SetOutput(os.Stdout)
	flag.Parse()

	if *url == "" && *file == "" {
		usage()
	}

	gowrk.Start(*url, *concurrent, *request, *unique, *dump, *file)
}
