package config

import (
	"fmt"
	"os"
	"strings"
)

// ClashNode represents a single proxy node parsed from a Clash/Mihomo YAML file.
// Password is never formatted in String() to prevent accidental credential leaks.
type ClashNode struct {
	Name        string
	Type        string
	Server      string
	Port        string
	Username    string
	Password    string
	DialerProxy string
}

func (n ClashNode) Summary() string {
	chain := ""
	if n.DialerProxy != "" {
		chain = fmt.Sprintf(" (dialer-proxy=%s)", n.DialerProxy)
	}
	return fmt.Sprintf("%s [%s] %s:%s auth=%t%s", n.Name, n.Type, n.Server, n.Port, n.Username != "" || n.Password != "", chain)
}

// ParseClashProxyNode extracts a named SOCKS5/HTTP node from a Clash YAML file
// without external YAML dependencies. Supports both block-style and inline `{...}` entries.
func ParseClashProxyNode(yamlText, nodeName string) (*ClashNode, error) {
	lines := strings.Split(strings.ReplaceAll(yamlText, "\r\n", "\n"), "\n")
	inProxies := false
	var current *ClashNode
	var nodes []ClashNode

	flush := func() {
		if current != nil && current.Name != "" {
			nodes = append(nodes, *current)
		}
		current = nil
	}

	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " \t"))
		if indent == 0 && !(inProxies && strings.HasPrefix(trimmed, "- ")) {
			flush()
			if strings.HasPrefix(trimmed, "proxies:") {
				inProxies = true
			} else {
				inProxies = false
			}
			continue
		}
		if !inProxies {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			flush()
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if strings.HasPrefix(rest, "{") && strings.HasSuffix(rest, "}") {
				n := parseInlineFlowNode(rest)
				nodes = append(nodes, n)
				continue
			}
			current = &ClashNode{}
			if k, v, ok := splitYAMLKV(rest); ok {
				applyNodeField(current, k, v)
			}
			continue
		}
		if current != nil {
			if k, v, ok := splitYAMLKV(trimmed); ok {
				applyNodeField(current, k, v)
			}
		}
	}
	flush()

	for i := range nodes {
		if strings.EqualFold(nodes[i].Name, nodeName) {
			return &nodes[i], nil
		}
	}
	return nil, fmt.Errorf("proxy node %q not found in Clash YAML", nodeName)
}

// ImportClashNodeFromFile loads a Clash YAML file and finds the named SOCKS5/HTTP node.
func ImportClashNodeFromFile(path, nodeName string) (*ClashNode, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read clash yaml: %w", err)
	}
	node, err := ParseClashProxyNode(string(raw), nodeName)
	if err != nil {
		return nil, err
	}
	t := strings.ToLower(node.Type)
	if t != "socks5" && t != "http" {
		return nil, fmt.Errorf("node %q has type %q; only socks5/http static nodes can be imported directly", node.Name, node.Type)
	}
	if node.Server == "" || node.Port == "" {
		return nil, fmt.Errorf("node %q missing server or port", node.Name)
	}
	return node, nil
}

func parseInlineFlowNode(flow string) ClashNode {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(flow, "{"), "}"))
	var n ClashNode
	for _, part := range splitRespectingQuotes(inner, ',') {
		if k, v, ok := splitYAMLKV(strings.TrimSpace(part)); ok {
			applyNodeField(&n, k, v)
		}
	}
	return n
}

func splitRespectingQuotes(s string, sep byte) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			cur.WriteByte(ch)
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			cur.WriteByte(ch)
			continue
		}
		if ch == sep {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func splitYAMLKV(s string) (string, string, bool) {
	idx := strings.IndexByte(s, ':')
	if idx <= 0 {
		return "", "", false
	}
	k := strings.TrimSpace(s[:idx])
	v := strings.TrimSpace(s[idx+1:])
	// Strip inline comments when unquoted.
	if v != "" && v[0] != '"' && v[0] != '\'' {
		if c := strings.Index(v, " #"); c >= 0 {
			v = strings.TrimSpace(v[:c])
		}
	}
	v = unquoteYAML(v)
	return k, v, true
}

func unquoteYAML(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func applyNodeField(n *ClashNode, k, v string) {
	switch strings.ToLower(k) {
	case "name":
		n.Name = v
	case "type":
		n.Type = strings.ToLower(v)
	case "server":
		n.Server = v
	case "port":
		n.Port = v
	case "username":
		n.Username = v
	case "password":
		n.Password = v
	case "dialer-proxy":
		n.DialerProxy = v
	}
}
