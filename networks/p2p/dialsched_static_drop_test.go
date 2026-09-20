package p2p

import (
	"errors"
	"io"
	"net"
	"testing"

	"github.com/kaiachain/kaia/networks/p2p/discover"
	"github.com/stretchr/testify/assert"
)

// A static node is dropped after dialMaxRetries failed dials,
// and a dial refused by the remote during the handshake (KIP-311 allowlist) or failing to
// connect (peer restarting) both count. In v2.2.0 the entries from static-nodes.json /
// admin_addPeer were DT_UNLIMITED and retried forever.
type refusingBackend struct{ calls int }

func (b *refusingBackend) SetupConn(fd net.Conn, flags connFlag, dest *discover.Node) error {
	b.calls++
	fd.Close()
	return errors.New("remote closed the connection: outside CNPeers (DiscUselessPeer)")
}

type pipeDialer struct{}

func (pipeDialer) Dial(*discover.Node) (net.Conn, error) {
	c1, c2 := net.Pipe()
	go io.Copy(io.Discard, c2)
	return c1, nil
}
func (d pipeDialer) DialMulti(n *discover.Node) ([]net.Conn, error) {
	c, err := d.Dial(n)
	return []net.Conn{c}, err
}

type downDialer struct{}

func (downDialer) Dial(*discover.Node) (net.Conn, error) {
	return nil, errors.New("connect: connection refused")
}
func (downDialer) DialMulti(*discover.Node) ([]net.Conn, error) {
	return nil, errors.New("connect: connection refused")
}

func TestDialSched_StaticNodeDroppedOnRefusalOrConnectError(t *testing.T) {
	cases := []struct {
		name    string
		dialer  NodeDialer
		backend DialBackend
	}{
		{"refused by the remote during the handshake (allowlist)", pipeDialer{}, &refusingBackend{}},
		{"peer down (connect error)", downDialer{}, &refusingBackend{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			static := testNode(1, "10.0.0.1", discover.NodeTypeCN)
			ds := NewDialSched(DialConfig{staticNodes: []*discover.Node{static}, dialer: c.dialer}, nil, c.backend)
			for i := 1; i <= dialMaxRetries+1; i++ {
				flags := ds.markDialStart(static)
				ds.dialOnce(cloneNode(static), flags)
				t.Logf("failed dial %d: still static=%v connFails=%d", i, ds.static.contains(static.ID), ds.connFails[static.ID])
			}
			assert.False(t, ds.static.contains(static.ID), "the static node is gone after dialMaxRetries+1 failures")
		})
	}
}
