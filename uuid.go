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
	OnboardingServiceUUID = OnboardingUUID(serviceHandle)

	OnboardingCharUUID      = OnboardingUUID(onboardingCharHandle)
	OnboardingStatePropUUID = OnboardingUUID(stateDescrHandle)
	OnboardingErrorPropUUID = OnboardingUUID(errDescrHandle)

	ResponseCharUUID           = OnboardingUUID(transmitCharHandle)
	ResponsePropChunkStartUUID = OnboardingUUID(transmitChunkDescrHandle)
	ResponsePropSizeUUID       = OnboardingUUID(transmitSizeDescrHandle)

	RequestCharUUID     = OnboardingUUID(transmitRequestCharHandle)
	RequestSizePropUUID = OnboardingUUID(transmitRequestSizeDescrHandle)
)

func OnboardingUUID(handle string) string {
	return UUIDBase + handle + UUIDSuffix
}
