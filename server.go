package main

import (
	"context"
	"fmt"
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
	serviceHandle         = "99df"
	commCharHandle        = "99e0"
	descrHandle           = "99e1"
	OnboardingServiceUUID = UUIDBase + serviceHandle + UUIDSuffix
	descrString           = "Communication for snapd onboarding"
)

type Server struct {
	UUID string
}

func runServer(devName string) error {
	app := NewServer()
	// XXX: detect ctrl-c and app.Close() ?
	uuid := ble.MustParse(app.UUID)
	testSvc := ble.NewService(uuid)
	testSvc.AddCharacteristic(NewSnapdChar())
	if err := ble.AddService(testSvc); err != nil {
		log.Fatalf("can't add service: %s", err)
	}

	// Advertise for specified durantion, or until interrupted by user.
	log.Info("Advertising...")
	ctx := ble.WithSigHandler(context.WithTimeout(context.Background(), 10*60*time.Second))
	err := ble.AdvertiseNameAndServices(ctx, "Ubuntu Core", uuid)
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

func NewSnapdChar() *ble.Characteristic {
	s := snapdChar{}
	c := &ble.Characteristic{
		UUID: ble.MustParse(UUIDBase + commCharHandle + UUIDSuffix),
	}
	d := &ble.Descriptor{
		UUID: ble.MustParse(UUIDBase + descrHandle + UUIDSuffix),
	}
	d.HandleRead(ble.ReadHandlerFunc(s.read))
	d.HandleWrite(ble.WriteHandlerFunc(s.written))
	c.AddDescriptor(d)
	// c.HandleNotify(ble.NotifyHandlerFunc(s.echo))
	// c.HandleIndicate(ble.NotifyHandlerFunc(s.echo))
	return c
}

type snapdChar struct {
	cnt int
}

func (s *snapdChar) read(req ble.Request, rsp ble.ResponseWriter) {
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

func (s *snapdChar) written(req ble.Request, rsp ble.ResponseWriter) {
	data := req.Data()
	log.Tracef("got data: %x", data)
	log.Tracef("          %q", string(data))
}

// func (s *snapdChar) echo(req ble.Request, n ble.Notifier) {
// notify
// }
