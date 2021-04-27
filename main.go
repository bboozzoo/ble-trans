package main

import (
	"fmt"
	"os"

	"github.com/go-ble/ble"
	ble_linux "github.com/go-ble/ble/linux"
	"github.com/go-ble/ble/linux/hci/evt"
	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetLevel(log.TraceLevel)
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	connectionChan := make(chan string)

	dev, err := ble_linux.NewDeviceWithName("snapd",
		ble.OptConnectHandler(func(e evt.LEConnectionComplete) {
			log.Infof("connect handler, peer: %x handle: %v", e.PeerAddress(), e.ConnectionHandle())
			a := e.PeerAddress()
			addr := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", a[0], a[1], a[2], a[3], a[4], a[5])
			connectionChan <- addr
		}),
		ble.OptDisconnectHandler(func(e evt.DisconnectionComplete) {
			log.Infof("disconnected, handle: %v", e.ConnectionHandle())
		}),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot obtain device: %v", err)
		os.Exit(1)
	}
	ble.SetDefaultDevice(dev)

	// XXX: use proper cmdline parser
	what := "server"
	iface := "hci0"

	if len(os.Args) > 1 {
		what = os.Args[1]
	}

	switch what {
	case "server":
		err = runServer(iface)
	case "client":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "missing client address\n")
			os.Exit(1)
		}
		err = client(os.Args[2])
	case "device":
		err = runDevice(connectionChan)
	case "configurator":
		fallthrough
	default:
		err = fmt.Errorf("unknown action %q: try client/server", what)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
