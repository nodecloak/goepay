//go:build linux && amd64

// Test-only child sandbox. It is never linked into the payment applications.
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"unsafe"

	"golang.org/x/sys/unix"
)

//go:embed policy.json
var policyJSON []byte

type policy struct {
	Schema       int               `json:"schema"`
	Architecture string            `json:"architecture"`
	Denied       map[string]uint32 `json:"denied"`
}

const denyErrno = uint32(unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM))

func loadPolicy() (policy, error) {
	var p policy
	if err := json.Unmarshal(policyJSON, &p); err != nil {
		return p, err
	}
	if p.Schema != 1 || p.Architecture != "linux-amd64" || len(p.Denied) < 35 {
		return p, fmt.Errorf("invalid sandbox policy")
	}
	return p, nil
}
func makeFilter(p policy, firstExec bool) ([]unix.SockFilter, error) {
	program := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: 0xc000003e, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K, K: 0x40000000, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: denyErrno},
	}
	seen := map[uint32]bool{}
	for _, n := range p.Denied {
		if n == 0 || seen[n] {
			return nil, fmt.Errorf("invalid/duplicate denied syscall")
		}
		seen[n] = true
	}
	numbers := make([]uint32, 0, len(seen))
	for n := range seen {
		numbers = append(numbers, n)
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	for _, n := range numbers {
		if firstExec && n == unix.SYS_EXECVE {
			continue
		}
		program = append(program, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: n, Jf: 1}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: denyErrno})
	}
	return append(program, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW}), nil
}
func run() error {
	if len(os.Args) < 4 || os.Args[2] != "--" || os.Args[3] != "/usr/local/bin/php" {
		return fmt.Errorf("exact test PHP command required")
	}
	root := os.Args[1]
	if !regexp.MustCompile(`^/tmp/goepay-phpinterop-[a-f0-9]{32}/cases/[a-f0-9]{32}/rootfs$`).MatchString(root) {
		return fmt.Errorf("owned test root required")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root || os.Geteuid() != 0 {
		return fmt.Errorf("root/canonical owned case directory required")
	}
	p, err := loadPolicy()
	if err != nil {
		return err
	}
	filter, err := makeFilter(p, true)
	if err != nil {
		return err
	}
	for fd := 0; fd < 3; fd++ {
		var stat unix.Stat_t
		if err = unix.Fstat(fd, &stat); err != nil {
			return err
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFSOCK {
			return fmt.Errorf("socket stdio is forbidden")
		}
	}
	// Inherited descriptors must not provide a network path around socket denial.
	if err = unix.CloseRange(3, ^uint(0), unix.CLOSE_RANGE_CLOEXEC); err != nil {
		return fmt.Errorf("close-range preflight: %w", err)
	}
	runtime.LockOSThread()
	if err = unix.Chroot(root); err != nil {
		return fmt.Errorf("chroot unavailable: %w", err)
	}
	if err = unix.Chdir("/"); err != nil {
		return err
	}
	if err = unix.Setgroups([]int{}); err != nil {
		return err
	}
	if err = unix.Setgid(65534); err != nil {
		return err
	}
	if err = unix.Setuid(65534); err != nil {
		return err
	}
	if err = unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	prog := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	if result, _, e := unix.RawSyscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&prog))); e != 0 || result != 0 {
		return fmt.Errorf("network seccomp unavailable: result=%d errno=%v", result, e)
	}
	// The verified preload constructor installs the second filter, including
	// execve, before PHP main. The executor rejects output without its nonce proof.
	return unix.Exec(os.Args[3], os.Args[3:], os.Environ())
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "PHP_SANDBOX_UNAVAILABLE:", err)
		os.Exit(125)
	}
}
