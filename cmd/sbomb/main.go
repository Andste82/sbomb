package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println("sbomb 0.0.0-milestone00")
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("sbomb 0.0.0-milestone00")
		return
	}
	if len(os.Args) == 1 {
		fmt.Println("sbomb 0.0.0-milestone00")
		return
	}
	fmt.Fprintf(os.Stderr, "usage: sbomb [version|--version]\n")
	os.Exit(1)
}
