package gont

import (
	"net"
)

var (
	DefaultIPv4Mask = net.IPNet{
		IP:   net.IPv4zero,
		Mask: net.CIDRMask(0, net.IPv4len*8),
	}

	DefaultIPv6Mask = net.IPNet{
		IP:   net.IPv6zero,
		Mask: net.CIDRMask(0, net.IPv6len*8),
	}
)
