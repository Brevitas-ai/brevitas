//go:build windows

package keyring

import (
	"context"
	"syscall"
	"unsafe"
)

// Windows Credential Manager bindings (advapi32) — no external dependencies.
const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFound           = 1168
)

var (
	advapi32       = syscall.NewLazyDLL("advapi32.dll")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func osBackend() string { return "Windows Credential Manager" }

func osSet(_ context.Context, secret string) error {
	target, err := syscall.UTF16PtrFromString(Service)
	if err != nil {
		return err
	}
	user, err := syscall.UTF16PtrFromString(Account)
	if err != nil {
		return err
	}

	blob := []byte(secret)
	cred := credentialW{
		Type:               credTypeGeneric,
		TargetName:         target,
		Persist:            credPersistLocalMachine,
		CredentialBlobSize: uint32(len(blob)),
		UserName:           user,
	}
	if len(blob) > 0 {
		cred.CredentialBlob = &blob[0]
	}

	r, _, callErr := procCredWrite.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r == 0 {
		return &backendError{op: "CredWrite", err: callErr}
	}
	return nil
}

func osGet(_ context.Context) (string, error) {
	target, err := syscall.UTF16PtrFromString(Service)
	if err != nil {
		return "", err
	}

	var pcred *credentialW
	r, _, callErr := procCredRead.Call(
		uintptr(unsafe.Pointer(target)),
		credTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&pcred)),
	)
	if r == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && uintptr(errno) == errorNotFound {
			return "", ErrNotFound
		}
		return "", &backendError{op: "CredRead", err: callErr}
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(pcred)))

	if pcred.CredentialBlobSize == 0 || pcred.CredentialBlob == nil {
		return "", nil
	}
	blob := unsafe.Slice(pcred.CredentialBlob, pcred.CredentialBlobSize)
	return string(blob), nil
}

func osDelete(_ context.Context) error {
	target, err := syscall.UTF16PtrFromString(Service)
	if err != nil {
		return err
	}
	r, _, callErr := procCredDelete.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if r == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && uintptr(errno) == errorNotFound {
			return ErrNotFound
		}
		return &backendError{op: "CredDelete", err: callErr}
	}
	return nil
}
