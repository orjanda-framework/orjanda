// Command orjanda is the Orjanda framework CLI binary (TAD §16, PRD §21).
// Commands run inside an Application directory delegate to the app via
// `go run .` so user Documents register in-process; outside one they operate
// on a core-only site (see cli.Main).
package main

import "github.com/orjanda-framework/orjanda/cli"

func main() {
	cli.Main(nil)
}
