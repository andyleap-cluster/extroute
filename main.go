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

type UDPTuple struct {
	SrcIP net.IP
	SrcPort uint16
	DstIP net.IP
	DstPort uint16
}

var udpMap = make(map[UDPTuple])

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
		mode := strings.Split(parts[0], ":")
		from, to := parts[0], parts[1]
		network := "tcp"
		if len(mode) == 2 {
			from = mode[1]
			network = mode[0]
		}
		l, err := net.Listen(network, ":"+from)
		if err != nil {
			log.Fatal(err)
		}
		switch network {
		case "tcp":
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
		case "udp":
			go func(to string, l net.Listener) {
				defer mainCond.Broadcast()
				for {
					fc, err := l.Accept()
					log.Println("forwarding to ", to)
					if err != nil {
						log.Fatal(err)
					}
				}
			}(to, l)
		}
	}
	mainCond.Wait()
}
