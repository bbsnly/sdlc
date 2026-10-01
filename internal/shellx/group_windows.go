//go:build windows

package shellx

import (
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// InGroup has cmd, not yet started, run in a job object of its own, which
// ends every process in it once its handle is closed: a test runner's
// children otherwise keep writing to the worktree after the command that
// started them was stopped. Run puts the process in the job as soon as it has
// started; a child it starts before then is not in the job, and outlives a
// stop. Only sdlc holds the handle, so a stop killed outright, which no signal
// handler sees, closes it too and takes the job down with it.
func InGroup(cmd *exec.Cmd) {
	job, err := newJob()
	if err != nil {
		// Start refuses a command with Err set: one that could not be put in
		// a job is not run outside one.
		cmd.Err = err
		return
	}
	g := &group{job: job}
	groups.Lock()
	if groups.of == nil {
		groups.of = map[*exec.Cmd]*group{}
	}
	groups.of[cmd] = g
	groups.Unlock()
	cmd.Cancel = func() error {
		if g.joined.Load() && kernel32("TerminateJobObject", uintptr(g.job), 1) == nil {
			return nil
		}
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = waitDelay
}

// groups holds the job InGroup made for each command, until Run lets it go.
var groups struct {
	sync.Mutex
	of map[*exec.Cmd]*group
}

type group struct {
	job    syscall.Handle
	joined atomic.Bool
}

// The parts of JOBOBJECT_EXTENDED_LIMIT_INFORMATION, laid out as Windows lays
// them out, and the two values the job is made with.
type jobBasicLimits struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobIOCounters struct {
	ReadOperationCount, WriteOperationCount, OtherOperationCount uint64
	ReadTransferCount, WriteTransferCount, OtherTransferCount    uint64
}

type jobExtendedLimits struct {
	BasicLimitInformation jobBasicLimits
	IoInfo                jobIOCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

const (
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x2000

	processSetQuota  = 0x0100
	processTerminate = 0x0001
)

// newJob makes a job that kills everything in it when its handle is closed.
func newJob() (syscall.Handle, error) {
	r, err := kernel32Handle("CreateJobObjectW", 0, 0)
	if err != nil {
		return 0, err
	}
	job := syscall.Handle(r)
	set, err := kernel32Proc("SetInformationJobObject")
	if err != nil {
		_ = syscall.CloseHandle(job)
		return 0, err
	}
	var limits jobExtendedLimits
	limits.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	// The pointer is made in the call itself, which keeps what it points at
	// where it is until the call returns.
	if r, _, err := set.Call(uintptr(job), jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); r == 0 {
		_ = syscall.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

// joinGroup puts cmd's process, just started, in the job InGroup made for it.
// The pid cannot have been taken by another process: cmd still holds a handle
// to its own until it has been waited for.
func joinGroup(cmd *exec.Cmd) error {
	groups.Lock()
	g := groups.of[cmd]
	groups.Unlock()
	if g == nil {
		return nil
	}
	p, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer func() { _ = syscall.CloseHandle(p) }()
	if err := kernel32("AssignProcessToJobObject", uintptr(g.job), uintptr(p)); err != nil {
		return err
	}
	g.joined.Store(true)
	return nil
}

// leaveGroup closes cmd's job once it has been waited for, which ends
// anything the command left running in it.
func leaveGroup(cmd *exec.Cmd) {
	groups.Lock()
	g := groups.of[cmd]
	delete(groups.of, cmd)
	groups.Unlock()
	if g != nil {
		_ = syscall.CloseHandle(g.job)
	}
}

// kernel32 calls the kernel32.dll procedure name with arguments that hold
// no pointers; it reports failure with a zero.
func kernel32(name string, args ...uintptr) error {
	_, err := kernel32Handle(name, args...)
	return err
}

func kernel32Handle(name string, args ...uintptr) (uintptr, error) {
	proc, err := kernel32Proc(name)
	if err != nil {
		return 0, err
	}
	r, _, err := proc.Call(args...)
	if r == 0 {
		return 0, err
	}
	return r, nil
}

// kernel32Proc is the kernel32.dll procedure name. The DLL and its
// procedures are found here, when a landing runs a command, and never as
// package values, which every tool call would pay for. kernel32.dll is one of
// the DLLs Windows always loads from its own directory, whatever the search
// path says.
func kernel32Proc(name string) (*syscall.LazyProc, error) {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc(name)
	if err := proc.Find(); err != nil {
		return nil, err
	}
	return proc, nil
}
