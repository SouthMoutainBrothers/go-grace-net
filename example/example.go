package main

import (
	grace "github.com/SouthMoutainBrothers/go-grace-net"
	"fmt"
	"time"
	"net"
	"os"
	"log"
)


func main()  {
	err:=grace.ListenAndServe("tcp4", ":24680", grace.HandleFunc(echo) )
	if err != nil{
		panic(err)
	}
	log.Println("quit....", os.Getpid())
}

func echo(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 1000)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Println("[Fatal] read error, ", err)
	} else {
		log.Printf("read from %v, read %d bytes\n", conn.RemoteAddr(), n)
		time.Sleep(10 * time.Second)
		conn.Write(buf)
		pid := fmt.Sprintf("send by pid[%d]\n", os.Getpid())
		conn.Write([]byte(pid))
	}

}