package mediaproxy

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
)

type Target struct {
	Scheme   string
	Host     string
	Port     int
	BasePath string
}

type Config struct {
	AllowPrivateTargets bool
	PreserveHost        bool
	TrustProxyEnv       bool
	PublicPrefix        string
	TLSConfig           *tls.Config
	Resolver            HostResolver
	DialContext         DialContextFunc
}

type HostResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type DialContextFunc func(context.Context, string, string) (net.Conn, error)

type Logger func(event string, fields map[string]any)

type Executor struct {
	cfg       Config
	transport *http.Transport
	client    *http.Client
	log       Logger
}
