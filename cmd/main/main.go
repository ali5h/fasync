package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"

	"github.com/ali5h/fasync"
)

func main() {
	count := 0

	// read files from argv
	for _, name := range os.Args[1:] {
		var f *os.File
		if name == "-" {
			f = os.Stdin
		} else {
			var err error
			f, err = os.Open(name)
			if err != nil {
				fmt.Printf("open name:%s error: %v\n", name, err)
				continue
			}
		}
		// we need non-blocking reads
		syscall.SetNonblock(int(f.Fd()), true)
		fasync.AddFile(f, os.Stdout)
		count++
	}

	if count == 0 {
		fmt.Printf("usage: main [FILE]...\n")
		fmt.Printf("  FILE file to read (inlcuding /dev/stdin)\n")
	}

	ctx := context.Background()
	// run all the main loops
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fasync.MainLoop(ctx)
	}()
	wg.Wait()
}
