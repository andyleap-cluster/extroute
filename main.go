package main

import (
	"io"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jessevdk/go-flags"
)

var options struct {
	Routes []string `long:"route"`
}

type UDPProxyConn struct {
	Src     netip.AddrPort
	Dst     *net.UDPConn
	timeout int64
	close   chan struct{}
}

type UDPProxy struct {
	conns map[netip.AddrPort]*UDPProxyConn
	lock  sync.RWMutex
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
		mode := strings.Split(parts[0], ":")
		from, to := parts[0], parts[1]
		network := "tcp"
		if len(mode) == 2 {
			from = mode[1]
			network = mode[0]
		}
		switch network {
		case "tcp":
			go func(from, to string) {
				defer mainCond.Broadcast()
				l, err := net.Listen("tcp", ":"+from)
				if err != nil {
					log.Fatal(err)
				}
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
			}(from, to)
		case "udp":
			udpProxy := &UDPProxy{
				conns: map[netip.AddrPort]*UDPProxyConn{},
			}
			go func(from, to string, udpProxy *UDPProxy) {
				defer mainCond.Broadcast()
				fromUDP, err := net.ResolveUDPAddr("udp", ":"+from)
				if err != nil {
					log.Fatal(err)
				}
				toUDP, err := net.ResolveUDPAddr("udp", to)
				if err != nil {
					log.Fatal(err)
				}
				l, err := net.ListenUDP("udp", fromUDP)
				if err != nil {
					log.Fatal(err)
				}
				buf := make([]byte, 1500)
				for {
					n, addr, err := l.ReadFromUDPAddrPort(buf)
					if err != nil {
						log.Fatal(err)
					}
					udpProxy.lock.RLock()
					conn, ok := udpProxy.conns[addr]
					udpProxy.lock.RUnlock()
					if !ok {
						udpProxy.lock.Lock()
						conn, ok = udpProxy.conns[addr]
						if !ok {
							conn = &UDPProxyConn{
								Src:     addr,
								timeout: time.Now().Unix(),
							}
							conn.Dst, err = net.DialUDP("tcp", nil, toUDP)
							if err != nil {
								log.Fatal(err)
							}
							udpProxy.conns[addr] = conn
							go func(c *UDPProxyConn) {
								buf := make([]byte, 1500)
								for {
									n, err := conn.Dst.Read(buf)
									if err != nil {
										return
									}
									atomic.StoreInt64(&c.timeout, time.Now().Unix())
									_, err = l.WriteToUDPAddrPort(buf[:n], c.Src)
								}
							}(conn)
						}
						udpProxy.lock.Unlock()
					}
					conn.Dst.Write(buf[:n])
					atomic.StoreInt64(&conn.timeout, time.Now().Unix())
				}
			}(from, to, udpProxy)
			go func(udpProxy *UDPProxy) {
				for {
					udpProxy.lock.Lock()
					t := time.Now().Unix() - 60
					for _, conn := range udpProxy.conns {
						if conn.timeout < t {
							conn.Dst.Close()
							delete(udpProxy.conns, conn.Src)
						}
					}
					udpProxy.lock.Unlock()
					time.Sleep(time.Second * 60)
				}
			}(udpProxy)
		}
	}
	mainCond.Wait()
}
