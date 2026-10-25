package main

import (
	"flag"
	"fmt"
)

var version = "dev"

func main() {
	show := flag.Bool("version", false, "print Shack version")
	flag.Usage = func() {
		fmt.Print("Shack — Atomweight Terminal Text Editor IDE with Sensible Day 1 Defaults\nUsage: shack [options] [files...]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *show {
		fmt.Println("shack", version)
		return
	}
	flag.Usage()
}
