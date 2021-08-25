package main

import (
	"io"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gliderlabs/ssh"
	"github.com/go-ping/ping"
	gossh "golang.org/x/crypto/ssh"
)

// direct-tcpip data struct as specified in RFC4254, Section 7.2
type localForwardChannelData struct {
	DestAddr string
	DestPort uint32

	OriginAddr string
	OriginPort uint32
}

func customDirectHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	d := localForwardChannelData{}
	if err := gossh.Unmarshal(newChan.ExtraData(), &d); err != nil {
		newChan.Reject(gossh.ConnectionFailed, "error parsing forward data: "+err.Error())
		return
	}

	dhost := d.DestAddr
	onL2 := false

	pinger, err := ping.NewPinger(dhost)
	if err != nil {
		newChan.Reject(gossh.Prohibited, dhost+" is not accessible")
		return
	}
	pinger.Count = 1
	pinger.Timeout = 1
	err = pinger.Run() // blocks until finished
	if err != nil {
		newChan.Reject(gossh.Prohibited, dhost+" is not accessible")
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

	if !onL2 {
		newChan.Reject(gossh.Prohibited, dhost+" is not on local L2")
		return
	}

	dest := net.JoinHostPort(d.DestAddr, strconv.FormatInt(int64(d.DestPort), 10))

	var dialer net.Dialer
	dconn, err := dialer.DialContext(ctx, "tcp", dest)
	if err != nil {
		newChan.Reject(gossh.ConnectionFailed, err.Error())
		return
	}

	ch, reqs, err := newChan.Accept()
	if err != nil {
		dconn.Close()
		return
	}
	go gossh.DiscardRequests(reqs)

	go func() {
		defer ch.Close()
		defer dconn.Close()
		io.Copy(ch, dconn)
	}()
	go func() {
		defer ch.Close()
		defer dconn.Close()
		io.Copy(dconn, ch)
	}()
}
func main() {
	log.Println("starting ssh server on port 2222...")

	server := ssh.Server{
		Addr: ":2222",
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"direct-tcpip": customDirectHandler,
		},
	}

	log.Fatal(server.ListenAndServe())
}
