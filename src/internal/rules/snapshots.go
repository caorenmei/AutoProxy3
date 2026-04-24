package rules

func cloneWebRuleSet(set WebRuleSet) WebRuleSet {
	return WebRuleSet{
		proxyDomains:     append([]string(nil), set.proxyDomains...),
		directDomains:    append([]string(nil), set.directDomains...),
		proxyURLPrefixes: append([]string(nil), set.proxyURLPrefixes...),
	}
}

func cloneHostRuleSet(set HostRuleSet) HostRuleSet {
	cloned := HostRuleSet{
		exactHosts:       make(map[string]struct{}, len(set.exactHosts)),
		wildcardPatterns: append([]string(nil), set.wildcardPatterns...),
	}
	for host := range set.exactHosts {
		cloned.exactHosts[host] = struct{}{}
	}
	return cloned
}
