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

const (

	// 99df99df-0000-1000-8000-00805f9b34fb
	// 99df99e0-0000-1000-8000-00805f9b34fb
	// 99df99e1-0000-1000-8000-00805f9b34fb

	UUIDBase   = "99df"
	UUIDSuffix = "-0000-1000-8000-00805f9b34fb"
	//UUIDSuffix            = "-d598-4874-8e86-7d042ee07ba"
	serviceHandle        = "99df"
	onboardingCharHandle = "99e0"
	stateDescrHandle     = "99e1"
	errDescrHandle       = "99e2"

	transmitCharHandle       = "99d0"
	transmitChunkDescrHandle = "99d1"
	transmitSizeDescrHandle  = "99d2"

	transmitRequestCharHandle      = "99c0"
	transmitRequestSizeDescrHandle = "99c1"
)

var (
	// UUID of the onboarding service implemented by the device
	OnboardingServiceUUID = OnboardingUUID(serviceHandle)

	// UUID of the onboarding characteristic
	OnboardingCharUUID = OnboardingUUID(onboardingCharHandle)
	// UUID of the onboarding state descriptor
	OnboardingStatePropUUID = OnboardingUUID(stateDescrHandle)
	// UUID of the onbiarding error descriptor
	OnboardingErrorPropUUID = OnboardingUUID(errDescrHandle)

	// UUID of the response characteristic, this is where responses from the
	// device can be obtained. Reading is done when the value read from the
	// characteristic is smaller than the MTU, or the total size accumulated
	// so far equals that held by the size descriptor.
	ResponseCharUUID = OnboardingUUID(transmitCharHandle)
	// UUID of the descriptor holding the offset of the currently read chunk
	// of the response
	ResponseChunkStartPropUUID = OnboardingUUID(transmitChunkDescrHandle)
	// UUID of the descriptor holding the total size of the response
	ResponseSizePropUUID = OnboardingUUID(transmitSizeDescrHandle)

	// UUID of the request characteristic, this is where requests from the
	// configurator are written to. The is considered done when the
	// configurator writes a chunk of data smaller than the MTU.
	RequestCharUUID = OnboardingUUID(transmitRequestCharHandle)
	// UUID of the descriptor holding the total size of the data written so
	// far
	RequestSizePropUUID = OnboardingUUID(transmitRequestSizeDescrHandle)
)

func OnboardingUUID(handle string) string {
	return UUIDBase + handle + UUIDSuffix
}
