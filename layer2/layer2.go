package layer2

import (
	"bytes"
	"encoding/gob"
	"log"
	"net"
	"os/exec"
	"strings"
	"syscall"

	"github.com/go-ping/ping"
)

func IPROUTE(dst net.IP) bool {
	pinger, err := ping.NewPinger(dst.String())
	if err != nil {
		// Can't be on same L2 if we can't ping it
		return false
	}
	pinger.Count = 1
	pinger.Timeout = 1
	err = pinger.Run() // blocks until finished
	if err != nil {
		// Can't be on same L2 if we can't ping it
		return false
	}
	onL2 := false
	cmd := exec.Command("ip", "neigh")
	out, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	hosts := strings.Split(string(out), "\n")
	for _, host := range hosts {
		if strings.Contains(host, dst.String()) {
			parts := strings.Split(host, " ")
			if parts[len(parts)-1] == "REACHABLE" || parts[len(parts)-1] == "DELAY" {
				onL2 = true
			}
		}
	}

	return onL2
}

func TTL1IPV4(dst net.IP) bool {

	// Set up the socket to receive inbound packets
	recvSocket, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_ICMP)
	if err != nil {
		log.Fatal("Receive socket: ", err)
	}

	// Set up the socket to send packets out.
	sendSocket, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_UDP)
	if err != nil {
		log.Fatal("Send socket: ", err)
	}
	// This sets the current hop TTL
	syscall.SetsockoptInt(sendSocket, 0x0, syscall.IP_TTL, 1)
	// This sets the timeout to wait for a response from the remote host
	time := syscall.NsecToTimeval(1000 * 1000 * 1000 * 2)
	syscall.SetsockoptTimeval(recvSocket, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &time)

	defer syscall.Close(recvSocket)
	defer syscall.Close(sendSocket)

	// Bind to the local socket to listen for ICMP packets
	syscall.Bind(recvSocket, &syscall.SockaddrInet4{Port: 33434, Addr: [4]byte{0, 0, 0, 0}})

	if dst.To4() != nil {
		var ip [4]byte
		copy(ip[:], dst.To4())
		syscall.Sendto(sendSocket, []byte{0x0}, 0, &syscall.SockaddrInet4{Port: 33434, Addr: ip})
	} else {
		var ip [16]byte
		copy(ip[:], dst.To16())
		syscall.Sendto(sendSocket, []byte{0x0}, 0, &syscall.SockaddrInet6{Port: 33434, Addr: ip})
	}
	// Max reasonable MTU is 9000
	var p = make([]byte, 9000)
	_, from, err := syscall.Recvfrom(recvSocket, p, 0)
	if err != nil {
		log.Fatal("Recieving: ", err)
	}

	var network bytes.Buffer        // Stand-in for a network connection
	enc := gob.NewEncoder(&network) // Will write to network.
	dec := gob.NewDecoder(&network) // Will read from network.
	err = enc.Encode(from)
	if err != nil {
		log.Fatal("encode error:", err)
	}

	var response syscall.SockaddrInet4
	err = dec.Decode(&response)
	if err != nil {
		log.Fatal("decode error:", err)
	}
	responseIP := net.IPv4(response.Addr[0], response.Addr[1], response.Addr[2], response.Addr[3])

	return responseIP.Equal(dst)
}
