// Package netrecover clears host state that survives a container restart:
// conntrack entries toward the Outline server and a leftover nftables table.
package netrecover

import (
	"net"
	"os"
	"os/exec"
	"strings"
)

const hostNetNS = "/host/netns"

// ClearPath drops conntrack state for the Outline server and the route cache.
// Entries live in the host network namespace, so restarting the container
// does not remove them. Best-effort: missing tools are ignored.
func ClearPath(server net.IP) {
	var ip string
	if server != nil {
		if v4 := server.To4(); v4 != nil {
			ip = v4.String()
		}
	}
	if ip != "" {
		run(host("conntrack", "-D", "-d", ip))
		run(host("conntrack", "-D", "-s", ip))
		run([]string{"conntrack", "-D", "-d", ip})
		run([]string{"conntrack", "-D", "-s", ip})
	}
	run(host("ip", "route", "flush", "cache"))
	run([]string{"ip", "route", "flush", "cache"})
}

// FlushStaleNAT removes an outline_gate nft table left behind in the host
// netns (host-network profile). Those rules keep redirecting traffic after
// the process is gone.
func FlushStaleNAT() {
	const script = "delete table inet outline_gate\n"
	runNFT(host("nft", "-f", "-"), script)
	runNFT([]string{"nft", "-f", "-"}, script)
}

func host(args ...string) []string {
	if _, err := os.Stat(hostNetNS); err != nil {
		return nil
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, "nsenter", "--net="+hostNetNS)
	out = append(out, args...)
	return out
}

func run(argv []string) {
	if len(argv) == 0 {
		return
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	_ = cmd.Run()
}

func runNFT(argv []string, script string) {
	if len(argv) == 0 {
		return
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(script)
	_ = cmd.Run()
}
