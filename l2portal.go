package main

import (
	"io"
	"log"
	"os/exec"
	"strings"

	"github.com/gliderlabs/ssh"
	"github.com/go-ping/ping"
)

func main() {
	log.Println("starting ssh server on port 2222...")

	server := ssh.Server{
		LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
			log.Println("SESSION")
			log.Println(dhost, dport)

			onL2 := false

			pinger, err := ping.NewPinger(dhost)
			if err != nil {
				panic(err)
			}
			pinger.Count = 1
			pinger.Timeout = 1
			err = pinger.Run() // blocks until finished
			if err != nil {
				return false
			}
			stats := pinger.Statistics()

			cmd := exec.Command("ip", "neigh")
			out, err := cmd.Output()
			if err != nil {
				panic(err)
			}
			hosts := strings.Split(string(out), "\n")
			for _, host := range hosts {
				if strings.Contains(host, stats.IPAddr.IP.String()) {
					parts := strings.Split(host, " ")
					if parts[len(parts)-1] == "REACHABLE" || parts[len(parts)-1] == "DELAY" {
						onL2 = true
					}
				}
			}

			if onL2 {
				log.Println("Accepted forward", dhost, dport)
				return true
			} else {
				log.Println("Rejected forward: ", dhost, "is not on local L2")
				return false
			}

		}),
		Addr: ":2222",
		Handler: ssh.Handler(func(s ssh.Session) {
			io.WriteString(s, "Local forwarding available...\n")
			select {}
		}),
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"session":      ssh.DefaultSessionHandler,
			"direct-tcpip": ssh.DirectTCPIPHandler,
		},
	}

	log.Fatal(server.ListenAndServe())
}
