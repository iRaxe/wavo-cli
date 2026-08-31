package main

import (
	"context"
	"fmt"
	"os"

	"github.com/iRaxe/wavo-cli/internal/brandcheck"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Wavo symlink branding scan requires a repository path.")
		fmt.Println("WavoSymlinkScanError")
		return
	}
	matches, err := brandcheck.ScanSymlinkTargets(context.Background(), os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Wavo symlink branding scan failed: %v\n", err)
		fmt.Println("WavoSymlinkScanError")
		return
	}
	if len(matches) == 0 {
		fmt.Println("WavoSymlinkScanClean")
		return
	}
	fmt.Print("WavoSymlinkScanMatches:")
	for _, match := range matches {
		fmt.Printf("\n%q (%s) -> %q", match.Path, match.Source, match.Target)
	}
	fmt.Println()
}
