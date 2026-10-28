//go:build linux && amd64

package main

import (
	"testing"

	"golang.org/x/sys/unix"
)

// This list is a separate Linux x86-64 contract: include modern io_uring and
// borrowed-descriptor paths, not just the traditional connect syscall.
func TestNetworkAndExecutionPolicyIsComplete(t *testing.T) {
	required := map[string]uint32{"socket": 41, "connect": 42, "accept": 43, "sendto": 44, "recvfrom": 45, "sendmsg": 46, "recvmsg": 47, "shutdown": 48, "bind": 49, "listen": 50, "getsockname": 51, "getpeername": 52, "socketpair": 53, "setsockopt": 54, "getsockopt": 55, "accept4": 288, "recvmmsg": 299, "sendmmsg": 307, "io_uring_setup": 425, "io_uring_enter": 426, "io_uring_register": 427, "pidfd_getfd": 438, "fork": 57, "vfork": 58, "clone": 56, "clone3": 435, "execve": 59, "execveat": 322, "ptrace": 101, "process_vm_readv": 310, "process_vm_writev": 311, "bpf": 321, "setns": 308, "unshare": 272}
	p, err := loadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	filter, err := makeFilter(p, false)
	if err != nil {
		t.Fatal(err)
	}
	for name, number := range required {
		if p.Denied[name] != number {
			t.Errorf("missing syscall contract %s=%d", name, number)
		}
		if got := evaluate(filter, 0xc000003e, number); got != denyErrno {
			t.Errorf("%s bypassed late policy: %#x", name, got)
		}
	}
	initial, err := makeFilter(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if evaluate(initial, 0xc000003e, 59) != unix.SECCOMP_RET_ALLOW {
		t.Fatal("trusted initial PHP exec must be possible before the constructor locks exec")
	}
	for _, number := range []uint32{41, 42, 44, 56, 57, 58, 322, 425, 426, 427, 435, 438} {
		if evaluate(initial, 0xc000003e, number) != denyErrno {
			t.Errorf("initial filter allowed %d", number)
		}
	}
	if evaluate(filter, 0x40000003, 1) == unix.SECCOMP_RET_ALLOW {
		t.Fatal("compatibility architecture bypass")
	}
	if evaluate(filter, 0xc000003e, 0x40000000|41) != denyErrno {
		t.Fatal("x32 syscall bypass")
	}
	for _, number := range []uint32{0, 1, 3, 9, 257, 318} {
		if evaluate(filter, 0xc000003e, number) != unix.SECCOMP_RET_ALLOW {
			t.Errorf("ordinary file/crypto syscall %d unexpectedly denied", number)
		}
	}
}

func evaluate(program []unix.SockFilter, arch, number uint32) uint32 {
	var accumulator uint32
	for pc := 0; pc < len(program); pc++ {
		i := program[pc]
		switch i.Code {
		case unix.BPF_LD | unix.BPF_W | unix.BPF_ABS:
			if i.K == 4 {
				accumulator = arch
			} else {
				accumulator = number
			}
		case unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K:
			if accumulator == i.K {
				pc += int(i.Jt)
			} else {
				pc += int(i.Jf)
			}
		case unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K:
			if accumulator >= i.K {
				pc += int(i.Jt)
			} else {
				pc += int(i.Jf)
			}
		case unix.BPF_RET | unix.BPF_K:
			return i.K
		default:
			panic("unexpected BPF opcode")
		}
	}
	panic("filter fell through")
}
