package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/go-ble/ble"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	appName = "snapd onboarding"

	// 99df99df-0000-1000-8000-00805f9b34fb
	// 99df99e0-0000-1000-8000-00805f9b34fb
	// 99df99e1-0000-1000-8000-00805f9b34fb

	UUIDBase   = "99df"
	UUIDSuffix = "-0000-1000-8000-00805f9b34fb"
	//UUIDSuffix            = "-d598-4874-8e86-7d042ee07ba"
	serviceHandle    = "99df"
	commCharHandle   = "99e0"
	stateDescrHandle = "99e1"

	transmitCharHandle       = "99d0"
	transmitChunkDescrHandle = "99d1"
	transmitSizeDescrHandle  = "99d2"

	OnboardingServiceUUID = UUIDBase + serviceHandle + UUIDSuffix
	descrString           = "Onboarding protocol sate"
)

type Server struct {
	UUID string
}

func runServer(devName string) error {
	app := NewServer()
	// XXX: detect ctrl-c and app.Close() ?
	uuid := ble.MustParse(app.UUID)
	testSvc := ble.NewService(uuid)
	data, err := ioutil.ReadFile("main.go")
	if err != nil {
		log.Fatalf("cannot open file: %v", err)
	}
	transmitChar, transmit := NewSnapdTransmit()
	testSvc.AddCharacteristic(transmitChar)
	transmit.transmitData(data)
	testSvc.AddCharacteristic(NewSnapdDeviceChar())
	if err := ble.AddService(testSvc); err != nil {
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

func NewSnapdDeviceChar() *ble.Characteristic {
	s := snapdDeviceChar{}
	c := &ble.Characteristic{
		UUID: ble.MustParse(UUIDBase + commCharHandle + UUIDSuffix),
	}
	c.HandleRead(ble.ReadHandlerFunc(s.read))
	c.HandleWrite(ble.WriteHandlerFunc(s.written))
	d := &ble.Descriptor{
		UUID: ble.MustParse(UUIDBase + stateDescrHandle + UUIDSuffix),
	}
	c.AddDescriptor(d)
	d.HandleRead(ble.ReadHandlerFunc(s.readProtocolState))
	// c.HandleNotify(ble.NotifyHandlerFunc(s.echo))
	// c.HandleIndicate(ble.NotifyHandlerFunc(s.echo))
	return c
}

type snapdDeviceChar struct {
	cnt int
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

	data := req.Data()
	log.Tracef("got data: %x", data)
	log.Tracef("          %q", string(data))
	log.Tracef("          offset: %v", req.Offset())
}

// func (s *snapdChar) echo(req ble.Request, n ble.Notifier) {
// notify
// }

type OnboardingState int

const (
	StateWaitHello OnboardingState = iota + 1
	StateWaitSessionSetup
	StateWaitReady
)

func (s *snapdDeviceChar) readProtocolState(req ble.Request, rsp ble.ResponseWriter) {

}

type snapdTransmit struct {
	data      []byte
	chunkSize uint32

	currentChunkStart uint32
	currentSize       uint32
	chunkStartChange  chan uint32
}

func NewSnapdTransmit() (*ble.Characteristic, *snapdTransmit) {
	s := snapdTransmit{
		chunkStartChange: make(chan uint32, 1),
	}
	c := &ble.Characteristic{
		UUID: ble.MustParse(UUIDBase + transmitCharHandle + UUIDSuffix),
	}
	c.HandleRead(ble.ReadHandlerFunc(s.readNextChunk))
	c.HandleWrite(ble.WriteHandlerFunc(s.handleRewindTo))
	c.HandleNotify(ble.NotifyHandlerFunc(s.notifyNewOffset))
	dSize := &ble.Descriptor{
		UUID: ble.MustParse(UUIDBase + transmitSizeDescrHandle + UUIDSuffix),
	}
	dSize.HandleRead(ble.ReadHandlerFunc(s.readCurrentSizeDescr))
	c.AddDescriptor(dSize)
	dChunk := &ble.Descriptor{
		UUID: ble.MustParse(UUIDBase + transmitChunkDescrHandle + UUIDSuffix),
	}
	dChunk.HandleRead(ble.ReadHandlerFunc(s.readCurrentChunkDescr))
	c.AddDescriptor(dChunk)
	return c, &s
}

func (s *snapdTransmit) transmitData(data []byte) {
	s.data = data
	s.currentSize = uint32(len(data))
	s.currentChunkStart = 0
}

func (s *snapdTransmit) chunk(size uint32) ([]byte, uint32) {
	thisChunkSize := s.currentSize - s.currentChunkStart
	if thisChunkSize > size {
		thisChunkSize = size
	}
	log.Tracef("this chunk start %v/%v size: %v", s.currentChunkStart, s.currentSize, thisChunkSize)
	return s.data[s.currentChunkStart : s.currentChunkStart+thisChunkSize], s.currentChunkStart + thisChunkSize
}

func (s *snapdTransmit) advanceChunk(size uint32) (start uint32, complete bool) {
	if s.currentChunkStart+size > s.currentSize {
		s.currentChunkStart = s.currentSize
		return s.currentChunkStart, true
	}
	s.currentChunkStart += size
	s.chunkStartChange <- s.currentChunkStart
	return s.currentChunkStart, false
}

func (s *snapdTransmit) rewindTo(start uint32) {
	// XXX range check
	log.Tracef("rewind to: %v", start)
	s.currentChunkStart = start
	s.chunkStartChange <- start
}

func (s *snapdTransmit) readNextChunk(req ble.Request, rsp ble.ResponseWriter) {
	if len(s.data) == 0 {
		return
	}
	if req.Offset() == 0 {
		log.Tracef("start reading chunk")
	}
	// XXX: support ReadBlob requests

	log.Tracef("    offset %v cap %v", req.Offset(), rsp.Cap())

	chunk, nextChunkStart := s.chunk(uint32(rsp.Cap()))
	log.Tracef("    chunk size: %v next offset: %v", len(chunk), nextChunkStart)

	start := req.Offset()
	toSend := len(chunk[start:])
	if toSend > rsp.Cap() {
		toSend = rsp.Cap()
	}
	contentToSend := chunk[start : start+toSend]
	n, err := rsp.Write(contentToSend)
	log.Tracef("sent, %v/%v bytes, err: %v", n, len(contentToSend), err)
	if err != nil {
		return
	}
	if req.Offset()+n == len(chunk) {
		// whole chunk was read, advance
		next, done := s.advanceChunk(uint32(len(chunk)))
		if !done {
			log.Tracef("chunk was read, advance to %v/%v", next, s.currentSize)
		} else {
			log.Tracef("transfer done")
			s.rewindTo(0)
		}
	}
}

func (s *snapdTransmit) readCurrentChunkDescr(req ble.Request, rsp ble.ResponseWriter) {
	if err := binary.Write(rsp, binary.LittleEndian, s.currentChunkStart); err != nil {
		log.Errorf("cannot write current chunk offset: %v", err)
	}
}

func (s *snapdTransmit) readCurrentSizeDescr(req ble.Request, rsp ble.ResponseWriter) {
	if err := binary.Write(rsp, binary.LittleEndian, s.currentSize); err != nil {
		log.Errorf("cannot write current chunk size: %v", err)
	}
}

func (s *snapdTransmit) handleRewindTo(req ble.Request, rsp ble.ResponseWriter) {
	var offset uint32
	if err := binary.Read(bytes.NewBuffer(req.Data()), binary.LittleEndian, &offset); err != nil {
		log.Errorf("cannot read rewind-to offset: %v", err)
	}
	s.rewindTo(offset)
}

func (s *snapdTransmit) notifyNewOffset(req ble.Request, n ble.Notifier) {
	for {
		select {
		case newStart, ok := <-s.chunkStartChange:
			if !ok {
				return
			}
			log.Tracef("notify new offset: %v", newStart)
			if err := binary.Write(n, binary.LittleEndian, newStart); err != nil {
				log.Errorf("cannot notify about new chunk offset: %v", err)
			}
		}
	}
}
