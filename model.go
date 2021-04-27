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
	"fmt"
	"time"

	"github.com/mvo5/ble-trans/netonboard"
)

type State int

const (
	StateWaitHello State = iota + 1
	StateDevice
	StateSessionSetup
	StateReady

	WaitTimeout = 5 * time.Second
)

type configuratorTransport interface {
	Connect(addr string) error
	Disconnect() error
	Send(data []byte) error
	Receive() ([]byte, error)
	WaitForState(ctx context.Context, state State) error
}

type configurator struct {
	device string
	cfg    *netonboard.Configurator
	t      configuratorTransport
}

func NewConfiguratorFor(addr string, t configuratorTransport) *configurator {
	return &configurator{
		device: addr,
		cfg:    &netonboard.Configurator{},
		t:      t,
	}
}

func (c *configurator) Configure() error {
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

	deviceReady, err := c.t.Receive()
	if err != nil {
		return fmt.Errorf("cannot receive 'ready' message: %v", err)
	}

	d, err := c.cfg.RcvReady(deviceReady)
	if err != nil {
		return fmt.Errorf("cannot process 'ready' message: %v", err)
	}

	fmt.Printf("got data: %v\n", d)

	return nil
}

type deviceTransport interface {
	Advertise() error
	Hide()
	WaitConnected() ([]byte, error)
	Disconnect(peer []byte)
	NotifyState(state State) error
	Send([]byte) error
	Receive(ctx context.Context) ([]byte, error)
}

type device struct {
	t   deviceTransport
	dev *netonboard.Device
}

func NewDevice(t deviceTransport) *device {
	return &device{
		t:   t,
		dev: &netonboard.Device{},
	}
}

func (d *device) WaitForConfiguration() error {
	// advertise the service
	d.t.Advertise()

	peer, err := d.t.WaitConnected()
	if err != nil {
		return fmt.Errorf("cannot wait for connection: %v", err)
	}
	fmt.Printf("connection from peer: %x\n", peer)

	// stop announcing
	d.t.Hide()

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
	if err := d.t.Send(devMsg); err != nil {
		return fmt.Errorf("cannot send 'device' message")
	}
	if err := d.t.NotifyState(StateDevice); err != nil {
		return fmt.Errorf("cannot announce new state: %v", err)
	}

	setupMsg, err := d.t.Receive(ctx)
	if err != nil {
		return fmt.Errorf("cannot receive 'hello' message: %v", err)
	}

	if err := d.dev.RcvSessionSetup(setupMsg); err != nil {
		return fmt.Errorf("cannot process 'session-setup' message: %v", err)
	}

	readyMsg, err := d.dev.Ready(nil)
	if err != nil {
		return fmt.Errorf("cannot generate 'ready' message: %v", err)
	}
	if err := d.t.Send(readyMsg); err != nil {
		return fmt.Errorf("cannot send 'ready' message: %v", err)
	}

	if err := d.t.NotifyState(StateReady); err != nil {
		return fmt.Errorf("cannot announce 'ready' state: %v", err)
	}
	return nil
}
