package layer2

import (
	"fmt"
	"net"
	"time"

	"github.com/hamptonmoore/ping"
)

func TTL1(dst net.IP) bool {
	pinger, err := ping.NewPinger(dst.String())
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
		return false
	}
	pinger.Timeout = time.Second
	pinger.Count = 1
	pinger.TTL = 1
	pinger.SetPrivileged(true)
	pinger.Run()

	return pinger.Statistics().PacketsRecv != 0

}
