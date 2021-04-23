package main

import (
	"runtime"
	"strings"

	"github.com/muka/go-bluetooth/api/service"
	"github.com/muka/go-bluetooth/bluez/profile/agent"
	"github.com/muka/go-bluetooth/bluez/profile/gatt"
)

const (
	appName = "snapd onboarding"

	//                               -0000-1000-8000-00805F9B34FB
	OnboardingServiceUUID = "2d06923d-c853-4d90-a8a9-91731cdefcfb"

	// XXX: are there better ones?
	serviceHandle  = "1000"
	commCharHandle = "2000"

	descrString = "Communication for snapd onboarding"
	descrHandle = "3000"
)

type Server struct {
	app *service.App
}

// NewServer creates a new ble server that advertises itself.
//
// The devName is the device name of the hci device, usually "hci0"
//
// The name can be obtained via "hcitool dev" or by inspecting the
// managed objects under org.bluez and finding the ones that implement
// org.bluez.GattManager1.
func NewServer(devName string) (*Server, error) {
	l := strings.SplitN(OnboardingServiceUUID, "-", 2)
	uuidPrefix := l[0]
	uuidSuffix := l[1]

	// create bluetooth "app" that advertises itself
	options := service.AppOptions{
		AdapterID: devName,
		AgentCaps: agent.CapNoInputNoOutput,
		// XXX: could we simply pass the fully uuid here instead of
		// this strange dance?
		UUIDSuffix: uuidSuffix,
		UUID:       uuidPrefix,
	}
	app, err := service.NewApp(options)
	if err != nil {
		return nil, err
	}
	runtime.SetFinalizer(app, app.Close)
	app.SetName(appName)

	if !app.Adapter().Properties.Powered {
		if err := app.Adapter().SetPowered(true); err != nil {
			return nil, err
		}
	}
	server := &Server{app: app}

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
		return []byte(descrString), nil
	}))
	if err = commChar.AddDescr(descr); err != nil {
		return nil, err
	}
	return server, nil
}

func (s *Server) Run() error {
	return s.app.Run()
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
