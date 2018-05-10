//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package agent

import (
	"fmt"
	"net"

	"go.uber.org/multierr"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/server/echo"
	"istio.io/istio/pkg/test/proxy/envoy"
)

// PortConfig contains meta information about a port
type PortConfig struct {
	Name     string
	Protocol model.Protocol
}

// Config represents the configuration for an Agent
type Config struct {
	ServiceName string
	Ports       []PortConfig
	TLSCert     string
	TLSCKey     string
	Version     string
	TmpDir      string
}

// Port contains the port mapping for a single configured port
type Port struct {
	Config      PortConfig
	EnvoyPort   int
	ServicePort int
}

// Agent bootstraps a local service/Envoy combination.
type Agent struct {
	Config         Config
	e              *envoy.Envoy
	app            *echo.Server
	envoyConfig    *envoyConfig
	envoyAdminPort int
	ports          []Port
}

// Start starts Envoy and the service.
func (a *Agent) Start() (err error) {
	if err = a.startService(); err != nil {
		return err
	}

	// Generate the port mappings between Envoy and the backend service.
	a.envoyAdminPort, a.ports, err = a.createPorts()
	if err != nil {
		return err
	}

	return a.startEnvoy()
}

// Stop stops Envoy and the service.
func (a *Agent) Stop() error {
	var err error
	if a.e != nil {
		err = a.e.Stop()
	}
	if a.app != nil {
		err = multierr.Append(err, a.app.Stop())
	}
	if a.envoyConfig != nil {
		a.envoyConfig.dispose()
		a.envoyConfig = nil
	}
	return err
}

// GetPorts returns the list of runtime ports after the Agent has been started.
func (a *Agent) GetPorts() []Port {
	return a.ports
}

// GetEnvoyAdminPort returns the admin port for Envoy after the Agent has been started.
func (a *Agent) GetEnvoyAdminPort() int {
	return a.envoyAdminPort
}

func (a *Agent) startService() error {
	// TODO(nmittler): Add support for other protocols
	for _, port := range a.Config.Ports {
		switch port.Protocol {
		case model.ProtocolHTTP:
			// Just verifying that all ports are HTTP for now.
		default:
			return fmt.Errorf("protocol %v not currently supported", port.Protocol)
		}
	}

	a.app = &echo.Server{
		HTTPPorts: make([]int, len(a.Config.Ports)),
		TLSCert:   a.Config.TLSCert,
		TLSCKey:   a.Config.TLSCKey,
		Version:   a.Config.Version,
	}
	return a.app.Start()
}

func (a *Agent) startEnvoy() (err error) {
	// Create the configuration object
	a.envoyConfig, err = (&envoyConfigBuilder{
		ServiceName: a.Config.ServiceName,
		AdminPort:   a.envoyAdminPort,
		Ports:       a.ports,
		tmpDir:      a.Config.TmpDir,
	}).build()
	if err != nil {
		return err
	}

	// Create and start envoy with the configuration
	a.e = &envoy.Envoy{
		ConfigFile: a.envoyConfig.configFile,
	}
	return a.e.Start()
}

func (a *Agent) createPorts() (adminPort int, ports []Port, err error) {
	if adminPort, err = findFreePort(); err != nil {
		return
	}

	servicePorts := a.app.HTTPPorts
	ports = make([]Port, len(servicePorts))
	for i, servicePort := range servicePorts {
		var envoyPort int
		envoyPort, err = findFreePort()
		if err != nil {
			return
		}

		ports[i] = Port{
			Config:      a.Config.Ports[i],
			ServicePort: servicePort,
			EnvoyPort:   envoyPort,
		}
	}
	return
}

func findFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
