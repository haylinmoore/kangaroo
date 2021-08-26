package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
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

func isIPInL2(ip net.IP) bool {
	pinger, err := ping.NewPinger(ip.String())
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
		if strings.Contains(host, ip.String()) {
			parts := strings.Split(host, " ")
			if parts[len(parts)-1] == "REACHABLE" || parts[len(parts)-1] == "DELAY" {
				onL2 = true
			}
		}
	}

	return onL2
}

func IPInPolicy(src net.IP, sets []string) bool {
	for _, name := range sets {
		ipSets := compiledIPSets[name]
		for _, ipnet := range ipSets {
			if ipnet.Contains(src) {
				return true
			}
		}
	}
	return false
}

func customDirectHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	d := localForwardChannelData{}
	if err := gossh.Unmarshal(newChan.ExtraData(), &d); err != nil {
		newChan.Reject(gossh.ConnectionFailed, "error parsing forward data: "+err.Error())
		return
	}

	dhost := d.DestAddr
	// Lets get the IP address of the remote host
	addrs, err := net.LookupIP(dhost)
	if err != nil {
		newChan.Reject(gossh.ConnectionFailed, "error looking up IP: "+err.Error())
		return
	}
	if len(addrs) == 0 {
		newChan.Reject(gossh.ConnectionFailed, "no IP found for "+dhost)
		return
	}
	dhost = addrs[0].String()

	// Now lets apply some policies
	srcip := conn.RemoteAddr().String()
	if strings.Contains(srcip, "]") {
		split := strings.Split(srcip, "]")
		srcip = split[0][1:]
	} else {
		srcip = strings.Split(srcip, ":")[0]
	}
	action := ""
	for name, policy := range Policies {
		fmt.Println("Checking policy", name, "for", srcip, "->", dhost)

		// Lets see if the policy applies
		if !IPInPolicy(net.ParseIP(srcip), []string{name}) {
			fmt.Println("Policy", name, "does not apply to", srcip)
			continue
		}
		fmt.Println("Policy", name, "does apply to", srcip)

		if IPInPolicy(net.ParseIP(dhost), policy.Allow) {
			// Accept it
			action = "accept"
			break
		}
		if IPInPolicy(net.ParseIP(dhost), policy.Deny) {
			// Accept it
			action = "deny"
			break
		}
		if policy.Default != "" {
			action = policy.Default
			break
		}
	}

	if action == "deny" {
		newChan.Reject(gossh.ConnectionFailed, "access to "+dhost+" is not allowed")
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

type IPSets map[string][]string

type Policy struct {
	L2Addr  bool
	Default string
	Allow   []string // ipset names
	Deny    []string // ipset names
}

type Config struct {
	Addr   string            `json:"addr"`
	Port   int               `json:"port"`
	Sets   IPSets            `json:"IPSet"`
	Policy map[string]Policy `json:"Policy"`
}

var compiledIPSets = make(map[string][]net.IPNet)
var Policies map[string]Policy

func main() {

	var config_file string
	flag.StringVar(&config_file, "c", "", "SSH config file")

	flag.Parse()
	default_IPSets := make(IPSets)
	default_IPSets["All"] = []string{"::/0"}
	config := Config{
		Addr: "",
		Port: 2222,
		Sets: default_IPSets,
	}

	if config_file != "" {
		fmt.Println("Loading config from", config_file)
		data, err := ioutil.ReadFile(config_file)
		if err != nil {
			log.Fatal(err)
		}
		err = json.Unmarshal(data, &config)
		if err != nil {
			log.Fatal(err)
		}
	}
	// for each IPSet
	for name, set := range config.Sets {
		// for each IP
		IPNets := make([]net.IPNet, 0)
		for _, ip := range set {
			if strings.Contains(ip, "/") {
				// It's a CIDR
				_, ipnet, err := net.ParseCIDR(ip)
				if err != nil {
					log.Fatal(err)
				}
				IPNets = append(IPNets, *ipnet)
			} else {
				parsedIP := net.ParseIP(ip)
				if parsedIP == nil {
					log.Fatal("Invalid IP:", ip)
				}
				if parsedIP.To4() == nil {
					IPNets = append(IPNets, net.IPNet{IP: parsedIP, Mask: net.CIDRMask(128, 128)})
				} else {
					IPNets = append(IPNets, net.IPNet{IP: parsedIP, Mask: net.CIDRMask(32, 32)})
				}
			}

		}
		compiledIPSets[name] = IPNets
	}

	Policies = config.Policy

	// list compiled ipsets
	for name, compiledIPSet := range compiledIPSets {
		fmt.Printf("%s: %v\n", name, compiledIPSet)
	}

	server := ssh.Server{
		Addr: fmt.Sprintf("%s:%d", config.Addr, config.Port),
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"direct-tcpip": customDirectHandler,
		},
	}
	log.Printf("starting ssh server on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
