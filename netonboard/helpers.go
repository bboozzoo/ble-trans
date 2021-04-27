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
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/json"
	"fmt"

	jose "gopkg.in/square/go-jose.v2"
)

func genNonce() ([]byte, error) {
	n := make([]byte, nonceSize)
	_, err := jose.RandReader.Read(n)
	if err != nil {
		return nil, fmt.Errorf("can't generate nonce: %v", err)
	}
	return n, nil
}

func GenSecret() ([]byte, error) {
	s := make([]byte, secretSize)
	_, err := jose.RandReader.Read(s)
	if err != nil {
		return nil, fmt.Errorf("can't generate secret: %v", err)
	}
	return s, nil
}

func GenDeviceKey() (*ecdsa.PrivateKey, error) {
	dk, err := ecdsa.GenerateKey(elliptic.P256(), jose.RandReader)
	if err != nil {
		return nil, fmt.Errorf("can't generate device key: %v", err)
	}
	return dk, nil
}

func Fatal(e error) ([]byte, error) {
	// refuse to send back a fatalerror
	if _, ok := e.(FatalError); ok {
		return nil, e
	}
	noe, ok := e.(*Error)
	if !ok {
		noe = &Error{
			Code: InternalErrorCode,
			Msg:  e.Error(),
		}
	}
	b, err := json.Marshal(&fatal{
		M:    "fatal",
		Code: int(noe.Code),
		Msg:  noe.Msg,
	})
	if err != nil {
		return nil, fmt.Errorf("can't serialize fatal message")
	}
	return b, nil
}

func parseFatal(b []byte, invalidFmt string, v ...interface{}) error {
	var f fatal
	err := json.Unmarshal(b, &f)
	if err != nil || f.M != "fatal" {
		return invalidMsg(invalidFmt, v...)
	}
	code := ErrorCode(f.Code)
	if code < InternalErrorCode || code >= UnknownCode {
		code = UnknownCode
	}
	return FatalError{Err: &Error{
		Code: code,
		Msg:  f.Msg,
	}}
}
