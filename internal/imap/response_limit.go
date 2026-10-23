package imap

import "net"

// Used for small metadata-only extensions. Limit bytes at the socket as well as
// the final result so a provider cannot fill an unbounded command collector.
type responseLimitConn struct {
	net.Conn
	remaining int64
}

func (c *responseLimitConn) Read(p []byte) (int, error) {
	if c.remaining <= 0 {
		c.Conn.Close()
		return 0, ErrPartTooLarge
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.Conn.Read(p)
	c.remaining -= int64(n)
	return n, err
}
