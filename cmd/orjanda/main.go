// Command orjanda is the Orjanda framework CLI binary.
// Full implementation arrives in Phase 10 (TAD §16, PRD §21). Phase 8 adds the
// `agent chat` terminal-mode entry point exercising the Runtime.Execute loop.
package main

import (
	"fmt"
	"os"

	"github.com/orjanda-framework/orjanda/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
