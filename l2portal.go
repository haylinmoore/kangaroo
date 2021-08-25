package main

import (
	"io"
	"log"
	"net"

	"github.com/gliderlabs/ssh"
	"github.com/j-keck/arping"
)

func main() {
	log.Println("starting ssh server on port 2222...")

	sessions := []ssh.Session{}

	server := ssh.Server{
		LocalPortForwardingCallback: ssh.LocalPortForwardingCallback(func(ctx ssh.Context, dhost string, dport uint32) bool {
			var user_session ssh.Session
			for _, session := range sessions {
				if session.Context() == ctx {
					user_session = session
					log.Printf("FOUND SESSION")
				}
			}
			if user_session == nil {
				log.Printf("NO SESSION FOUND")
				return false
			}

			dstIP := net.ParseIP(dhost)
			if dstIP == nil {
				log.Printf("invalid destination IP: %s", dhost)
				return false
			}

			onL2 := false

			if dstIP.To4() != nil {
				if _, _, err := arping.Ping(dstIP); err == nil {
					onL2 = true
				}
			} else {
				log.Printf("Dest is v6")
			}

			if onL2 {
				log.Printf("Accepted forward", dhost, dport)
				return true
			} else {
				log.Println("Rejected forward: ", dhost, "is not on local L2")
				io.WriteString(user_session, "Rejected forward: "+dhost+" is not on local L2\n")
				return false
			}

		}),
		Addr: ":2222",
		Handler: ssh.Handler(func(s ssh.Session) {
			sessions = append(sessions, s)
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
