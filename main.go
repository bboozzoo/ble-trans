package main

import (
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetLevel(log.TraceLevel)
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

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
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "missing client address\n")
			os.Exit(1)
		}
		err = client(os.Args[2])
	default:
		err = fmt.Errorf("unknown action %q: try client/server", what)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
