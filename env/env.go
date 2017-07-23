package env

import (
	"strings"

	"github.com/jhump/protoreflect/desc"
	"github.com/lycoris0731/evans/lib/parser"
	"github.com/lycoris0731/evans/model"
	"github.com/pkg/errors"
)

var (
	ErrUnselected         = errors.New("unselected")
	ErrUnknownTarget      = errors.New("unknown target")
	ErrUnknownPackage     = errors.New("unknown package")
	ErrUnknownService     = errors.New("unknown service")
	ErrInvalidServiceName = errors.New("invalid service name")
	ErrInvalidMessageName = errors.New("invalid message name")
	ErrInvalidRPCName     = errors.New("invalid RPC name")
)

// packages is used by showing all packages
// mapPackages is used by extract a package by package name
type cache struct {
	packages    model.Packages
	mapPackages map[string]*model.Package
}

type state struct {
	currentPackage string
	currentService string
}

type config struct {
	port int
}

type Env struct {
	desc  *parser.FileDescriptorSet
	state state

	config *config

	cache cache
}

func New(desc *parser.FileDescriptorSet, port int) (*Env, error) {
	env := &Env{desc: desc}
	env.cache.mapPackages = map[string]*model.Package{}
	env.config = &config{port: port}
	return env, nil
}

func (e *Env) GetPackages() model.Packages {
	if e.cache.packages != nil {
		return e.cache.packages
	}

	packNames := e.desc.GetPackages()
	packages := make(model.Packages, len(packNames))
	for i, name := range packNames {
		packages[i] = &model.Package{Name: name}
	}

	e.cache.packages = packages

	return packages
}

func (e *Env) GetServices() (model.Services, error) {
	if e.state.currentPackage == "" {
		return nil, errors.Wrap(ErrUnselected, "package")
	}

	// services, messages and rpc are cached by Env on startup
	name := e.state.currentPackage
	pkg, ok := e.cache.mapPackages[name]
	if ok {
		return pkg.Services, nil
	}

	return nil, errors.New("caching failed")
}

func (e *Env) GetMessages() (model.Messages, error) {
	if e.state.currentPackage == "" {
		return nil, errors.Wrap(ErrUnselected, "package")
	}

	name := e.state.currentPackage
	pack, ok := e.cache.mapPackages[name]
	if ok {
		return pack.Messages, nil
	}

	return nil, errors.New("caching failed")
}

func (e *Env) GetRPCs() (model.RPCs, error) {
	if e.state.currentService == "" {
		return nil, errors.Wrap(ErrUnselected, "service")
	}

	name := e.state.currentService
	svc, err := e.GetService(name)
	if err != nil {
		return nil, err
	}
	return svc.RPCs, nil
}

func (e *Env) GetService(name string) (*model.Service, error) {
	svc, err := e.GetServices()
	if err != nil {
		return nil, err
	}
	for _, svc := range svc {
		if name == svc.Name {
			return svc, nil
		}
	}
	return nil, errors.Wrap(ErrInvalidServiceName, name)
}

func (e *Env) GetMessage(name string) (*model.Message, error) {
	msg, err := e.GetMessages()
	if err != nil {
		return nil, err
	}
	for _, msg := range msg {
		msgName := e.getNameFromFQN(name)
		if msgName == msg.Name {
			return msg, nil
		}
	}
	return nil, errors.Wrap(ErrInvalidMessageName, name)
}

func (e *Env) GetRPC(name string) (*model.RPC, error) {
	rpcs, err := e.GetRPCs()
	if err != nil {
		return nil, err
	}
	for _, rpc := range rpcs {
		if name == rpc.Name {
			return rpc, nil
		}
	}
	return nil, errors.Wrap(ErrInvalidRPCName, name)
}

func (e *Env) UsePackage(name string) error {
	for _, p := range e.desc.GetPackages() {
		if name == p {
			e.state.currentPackage = name
			return e.loadPackage(p)
		}
	}
	return ErrUnknownPackage
}

func (e *Env) UseService(name string) error {
	// set package if setted service with package name
	if e.state.currentPackage == "" {
		s := strings.SplitN(name, ".", 2)
		if len(s) != 2 {
			return errors.New("please set package (package_name.service_name or set --package flag)")
		}
		if err := e.UsePackage(s[0]); err != nil {
			return err
		}
	}
	for _, svc := range e.desc.GetServices(e.state.currentPackage) {
		if name == svc.GetName() {
			e.state.currentService = name
			return nil
		}
	}
	return ErrUnknownService
}

func (e *Env) GetDSN() string {
	if e.state.currentPackage == "" {
		return ""
	}
	dsn := e.state.currentPackage
	if e.state.currentService != "" {
		dsn += "." + e.state.currentService
	}
	return dsn
}

// loadPackage loads all services and messages in itself
func (e *Env) loadPackage(name string) error {
	dSvc := e.desc.GetServices(name)
	dMsg := e.desc.GetMessages(name)

	services := make(model.Services, len(dSvc))
	for i, svc := range dSvc {
		services[i] = model.NewService(svc)
		services[i].RPCs = model.NewRPCs(svc)
	}

	messages := make(model.Messages, len(dMsg))
	for i, msg := range dMsg {
		messages[i] = model.NewMessage(msg)
		messages[i].Fields = model.NewFields(e.getMessage(e.state.currentPackage), msg)
	}

	_, ok := e.cache.mapPackages[name]
	if ok {
		return errors.New("duplicated loading")
	}
	e.cache.mapPackages[name] = &model.Package{
		Name:     name,
		Services: services,
		Messages: messages,
	}

	return nil
}

// Full Qualified Name
// It contains message or service with package name
// e.g.: .test.Person
func (e *Env) getNameFromFQN(fqn string) string {
	return strings.TrimLeft(fqn, "."+e.state.currentPackage+".")
}

func (e *Env) getMessage(pkgName string) func(typeName string) *desc.MessageDescriptor {
	messages := e.desc.GetMessages(pkgName)

	return func(msgName string) *desc.MessageDescriptor {
		for _, msg := range messages {
			// TODO: GetName が lower case になっている
			if msgName == strings.ToLower(msg.GetName()) {
				return msg
			}
		}
		// TODO: エラーを返す
		return nil
	}
}

func (e *Env) getService(pkgName string) func(typeName string) *desc.ServiceDescriptor {
	services := e.desc.GetServices(pkgName)

	return func(svcName string) *desc.ServiceDescriptor {
		for _, svc := range services {
			// TODO: GetName が lower case になっている
			if svcName == strings.ToLower(svc.GetName()) {
				return svc
			}
		}
		// TODO: エラーを返す
		return nil
	}
}

func (e *Env) Close() error {
	// return e.conn.Close()
	return nil
}
