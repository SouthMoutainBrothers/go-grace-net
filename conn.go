package gracenet

import (
	"errors"
	"net"
	"sync"
	"log"
)

type graceConn struct {
	net.Conn
	server *GraceListener
	m      sync.Mutex
	closed bool
}

func (c *graceConn) Close() (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("[grace conn] Unknown panic")
			}
		}
	}()

	c.m.Lock()
	if c.closed {
		c.m.Unlock()
		return
	}

	c.server.wg.Done()
	c.closed = true
	c.m.Unlock()

	if LogDetail {
		log.Println("conn close", c.RemoteAddr())
	}
	return c.Conn.Close()
}
