package main

import (
	"net/http"
	grace "github.com/SouthMoutainBrothers/go-grace-net"
	"fmt"
	"log"
	"os"
	"time"
)

func main()  {
	http.HandleFunc("/hello", func(w http.ResponseWriter, req *http.Request) {
		time.Sleep(10 * time.Second)
		s := fmt.Sprintf("hello, world! send by pid[%d]\n", os.Getpid())
		w.Write( []byte(s) )
	})
	log.Fatal(grace.ListenHTTPHandlerAndServe("tcp", ":12345", nil))
}
