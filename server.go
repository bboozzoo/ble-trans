package main

import (
	"fmt"
	"runtime"

	"github.com/muka/go-bluetooth/api/service"
	"github.com/muka/go-bluetooth/bluez/profile/agent"
	"github.com/muka/go-bluetooth/bluez/profile/gatt"
)

const (
	appName = "snapd onboarding"

	//                               -0000-1000-8000-00805F9B34FB
	//                        12634d89-d598-4874-8e86-7d042ee07ba
	OnboardingServiceUUID = "1234-0000-1000-8000-00805F9B34FB"

	// XXX: are there better ones?
	serviceHandle  = "1000"
	commCharHandle = "2000"

	descrString = "Communication for snapd onboarding"
	descrHandle = "3000"
)

func runServer(devName string) error {
	app, err := NewServer(devName)
	if err != nil {
		return err
	}
	// XXX: detect ctrl-c and app.Close() ?
	return app.Run()
}

type Server struct {
	app     *service.App
	readCnt int
}

// NewServer creates a new ble server that advertises itself.
//
// The devName is the device name of the hci device, usually "hci0"
//
// The name can be obtained via "hcitool dev" or by inspecting the
// managed objects under org.bluez and finding the ones that implement
// org.bluez.GattManager1.
func NewServer(devName string) (*Server, error) {
	uuidPrefix := "1234"
	uuidSuffix := "-0000-1000-8000-00805F9B34FB"
	fmt.Println(uuidPrefix)
	fmt.Println(uuidSuffix)

	// create bluetooth "app" that advertises itself
	options := service.AppOptions{
		AdapterID: devName,
		AgentCaps: agent.CapNoInputNoOutput,
		// XXX: could we simply pass the fully uuid here instead of
		// this strange dance?
		UUIDSuffix: uuidSuffix,
		UUID:       "1234",
	}
	app, err := service.NewApp(options)
	if err != nil {
		return nil, err
	}
	runtime.SetFinalizer(app, func(x *service.App) { x.Close() })
	app.SetName(appName)

	if !app.Adapter().Properties.Powered {
		if err := app.Adapter().SetPowered(true); err != nil {
			return nil, err
		}
	}
	server := &Server{
		app: app,
	}

	// add communication characteristic
	service1, err := app.NewService(serviceHandle)
	if err != nil {
		return nil, err
	}
	if err := app.AddService(service1); err != nil {
		return nil, err
	}
	commChar, err := service1.NewChar(commCharHandle)
	if err != nil {
		return nil, err
	}
	commChar.Properties.Flags = []string{
		gatt.FlagCharacteristicRead,
		gatt.FlagCharacteristicWrite,
	}
	commChar.OnRead(server.onRead)
	commChar.OnWrite(server.onWrite)
	if err = service1.AddChar(commChar); err != nil {
		return nil, err
	}

	// now add description
	descr, err := commChar.NewDescr(descrHandle)
	if err != nil {
		return nil, err
	}
	descr.Properties.Flags = []string{
		gatt.FlagDescriptorRead,
	}
	descr.OnRead(service.DescrReadCallback(func(c *service.Descr, options map[string]interface{}) ([]byte, error) {
		server.readCnt++
		return []byte(fmt.Sprintf("%s read: %v", descrString, server.readCnt)), nil
	}))
	if err = commChar.AddDescr(descr); err != nil {
		return nil, err
	}
	return server, nil
}

func (s *Server) Run() error {
	if err := s.app.Run(); err != nil {
		return err
	}

	// 0 means no timeout
	cancel, err := s.app.Advertise(0)
	if err != nil {
		return err
	}
	defer cancel()

	select {}

	return nil
}

func (s *Server) Close() error {
	if s.app != nil {
		s.app.Close()
	}
	return nil
}

func (s *Server) onRead(c *service.Char, options map[string]interface{}) ([]byte, error) {
	return nil, nil
}

func (s *Server) onWrite(c *service.Char, value []byte) ([]byte, error) {
	return nil, nil
}
