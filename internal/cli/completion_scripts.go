package cli

const bashCompletionScript = `# bash completion for olb
_olb() {
    local cur prev opts
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    # Main commands
    local commands="start stop reload status version config backend backend-add backend-remove backend-drain backend-enable backend-disable backend-stats health route route-add route-remove route-test cert cert-list cert-add cert-remove cert-renew cert-info metrics metrics-show metrics-export completion"

    # Global options
    local global_opts="--help --version --format"

    case "${COMP_CWORD}" in
        1)
            COMPREPLY=( $(compgen -W "${commands}" -- ${cur}) )
            return 0
            ;;
        *)
            case "${COMP_WORDS[1]}" in
                start)
                    local opts="--config -c --daemon -d --pid-file"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                stop|reload)
                    local opts="--pid-file --api-addr"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                status|backend|backend-stats|health|metrics|metrics-show|cert|cert-list|cert-info)
                    local opts="--api-addr --format"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                backend-add)
                    local opts="--api-addr --weight --health-check"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                backend-remove|backend-drain|backend-enable|backend-disable)
                    local opts="--api-addr"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                route-add)
                    local opts="--api-addr --backend --priority"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                route-remove|route-test)
                    local opts="--api-addr --format"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                cert-add)
                    local opts="--api-addr --auto --cert --key"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                cert-remove|cert-renew)
                    local opts="--api-addr"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                metrics-export)
                    local opts="--api-addr --output --format"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                config|config-show)
                    local opts="--api-addr --format"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                config-diff)
                    local opts="--api-addr --file"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                config-validate)
                    local opts="--config -c"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
                completion)
                    local opts="--shell"
                    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
                    return 0
                    ;;
            esac
            ;;
    esac
}

complete -F _olb olb
`

// zshCompletionScript is the zsh completion script
const zshCompletionScript = `#compdef olb

_olb() {
    local curcontext="$curcontext" state line
    typeset -A opt_args

    _arguments -C \
        '(-h --help)'{-h,--help}'[Show help]' \
        '(-v --version)'{-v,--version}'[Show version]' \
        '--format[Output format]:format:(json table)' \
        '1: :_olb_commands' \
        '*:: :->args'

    case "$line[1]" in
        start)
            _arguments \
                '(-c --config)'{-c,--config}'[Config file path]:file:_files -g "*.yaml"' \
                '(-d --daemon)'{-d,--daemon}'[Run in background]' \
                '--pid-file[PID file path]:file:_files'
            ;;
        stop|reload)
            _arguments \
                '--pid-file[PID file path]:file:_files' \
                '--api-addr[Admin API address]:address:'
            ;;
        status|backend|backend-stats|health|metrics|metrics-show|cert|cert-list|cert-info)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--format[Output format]:format:(json table)'
            ;;
        backend-add)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--weight[Backend weight]:weight:' \
                '--health-check[Health check path]:path:'
            ;;
        backend-remove|backend-drain|backend-enable|backend-disable)
            _arguments \
                '--api-addr[Admin API address]:address:'
            ;;
        route-add)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--backend[Backend pool name]:pool:' \
                '--priority[Route priority]:priority:'
            ;;
        route-remove|route-test)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--format[Output format]:format:(json table)'
            ;;
        cert-add)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--auto[Use ACME/Lets Encrypt]' \
                '--cert[Certificate file path]:file:_files' \
                '--key[Private key file path]:file:_files'
            ;;
        cert-remove|cert-renew)
            _arguments \
                '--api-addr[Admin API address]:address:'
            ;;
        metrics-export)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--output[Output file path]:file:_files' \
                '--format[Export format]:format:(json prometheus)'
            ;;
        config|config-show)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--format[Output format]:format:(yaml json)'
            ;;
        config-diff)
            _arguments \
                '--api-addr[Admin API address]:address:' \
                '--file[Config file to compare with]:file:_files -g "*.yaml"'
            ;;
        config-validate)
            _arguments \
                '(-c --config)'{-c,--config}'[Config file path]:file:_files -g "*.yaml"'
            ;;
        completion)
            _arguments \
                '--shell[Shell type]:shell:(bash zsh fish)'
            ;;
    esac
}

_olb_commands() {
    local commands=(
        "start:Start the load balancer"
        "stop:Stop the load balancer"
        "reload:Reload configuration"
        "status:Show system status"
        "version:Show version information"
        "config:Configuration commands"
        "backend:Backend management"
        "health:Health check commands"
        "route:Route management"
        "cert:Certificate management"
        "metrics:Metrics commands"
        "completion:Generate shell completion script"
    )
    _describe -t commands 'olb commands' commands "$@"
}

compdef _olb olb
`

// fishCompletionScript is the fish completion script
const fishCompletionScript = `# fish completion for olb

# Disable file completions for the olb command
complete -c olb -f

# Global options
complete -c olb -s h -l help -d "Show help"
complete -c olb -s v -l version -d "Show version"
complete -c olb -l format -d "Output format" -a "json table"

# Commands
complete -c olb -n "__fish_use_subcommand" -a "start" -d "Start the load balancer"
complete -c olb -n "__fish_use_subcommand" -a "stop" -d "Stop the load balancer"
complete -c olb -n "__fish_use_subcommand" -a "reload" -d "Reload configuration"
complete -c olb -n "__fish_use_subcommand" -a "status" -d "Show system status"
complete -c olb -n "__fish_use_subcommand" -a "version" -d "Show version information"
complete -c olb -n "__fish_use_subcommand" -a "config" -d "Configuration commands"
complete -c olb -n "__fish_use_subcommand" -a "backend" -d "Backend management"
complete -c olb -n "__fish_use_subcommand" -a "health" -d "Health check commands"
complete -c olb -n "__fish_use_subcommand" -a "route" -d "Route management"
complete -c olb -n "__fish_use_subcommand" -a "cert" -d "Certificate management"
complete -c olb -n "__fish_use_subcommand" -a "metrics" -d "Metrics commands"
complete -c olb -n "__fish_use_subcommand" -a "completion" -d "Generate shell completion script"

# start command options
complete -c olb -n "__fish_seen_subcommand_from start" -s c -l config -d "Config file path" -r
complete -c olb -n "__fish_seen_subcommand_from start" -s d -l daemon -d "Run in background"
complete -c olb -n "__fish_seen_subcommand_from start" -l pid-file -d "PID file path" -r

# stop/reload command options
complete -c olb -n "__fish_seen_subcommand_from stop reload" -l pid-file -d "PID file path" -r
complete -c olb -n "__fish_seen_subcommand_from stop reload" -l api-addr -d "Admin API address" -r

# status command options
complete -c olb -n "__fish_seen_subcommand_from status" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from status" -l format -d "Output format" -a "json table"

# backend command options
complete -c olb -n "__fish_seen_subcommand_from backend backend-stats" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from backend backend-stats" -l format -d "Output format" -a "json table"

# backend-add command options
complete -c olb -n "__fish_seen_subcommand_from backend-add" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from backend-add" -l weight -d "Backend weight" -r
complete -c olb -n "__fish_seen_subcommand_from backend-add" -l health-check -d "Health check path" -r

# backend-remove/drain/enable/disable command options
complete -c olb -n "__fish_seen_subcommand_from backend-remove backend-drain backend-enable backend-disable" -l api-addr -d "Admin API address" -r

# route command options
complete -c olb -n "__fish_seen_subcommand_from route route-add route-remove route-test" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from route route-test" -l format -d "Output format" -a "json table"

# route-add command options
complete -c olb -n "__fish_seen_subcommand_from route-add" -l backend -d "Backend pool name" -r
complete -c olb -n "__fish_seen_subcommand_from route-add" -l priority -d "Route priority" -r

# cert command options
complete -c olb -n "__fish_seen_subcommand_from cert cert-list cert-info" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from cert cert-list cert-info" -l format -d "Output format" -a "json table"

# cert-add command options
complete -c olb -n "__fish_seen_subcommand_from cert-add" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from cert-add" -l auto -d "Use ACME/Lets Encrypt"
complete -c olb -n "__fish_seen_subcommand_from cert-add" -l cert -d "Certificate file path" -r
complete -c olb -n "__fish_seen_subcommand_from cert-add" -l key -d "Private key file path" -r

# cert-remove/renew command options
complete -c olb -n "__fish_seen_subcommand_from cert-remove cert-renew" -l api-addr -d "Admin API address" -r

# metrics command options
complete -c olb -n "__fish_seen_subcommand_from metrics metrics-show" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from metrics metrics-show" -l format -d "Output format" -a "json table prometheus"

# metrics-export command options
complete -c olb -n "__fish_seen_subcommand_from metrics-export" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from metrics-export" -l output -d "Output file path" -r
complete -c olb -n "__fish_seen_subcommand_from metrics-export" -l format -d "Export format" -a "json prometheus"

# config command options
complete -c olb -n "__fish_seen_subcommand_from config config-show" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from config config-show" -l format -d "Output format" -a "yaml json"

# config-diff command options
complete -c olb -n "__fish_seen_subcommand_from config-diff" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from config-diff" -l file -d "Config file to compare with" -r

# config-validate command options
complete -c olb -n "__fish_seen_subcommand_from config-validate" -s c -l config -d "Config file path" -r

# completion command options
complete -c olb -n "__fish_seen_subcommand_from completion" -l shell -d "Shell type" -a "bash zsh fish"

# health command options
complete -c olb -n "__fish_seen_subcommand_from health" -l api-addr -d "Admin API address" -r
complete -c olb -n "__fish_seen_subcommand_from health" -l format -d "Output format" -a "json table"
`
