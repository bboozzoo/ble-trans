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
	"fmt"

	jose "gopkg.in/square/go-jose.v2"
)

const (
	nonceSize   = 16
	secretSize  = 32
	contEnc     = jose.A256GCM
	sessKeySize = 32
)

type hello struct {
	// M is hello
	M      string `json:"m"`
	Nonce1 []byte `json:"n1"`

	// XXX: list of protocols/versions
}

// for JOSE messages the message type m goes into the protected headers

type device struct {
	// M is device
	// this message is wrapped in a JWS object signed by the device
	// onboarding key

	Key    *jose.JSONWebKey `json:"k"`
	Nonce1 []byte           `json:"n1"`
	Nonce2 []byte           `json:"n2"`

	// XXX: list of protocols/versions
	// XXX: minimal device info
	// XXX max sizes?
}

type sessionSetup struct {
	// M is session
	// this goes from the configurator to the device as JWS object
	// "signed" with the onboarding secret and then itself
	// encrypted usign ECDH-ES against the device onboarding key

	SessionKey []byte `json:"sek"`
	Nonce2     []byte `json:"n2"`
}

type deviceReady struct {
	// M is ready
	// from device, this and messages after this are encrypted
	// with the session key
	// Good point just before replying with this to start
	// signaling visually that the device is fully engaged being configured

	// Seq must be 1
	Seq int `json:"sq"`

	Nonce1 []byte `json:"n1"`

	// D can be set optionally to give upfront device information
	// relevant to configuration
	D map[string]interface{} `json:"d,omitempty"`

	// XXX include in D or separately a list of supported
	// configuration facets, they could look like:
	// wifi, onboard, subiquity? or brand-foo
}

type exchg struct {
	// M is cfg if from configurator to device
	// M is reply if from device to configurator
	// XXX need a way to split these into multiple messages if they
	// are too big

	// Seq is the message sequence number
	// ready gets 1, first reply gets 2, then 3 etc
	// first cfg gets 1, 2nd cfg gets 2, etc
	Seq int `json:"sq"`

	// D is for directives or data
	D map[string]interface{} `json:"d"`
}

// XXX do we need some support for keep alive messages?

type notification struct {
}

// type answer

type fatal struct {
	// M is fatal
	M string `json:"m"`

	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type ErrorCode int

const (
	InternalErrorCode ErrorCode = iota + 1
	ProtocolErrorCode
	InvalidMsgCode
	InvalidEncryptedMsgCode
	InvalidSecretOrMsgSignatureCode
	InvalidDeviceKeyOrMsgSignatureCode
	UnknownCode
)

type Error struct {
	Code ErrorCode
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (%d)", e.Msg, e.Code)
}

func errorMaker(code ErrorCode) func(msgFmt string, v ...interface{}) *Error {
	return func(msgFmt string, v ...interface{}) *Error {
		return &Error{
			Code: code,
			Msg:  fmt.Sprintf(msgFmt, v...),
		}
	}
}

var (
	internal                       = errorMaker(InternalErrorCode)
	invalidMsg                     = errorMaker(InvalidMsgCode)
	invalidEncryptedMsg            = errorMaker(InvalidEncryptedMsgCode)
	invalidSecretOrMsgSignature    = errorMaker(InvalidSecretOrMsgSignatureCode)
	invalidDeviceKeyOrMsgSignature = errorMaker(InvalidDeviceKeyOrMsgSignatureCode)
	protocol                       = errorMaker(ProtocolErrorCode)
)

// FatalError represents when the counterparty sent back a fatal message.
type FatalError struct {
	Err *Error
}

func (fe FatalError) Error() string {
	return fe.Err.Error()
}
