package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"github.com/go-ble/ble"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	// "github.com/mvo5/ble-trans/netonboard"
)

const (
	appName = "snapd onboarding"

	descrString = "Onboarding protocol sate"
)

type Server struct {
	UUID string
}

func runServer(devName string) error {
	app := NewServer()
	// XXX: detect ctrl-c and app.Close() ?
	uuid := ble.MustParse(app.UUID)
	onboardingSvc := ble.NewService(uuid)
	data, err := ioutil.ReadFile("main.go")
	if err != nil {
		log.Fatalf("cannot open file: %v", err)
	}
	responseChar, response := NewSnapdResponseTransmit()
	onboardingSvc.AddCharacteristic(responseChar)
	response.transmitData(data)
	devChar, _ := NewSnapdDeviceChar()
	onboardingSvc.AddCharacteristic(devChar)
	requestChar, _ := NewSnapdRequestTransmit()
	onboardingSvc.AddCharacteristic(requestChar)

	if err := ble.AddService(onboardingSvc); err != nil {
		log.Fatalf("can't add service: %s", err)
	}

	// Advertise for specified duration, or until interrupted by user.
	log.Info("Advertising...")
	ctx := ble.WithSigHandler(context.WithTimeout(context.Background(), 10*60*time.Second))
	err = ble.AdvertiseNameAndServices(ctx, "Ubuntu Core", uuid)
	switch {
	case err == nil:
	case errors.Is(err, context.DeadlineExceeded):
		log.Infof("advertising finished")
	case errors.Is(err, context.Canceled):
		log.Infof("canceled")
		return fmt.Errorf("interrupted")
	default:
		log.Errorf("failed: %v", err)
		return fmt.Errorf("cannot advertise: %v", err)
	}

	select {}
}

func NewServer() *Server {
	return &Server{
		UUID: OnboardingServiceUUID,
	}
}

func NewSnapdDeviceChar() (*ble.Characteristic, *snapdDeviceChar) {
	s := snapdDeviceChar{
		stateChange: make(chan uint32, 1),
	}
	c := &ble.Characteristic{
		UUID: ble.MustParse(OnboardingCharUUID),
	}
	c.HandleRead(ble.ReadHandlerFunc(s.read))
	c.HandleWrite(ble.WriteHandlerFunc(s.written))
	c.HandleNotify(ble.NotifyHandlerFunc(s.notifyProtocolState))
	d := &ble.Descriptor{
		UUID: ble.MustParse(OnboardingStatePropUUID),
	}
	c.AddDescriptor(d)
	d.HandleRead(ble.ReadHandlerFunc(s.readProtocolState))
	return c, &s
}

type snapdDeviceChar struct {
	cnt         int
	stateChange chan uint32
	state       uint32
}

func (s *snapdDeviceChar) setState(state State) {
	s.state = uint32(state)
	s.stateChange <- uint32(state)
}

func (s *snapdDeviceChar) read(req ble.Request, rsp ble.ResponseWriter) {
	if req.Offset() == 0 {
		s.cnt++
	}
	content := fmt.Sprintf("Communication for snapd onboarding read: %v", s.cnt)
	if req.Offset() == 0 {
		log.Tracef("starting sending of %q len(%v)", content, len(content))
	}
	log.Tracef("    offset %v cap %v", req.Offset(), rsp.Cap())

	start := req.Offset()
	toSend := len(content[start:])
	if toSend > rsp.Cap() {
		toSend = rsp.Cap()
	}
	n, err := rsp.Write([]byte(content[start : start+toSend]))
	log.Tracef("sent, %v bytes, err: %v", n, err)
	if err != nil {
		return
	}
}

func (s *snapdDeviceChar) written(req ble.Request, rsp ble.ResponseWriter) {
	// support resetting to given state?

	data := req.Data()
	log.Tracef("got data: %x", data)
	log.Tracef("          %q", string(data))
	log.Tracef("          offset: %v", req.Offset())
}

func (s *snapdDeviceChar) notifyProtocolState(req ble.Request, n ble.Notifier) {
	for {
		select {
		case state := <-s.stateChange:
			log.Tracef("dev: notify state change to %v", state)
			if err := binary.Write(n, binary.LittleEndian, state); err != nil {
				log.Errorf("dev: cannot write current chunk size: %v", err)
			}
		}
	}
}

func (s *snapdDeviceChar) readProtocolState(req ble.Request, rsp ble.ResponseWriter) {
	if err := binary.Write(rsp, binary.LittleEndian, s.state); err != nil {
		log.Errorf("dev: cannot write current chunk size: %v", err)
	}
}

type snapdResponseTransmit struct {
	data      []byte
	chunkSize uint32

	currentChunkStart uint32
	currentSize       uint32
	chunkStartChange  chan uint32

	lock sync.Mutex
}

func NewSnapdResponseTransmit() (*ble.Characteristic, *snapdResponseTransmit) {
	s := snapdResponseTransmit{
		chunkStartChange: make(chan uint32, 1),
	}
	c := &ble.Characteristic{
		UUID: ble.MustParse(ResponseCharUUID),
	}
	c.HandleRead(ble.ReadHandlerFunc(s.readNextChunk))
	c.HandleWrite(ble.WriteHandlerFunc(s.handleRewindTo))
	// c.HandleNotify(ble.NotifyHandlerFunc(s.notifyNewOffset))
	dSize := &ble.Descriptor{
		UUID: ble.MustParse(ResponsePropSizeUUID),
	}
	dSize.HandleRead(ble.ReadHandlerFunc(s.readCurrentSizeDescr))
	c.AddDescriptor(dSize)
	dChunk := &ble.Descriptor{
		UUID: ble.MustParse(ResponsePropChunkStartUUID),
	}
	dChunk.HandleRead(ble.ReadHandlerFunc(s.readCurrentChunkDescr))
	c.AddDescriptor(dChunk)
	return c, &s
}

func (s *snapdResponseTransmit) transmitData(data []byte) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.data = data
	s.currentSize = uint32(len(data))
	s.currentChunkStart = 0
}

func (s *snapdResponseTransmit) chunk(size uint32) ([]byte, uint32) {
	thisChunkSize := s.currentSize - s.currentChunkStart
	if thisChunkSize > size {
		thisChunkSize = size
	}
	log.Tracef("rsp: this chunk start %v/%v size: %v", s.currentChunkStart, s.currentSize, thisChunkSize)
	return s.data[s.currentChunkStart : s.currentChunkStart+thisChunkSize], s.currentChunkStart + thisChunkSize
}

func (s *snapdResponseTransmit) advanceChunk(size uint32) (start uint32, complete bool) {
	defer func() {
		log.Tracef("advance, start: %v complete: %v", start, complete)
	}()
	log.Tracef("advance called")
	if s.currentChunkStart+size >= s.currentSize {
		s.currentChunkStart = s.currentSize
		log.Tracef("sending complete")
		return s.currentChunkStart, true
	}
	s.currentChunkStart += size
	// s.chunkStartChange <- s.currentChunkStart
	return s.currentChunkStart, false
}

func (s *snapdResponseTransmit) rewindTo(start uint32) {
	// XXX range check
	log.Tracef("rsp: rewind to: %v", start)
	s.currentChunkStart = start
	// s.chunkStartChange <- start
}

func (s *snapdResponseTransmit) readNextChunk(req ble.Request, rsp ble.ResponseWriter) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if len(s.data) == 0 {
		return
	}
	if req.Offset() == 0 {
		log.Tracef("rsp: start reading chunk")
	} else {
		// handle this properly?
		rsp.SetStatus(ble.ErrAttrNotLong)
		return
	}
	// XXX: support ReadBlob requests

	log.Tracef("rsp:    offset %v cap %v", req.Offset(), rsp.Cap())

	chunk, nextChunkStart := s.chunk(uint32(rsp.Cap()))
	log.Tracef("rsp:    chunk size: %v next offset: %v", len(chunk), nextChunkStart)

	start := req.Offset()
	toSend := len(chunk[start:])
	if toSend > rsp.Cap() {
		toSend = rsp.Cap()
	}
	contentToSend := chunk[start : start+toSend]
	n, err := rsp.Write(contentToSend)
	log.Tracef("rsp: sent, %v/%v bytes, err: %v", n, len(contentToSend), err)
	if err != nil {
		log.Errorf("rsp: writing failed: %v", err)
		return
	}
	if req.Offset()+n == len(chunk) {
		log.Tracef("chunk done")
		// whole chunk was read, advance
		next, done := s.advanceChunk(uint32(len(chunk)))
		if !done {
			log.Tracef("rsp: chunk was read, advance to %v/%v", next, s.currentSize)
		} else {
			log.Tracef("rsp: transfer done")
			s.rewindTo(0)
		}
	}
}

func (s *snapdResponseTransmit) readCurrentChunkDescr(req ble.Request, rsp ble.ResponseWriter) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if err := binary.Write(rsp, binary.LittleEndian, s.currentChunkStart); err != nil {
		log.Errorf("rsp: cannot write current chunk offset: %v", err)
	}
}

func (s *snapdResponseTransmit) readCurrentSizeDescr(req ble.Request, rsp ble.ResponseWriter) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if err := binary.Write(rsp, binary.LittleEndian, s.currentSize); err != nil {
		log.Errorf("rsp: cannot write current chunk size: %v", err)
	}
}

func (s *snapdResponseTransmit) handleRewindTo(req ble.Request, rsp ble.ResponseWriter) {
	s.lock.Lock()
	defer s.lock.Unlock()

	var offset uint32
	if err := binary.Read(bytes.NewBuffer(req.Data()), binary.LittleEndian, &offset); err != nil {
		log.Errorf("rsp: cannot read rewind-to offset: %v", err)
	}
	s.rewindTo(offset)
}

func (s *snapdResponseTransmit) notifyNewOffset(req ble.Request, n ble.Notifier) {
	log.Tracef("rsp: notify new offset")
	for {
		select {
		case newStart, ok := <-s.chunkStartChange:
			log.Tracef("rsp: notify new offset: %v (ok %v)", newStart, ok)
			if !ok {
				log.Tracef("chunk start change closed")
				return
			}
			if err := binary.Write(n, binary.LittleEndian, newStart); err != nil {
				log.Errorf("rsp: cannot notify about new chunk offset: %v", err)
			}
		}
	}
}

type snapdRequestTransmit struct {
	data      []byte
	dataReady bool

	currentSize       uint32
	currentSizeChange chan uint32

	lock sync.Mutex

	readyFunc func([]byte)
}

func NewSnapdRequestTransmit() (*ble.Characteristic, *snapdRequestTransmit) {
	s := snapdRequestTransmit{
		currentSizeChange: make(chan uint32, 1),
	}
	c := &ble.Characteristic{
		UUID: ble.MustParse(RequestCharUUID),
	}
	c.HandleWrite(ble.WriteHandlerFunc(s.handleDataWrite))
	c.HandleNotify(ble.NotifyHandlerFunc(s.notifySize))
	dSize := &ble.Descriptor{
		UUID: ble.MustParse(RequestSizePropUUID),
	}
	dSize.HandleRead(ble.ReadHandlerFunc(s.readCurrentSizeDescr))
	c.AddDescriptor(dSize)
	return c, &s
}

func (s *snapdRequestTransmit) handleDataWrite(req ble.Request, rsp ble.ResponseWriter) {
	s.lock.Lock()
	defer s.lock.Unlock()

	data := req.Data()
	if len(data) == 0 {
		// done?
		s.dataReady = true
		log.Tracef("req: done?\n%s", string(s.data))
		s.notifyReadCompleteLocked()
		return
	}
	log.Tracef("req: got %v bytes", len(data))
	s.data = append(s.data, data...)
	s.currentSize += uint32(len(data))
}

func (s *snapdRequestTransmit) notifySize(req ble.Request, n ble.Notifier) {
	for {
		select {
		case newStart, ok := <-s.currentSizeChange:
			if !ok {
				return
			}
			log.Tracef("req: notify new offset: %v", newStart)
			if err := binary.Write(n, binary.LittleEndian, newStart); err != nil {
				log.Errorf("req: cannot notify about new chunk offset: %v", err)
			}
		}
	}
}

func (s *snapdRequestTransmit) readCurrentSizeDescr(req ble.Request, rsp ble.ResponseWriter) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if err := binary.Write(rsp, binary.LittleEndian, s.currentSize); err != nil {
		log.Errorf("req: cannot write current chunk size: %v", err)
	}
}

func (s *snapdRequestTransmit) reset() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.data = nil
	s.dataReady = false
	s.currentSize = 0
}

func (s *snapdRequestTransmit) notifyReadCompleteLocked() {
	if s.readyFunc != nil {
		s.readyFunc(s.data)
		s.readyFunc = nil
	}
}

func (s *snapdRequestTransmit) waitForReady(f func([]byte)) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.readyFunc = f

	if s.dataReady {
		s.notifyReadCompleteLocked()
		return
	}
}

func runDevice(connectChan <-chan string) error {
	dt := newInitializedDeviceTransport(connectChan)
	dev, err := NewDevice(dt)
	if err != nil {
		return err
	}
	return dev.WaitForConfiguration()
}

type bleDeviceTransport struct {
	rsp *snapdResponseTransmit // dev -> configurator
	req *snapdRequestTransmit  // configurator -> dev
	dev *snapdDeviceChar

	incomingConnection <-chan string

	advertiseCancel context.CancelFunc
	advertiseResult chan error
}

func newInitializedDeviceTransport(connectChan <-chan string) deviceTransport {
	onboardingSvc := ble.NewService(ble.MustParse(OnboardingServiceUUID))
	responseChar, response := NewSnapdResponseTransmit()
	onboardingSvc.AddCharacteristic(responseChar)
	requestChar, request := NewSnapdRequestTransmit()
	onboardingSvc.AddCharacteristic(requestChar)
	deviceChar, dev := NewSnapdDeviceChar()
	onboardingSvc.AddCharacteristic(deviceChar)

	if err := ble.AddService(onboardingSvc); err != nil {
		log.Fatalf("can't add service: %s", err)
	}
	return &bleDeviceTransport{
		rsp:                response,
		req:                request,
		dev:                dev,
		incomingConnection: connectChan,
		advertiseResult:    make(chan error),
	}
}

func (b *bleDeviceTransport) Advertise() error {
	log.Tracef("advertising...")
	ctx, cancel := context.WithCancel(context.Background())
	b.advertiseCancel = cancel
	go func() {
		err := ble.AdvertiseNameAndServices(ctx, "Ubuntu Core", ble.MustParse(OnboardingServiceUUID))
		if err != nil {
			log.Tracef("advertising failed")
			cancel()
		}
		b.advertiseResult <- err
	}()
	return nil
}
func (b *bleDeviceTransport) Hide() {
	log.Tracef("stop advertising")
	if b.advertiseCancel != nil {
		b.advertiseCancel()
	}
	err := <-b.advertiseResult
	if err != nil {
		log.Tracef("advertising err: %v", err)
	}
}

func (b *bleDeviceTransport) WaitConnected() (string, error) {
	log.Tracef("wait for connection")
	peer := <-b.incomingConnection
	log.Tracef("got connection from %s", peer)
	return peer, nil
}

func (b *bleDeviceTransport) Disconnect(peer []byte) {
	// XXX
}

func (b *bleDeviceTransport) NotifyState(state State) error {
	log.Tracef("notify state change to %v", state)
	b.dev.setState(state)
	return nil
}

func (b *bleDeviceTransport) Send(data []byte) error {
	log.Tracef("send %v bytes of data", len(data))
	b.rsp.transmitData(data)
	return nil
}

func (b *bleDeviceTransport) PrepareReceive() error {
	log.Tracef("prepare receive")
	b.req.reset()
	return nil
}

func (b *bleDeviceTransport) Receive(ctx context.Context) ([]byte, error) {
	dataChan := make(chan []byte, 1)
	b.req.waitForReady(func(data []byte) {
		dataChan <- data
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case data := <-dataChan:
		log.Tracef("got %v bytes ready", len(data))
		b.req.reset()
		return data, nil
	}
}
