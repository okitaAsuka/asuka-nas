package main

import (
	"flag"
	"fmt"
	"github.com/okitaAsuka/asuka-nas/internal/nas"
	"os"
)

func main() {
	path := flag.String("config", "config.json", "deployment configuration")
	flag.Parse()
	f, e := os.Open(*path)
	if e != nil {
		fmt.Fprintln(os.Stderr, "config_unavailable")
		os.Exit(1)
	}
	defer f.Close()
	if _, e = nas.LoadConfig(f); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	fmt.Println("configuration_valid (no network or file operations executed)")
}
