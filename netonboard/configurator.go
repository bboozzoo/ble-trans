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

package netonboard

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/json"
	"fmt"

	jose "gopkg.in/square/go-jose.v2"
)

// Configurator implements the configurator protocol side that wants
// to configure and network onboard a device.
type Configurator struct {
	onbSecret []byte
	onbDevKey *ecdsa.PublicKey
	nonce1    []byte
	nonce2    []byte
	sek       []byte

	receivedSeq int
	seq         int

	ready  bool
	cfgEnc jose.Encrypter
}

// XXX SetOnboardingSecretFromPassphrase, generate the secret by using a KDF

func (c *Configurator) SetOnboardingSecret(s []byte) error {
	if len(s) != secretSize {
		return fmt.Errorf("onboarding secret has the wrong size")
	}
	c.onbSecret = s
	return nil
}

func (c *Configurator) SetOnboardingDeviceKey(k *ecdsa.PublicKey) error {
	if k.Curve != elliptic.P256() {
		return fmt.Errorf("expected P256 key")
	}
	c.onbDevKey = k
	return nil
}

func (c *Configurator) Hello() ([]byte, error) {
	nonce1, err := genNonce()
	if err != nil {
		return nil, err
	}
	c.nonce1 = nonce1
	b, err := json.Marshal(&hello{
		M:      "hello",
		Nonce1: nonce1,
	})
	if err != nil {
		return nil, fmt.Errorf("can't serialize hello: %v", err)
	}
	return b, nil
}

// 1.15 can do this for us
func cmpPK(pk1, pk2 *ecdsa.PublicKey) bool {
	return pk1.X.Cmp(pk2.X) == 0 && pk1.Y.Cmp(pk2.Y) == 0 && pk1.Curve == pk2.Curve
}

func (c *Configurator) RcvDevice(b []byte) error {
	if c.nonce1 == nil {
		return internal("nonce1 must have been sent")
	}
	sig, err := jose.ParseSigned(string(b))
	if err != nil {
		return parseFatal(b, "can't parse signed device: %v", err)
	}
	if len(sig.Signatures) != 1 || sig.Signatures[0].Protected.ExtraHeaders["m"] != "device" {
		return invalidMsg("invalid device message")
	}
	var dev device
	if c.onbDevKey == nil {
		// XXX have a flag to allow proceeding without knowing
		// the onboarding device key beforehand
		b := sig.UnsafePayloadWithoutVerification()
		if err = json.Unmarshal(b, &dev); err != nil {
			return invalidMsg("can't deserialize device: %v", err)
		}
		_, err = sig.Verify(dev.Key)
		if err != nil {
			return invalidDeviceKeyOrMsgSignature("can't verify device signature: %v", err)
		}
		pk, ok := dev.Key.Key.(*ecdsa.PublicKey)
		if !ok && pk.Curve != elliptic.P256() {
			return invalidDeviceKeyOrMsgSignature("device didn't advertise expected public key")
		}
		c.onbDevKey = pk
	} else {
		b, err = sig.Verify(c.onbDevKey)
		if err != nil {
			return invalidDeviceKeyOrMsgSignature("can't verify device signature: %v", err)
		}
		if err = json.Unmarshal(b, &dev); err != nil {
			return invalidMsg("can't deserialize device: %v", err)
		}
		pk, ok := dev.Key.Key.(*ecdsa.PublicKey)
		if !ok && !cmpPK(pk, c.onbDevKey) {
			return invalidDeviceKeyOrMsgSignature("device didn't advertise expected public key")
		}
	}
	if !bytes.Equal(c.nonce1, dev.Nonce1) {
		return protocol("device didn't sign correct nonce")
	}
	if len(dev.Nonce2) != nonceSize {
		return protocol("nonce2 has the wrong size")
	}
	c.nonce2 = dev.Nonce2
	return nil
}

func (c *Configurator) SessionSetup() ([]byte, error) {
	if c.onbSecret == nil {
		return nil, internal("onboarding secret must be set")
	}
	if c.nonce2 == nil {
		return nil, protocol("nonce2 must have been received")
	}
	// generate session key
	sek := make([]byte, sessKeySize)
	if _, err := jose.RandReader.Read(sek); err != nil {
		return nil, internal("cannot generate session key")
	}
	c.sek = sek

	sopts := &jose.SignerOptions{}
	cftorSign, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.HS256,
		Key:       c.onbSecret,
	}, sopts.WithBase64(true))
	if err != nil {
		return nil, internal("can't prepare to hash session")
	}
	b, err := json.Marshal(&sessionSetup{
		SessionKey: c.sek,
		Nonce2:     c.nonce2,
	})
	if err != nil {
		return nil, internal("can't serialize session")
	}
	signed, err := cftorSign.Sign(b)
	if err != nil {
		return nil, internal("can't hash session")
	}

	eopts := &jose.EncrypterOptions{}
	devRcpt := jose.Recipient{
		Algorithm: jose.ECDH_ES,
		Key:       c.onbDevKey,
	}
	enc, err := jose.NewEncrypter(contEnc, devRcpt, eopts.WithHeader("m", "session"))
	if err != nil {
		return nil, internal("can't prepare session encryption")
	}
	encrypted, err := enc.Encrypt([]byte(signed.FullSerialize()))
	if err != nil {
		return nil, internal("can't encrypt session")
	}
	return []byte(encrypted.FullSerialize()), nil
}

func (c *Configurator) sessDecrypt(b []byte, m string) ([]byte, error) {
	if c.sek == nil {
		return nil, internal("session key must be established")
	}
	encrypted, err := jose.ParseEncrypted(string(b))
	if err != nil {
		return nil, parseFatal(b, "can't parse %s", m)
	}
	if encrypted.Header.ExtraHeaders["m"] != m {
		return nil, protocol("expected %s", m)
	}
	b, err = encrypted.Decrypt(c.sek)
	if err != nil {
		return nil, invalidEncryptedMsg("can't decrypt %s", m)
	}
	return b, nil
}

func (c *Configurator) RcvReady(b []byte) (map[string]interface{}, error) {
	b, err := c.sessDecrypt(b, "ready")
	if err != nil {
		return nil, err
	}
	var deviceReady deviceReady
	if err := json.Unmarshal(b, &deviceReady); err != nil {
		return nil, invalidMsg("can't deserialize ready")
	}
	if !bytes.Equal(c.nonce1, deviceReady.Nonce1) {
		return nil, protocol("device didn't reply ready with correct nonce")
	}
	c.receivedSeq++
	if deviceReady.Seq != c.receivedSeq || c.receivedSeq != 1 {
		return nil, protocol("out of sequence ready")
	}
	c.ready = true
	return deviceReady.D, nil
}

func (c *Configurator) sessEnc(m string) (jose.Encrypter, error) {
	if c.sek == nil {
		return nil, internal("session key must be established")
	}
	rcpt := jose.Recipient{
		Algorithm: jose.DIRECT,
		Key:       c.sek,
	}
	eopts := &jose.EncrypterOptions{}
	enc, err := jose.NewEncrypter(contEnc, rcpt, eopts.WithHeader("m", m))
	if err != nil {
		return nil, internal("can't prepare session key encryption")
	}
	return enc, nil
}

func (c *Configurator) Cfg(directives map[string]interface{}) ([]byte, error) {
	if !c.ready {
		return nil, protocol("must have received ready")
	}
	if c.cfgEnc == nil {
		enc, err := c.sessEnc("cfg")
		if err != nil {
			return nil, err
		}
		c.cfgEnc = enc
	}
	c.seq++
	b, err := json.Marshal(&exchg{
		Seq: c.seq,
		D:   directives,
	})
	if err != nil {
		return nil, internal("can't serialize cfg")
	}
	encrypted, err := c.cfgEnc.Encrypt(b)
	if err != nil {
		return nil, internal("can't encrypt cfg")
	}
	return []byte(encrypted.FullSerialize()), nil
}

func (c *Configurator) RcvReply(b []byte) (map[string]interface{}, error) {
	b, err := c.sessDecrypt(b, "reply")
	if err != nil {
		return nil, err
	}
	var exchg exchg
	if err := json.Unmarshal(b, &exchg); err != nil {
		return nil, invalidMsg("can't deserialize reply")
	}
	c.receivedSeq++
	if exchg.Seq != c.receivedSeq {
		return nil, protocol("out of sequence reply")
	}
	return exchg.D, nil
}
