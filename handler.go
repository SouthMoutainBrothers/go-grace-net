package gracenet

import (
	"net"
)

type Handler interface {
	HandleConn( net.Conn )
}

type HandleFunc func ( net.Conn )

func (f HandleFunc) HandleConn ( c net.Conn )  {
	f(c)
}