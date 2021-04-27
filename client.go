package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/go-ble/ble"

	log "github.com/sirupsen/logrus"
	// "github.com/mvo5/ble-trans/netonboard"
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

	snapdChar := p.FindCharacteristic(ble.NewCharacteristic(ble.MustParse(UUIDBase + onboardingCharHandle + UUIDSuffix)))
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
		offset, err := readUint32Size(req)
		if err != nil {
			log.Errorf("cannot unpack new offset: %v", err)
			return
		}
		log.Tracef("new offset: %v", offset)
	}))

	data := make([]byte, 0, size)
	got := uint32(0)
	// read file
	if err := cln.WriteCharacteristic(transmitChar, asUint32Size(0), false); err != nil {
		return fmt.Errorf("cannot rewind: %v", err)
	}
	for {
		// XXX: use ReadLongCharacteristic once server supports ReadBlob?
		currentData, err := cln.ReadCharacteristic(transmitChar)
		if err != nil {
			return fmt.Errorf("cannot read characteristic: %v", err)
		}
		data = append(data, currentData...)
		log.Tracef("got %v bytes", len(currentData))
		chunkOffset, err := readUint32SizeFromDescriptor(cln, transmitChunk)
		if err != nil {
			return fmt.Errorf("cannot read chunk: %v", err)
		}
		log.Tracef("chunk offset: %v", chunkOffset)
		got += uint32(len(currentData))
		log.Tracef("got: %v", got)
		if got == size {
			log.Infof("got everything")
			log.Tracef("data:\n%s", string(data))
			break
		}
	}

	toSendData, err := ioutil.ReadFile("client.go")
	if err != nil {
		return fmt.Errorf("cannot read file: %v", err)
	}
	if err := sendData(cln, 200, toSendData); err != nil {
		return fmt.Errorf("cannot send data: %v", err)
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			data, err := cln.ReadLongCharacteristic(snapdChar)
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

func sendData(cln ble.Client, mtu int, data []byte) error {
	p := cln.Profile()
	char := p.FindCharacteristic(ble.NewCharacteristic(ble.MustParse(UUIDBase + transmitRequestCharHandle + UUIDSuffix)))
	desc := p.FindDescriptor(ble.NewDescriptor(ble.MustParse(UUIDBase + transmitRequestSizeDescrHandle + UUIDSuffix)))

	log.Tracef("send %v bytes of data", len(data))
	start := 0
	for {
		count := mtu
		if start+count > len(data) {
			count = len(data) - start
		}
		chunk := data[start : start+count]
		log.Tracef("sending %v bytes, offset %v/%v", len(chunk), start, len(data))
		if err := cln.WriteCharacteristic(char, chunk, false); err != nil {
			log.Errorf("cannot send: %v", err)
			return err
		}
		start += len(chunk)
		size, err := readUint32SizeFromDescriptor(cln, desc)
		if err != nil {
			return fmt.Errorf("cannot read size: %v", err)
		}
		log.Tracef("size at client: %v", size)
		if start == len(data) && len(chunk) == 0 {
			log.Tracef("all done")
			break
		}
	}
	return nil
}

func runConfigurator(addr string) error {
	ct := newConfiguratorTransport()
	cfg := NewConfiguratorFor(addr, ct)
	return cfg.Configure()
}

type bleConfiguratorTransport struct {
	c   ble.Client
	mtu uint

	onboardingService *ble.Service

	// configurator -> dev
	requestChar     *ble.Characteristic
	requestSizeDesc *ble.Descriptor

	// dev -> configurator
	responseChar       *ble.Characteristic
	responseSizeDesc   *ble.Descriptor
	responseOffsetDesc *ble.Descriptor

	onboardingChar  *ble.Characteristic
	stateDesc       *ble.Descriptor
	currentState    State
	stateLock       sync.Mutex
	stateNotifyFunc func(State) (clear bool)
}

func newConfiguratorTransport() *bleConfiguratorTransport {
	return &bleConfiguratorTransport{}
}

func hasOnboardingService(a ble.Advertisement) bool {
	onboardingSvc := ble.MustParse(OnboardingServiceUUID)
	found := false
	for _, svc := range a.Services() {
		if svc.Equal(onboardingSvc) {
			found = true
			break
		}
	}
	return found
}

func (b *bleConfiguratorTransport) Connect(addr string) error {
	ctx := ble.WithSigHandler(context.WithTimeout(context.Background(), scanTimeout))

	filter := func(a ble.Advertisement) bool {
		log.Debugf("found device: %v (%v)", a.Addr(), a.LocalName())
		hasOnboarding := hasOnboardingService(a)
		addressMatch := strings.ToLower(a.Addr().String()) == addr
		if addressMatch && !hasOnboarding {
			log.Warnf("matching MAC but no onboarding service")
		}
		return addressMatch
	}

	cln, err := ble.Connect(ctx, filter)
	if err != nil {
		return fmt.Errorf("cannot connect to %s: %v", addr, err)
	}
	b.c = cln

	log.Infof("discovering profiles")
	p, err := b.c.DiscoverProfile(true)
	if err != nil {
		return fmt.Errorf("cannot discover profiles: %v", err)
	}

	log.Tracef("changing MTU to %v", MTU)
	txMtu, err := b.c.ExchangeMTU(MTU)
	if err != nil {
		return fmt.Errorf("cannot change MTU: %v", err)
	}
	log.Infof("MTU: %v\n", txMtu)
	if txMtu != MTU {
		log.Warnf("got different MTU %v", txMtu)
	}
	b.mtu = uint(txMtu)

	onboardingService := ble.NewService(ble.MustParse(OnboardingServiceUUID))
	srv := p.FindService(onboardingService)
	if srv == nil {
		log.Warnf("service not found")
		return nil
	}

	b.onboardingService = srv

	snapdChar := p.FindCharacteristic(ble.NewCharacteristic(ble.MustParse(OnboardingCharUUID)))
	if snapdChar == nil {
		return fmt.Errorf("characteristic not found")
	}

	b.responseChar = p.FindCharacteristic(ble.NewCharacteristic(ble.MustParse(ResponseCharUUID)))
	if b.responseChar == nil {
		return fmt.Errorf("cannot find response characteristic")
	}
	b.responseSizeDesc = p.FindDescriptor(ble.NewDescriptor(ble.MustParse(ResponsePropSizeUUID)))
	if b.responseSizeDesc == nil {
		return fmt.Errorf("cannot find response size descriptor")
	}
	b.responseOffsetDesc = p.FindDescriptor(ble.NewDescriptor(ble.MustParse(ResponsePropChunkStartUUID)))
	if b.responseOffsetDesc == nil {
		return fmt.Errorf("cannot find response offset descriptor")
	}

	b.requestChar = p.FindCharacteristic(ble.NewCharacteristic(ble.MustParse(RequestCharUUID)))
	if b.requestChar == nil {
		return fmt.Errorf("cannot find request transmit characteristic")
	}
	b.requestSizeDesc = p.FindDescriptor(ble.NewDescriptor(ble.MustParse(RequestSizePropUUID)))
	if b.requestSizeDesc == nil {
		return fmt.Errorf("cannot find request transmit size descriptor")
	}

	b.onboardingChar = p.FindCharacteristic(ble.NewCharacteristic(ble.MustParse(OnboardingCharUUID)))
	if b.onboardingChar == nil {
		return fmt.Errorf("cannot find onboarding characteristic")
	}
	b.stateDesc = p.FindDescriptor(ble.NewDescriptor(ble.MustParse(OnboardingStatePropUUID)))
	if b.stateDesc == nil {
		return fmt.Errorf("cannot find onboarding state descriptor")
	}
	b.c.Subscribe(b.onboardingChar, false, ble.NotificationHandler(func(req []byte) {
		b.stateLock.Lock()
		defer b.stateLock.Unlock()

		log.Tracef("got req: %x", req)
		newState, err := readUint32Size(req)
		if err != nil {
			log.Errorf("cannot unpack new offset: %v", err)
			return
		}
		log.Tracef("state change: %v -> %v", b.currentState, newState)
		b.currentState = State(newState)
		if b.stateNotifyFunc != nil && b.stateNotifyFunc(b.currentState) {
			b.stateNotifyFunc = nil
		}
	}))

	return nil
}
func (b *bleConfiguratorTransport) Disconnect() error {
	if b.c == nil {
		return nil
	}
	log.Infof("disconnecting from %v", b.c.Addr())
	if err := b.c.CancelConnection(); err != nil {
		return fmt.Errorf("cannot disconnect: %v", err)
	}
	<-b.c.Disconnected()
	log.Warnf("disconnected...")
	return nil
}

func (b *bleConfiguratorTransport) Send(data []byte) error {
	return sendData(b.c, int(b.mtu), data)
}

func (b *bleConfiguratorTransport) Receive() ([]byte, error) {
	size, err := readUint32SizeFromDescriptor(b.c, b.responseSizeDesc)
	if err != nil {
		return nil, fmt.Errorf("cannot read response size: %v", err)
	}
	log.Infof("response size: %v", size)

	// rewind
	if err := b.c.WriteCharacteristic(b.responseChar, asUint32Size(0), false); err != nil {
		return nil, fmt.Errorf("cannot rewind: %v", err)
	}
	data := make([]byte, 0, size)
	got := uint32(0)

	for {
		// XXX: use ReadLongCharacteristic once server supports ReadBlob?
		currentData, err := b.c.ReadCharacteristic(b.responseChar)
		if err != nil {
			return nil, fmt.Errorf("cannot read characteristic: %v", err)
		}
		data = append(data, currentData...)
		log.Tracef("got %v bytes", len(currentData))
		// XXX: do we need this?
		chunkOffset, err := readUint32SizeFromDescriptor(b.c, b.responseOffsetDesc)
		if err != nil {
			return nil, fmt.Errorf("cannot read chunk: %v", err)
		}
		log.Tracef("chunk offset: %v", chunkOffset)

		got += uint32(len(currentData))
		log.Tracef("got: %v", got)
		if got == size {
			log.Infof("got everything")
			log.Tracef("data:\n%s", string(data))
			break
		}
	}
	return data, nil
}

func (b *bleConfiguratorTransport) WaitForState(ctx context.Context, state State) error {
	b.stateLock.Lock()

	log.Tracef("wait for state %v (current %v)", state, b.currentState)
	if b.currentState == state {
		b.stateLock.Unlock()
		return nil
	}

	inExpectedState := make(chan struct{}, 1)

	b.stateNotifyFunc = func(newState State) (clear bool) {
		if newState == state {
			inExpectedState <- struct{}{}
			return true
		}
		return false
	}
	b.stateLock.Unlock()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-inExpectedState:
			log.Tracef("reached state %v", state)
		}
	}
	return nil
}
