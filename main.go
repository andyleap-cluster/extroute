package main

import (
	"io"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/jessevdk/go-flags"
)

var options struct {
	Routes []string `long:"route"`
}

func main() {
	_, err := flags.Parse(&options)
	if err != nil {
		log.Fatal(err)
	}
	mainCond := sync.NewCond(&sync.Mutex{})
	mainCond.L.Lock()
	defer mainCond.L.Unlock()
	for _, route := range options.Routes {
		parts := strings.Split(route, ">")
		from, to := parts[0], parts[1]
		l, err := net.Listen("tcp", ":"+from)
		if err != nil {
			log.Fatal(err)
		}
		go func(to string, l net.Listener) {
			defer mainCond.Broadcast()
			for {
				fc, err := l.Accept()
				log.Println("forwarding to ", to)
				if err != nil {
					log.Fatal(err)
				}
				go func(fc net.Conn) {
					defer fc.Close()
					tc, err := net.Dial("tcp", to)
					if err != nil {
						log.Print(err)
						return
					}
					cond := sync.NewCond(&sync.Mutex{})
					cond.L.Lock()
					defer func() {
						cond.L.Unlock()
						tc.Close()
						fc.Close()
					}()
					go func() {
						_, err := io.Copy(tc, fc)
						if err != nil {
							log.Println(err)
						}
						cond.L.Lock()
						defer cond.L.Unlock()
						cond.Broadcast()
					}()
					go func() {
						_, err := io.Copy(fc, tc)
						if err != nil {
							log.Println(err)
						}
						cond.L.Lock()
						defer cond.L.Unlock()
						cond.Broadcast()
					}()
					cond.Wait()
				}(fc)
			}
		}(to, l)
	}
	mainCond.Wait()
}
