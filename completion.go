package cribrum

import (
	"fmt"
	"io"
)

const bashCompletion = `_cribrum() {
    local cur prev opts
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    opts="-listen -upstream -table -proto -iface -ttl -max-ttl -allow-private -debug -completion -version -h -help"
    case "${prev}" in
        -iface)
            local ifaces
            ifaces=$(ls /sys/class/net 2>/dev/null)
            COMPREPLY=( $(compgen -W "${ifaces}" -- "${cur}") )
            return 0
            ;;
        -completion)
            COMPREPLY=( $(compgen -W "bash zsh" -- "${cur}") )
            return 0
            ;;
        -listen|-upstream|-table|-proto|-ttl|-max-ttl)
            return 0
            ;;
    esac
    COMPREPLY=( $(compgen -W "${opts}" -- "${cur}") )
}
complete -F _cribrum cribrum
`

const zshCompletion = `#compdef cribrum
_cribrum() {
    _arguments \
        '-listen[Local bind address (UDP and TCP)]:addr:' \
        '-upstream[Upstream DNS resolver]:addr:' \
        '-table[Kernel routing table ID]:id:' \
        '-proto[Route protocol ID owned by cribrum]:id:' \
        '-iface[Egress tunnel interface (empty for blackhole)]:iface:_net_interfaces' \
        '-ttl[Minimum route lifetime in seconds]:sec:' \
        '-max-ttl[Maximum route lifetime in seconds]:sec:' \
        '-allow-private[Allow RFC 1918, CGNAT and ULA addresses]' \
        '-debug[Log every route change]' \
        '-completion[Generate completion script]:shell:(bash zsh)' \
        '-version[Print version information]' \
        '(-h -help)'{-h,-help}'[Show help message]'
}
_cribrum "$@"
`

// WriteCompletion writes a shell autocompletion script for bash or zsh to w.
func WriteCompletion(w io.Writer, shell string) error {
	var script string
	switch shell {
	case "bash":
		script = bashCompletion
	case "zsh":
		script = zshCompletion
	default:
		return fmt.Errorf("cribrum: unsupported shell %q (expected bash or zsh)", shell)
	}
	_, err := io.WriteString(w, script)
	return err
}
