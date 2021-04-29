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

package netonboard_test

import (
	"crypto/ecdsa"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/mvo5/ble-trans/netonboard"
)

func Test(t *testing.T) { TestingT(t) }

type protoSuite struct {
	onbs      []byte
	onbDevKey *ecdsa.PrivateKey

	dev   *netonboard.Device
	cftor *netonboard.Configurator
}

var _ = Suite(&protoSuite{})

func (s *protoSuite) SetUpSuite(c *C) {
	onbs, err := netonboard.GenSecret()
	c.Assert(err, IsNil)
	s.onbs = onbs

	onbDevKey, err := netonboard.GenDeviceKey()
	c.Assert(err, IsNil)
	s.onbDevKey = onbDevKey
}

func (s *protoSuite) dump(c *C, b []byte) {
	c.Logf("%03d %s\n", len(b), b)
}

func (s *protoSuite) SetUpTest(c *C) {
	s.dev = &netonboard.Device{}
	s.cftor = &netonboard.Configurator{}

	s.dev.SetOnboardingSecret(s.onbs)
	s.dev.SetOnboardingDeviceKey(s.onbDevKey)
}

func (s *protoSuite) TestSessionUpToDeviceConfiguratorDoesNotKnowOnboardingDeviceKey(c *C) {
	hello, err := s.cftor.Hello()
	c.Assert(err, IsNil)
	s.dump(c, hello)

	err = s.dev.RcvHello(hello)
	c.Assert(err, IsNil)

	dev, err := s.dev.Device()
	c.Assert(err, IsNil)
	s.dump(c, dev)

	err = s.cftor.RcvDevice(dev)
	c.Assert(err, IsNil)
}

func (s *protoSuite) TestSessionStartConfiguratorKnowsOnboardingDeviceKey(c *C) {
	err := s.cftor.SetOnboardingSecret(s.onbs)
	c.Assert(err, IsNil)
	err = s.cftor.SetOnboardingDeviceKey(&s.onbDevKey.PublicKey)
	c.Assert(err, IsNil)

	hello, err := s.cftor.Hello()
	c.Assert(err, IsNil)
	s.dump(c, hello)

	err = s.dev.RcvHello(hello)
	c.Assert(err, IsNil)

	dev, err := s.dev.Device()
	c.Assert(err, IsNil)
	s.dump(c, dev)

	err = s.cftor.RcvDevice(dev)
	c.Assert(err, IsNil)

	ss, err := s.cftor.SessionSetup()
	c.Assert(err, IsNil)
	s.dump(c, ss)

	err = s.dev.RcvSessionSetup(ss)
	c.Assert(err, IsNil)

	rdy, err := s.dev.Ready(nil)
	c.Assert(err, IsNil)
	s.dump(c, rdy)

	d, err := s.cftor.RcvReady(rdy)
	c.Assert(err, IsNil)
	c.Assert(d, HasLen, 0)
}

func (s *protoSuite) setupSession(c *C) {
	err := s.cftor.SetOnboardingSecret(s.onbs)
	c.Assert(err, IsNil)
	err = s.cftor.SetOnboardingDeviceKey(&s.onbDevKey.PublicKey)
	c.Assert(err, IsNil)

	hello, err := s.cftor.Hello()
	c.Assert(err, IsNil)

	err = s.dev.RcvHello(hello)
	c.Assert(err, IsNil)

	dev, err := s.dev.Device()
	c.Assert(err, IsNil)

	err = s.cftor.RcvDevice(dev)
	c.Assert(err, IsNil)

	ss, err := s.cftor.SessionSetup()
	c.Assert(err, IsNil)

	err = s.dev.RcvSessionSetup(ss)
	c.Assert(err, IsNil)
}

func (s *protoSuite) TestReadyWithData(c *C) {
	s.setupSession(c)

	rdy, err := s.dev.Ready(map[string]interface{}{
		"networks": []string{"a", "b"},
	})
	c.Assert(err, IsNil)
	s.dump(c, rdy)

	d, err := s.cftor.RcvReady(rdy)
	c.Assert(err, IsNil)
	c.Assert(d, DeepEquals, map[string]interface{}{
		"networks": []interface{}{"a", "b"},
	})
}

func (s *protoSuite) TestCfgReply(c *C) {
	s.setupSession(c)

	rdy, err := s.dev.Ready(nil)
	c.Assert(err, IsNil)

	d, err := s.cftor.RcvReady(rdy)
	c.Assert(err, IsNil)

	cfg, err := s.cftor.Cfg(map[string]interface{}{
		"list": true,
	})
	c.Assert(err, IsNil)
	s.dump(c, cfg)

	d, err = s.dev.RcvCfg(cfg)
	c.Assert(err, IsNil)
	c.Assert(d, DeepEquals, map[string]interface{}{
		"list": true,
	})

	reply, err := s.dev.Reply(map[string]interface{}{
		"list": []string{"a", "b", "c"},
	})
	c.Assert(err, IsNil)
	s.dump(c, reply)

	d, err = s.cftor.RcvReply(reply)
	c.Assert(err, IsNil)
	c.Assert(d, DeepEquals, map[string]interface{}{
		"list": []interface{}{"a", "b", "c"},
	})
}

func (s *protoSuite) TestFatalInsteadOfDevice(c *C) {
	hello, err := s.cftor.Hello()
	c.Assert(err, IsNil)
	s.dump(c, hello)

	e := &netonboard.Error{
		Code: netonboard.ProtocolErrorCode,
		Msg:  "bad nonce",
	}
	f, err := netonboard.Fatal(e)
	c.Assert(err, IsNil)
	s.dump(c, f)

	err = s.cftor.RcvDevice(f)
	c.Assert(err, DeepEquals, netonboard.FatalError{Err: e})

	// wouldn't be sent back again
	b, err2 := netonboard.Fatal(err)
	c.Check(b, IsNil)
	c.Check(err2, Equals, err)
}

func (s *protoSuite) TestFatalInsteadOfSessionSetup(c *C) {
	err := s.cftor.SetOnboardingSecret(s.onbs)
	c.Assert(err, IsNil)
	err = s.cftor.SetOnboardingDeviceKey(&s.onbDevKey.PublicKey)
	c.Assert(err, IsNil)

	hello, err := s.cftor.Hello()
	c.Assert(err, IsNil)
	s.dump(c, hello)

	err = s.dev.RcvHello(hello)
	c.Assert(err, IsNil)

	dev, err := s.dev.Device()
	c.Assert(err, IsNil)
	s.dump(c, dev)

	e := &netonboard.Error{
		Code: netonboard.InvalidDeviceKeyOrMsgSignatureCode,
		Msg:  "can't verify device signature",
	}
	f, err := netonboard.Fatal(e)
	c.Assert(err, IsNil)
	s.dump(c, f)

	err = s.dev.RcvSessionSetup(f)
	c.Assert(err, DeepEquals, netonboard.FatalError{Err: e})
}

func (s *protoSuite) TestFatalInsteadOfReady(c *C) {
	s.setupSession(c)

	e := &netonboard.Error{
		Code: netonboard.InvalidSecretOrMsgSignatureCode,
		Msg:  "can't verify session against secret",
	}
	f, err := netonboard.Fatal(e)
	c.Assert(err, IsNil)
	s.dump(c, f)

	d, err := s.cftor.RcvReady(f)
	c.Assert(err, DeepEquals, netonboard.FatalError{Err: e})
	c.Check(d, HasLen, 0)
}

func (s *protoSuite) TestFatalInsteadOfCfg(c *C) {
	s.setupSession(c)

	rdy, err := s.dev.Ready(nil)
	c.Assert(err, IsNil)

	d, err := s.cftor.RcvReady(rdy)
	c.Assert(err, IsNil)

	e := &netonboard.Error{
		Code: netonboard.ProtocolErrorCode,
		Msg:  "bad message",
	}
	f, err := netonboard.Fatal(e)
	c.Assert(err, IsNil)
	s.dump(c, f)

	d, err = s.dev.RcvCfg(f)
	c.Assert(err, DeepEquals, netonboard.FatalError{Err: e})
	c.Check(d, HasLen, 0)

}
