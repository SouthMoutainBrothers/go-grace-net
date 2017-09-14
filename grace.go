// Package grace use to hot reload
// Description: http://grisha.org/blog/2014/06/03/graceful-restart-in-golang/

package gracenet

import (
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"net"
	"strconv"
	"fmt"
	"os/exec"
	"log"
	"os/signal"
	"net/http"
	"errors"
	"context"
)

const (
	// StateInit represent the application inited
	StateInit = iota
	// StateRunning represent the application is running
	StateRunning
	// StateShuttingDown represent the application is shutting down
	StateShuttingDown
	// StateTerminate represent the application is killed
	StateTerminate
)

const (
	// Used to indicate a graceful restart in the new process.
	envCountKey       = "LISTEN_FDS"
	envCountKeyPrefix = envCountKey + "="
)

var (

	// DefaultReadTimeOut is the HTTP read timeout
	DefaultReadTimeOut time.Duration
	// DefaultWriteTimeOut is the HTTP Write timeout
	DefaultWriteTimeOut time.Duration

	// DefaultTimeout is the shutdown server's timeout. default is 60s
	DefaultTimeout = 60 * time.Second

	// LOG grace detail
	LogDetail = true

	hookableSignals []os.Signal

	// In order to keep the working directory the same as when we started we record
	// it at startup.
	originalWD, _ = os.Getwd()

	//grace servers info
	inherited   []net.Listener
	listeners   []*GraceListener
	mutex       sync.Mutex
	inheritOnce sync.Once
	status      int
	quit_chan   chan bool

	// used in tests to override the default behavior of starting from fd 3.
	fdStart int

)

func ListenHTTPHandlerAndServe(proto, addr string, http_handler http.Handler) error {
	server := &http.Server{Addr: addr, Handler: http_handler}
	return ListenHTTPAndServe(proto, addr, server)
}

func ListenHTTPAndServe(proto, addr string, server *http.Server) error  {
	gl, err:= ListenHTTP(proto, addr, server)
	if err == nil{
		if LogDetail {
			log.Printf("listen on %s ok, pid %d", addr, os.Getpid())
		}
		ShutDownParentProcess()

		mutex.Lock()
		status = StateRunning
		mutex.Unlock()

		return gl.Server.Serve(gl)

	}else {
		return err
	}

	return nil
}


func ListenHTTP(proto, addr string, server *http.Server) (gl *GraceListener,err error){
	if server == nil{
		return nil, errors.New("HTTP Server is nil")
	}

	switch proto {
	case "tcp", "tcp4", "tcp6":
		laddr, err := net.ResolveTCPAddr(proto, addr )
		if err != nil {
			return nil, err
		}
		gl,err = ListenTCP( proto, laddr )
	case "unix", "unixpacket":
		laddr, err := net.ResolveUnixAddr(proto, addr)
		if err != nil {
			return nil, err
		}
		gl,err = ListenUnix(proto, laddr)

	default:
		return nil, net.UnknownNetworkError(proto)
	}

	if err == nil {
		gl.Server = server
	}
	return
}

func ListenAndServe(proto, addr string, h Handler ) error  {
	gl, err:= Listen(proto, addr)
	if err == nil{
		if LogDetail {
			log.Printf("listen on %s ok, pid %d", addr, os.Getpid())
		}
		ShutDownParentProcess()

		gl.serve( h )

		return nil
	}else {
		return err
	}

	return nil
}

func Listen(proto, addr string) (*GraceListener, error) {
	switch proto {
	case "tcp", "tcp4", "tcp6":
		laddr, err := net.ResolveTCPAddr(proto, addr )
		if err != nil {
			return nil, err
		}
		return ListenTCP( proto, laddr )
	case "unix", "unixpacket":
		laddr, err := net.ResolveUnixAddr(proto, addr)
		if err != nil {
			return nil, err
		}
		return ListenUnix(proto, laddr)

	default:
		return nil, net.UnknownNetworkError(proto)
	}
}

func ListenTCP(proto string, laddr *net.TCPAddr) (*GraceListener, error) {
	if err := inheritListeners(); err != nil {
		return nil, err
	}

	mutex.Lock()
	defer mutex.Unlock()

	// look for an inherited listener
	for i, l := range inherited {
		if l == nil { // we nil used inherited listeners
			continue
		}
		if isSameAddr(l.Addr(), laddr) {
			inherited[i] = nil
			if LogDetail {
				log.Println("[grace net] inhired tcp socket !")
			}
			gl := newGraceListener( l, TCP )

			listeners = append(listeners, gl)
			return gl, nil
		}
	}

	// make a fresh listener
	l, err := net.ListenTCP(proto, laddr)
	if err != nil {
		return nil, err
	}
	gl := newGraceListener( l, TCP )

	listeners = append(listeners, gl)
	return gl, nil
}

func ListenUnix(proto string, laddr *net.UnixAddr) (*GraceListener, error) {
	if err := inheritListeners(); err != nil {
		return nil, err
	}

	mutex.Lock()
	defer mutex.Unlock()

	// look for an inherited listener
	for i, l := range inherited {
		if l == nil { // we nil used inherited listeners
			continue
		}
		if isSameAddr(l.Addr(), laddr) {
			inherited[i] = nil

			gl := newGraceListener( l, UNIX )

			listeners = append(listeners, gl)
			return gl, nil
		}
	}

	// make a fresh listener
	l, err := net.ListenUnix( proto, laddr )
	if err != nil {
		if err.(*net.OpError).Err.(*os.SyscallError).Syscall == "bind" {
			log.Printf( "[grace net] clean unix path: %s",  laddr.String())
			os.Remove(laddr.String())

			l, err = net.ListenUnix( proto, laddr )
			if err != nil {
				return nil, err
			}

		} else {
			return nil, err
		}
	}
	gl := newGraceListener( l, UNIX )

	l.SetUnlinkOnClose(false)

	listeners = append(listeners, gl)
	return gl, nil
}


func inheritListeners() error {
	var retErr error
	inheritOnce.Do(func() {
		mutex.Lock()
		defer mutex.Unlock()
		countStr := os.Getenv(envCountKey)
		if countStr == "" {
			return
		}
		count, err := strconv.Atoi(countStr)
		if err != nil {
			retErr = fmt.Errorf("[grace net] found invalid count value: %s=%s", envCountKey, countStr)
			return
		}

		// In tests this may be overridden.
		fdStart := fdStart
		if fdStart == 0 {
			// In normal operations if we are inheriting, the listeners will begin at
			// fd 3.
			fdStart = 3
		}

		for i := fdStart; i < fdStart+count; i++ {
			file := os.NewFile(uintptr(i), "listener")
			l, err := net.FileListener(file)
			if err != nil {
				file.Close()
				retErr = fmt.Errorf("[grace net] error inheriting socket fd %d: %s", i, err)
				return
			}
			if err := file.Close(); err != nil {
				retErr = fmt.Errorf("[grace net] error closing inherited socket fd %d: %s", i, err)
				return
			}

			inherited = append(inherited, l)
		}
	})
	return retErr
}

func isSameAddr(a1, a2 net.Addr) bool {
	if a1.Network() != a2.Network() {
		return false
	}
	a1s := a1.String()
	a2s := a2.String()
	if a1s == a2s {
		return true
	}

	// This allows for ipv6 vs ipv4 local addresses to compare as equal. This
	// scenario is common when listening on localhost.
	const ipv6prefix = "[::]"
	a1s = strings.TrimPrefix(a1s, ipv6prefix)
	a2s = strings.TrimPrefix(a2s, ipv6prefix)
	const ipv4prefix = "0.0.0.0"
	a1s = strings.TrimPrefix(a1s, ipv4prefix)
	a2s = strings.TrimPrefix(a2s, ipv4prefix)
	return a1s == a2s
}


func StartProcess() (int, error) {
	mutex.Lock()
	defer mutex.Unlock()

	files := make([]*os.File, len(listeners))

	for i , srvPtr := range listeners {
		f,  err := srvPtr.File()
		if err == nil{
			files[i] = f
		}
	}
	if LogDetail {
		log.Println(files)
	}
	// Use the original binary location. This works with symlinks such that if
	// the file it points to has been changed we will use the updated symlink.
	path, err := exec.LookPath(os.Args[0])
	if err != nil {
		return 0, err
	}

	var args []string
	if len(os.Args) > 1 {
		for _, arg := range os.Args[1:] {
			args = append(args, arg)
		}
	}

	// Pass on the environment and replace the old count key with the new one.
	var env []string
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, envCountKeyPrefix) {
			env = append(env, v)
		}
	}
	env = append(env, fmt.Sprintf("%s%d", envCountKeyPrefix, len(listeners)))


	cmd := exec.Command(path, args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env    = env
	cmd.Dir    = originalWD
	cmd.ExtraFiles = files

	err = cmd.Start()
	if err != nil {
		log.Fatalf("[grace net] Restart: Failed to launch, error: %v", err)
		return 0, err
	}

	return cmd.Process.Pid, nil
}

func ShutDownParentProcess() error  {
	ppid := os.Getppid()
	if ppid != 1 && os.Getenv(envCountKey) != "" {
		//send kill signal
		if err := syscall.Kill(ppid, syscall.SIGTERM); err != nil {
			log.Fatalf("[grace net] failed to kill parent[%d] %s\n", ppid , err )
			return err
		}else {
			log.Printf("[grace net] kill parent[%d] success !\n", ppid)
		}
	}else {
		if LogDetail {
			log.Println("[grace net] dont need kill parent")
		}
	}
	return nil
}

func shutdown()  {
	<- quit_chan

	mutex.Lock()
	defer mutex.Unlock()
	if status == StateShuttingDown || status == StateTerminate {
		return
	}

	status = StateShuttingDown
	if LogDetail {
		log.Println("[grace net] status is shutting down")
	}
	tick := time.Tick(DefaultTimeout)

	ch := make(chan bool)

	go func() {
		for _ , srvPtr := range listeners {
			if srvPtr.Server != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := srvPtr.Server.Shutdown(ctx); err != nil {
					log.Printf( "[grace net] HTTP Server Shutdown: %s", err)
				}else {
					if LogDetail {
						log.Println("[grace net] http server shutdown succ")
					}
				}
			} else {
				err := srvPtr.Close()
				if err != nil{
					log.Printf("[grace net] close server fail, error [ %s ]\n", err)
				}else {
					if LogDetail {
						log.Println("[grace net] close server succ")
					}
				}
			}
		}
		ch <- true

	}()

	select {
	case <-tick:
		log.Println("[grace net] waiting too long time, force close.")
		break
	case <-ch:
		break
	}

	for _ , srvPtr := range listeners {
		srvPtr.closed <- true
	}

	status = StateTerminate
}


func init() {
	status = StateInit
	quit_chan = make(chan bool)

	hookableSignals = []os.Signal{
		syscall.SIGUSR2,
		syscall.SIGTERM,
	}

	go shutdown()
	go signalHandler()

}

//信号处理函数
func signalHandler() {
	//初始化chan、注册信号处理
	sigChan := make(chan os.Signal, 10)
	signal.Notify(
		sigChan,
		hookableSignals...,
	)

	pid := syscall.Getpid()

	var sig os.Signal

	for {
		sig = <-sigChan
		switch sig {
		case syscall.SIGUSR2:
			log.Printf("Pid[%d] Received SIG USR2. start new process.\n", pid )
			new_pid, err := StartProcess()
			if err != nil {
				log.Println("Fork err:", err)
			}else {
				if LogDetail {
					log.Println("Fork new process ", new_pid)
				}
			}
		case syscall.SIGTERM:
			log.Printf("Pid[%d] Received SIGTERM.\n", pid )

			if status == StateShuttingDown || status == StateTerminate {
				break
			}

			quit_chan <- true
		default:
			log.Printf("Received %v: nothing i care about...\n", sig)
		}
	}
}


