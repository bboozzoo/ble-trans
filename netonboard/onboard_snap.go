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

/* These are requests and results used by the API between snapd and
   the onboarding snap */

import (
	jose "gopkg.in/square/go-jose.v2"
)

type OnboardInfo struct {
	// XXX network connectivity info
	Onboarded  bool `json:"onboarded"`
	InProgress bool `json:"in-progress"`
}

type OnboardSecrets struct {
	Secret    []byte           `json:"secret"`
	DeviceKey *jose.JSONWebKey `json:"device-key"`
}

type OnboardSessionAction struct {
	// Action can be start|handle|reply|...
	Action string `json:"action"`
	Msg    []byte `json:"msg,omitempty"`

	// Fields for reply action.
	Exchange string                 `json:"exchange,omitempty"`
	D        map[string]interface{} `json:"d,omitempty"`

	// Fields for fatal action.
	FatalCode ErrorCode `json:"fatal-code,omitempty"`
	FatalMsg  string    `json:"fatal-msg,omitempty"`
}

type OnboardSessionResponse struct {
	MsgType string `json:"msg-type,omitempty"`
	// XXX we will need to support many messages if we do splitting
	// to limit cfg/reply sizes
	Msg []byte `json:"msg,omitempty"`

	// Exchange is set when snapd and the onboarding snap need
	// to collaborate to produce a reply. Msg is then empty.
	// In that case the onboarding snap needs to compute values
	// Based on MsgType and D, and invoke action=reply
	// with the exchange value until the a response with msg
	// is produced.
	Exchange string `json:"exchange,omitempty"`

	D map[string]interface{} `json:"d,omitempty"`
}

type ErrorKind string

const (
	ErrorKindFatal         ErrorKind = "netonboard-fatal"
	ErrorKindFatalReceived ErrorKind = "netonboard-fatal-received"
)

type ErrorValue struct {
	Code  ErrorCode `json:"code,omitempty"`
	Fatal []byte    `json:"fatal,omitempty"`
}
