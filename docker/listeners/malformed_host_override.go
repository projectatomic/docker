// +build !windows

package listeners

import (
	"net"
	"strings"
)

// MalformedHostHeaderOverride is a wrapper to be able
// to overcome the 400 Bad request coming from old docker
// clients that send an invalid Host header.
type MalformedHostHeaderOverride struct {
	net.Listener
}

// MalformedHostHeaderOverrideConn wraps the underlying unix
// connection and keeps track of the first read from http.Server
// which just reads the headers.
type MalformedHostHeaderOverrideConn struct {
	net.Conn
	first bool
}

// Read reads the first *read* request from http.Server to inspect
// the Host header. If the Host starts with / then we're talking to
// an old docker client which send an invalid Host header. To not
// error out in http.Server we rewrite the first bytes of the request
// to sanitize the Host header itself.
// In case we're not dealing with old docker clients the data is just passed
// to the server w/o modification.
func (l *MalformedHostHeaderOverrideConn) Read(b []byte) (n int, err error) {
	// http.Server uses a 4k buffer
	if l.first && len(b) == 4096 {
		// This keeps track of the first read from http.Server which just reads
		// the headers
		l.first = false
		// The first read of the connection by http.Server is done limited to
		// DefaultMaxHeaderBytes (usually 1 << 20) + 4096.
		// Here we do the first read which gets us all the http headers to
		// be inspected and modified below.
		c, err := l.Conn.Read(b)
		if err != nil {
			return c, err
		}
		parts := strings.Split(string(b[:c]), "\n")
		head := []string{parts[0]}
		if len(parts) > 0 {
			if !strings.HasPrefix(parts[1], "Host:") {
				// old docker clients sends the Host header at parts[1]
				// which is the second line of the http request
				// if we're not talking to an old docker client, just skip
				head = parts
			} else if !strings.HasPrefix(parts[1], "Host: /") {
				// we're talking to a newer docker clients if Host doesn't start
				// with a slash
				head = parts
			} else {
				// we're now talking to an old docker client
				// Sanitize Host header
				head = append(head, "Host: \r")
				// Inject `Connection: close` to ensure we don't reuse this connection
				head = append(head, "Connection: close\r")
				// append the remaining headers
				if len(parts) > 1 {
					head = append(head, parts[2:]...)
				}
			}
		}
		newHead := strings.Join(head, "\n")
		copy(b, []byte(newHead))
		return len(newHead), nil
	}
	return l.Conn.Read(b)
}

// Accept makes the listener accepts connections and wraps the connection
// in a MalformedHostHeaderOverrideConn initilizing first to true.
func (l *MalformedHostHeaderOverride) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return c, err
	}
	return &MalformedHostHeaderOverrideConn{c, true}, nil
}
