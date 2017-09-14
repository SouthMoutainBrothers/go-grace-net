## go-grace-net

golang grace reload for tcp and unix sockets


## quickstart

```go
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
	http.HandleFunc("/hello", func (w http.ResponseWriter, req *http.Request) {
		time.Sleep(10 * time.Second)
		s := fmt.Sprintf("hello, world! send by pid[%d]\n", os.Getpid())
		w.Write([]byte(s))
	})
	log.Fatal(grace.ListenHTTPHandlerAndServe("tcp", ":12345", nil))
}
```



```shell
one shell exec
curl -v 127.0.0.1:12345/hello

another shell exec
kill -usr2 pid
```



## How to implement

Thanks to [graceful-restart-in-golang](http://grisha.org/blog/2014/06/03/graceful-restart-in-golang/) and [facebook go grace project](https://github.com/facebookgo/grace).



## License

go-grace-net source code is licensed under the Apache Licence, Version 2.0 (http://www.apache.org/licenses/LICENSE-2.0.html).