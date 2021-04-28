// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"strings"
	"time"

	"github.com/mvo5/ble-trans/netonboard"
)

type State int

const (
	StateWaitHello State = iota + 1
	StateDevice
	StateReady
	StateCfgComplete
	StateError

	WaitTimeout = 30 * time.Second
)

var (
	// XXX: make this an input to device and configurator
	secret = []byte(strings.Repeat("a", 32))

	wifiSsids interface{}
)

type configuratorTransport interface {
	Connect(addr string) error
	Disconnect() error

	Send(data []byte) error
	Receive() ([]byte, error)

	// WaitForState waits for device to get to desired state or the error
	// state, in which case the error message will contain the last error
	// reported by the device.
	WaitForState(ctx context.Context, state State) error
}

type configurator struct {
	device string
	cfg    *netonboard.Configurator
	t      configuratorTransport

	onbs      []byte
	onbDevKey *ecdsa.PrivateKey
}

func NewConfiguratorFor(addr string, t configuratorTransport) (*configurator, error) {
	cfg := &netonboard.Configurator{}
	if err := cfg.SetOnboardingSecret(secret); err != nil {
		return nil, fmt.Errorf("cannot set secret: %v", err)
	}

	return &configurator{
		device: addr,
		cfg:    cfg,
		t:      t,
	}, nil
}

func (c *configurator) Configure(scenario string) error {
	if err := c.t.Connect(c.device); err != nil {
		return fmt.Errorf("cannot connect to device %v: %v", c.device, err)
	}
	defer c.t.Disconnect()

	hello, err := c.cfg.Hello()
	if err != nil {
		return fmt.Errorf("cannot generate hello message: %v", err)
	}

	if err := c.t.Send(hello); err != nil {
		return fmt.Errorf("cannot send hello message: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	err = c.t.WaitForState(ctx, StateDevice)
	if err != nil {
		return fmt.Errorf("wait for state change failed: %v", err)
	}

	deviceMsg, err := c.t.Receive()
	if err != nil {
		return fmt.Errorf("cannot receive 'device' message: %v", err)
	}

	if err := c.cfg.RcvDevice(deviceMsg); err != nil {
		return fmt.Errorf("cannot process 'device' message: %v", err)
	}

	setup, err := c.cfg.SessionSetup()
	if err != nil {
		return fmt.Errorf("cannot generate session-setup message: %v", err)
	}
	if err := c.t.Send(setup); err != nil {
		return fmt.Errorf("cannot send session setup message: %v", err)
	}

	err = c.t.WaitForState(ctx, StateReady)
	if err != nil {
		return fmt.Errorf("wait for state change failed: %v", err)
	}

	switch scenario {
	case "1":
		return c.scenario1()
	case "2":
		return c.scenario2()
	default:
		return fmt.Errorf("unsupported scenario %q", scenario)
	}

	// deviceReady, err := c.t.Receive()
	// if err != nil {
	// 	return fmt.Errorf("cannot receive 'ready' message: %v", err)
	// }

	// d, err := c.cfg.RcvReady(deviceReady)
	// if err != nil {
	// 	return fmt.Errorf("cannot process 'ready' message: %v", err)
	// }

	// fmt.Printf("got data: %v\n", d)

	// cfgMsg, err := c.cfg.Cfg(map[string]interface{}{
	// 	"core.onboard": struct{}{},
	// })
	// if err != nil {
	// 	return fmt.Errorf("cannot generate 'cfg' message: %v", err)
	// }
	// if err := c.t.Send(cfgMsg); err != nil {
	// 	return fmt.Errorf("cannot send 'cfg' message: %v", err)
	// }

	// return nil
}

func (c *configurator) scenario1() error {
	deviceReady, err := c.t.Receive()
	if err != nil {
		return fmt.Errorf("cannot receive 'ready' message: %v", err)
	}

	d, err := c.cfg.RcvReady(deviceReady)
	if err != nil {
		return fmt.Errorf("cannot process 'ready' message: %v", err)
	}

	fmt.Printf("got data:\n%v\n", d)

	cfgMsg, err := c.cfg.Cfg(map[string]interface{}{
		"wifi.list-ssids": true,
	})
	if err != nil {
		return fmt.Errorf("cannot generate 'cfg' message: %v", err)
	}
	if err := c.t.Send(cfgMsg); err != nil {
		return fmt.Errorf("cannot send 'cfg' message: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	err = c.t.WaitForState(ctx, StateReady)
	if err != nil {
		return fmt.Errorf("wait for state change failed: %v", err)
	}

	replyMsg, err := c.t.Receive()
	if err != nil {
		return fmt.Errorf("cannot receive 'reply' message: %v", err)
	}

	reply, err := c.cfg.RcvReply(replyMsg)
	if err != nil {
		return fmt.Errorf("cannot process 'reply' message: %v", err)
	}
	fmt.Printf("got reply:\n%v\n", reply)

	ssidsList := reply["wifi.ssids"].([]interface{})
	if len(ssidsList) == 0 {
		return fmt.Errorf("ssids list is empty")
	}

	ctx, cancel = context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	err = c.t.WaitForState(ctx, StateReady)
	if err != nil {
		return fmt.Errorf("wait for state change failed: %v", err)
	}

	cfgMsg, err = c.cfg.Cfg(map[string]interface{}{
		"core.onboard":   struct{}{},
		"wifi.configure": ssidsList[0],
	})
	if err != nil {
		return fmt.Errorf("cannot generate 'cfg' message: %v", err)
	}
	if err := c.t.Send(cfgMsg); err != nil {
		return fmt.Errorf("cannot send 'cfg' message: %v", err)
	}

	ctx, cancel = context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	err = c.t.WaitForState(ctx, StateCfgComplete)
	if err != nil {
		return fmt.Errorf("wait for state change failed: %v", err)
	}

	return nil
}

func (c *configurator) scenario2() error {
	deviceReady, err := c.t.Receive()
	if err != nil {
		return fmt.Errorf("cannot receive 'ready' message: %v", err)
	}

	d, err := c.cfg.RcvReady(deviceReady)
	if err != nil {
		return fmt.Errorf("cannot process 'ready' message: %v", err)
	}

	fmt.Printf("got data:\n%v\n", d)
	ssidsList := d["wifi.ssids"].([]interface{})
	if len(ssidsList) == 0 {
		return fmt.Errorf("ssids list is empty")
	}

	cfgMsg, err := c.cfg.Cfg(map[string]interface{}{
		"core.onboard":   struct{}{},
		"wifi.configure": ssidsList[0],
	})
	if err != nil {
		return fmt.Errorf("cannot generate 'cfg' message: %v", err)
	}
	if err := c.t.Send(cfgMsg); err != nil {
		return fmt.Errorf("cannot send 'cfg' message: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	err = c.t.WaitForState(ctx, StateCfgComplete)
	if err != nil {
		return fmt.Errorf("wait for state change failed: %v", err)
	}

	return nil
}

type waitFunc func(context.Context) error

type deviceTransport interface {
	Advertise() error
	StopAdvertising()

	WaitConnected() (peer string, err error)
	Disconnect(peer []byte)
	Disconnected() <-chan string

	NotifyState(state State) error
	// prepare for client?
	Send([]byte) (waitFunc, error)

	PrepareReceive() error
	Receive(ctx context.Context) ([]byte, error)

	SetError(err error)

	Reset()
}

type device struct {
	t   deviceTransport
	dev *netonboard.Device
}

func NewDevice(t deviceTransport) (*device, error) {
	dev := &netonboard.Device{}
	if err := dev.SetOnboardingSecret(secret); err != nil {
		return nil, fmt.Errorf("cannot set onbording secret: %v", err)
	}
	onbDevKey, err := netonboard.GenDeviceKey()
	if err != nil {
		return nil, fmt.Errorf("cannot generate device key: %v", err)
	}
	if err := dev.SetOnboardingDeviceKey(onbDevKey); err != nil {
		return nil, fmt.Errorf("cannot set onboarding device key: %v", err)
	}
	return &device{
		t:   t,
		dev: dev,
	}, nil
}

func (d *device) WaitForConfiguration(scenario string) error {
	// advertise the service
	d.t.Advertise()

	// XXX: move this outside?
	peer, err := d.t.WaitConnected()
	if err != nil {
		return fmt.Errorf("cannot wait for connection: %v", err)
	}
	fmt.Printf("connection from peer: %s\n", peer)

	// XXX: observing an error from go-ble:
	// 2021/04/28 07:51:37 skt: can't find the cmd for CommandCompleteEP: 01 0A 20 0C
	// stop announcing
	// d.t.Hide()

	ctx, cancel := context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	helloMsg, err := d.t.Receive(ctx)
	if err != nil {
		return fmt.Errorf("cannot receive 'hello' message: %v", err)
	}

	if err := d.dev.RcvHello(helloMsg); err != nil {
		return fmt.Errorf("cannot process 'hello' message: %v", err)
	}

	devMsg, err := d.dev.Device()
	if err != nil {
		return fmt.Errorf("cannot generate 'device' message: %v", err)
	}
	wait, err := d.t.Send(devMsg)
	if err != nil {
		return fmt.Errorf("cannot send 'device' message")
	}
	if err := d.t.NotifyState(StateDevice); err != nil {
		return fmt.Errorf("cannot announce new state: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	if err := wait(ctx); err != nil {
		return fmt.Errorf("cannot wait for 'device' message to be sent: %v", err)
	}

	ctx, cancel = context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	setupMsg, err := d.t.Receive(ctx)
	if err != nil {
		return fmt.Errorf("cannot receive 'session-setup' message: %v", err)
	}

	if err := d.dev.RcvSessionSetup(setupMsg); err != nil {
		return fmt.Errorf("cannot process 'session-setup' message: %v", err)
	}

	switch scenario {
	case "1":
		return d.scenario1()
	case "2":
		return d.scenario2()
	default:
		return fmt.Errorf("unsupported scenario: %q", scenario)
	}

	// readyMsg, err := d.dev.Ready(nil)
	// if err != nil {
	// 	return fmt.Errorf("cannot generate 'ready' message: %v", err)
	// }

	// wait, err = d.t.Send(readyMsg)
	// if err != nil {
	// 	return fmt.Errorf("cannot send 'ready' message: %v", err)
	// }

	// if err := d.t.NotifyState(StateReady); err != nil {
	// 	return fmt.Errorf("cannot announce 'ready' state: %v", err)
	// }

	// ctx, cancel = context.WithTimeout(context.Background(), WaitTimeout)
	// defer cancel()
	// if err := wait(ctx); err != nil {
	// 	return fmt.Errorf("cannot wait for 'ready' message to be sent: %v", err)
	// }

	// ctx, cancel = context.WithTimeout(context.Background(), WaitTimeout)
	// defer cancel()
	// cfgMsg, err := d.t.Receive(ctx)
	// if err != nil {
	// 	return fmt.Errorf("cannot receive 'session-setup' message: %v", err)
	// }

	// config, err := d.dev.RcvCfg(cfgMsg)
	// if err != nil {
	// 	return fmt.Errorf("cannot process 'cfg' message: %v", err)
	// }

	// fmt.Printf("got config:\n%v\n", config)
	// if config["core.onboard"] == struct{}{} {
	// 	return nil
	// }

	// return nil
}

func (d *device) scenario1() error {
	readyMsg, err := d.dev.Ready(map[string]interface{}{
		"configurable": []string{"wifi", "core"},
	})
	if err != nil {
		return fmt.Errorf("cannot generate 'ready' message: %v", err)
	}

	wait, err := d.t.Send(readyMsg)
	if err != nil {
		return fmt.Errorf("cannot send 'ready' message: %v", err)
	}

	if err := d.t.NotifyState(StateReady); err != nil {
		return fmt.Errorf("cannot announce 'ready' state: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	if err := wait(ctx); err != nil {
		return fmt.Errorf("cannot wait for 'ready' message to be sent: %v", err)
	}

	var config map[string]interface{}
	for cnt := 0; cnt < 2; cnt++ {
		ctx, cancel = context.WithTimeout(context.Background(), WaitTimeout)
		defer cancel()
		cfgMsg, err := d.t.Receive(ctx)
		if err != nil {
			return fmt.Errorf("cannot receive 'session-setup' message: %v", err)
		}

		config, err = d.dev.RcvCfg(cfgMsg)
		if err != nil {
			return fmt.Errorf("cannot process 'cfg' message: %v", err)
		}

		fmt.Printf("got config:\n%v\n", config)
		if _, ok := config["core.onboard"]; ok {
			if config["wifi.configure"] == nil {
				return fmt.Errorf("unexpected content of wifi.configure in scenario 1: %v", config)
			}
			if err := d.t.NotifyState(StateCfgComplete); err != nil {
				return fmt.Errorf("cannot announce 'ready' state: %v", err)
			}
			// done
			return nil
		}

		listSsids := config["wifi.list-ssids"].(bool)
		if !listSsids {
			return fmt.Errorf("unexpected content of wifi.list-ssids in scenario 1: %v pass: %v", config, cnt)
		}

		reply, err := d.dev.Reply(map[string]interface{}{
			"wifi.ssids": wifiSsids,
		})
		if err != nil {
			return fmt.Errorf("cannot generate 'reply' message: %v", err)
		}
		wait, err = d.t.Send(reply)
		if err != nil {
			return fmt.Errorf("cannot send 'reply' message: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), WaitTimeout)
		defer cancel()
		if err := wait(ctx); err != nil {
			return fmt.Errorf("cannot wait for 'reply' message to be sent: %v", err)
		}
		if err := d.t.NotifyState(StateReady); err != nil {
			return fmt.Errorf("cannot announce 'ready' state: %v", err)
		}
	}

	return fmt.Errorf("unexpected outcome of scenario 1, last config: %v", config)
}

func (d *device) scenario2() error {
	readyMsg, err := d.dev.Ready(map[string]interface{}{
		"configurable": []string{"wifi", "core"},
		"wifi.ssids":   wifiSsids,
	})
	if err != nil {
		return fmt.Errorf("cannot generate 'ready' message: %v", err)
	}

	wait, err := d.t.Send(readyMsg)
	if err != nil {
		return fmt.Errorf("cannot send 'ready' message: %v", err)
	}

	if err := d.t.NotifyState(StateReady); err != nil {
		return fmt.Errorf("cannot announce 'ready' state: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	if err := wait(ctx); err != nil {
		return fmt.Errorf("cannot wait for 'ready' message to be sent: %v", err)
	}

	ctx, cancel = context.WithTimeout(context.Background(), WaitTimeout)
	defer cancel()
	cfgMsg, err := d.t.Receive(ctx)
	if err != nil {
		return fmt.Errorf("cannot receive 'session-setup' message: %v", err)
	}

	config, err := d.dev.RcvCfg(cfgMsg)
	if err != nil {
		return fmt.Errorf("cannot process 'cfg' message: %v", err)
	}

	fmt.Printf("got config:\n%v\n", config)
	if _, ok := config["core.onboard"]; !ok {
		return fmt.Errorf("unexpected content of core.onboard in scenario 2: %v", config)
	}
	if config["wifi.configure"] == nil {
		return fmt.Errorf("unexpected content of wifi.configure in scenario 2: %v", config)
	}
	if err := d.t.NotifyState(StateCfgComplete); err != nil {
		return fmt.Errorf("cannot announce 'cfg-complete' state: %v", err)
	}
	return nil
}
