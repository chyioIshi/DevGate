package proxy

import (
	"net/http/httputil"
	"net/netip"
	"strings"
)

const maxForwardedForHops = 32

func setXForwardedFor(pr *httputil.ProxyRequest, trustedCIDRs []netip.Prefix) {
	parsedRemoteIP, ok := parseRemoteIP(pr.In.RemoteAddr)
	if !ok {
		pr.Out.Header.Del("X-Forwarded-For")
		return
	}

	var parsedForwardedFor []netip.Addr
	if isTrustedProxy(parsedRemoteIP, trustedCIDRs) {
		if parsed, valid := parseXForwardedFor(
			pr.In.Header.Values("X-Forwarded-For"),
		); valid {
			parsedForwardedFor = parsed
		}
	}

	sanitized := sanitizeXForwardedFor(
		parsedRemoteIP,
		parsedForwardedFor,
		trustedCIDRs,
	)
	formatted, ok := formatXForwardedFor(sanitized)
	if !ok || formatted == "" {
		pr.Out.Header.Del("X-Forwarded-For")
		return
	}
	pr.Out.Header.Set("X-Forwarded-For", formatted)
}

func formatXForwardedFor(addresses []netip.Addr) (string, bool) {
	if len(addresses) == 0 {
		return "", true
	}
	if len(addresses) > maxForwardedForHops+1 {
		return "", false
	}
	formatted := make([]string, len(addresses))
	for i, addr := range addresses {
		if !addr.IsValid() {
			return "", false
		}
		if addr.Zone() != "" {
			return "", false
		}
		addr = addr.Unmap()
		formatted[i] = addr.String()
	}
	return strings.Join(formatted, ", "), true
}

func sanitizeXForwardedFor(
	remoteIP netip.Addr,
	forwardedFor []netip.Addr,
	trustedCIDRs []netip.Prefix,
) []netip.Addr {
	if !remoteIP.IsValid() {
		return nil
	}

	normalizedRemoteIP := remoteIP.Unmap()
	fallback := func() []netip.Addr {
		return []netip.Addr{normalizedRemoteIP}
	}

	if !isTrustedProxy(remoteIP, trustedCIDRs) || len(forwardedFor) > maxForwardedForHops {
		return fallback()
	}

	start := len(forwardedFor)
	for i := len(forwardedFor) - 1; i >= 0; i-- {
		addr := forwardedFor[i]
		if !addr.IsValid() {
			return fallback()
		}

		start = i
		if !isTrustedProxy(addr, trustedCIDRs) {
			break
		}
	}

	sanitized := make([]netip.Addr, 0, len(forwardedFor)-start+1)
	for _, addr := range forwardedFor[start:] {
		sanitized = append(sanitized, addr.Unmap())
	}
	sanitized = append(sanitized, normalizedRemoteIP)

	return sanitized
}

func parseXForwardedFor(values []string) ([]netip.Addr, bool) {
	if len(values) == 0 {
		return nil, true
	}
	addresses := make([]netip.Addr, 0, min(len(values), maxForwardedForHops))

	for _, value := range values {
		value = strings.TrimSpace(value)
		parsedAddresses := strings.Split(value, ",")
		for _, parsedAddress := range parsedAddresses {
			if len(addresses) >= maxForwardedForHops {
				return nil, false
			}
			addr, err := netip.ParseAddr(strings.TrimSpace(parsedAddress))
			if err != nil {
				return nil, false
			}
			if addr.Zone() != "" {
				return nil, false
			}
			addresses = append(addresses, addr.Unmap())
		}
	}

	return addresses, true
}

func parseRemoteIP(remoteAddr string) (netip.Addr, bool) {
	parsedAddr, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		return netip.Addr{}, false
	}
	addr := parsedAddr.Addr()
	port := parsedAddr.Port()
	addr = addr.WithZone("")
	if port == 0 {
		return netip.Addr{}, false
	}
	return addr, true
}

func isTrustedProxy(addr netip.Addr, trustedCIDRs []netip.Prefix) bool {
	if !addr.IsValid() {
		return false
	}
	unmappedAddr := addr.Unmap()
	for _, cidr := range trustedCIDRs {
		if !cidr.IsValid() {
			continue
		}
		if cidr.Contains(addr) || cidr.Contains(unmappedAddr) {
			return true
		}
	}
	return false
}
