package cribrum

import (
	"fmt"
	"io"
)

// WriteCompletion writes a shell autocompletion script for bash or zsh to w.
func WriteCompletion(w io.Writer, shell string) error {
	switch shell {
	case "bash":
		_, err := fmt.Fprintln(w, `_cribrum() {
    local cur prev opts
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    opts="-listen -upstream -table -iface -ttl -completion -version -h -help"
    case "${prev}" in
        -iface)
            local ifaces=$(ls /sys/class/net 2>/dev/null)
            COMPREPLY=( $(compgen -W "${ifaces}" -- ${cur}) )
            return 0
            ;;
        -completion)
            COMPREPLY=( $(compgen -W "bash zsh" -- ${cur}) )
            return 0
            ;;
    esac
    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
}
complete -F _cribrum cribrum`)
		return err
	case "zsh":
		_, err := fmt.Fprintln(w, `#compdef cribrum
_cribrum() {
    _arguments \
        '-listen[Local UDP bind address]:addr:' \
        '-upstream[Upstream DNS resolver]:addr:' \
        '-table[Kernel routing table ID]:id:' \
        '-iface[Egress tunnel interface]:iface:_net_interfaces' \
        '-ttl[Minimum route TTL in seconds]:sec:' \
        '-completion[Generate completion script]:shell:(bash zsh)' \
        '-version[Print version information]' \
        '(-h -help)'{-h,-help}'[Show help message]'
}
_cribrum "$@"`)
		return err
	default:
		return fmt.Errorf("cribrum: unsupported shell %q (expected bash or zsh)", shell)
	}
}
