package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("1.18.4")
		return
	}
	fmt.Fprintln(os.Stderr, "unsupported invocation")
	os.Exit(2)
}
