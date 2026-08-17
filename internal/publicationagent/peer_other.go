//go:build !linux

package publicationagent

import (
	"errors"
	"net"
)

func peerUID(_ *net.UnixConn) (uint32, error) {
	return 0, errors.New("peer_credentials_unsupported")
}
