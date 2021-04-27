package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/go-ble/ble"

	log "github.com/sirupsen/logrus"
)

var (
	scanTimeout = 10 * time.Second
	MTU         = 512
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

func client(hwaddr string) (err error) {
	hwaddr = strings.ToLower(hwaddr)

	log.SetLevel(log.TraceLevel)
	log.Infof("Looking for %s", hwaddr)

	filter := func(a ble.Advertisement) bool {
		log.Debugf("found device: %v (%v)", a.Addr(), a.LocalName())
		return strings.ToLower(a.Addr().String()) == hwaddr
	}

	ctx := ble.WithSigHandler(context.WithTimeout(context.Background(), scanTimeout))
	cln, err := ble.Connect(ctx, filter)
	if err != nil {
		return fmt.Errorf("cannot connect to %s: %v", hwaddr, err)
	}

	defer func() {
		log.Infof("Disconnecting [ %s ]... (this might take up to few seconds on OS X)\n", cln.Addr())
		cln.CancelConnection()
		<-cln.Disconnected()
		log.Warnf("[ %s ] is disconnected \n", cln.Addr())
	}()

	log.Infof("Discovering profile...\n")
	p, err := cln.DiscoverProfile(true)
	if err != nil {
		log.Fatalf("can't discover profile: %s", err)
	}

	log.Tracef("changing MTU to %v", MTU)
	txMtu, err := cln.ExchangeMTU(MTU)
	if err != nil {
		return fmt.Errorf("cannot change MTU: %v", err)
	}
	log.Infof("MTU: %v\n", txMtu)
	if txMtu != MTU {
		log.Warnf("got different MTU %v", txMtu)
	}

	// find descriptor already?
	onboardingService := ble.NewService(ble.MustParse(OnboardingServiceUUID))
	srv := p.FindService(onboardingService)
	if srv == nil {
		log.Warnf("service not found")
		return nil
	}

	snapdChar := p.FindCharacteristic(ble.NewCharacteristic(ble.MustParse(UUIDBase + commCharHandle + UUIDSuffix)))
	if snapdChar == nil {
		return fmt.Errorf("characteristic not found")
	}

	transmitChar := p.FindCharacteristic(ble.NewCharacteristic(ble.MustParse(UUIDBase + transmitCharHandle + UUIDSuffix)))
	if transmitChar == nil {
		return fmt.Errorf("transmit characteristic not found")
	}
	transmitDataSize := p.FindDescriptor(ble.NewDescriptor(ble.MustParse(UUIDBase + transmitSizeDescrHandle + UUIDSuffix)))
	if transmitDataSize == nil {
		return fmt.Errorf("cannot find transmit size descriptor")
	}
	transmitChunk := p.FindDescriptor(ble.NewDescriptor(ble.MustParse(UUIDBase + transmitChunkDescrHandle + UUIDSuffix)))
	if transmitChunk == nil {
		return fmt.Errorf("cannot find transmit chunk descriptor")
	}
	size, err := readUint32SizeFromDescriptor(cln, transmitDataSize)
	if err != nil {
		return fmt.Errorf("cannot read size: %v", err)
	}
	log.Infof("data size: %v", size)

	cln.Subscribe(transmitChar, false, ble.NotificationHandler(func(req []byte) {
		log.Tracef("got req: %x", req)
	}))

	data := make([]byte, 0, size)
	got := uint32(0)
	// read file
	if err := cln.WriteCharacteristic(transmitChar, asUint32Size(0), false); err != nil {
		return fmt.Errorf("cannot rewind: %v", err)
	}
	for {
		currentData, err := cln.ReadCharacteristic(transmitChar)
		if err != nil {
			return fmt.Errorf("cannot read characteristic: %v", err)
		}
		data = append(data, currentData...)
		chunkOffset, err := readUint32SizeFromDescriptor(cln, transmitChunk)
		if err != nil {
			return fmt.Errorf("cannot read chunk: %v", err)
		}
		log.Tracef("chunk offset: %v", chunkOffset)
		got += uint32(len(currentData))
		if got == size {
			log.Infof("got everything")
			log.Tracef("data:\n%s", string(data))
			break
		}
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			data, err := cln.ReadCharacteristic(snapdChar)
			if err != nil {
				return fmt.Errorf("cannot read characteristic %v: %v", snapdChar.UUID.String(), err)
			}
			log.Infof("value: %q", string(data))
		}
	}

	return nil
}

func readUint32SizeFromDescriptor(cln ble.Client, desc *ble.Descriptor) (val uint32, err error) {
	sizeRaw, err := cln.ReadDescriptor(desc)
	if err != nil {
		return 0, fmt.Errorf("cannot read descriptor value: %v", err)
	}
	size, err := readUint32Size(sizeRaw)
	if err != nil {
		return 0, fmt.Errorf("cannot convert value: %v", err)
	}
	log.Tracef("value: %v", size)
	return size, nil
}

func readUint32Size(data []byte) (val uint32, err error) {
	if err := binary.Read(bytes.NewBuffer(data), binary.LittleEndian, &val); err != nil {
		return 0, err
	}
	return val, nil
}

func asUint32Size(val uint32) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, val)
	return buf.Bytes()
}
