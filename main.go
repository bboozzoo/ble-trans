package main

import (
	"fmt"
	"os"
)

func main() {
	// XXX: use proper cmdline parser
	what := "server"
	iface := "hci0"

	if len(os.Args) > 1 {
		what = os.Args[1]
	}

	var err error
	switch what {
	case "server":
		err = runServer(iface)
	case "client":
		err = client(iface, "B8:27:EB:6F:C1:30")
		//err = runClient(iface)
		err = fmt.Errorf("client not implemented yet")
	default:
		err = fmt.Errorf("unknown action %q: try client/server", what)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
