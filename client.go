package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/muka/go-bluetooth/api"
	"github.com/muka/go-bluetooth/bluez/profile/adapter"
	"github.com/muka/go-bluetooth/bluez/profile/agent"
	"github.com/muka/go-bluetooth/bluez/profile/device"
	log "github.com/sirupsen/logrus"
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

func client(adapterID, hwaddr string) (err error) {
	log.SetLevel(log.TraceLevel)
	log.Infof("Discovering %s on %s", hwaddr, adapterID)

	a, err := adapter.NewAdapter1FromAdapterID(adapterID)
	if err != nil {
		return err
	}

	//Connect DBus System bus
	conn, err := dbus.SystemBus()
	if err != nil {
		return err
	}

	// do not reuse agent0 from service
	agent.NextAgentPath()

	ag := agent.NewSimpleAgent()
	err = agent.ExposeAgent(conn, ag, agent.CapNoInputNoOutput, true)
	if err != nil {
		return fmt.Errorf("SimpleAgent: %s", err)
	}

	dev, err := findDevice(a, hwaddr)
	if err != nil {
		return fmt.Errorf("findDevice: %s", err)
	}

	watchProps, err := dev.WatchProperties()
	if err != nil {
		return err
	}
	go func() {
		for propUpdate := range watchProps {
			log.Debugf("--> updated %s=%v", propUpdate.Name, propUpdate.Value)
		}
	}()

	err = connect(dev, ag, adapterID)
	if err != nil {
		return err
	}

	uuids, err := dev.GetUUIDs()
	if err != nil {
		return err
	}
	found := false
	for _, u := range uuids {
		log.Infof("found service %v", u)
		if u == OnboardingServiceUUID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("service %v not found in device %v",
			OnboardingServiceUUID, hwaddr)
	}
	log.Debugf("props: %+v", dev.Properties)
	chars, err := dev.GetCharacteristics()
	if err != nil {
		return fmt.Errorf("cannot get characteristics: %v", err)
	}
	for _, c := range chars {
		_, u := c.GetUUID()
		log.Debugf("characteristic UUID: %v", u)
	}
	char, err := dev.GetCharByUUID("12342000-0000-1000-8000-00805f9b34fb")
	if err != nil {
		return err
	}
	for {
		v, err := char.ReadValue(nil)
		if err != nil {
			return err
		}
		log.Infof("value: %x", v)
		log.Infof("       %s", string(v))
		time.Sleep(5 * time.Second)
	}

	// retrieveServices(a, dev)

	select {}
	// return nil
}

func findDevice(a *adapter.Adapter1, hwaddr string) (*device.Device1, error) {

	// do we have a cached device?
	// devices, err := a.GetDevices()
	// if err != nil {
	// 	return nil, err
	// }

	// if len(devices) == 0 {
	// 	log.Infof("no cached devices")
	// }
	// for _, dev := range devices {
	// 	p, err := dev.GetProperties()
	// 	if err != nil {
	// 		log.Errorf("Failed to load dev props: %s", err)
	// 		continue
	// 	}

	// 	log.Debugf("device %v services resolved? %v %v", p.Address, p.ServicesResolved, p.UUIDs)
	// 	if !hasUUID(p.UUIDs, snapdOnboardingService) {
	// 		continue
	// 	}
	// 	log.Infof("Found cached device Connected=%t Trusted=%t Paired=%t", p.Connected, p.Trusted, p.Paired)
	// 	return dev, nil
	// }

	dev, err := discover(a, hwaddr)
	if err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, errors.New("Device not found, is it advertising?")
	}

	return dev, nil
}

func discover(a *adapter.Adapter1, hwaddr string) (*device.Device1, error) {

	err := a.FlushDevices()
	if err != nil {
		return nil, err
	}

	discovery, cancel, err := api.Discover(a, &adapter.DiscoveryFilter{
		// UUIDs:     []string{snapdOnboardingService},
		Transport: adapter.DiscoveryFilterTransportLE,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot run discovery: %v", err)
	}

	defer cancel()

	for ev := range discovery {
		log.Infof("new device at %v", ev.Path)
		dev, err := device.NewDevice1(ev.Path)
		if err != nil {
			return nil, err
		}

		if dev == nil || dev.Properties == nil {
			continue
		}

		p := dev.Properties

		n := p.Alias
		if p.Name != "" {
			n = p.Name
		}
		log.Debugf("device %v services resolved? %v %v", p.Address, p.ServicesResolved, p.UUIDs)

		if p.Address == hwaddr {
			return dev, nil
		} else {
			continue
		}
		for i := 0; i < 10; i++ {
			resolved, err := dev.GetServicesResolved()
			if err != nil {
				return nil, fmt.Errorf("cannot resolve services: %v", err)
			}
			if resolved {
				log.Debugf("services resolved")
				break
			}
			log.Tracef("waiting to resolve services of %v", p.Address)
			time.Sleep(1 * time.Second)
		}
		if !hasUUID(p.UUIDs, OnboardingServiceUUID) {
			continue
		}
		log.Debugf("Discovered (%s) %s", n, p.Address)
		return dev, nil
	}

	return nil, nil
}

func connect(dev *device.Device1, ag *agent.SimpleAgent, adapterID string) error {

	props, err := dev.GetProperties()
	if err != nil {
		return fmt.Errorf("Failed to load props: %s", err)
	}

	log.Infof("Found device name=%s addr=%s rssi=%d", props.Name, props.Address, props.RSSI)

	if props.Connected {
		log.Trace("Device is connected")
		return nil
	}

	if !props.Paired || !props.Trusted {
		log.Trace("Pairing device")

		err := dev.Pair()
		if err != nil {
			return fmt.Errorf("Pair failed: %s", err)
		}

		log.Info("Pair succeed, connecting...")
		agent.SetTrusted(adapterID, dev.Path())
	}

	if !props.Connected {
		log.Trace("Connecting device")
		err = dev.Connect()
		if err != nil {
			if !strings.Contains(err.Error(), "Connection refused") {
				return fmt.Errorf("Connect failed: %s", err)
			}
		}
	}

	return nil
}

func retrieveServices(a *adapter.Adapter1, dev *device.Device1) error {

	log.Debug("Listing exposed services")

	list, err := dev.GetAllServicesAndUUID()
	if err != nil {
		return err
	}

	if len(list) == 0 {
		log.Debug("got nothing try again")
		time.Sleep(time.Second * 2)
		return retrieveServices(a, dev)
	}

	for _, servicePath := range list {
		log.Debugf("%s", servicePath)
	}

	return nil
}
