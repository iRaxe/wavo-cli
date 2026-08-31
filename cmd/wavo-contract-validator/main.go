package main

import (
	"fmt"
	"os"

	"github.com/iRaxe/wavo-cli/internal/swaggercontract"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Wavo Swagger contract validator requires one file path.")
		os.Exit(2)
	}
	if err := swaggercontract.ValidateFile(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "Wavo Swagger contract validation failed: %v\n", err)
		os.Exit(2)
	}
}
