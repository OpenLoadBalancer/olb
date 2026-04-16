package cmdi

// dangerousCommands maps command names to threat scores.
// Higher scores indicate more dangerous commands.
var dangerousCommands = map[string]int{
	// File operations
	"cat": 40, "ls": 40, "wget": 75, "curl": 75,
	// System info
	"id": 70, "whoami": 70, "uname": 70, "hostname": 60,
	// Network tools (reverse shell risk)
	"nc": 90, "ncat": 90, "netcat": 90,
	// Destructive
	"rm": 80, "chmod": 70, "chown": 70,
	// Credential access
	"passwd": 85, "shadow": 85,
	// Scripting languages
	"python": 85, "python3": 85, "perl": 85, "ruby": 85, "php": 85,
	// Encoding/data tools
	"base64": 75, "xxd": 70, "dd": 70,
	// DNS/network
	"nslookup": 60, "dig": 60, "ping": 50,
	// Remote access
	"telnet": 80, "ssh": 80, "scp": 80,
	// Text processing with code execution
	"awk": 60, "sed": 60, "xargs": 70,
	// Environment
	"env": 60, "export": 50, "printenv": 60,

	// Windows-specific commands
	"cmd": 85, "powershell": 90, "pwsh": 90,
	"net": 80, "net1": 80, "netsh": 85,
	"reg": 85, "regsvr32": 90, "rundll32": 90,
	"wmic": 85, "taskkill": 80, "tasklist": 70,
	"sc": 75, "schtasks": 85,
	"certutil": 90, "bitsadmin": 90, "mshta": 95,
	"cscript": 80, "wscript": 80,
	"psexec": 95, "at": 70,
	"format": 90, "del": 70, "rmdir": 70,
	"xcopy": 60, "robocopy": 60, "type": 40,
	"copy": 50, "move": 50, "ren": 50,
	"icacls": 75, "takeown": 75,
	"systeminfo": 75, "driverquery": 60,
	"qwinsta": 70, "rwinsta": 70,
	"dsquery": 80, "nltest": 80,
	"set": 30, "ver": 30, "vol": 30,
}

// shellPaths are absolute paths to shell interpreters.
var shellPaths = []string{
	"/bin/sh", "/bin/bash", "/bin/zsh", "/bin/csh", "/bin/ksh",
	"/usr/bin/env", "/usr/bin/python", "/usr/bin/perl",
	"cmd.exe", "powershell", "powershell.exe", "powershell_ise.exe",
	"pwsh.exe",
	"mshta.exe",
	"bash.exe", "wsl.exe",
	"c:/windows/system32/cmd.exe", "\\windows\\system32\\cmd.exe",
}
