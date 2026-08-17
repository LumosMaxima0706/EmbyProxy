package mediaproxy

import "errors"

var (
	ErrInvalidScheme       = errors.New("unsupported target scheme")
	ErrInvalidHost         = errors.New("invalid target host")
	ErrInvalidPort         = errors.New("invalid target port")
	ErrPrivateTarget       = errors.New("private target blocked")
	ErrInvalidBasePath     = errors.New("invalid target base path")
	ErrInvalidRequestPath  = errors.New("invalid request path")
	ErrUpstreamUnavailable = errors.New("upstream unavailable")
)
