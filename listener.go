package gracenet

import (
	"net"
	"os"
	"time"
	"sync"
	"net/http"
	"errors"
	"log"
	"strings"
)

type GraceListener struct {
	net.Listener
	*http.Server
	stop    chan error
	closed  chan bool
	stopped bool
	family  uint8
	state   uint8
	wg      sync.WaitGroup
}

const (
	TCP  = iota
	UNIX
)

func newGraceListener(l net.Listener, family uint8) (gl *GraceListener) {
	gl = &GraceListener{
		Listener: l,
		stop:     make(chan error),
		closed:   make(chan bool),
		family:   family,
		state:    StateInit,
		stopped:  false,
	}
	go func() {
		<-gl.stop
		gl.stopped = true
		if LogDetail {
			log.Println("listener closing...")
		}
		gl.stop <- gl.Listener.Close()

	}()

	return
}

func (gl *GraceListener) Accept() (c net.Conn, err error) {
	var conn net.Conn

	switch gl.family {
	case TCP:
		conn, err = gl.Listener.(*net.TCPListener).AcceptTCP()
		if err != nil {
			return nil, err
		}

		c = &graceConn{
			Conn:   conn,
			server: gl,
		}

		gl.wg.Add(1)

		return

	case UNIX:
		conn, err = gl.Listener.(*net.UnixListener).AcceptUnix()
		if err != nil {
			return nil, err
		}
		c = &graceConn{
			Conn:   conn,
			server: gl,
		}
		gl.wg.Add(1)
		return

	default:
		return nil, errors.New("not support family")
	}

	return nil, errors.New("not support family")
}

func (gl *GraceListener) Close() error {
	if gl.stopped {
		return errors.New("grace listener closed")
	}
	gl.stop <- nil
	err := <-gl.stop
	if err == nil {
		if LogDetail {
			log.Println("[grace listener] Waiting for connections to finish...")
		}
		gl.wg.Wait()

		return nil
	}
	return err
}


func (gl *GraceListener) Addr() net.Addr{
	switch gl.family {
	case TCP:
		tl := gl.Listener.(*net.TCPListener)
		return tl.Addr()
	case UNIX:
		tl := gl.Listener.(*net.UnixListener)
		return tl.Addr()
	default:
		// imporsiable
		return nil
	}
}

func (gl *GraceListener) File() (f *os.File, err error) {
	// returns a dup(2) - FD_CLOEXEC flag *not* set
	switch gl.family {
	case TCP:
		tl := gl.Listener.(*net.TCPListener)
		return tl.File()
	case UNIX:
		tl := gl.Listener.(*net.UnixListener)
		return tl.File()
	default:
		// imporsiable
		return nil, errors.New("[grace net] impossible, unknow to convert listener !")
	}
}

func (gl *GraceListener) serve(h Handler) {
	mutex.Lock()
	status = StateRunning
	mutex.Unlock()

	var tempDelay time.Duration // how long to sleep on accept failure

	for {
		conn, err := gl.Accept()

		if err != nil {
			netErr, ok := err.(net.Error)
			if ok && netErr.Timeout() && netErr.Temporary() { //listener timeout, not error
				if tempDelay == 0 {
					tempDelay = 2 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				log.Printf("[grace listener] Accept error: %v; retrying in %v", netErr, tempDelay)

				time.Sleep(tempDelay)
				continue
			} else if strings.Contains(err.Error(), "use of closed network connection") {
				if !gl.stopped {
					log.Fatal("[grace listener] listened socket has closed !!!")
				}

				break
			} else {
				log.Printf("[grace listener] accept conn error: [%s] \n", err)
				break
			}
		} else {
			tempDelay = 0
			go h.HandleConn(conn)
		}
	}

	gl.Close()

	<-gl.closed
}


func ( gl *GraceListener )ServeHTTPForever( ) error {
	if gl == nil || gl.Server == nil {
		return errors.New("[grace net] listener is nil !")
	}

	ShutDownParentProcess()

	mutex.Lock()
	status = StateRunning
	mutex.Unlock()

	switch gl.family {
	case TCP:
		fallthrough
	case UNIX:
		gl.Server.Serve(gl)
	default:
		return errors.New("[grace net] impossible, unknow to serve listener !")
	}

	return nil
}


func ( gl *GraceListener )ServeForever( h Handler) error {
	if gl == nil{
		return errors.New("[grace net] listener is nil !")
	}

	ShutDownParentProcess()

	mutex.Lock()
	status = StateRunning
	mutex.Unlock()

	switch gl.family {
	case TCP:
		fallthrough
	case UNIX:
		gl.serve(h)
	default:
		return errors.New("[grace net] impossible, unknow to serve listener !")
	}

	return nil
}
