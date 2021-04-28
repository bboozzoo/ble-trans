package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-ble/ble"
	ble_linux "github.com/go-ble/ble/linux"
	"github.com/go-ble/ble/linux/hci/evt"
	"github.com/jessevdk/go-flags"
	log "github.com/sirupsen/logrus"
)

type ConnectEvent struct {
	Peer   string
	Handle uint16
}

type DisconnectEvent struct {
	Handle uint16
}

type Options struct {
	Debug      bool `long:"debug" description:"Show debug log"`
	Positional struct {
		Mode     string `description:"Mode (client|server|device|configurator)"`
		ModeArgs []string
	} `positional-args:"yes"`
}

func main() {
	var opts Options
	_, err := flags.Parse(&opts)
	if err != nil {
		os.Exit(1)
	}
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	if opts.Debug {
		log.SetLevel(log.TraceLevel)
		log.Debugf("debug logging enabled")
	}

	connectChan := make(chan ConnectEvent, 10)
	disconnectChan := make(chan DisconnectEvent, 10)

	dev, err := ble_linux.NewDeviceWithName("snapd",
		ble.OptConnectHandler(func(e evt.LEConnectionComplete) {
			log.Infof("connect handler, peer: %x handle: %v", e.PeerAddress(), e.ConnectionHandle())
			a := e.PeerAddress()
			addr := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", a[0], a[1], a[2], a[3], a[4], a[5])
			connectChan <- ConnectEvent{Peer: addr, Handle: e.ConnectionHandle()}
		}),
		ble.OptDisconnectHandler(func(e evt.DisconnectionComplete) {
			log.Infof("disconnected, handle: %v", e.ConnectionHandle())
			disconnectChan <- DisconnectEvent{Handle: e.ConnectionHandle()}
		}),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot obtain device: %v", err)
		os.Exit(1)
	}
	ble.SetDefaultDevice(dev)

	// XXX: use proper cmdline parser
	iface := "hci0"

	mode := opts.Positional.Mode
	args := opts.Positional.ModeArgs
	log.Infof("mode: %v %v", mode, args)

	switch mode {
	case "server":
		err = runServer(iface)
	case "client":
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "missing client address\n")
			os.Exit(1)
		}
		err = client(args[0])
	case "device":
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "missing scenario, try '1', '2'\n")
			os.Exit(1)
		}
		if err = loadSssids(); err != nil {
			fmt.Fprintf(os.Stderr, "cannot load ssids list: %v", err)
		} else {
			err = runDevice(connectChan, disconnectChan, args[0])
		}
	case "configurator":
		if len(args) != 2 {
			fmt.Fprintf(os.Stderr, "missing client address and/or scenario (try '1', '2')\n")
			os.Exit(1)
		}
		err = runConfigurator(args[0], args[1])
	default:
		err = fmt.Errorf("unknown action %q", mode)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func loadSssids() error {
	f, err := os.Open("ssids.json")
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&wifiSsids); err != nil {
		return err
	}
	log.Tracef("got ssids: %v", wifiSsids)
	return nil
}
