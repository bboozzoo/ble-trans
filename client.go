package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-ble/ble"
	ble_linux "github.com/go-ble/ble/linux"

	log "github.com/sirupsen/logrus"
)

var (
	scanTimeout = 10 * time.Second
	MTU         = 512
)

func hasUUID(uuids []string, uuid string) bool {
	for _, u := range uuids {
		log.Infof("checking UUID %v", u)
		if u == OnboardingServiceUUID {
			return true
		}
	}
	return false
}

func client(hwaddr string) (err error) {
	dev, err := ble_linux.NewDevice()
	if err != nil {
		return fmt.Errorf("cannot obtain device: %v", err)
	}
	ble.SetDefaultDevice(dev)

	hwaddr = strings.ToLower(hwaddr)

	log.SetLevel(log.TraceLevel)
	log.Infof("Looking for %s", hwaddr)

	filter := func(a ble.Advertisement) bool {
		log.Debugf("found device: %v (%v)", a.Addr(), a.LocalName())
		return strings.ToLower(a.Addr().String()) == hwaddr
	}

	ctx := ble.WithSigHandler(context.WithTimeout(context.Background(), scanTimeout))
	cln, err := ble.Connect(ctx, filter)
	if err != nil {
		return fmt.Errorf("cannot connect to %s: %v", hwaddr, err)
	}

	defer func() {
		log.Infof("Disconnecting [ %s ]... (this might take up to few seconds on OS X)\n", cln.Addr())
		cln.CancelConnection()
		<-cln.Disconnected()
		log.Warnf("[ %s ] is disconnected \n", cln.Addr())
	}()

	log.Infof("Discovering profile...\n")
	p, err := cln.DiscoverProfile(true)
	if err != nil {
		log.Fatalf("can't discover profile: %s", err)
	}

	log.Tracef("changing MTU to %v", MTU)
	txMtu, err := cln.ExchangeMTU(MTU)
	if err != nil {
		return fmt.Errorf("cannot change MTU: %v", err)
	}
	log.Infof("MTU: %v\n", txMtu)
	if txMtu != MTU {
		log.Warnf("got different MTU %v", txMtu)
	}

	// find descriptor already?
	onboardingService := ble.NewService(ble.MustParse(OnboardingServiceUUID))
	srv := p.FindService(onboardingService)
	if srv == nil {
		log.Warnf("service not found")
		return nil
	}

	desc := p.FindDescriptor(ble.NewDescriptor(ble.MustParse(UUIDBase + descrHandle + UUIDSuffix)))
	if desc == nil {
		return fmt.Errorf("descriptor not found")
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			data, err := cln.ReadDescriptor(desc)
			if err != nil {
				return fmt.Errorf("cannot read descriptor %v: %v", desc.UUID.String(), err)
			}
			log.Infof("value: %q", string(data))
		}
	}

	return nil
}
