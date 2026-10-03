package serviceauth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"managed-llama/internal/config"
)

// Administrators, SYSTEM and Windows Modules Installer may own privileged code.
func trustedSID(sid string) bool {
	return sid == "S-1-5-18" || sid == "S-1-5-32-544" || sid == "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
}

// ACL and ACE layouts are the Windows ACL / ACCESS_ALLOWED_ACE structures.
type aclHeader struct {
	Revision, Reserved     byte
	Size, Count, Reserved2 uint16
}
type aceHeader struct {
	Type, Flags byte
	Size        uint16
}
type allowedACE struct {
	Header aceHeader
	Mask   uint32
	SID    uint32
}

var getAce = windows.NewLazySystemDLL("advapi32.dll").NewProc("GetAce")

func checkACL(path string, ancestor bool, private bool) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	return checkDescriptor(sd, ancestor, private)
}

func checkDescriptor(sd *windows.SECURITY_DESCRIPTOR, ancestor, private bool) error {
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if owner == nil || !trustedSID(owner.String()) {
		return errors.New("owner must be Administrators, SYSTEM or TrustedInstaller")
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if acl == nil {
		return errors.New("unrestricted DACL is not permitted")
	}
	// On ancestors, adding an unrelated child is harmless, but deleting/replacing
	// a path component or changing its permissions is not.
	mask := uint32(windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.GENERIC_ALL | windows.GENERIC_WRITE | 0x40)
	if !ancestor {
		mask |= 0x2 | 0x4 | 0x10 | 0x100
	}
	if private {
		mask = ^uint32(0)
	}
	count := (*aclHeader)(unsafe.Pointer(acl)).Count
	for i := uint32(0); i < uint32(count); i++ {
		var entry *allowedACE
		ok, _, callErr := getAce.Call(uintptr(unsafe.Pointer(acl)), uintptr(i), uintptr(unsafe.Pointer(&entry)))
		if ok == 0 {
			return callErr
		}
		if entry.Header.Flags&0x08 != 0 {
			continue
		} // INHERIT_ONLY_ACE
		if entry.Header.Type == 1 {
			continue
		} // deny ACE; ignoring it is conservative
		if entry.Header.Type != 0 || entry.Header.Size < 16 {
			return errors.New("unsupported access ACE; refusing privileged path")
		}
		sid := (*windows.SID)(unsafe.Pointer(&entry.SID)).String()
		if entry.Mask&mask != 0 && !trustedSID(sid) && sid != "S-1-3-4" {
			return errors.New("non-administrators have access to a protected path")
		}
	}
	return nil
}

// ValidatePath refuses network paths and every reparse point, and checks parent
// permissions as well as the file itself. It never changes a user's ACLs.
func ValidatePath(path string) error {
	if !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) {
		return errors.New("service paths must be absolute local paths")
	}
	path = filepath.Clean(path)
	for current, first := path, true; ; first = false {
		p, err := windows.UTF16PtrFromString(current)
		if err != nil {
			return err
		}
		attrs, err := windows.GetFileAttributes(p)
		if err != nil {
			return fmt.Errorf("service path %s: %w", current, err)
		}
		if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("service path contains a reparse point: %s", current)
		}
		if err := checkACL(current, !first, false); err != nil {
			return fmt.Errorf("unsafe service path %s: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return nil
}

func contained(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Use a dedicated protected installation directory for configuration, binaries,
// DLLs and models. This also prevents replacement of a dependency beside an EXE.
func ValidateConfig(configPath string, cfg config.Config) error {
	if !net.ParseIP(cfg.Host).IsLoopback() && !strings.EqualFold(cfg.Host, "localhost") {
		return errors.New("service llama host must be loopback")
	}
	root := filepath.Dir(configPath)
	if !filepath.IsAbs(cfg.ServerPath) || !contained(root, cfg.ServerPath) {
		return errors.New("service server_path must be absolute and inside the protected config directory")
	}
	models := cfg.ModelsDir
	if !filepath.IsAbs(models) {
		models = filepath.Join(root, models)
	}
	if !contained(root, models) {
		return errors.New("service models_dir must be inside the protected config directory")
	}
	if err := ValidatePath(root); err != nil {
		return err
	}
	if err := ValidatePath(configPath); err != nil {
		return err
	}
	if err := ValidatePath(cfg.ServerPath); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return ValidatePath(path)
	})
}

func KeyPath(configPath string) string { return configPath + ".service-key" }

func ReadKey(configPath string) (string, error) {
	path := KeyPath(configPath)
	if err := ValidatePath(path); err != nil {
		return "", err
	}
	if err := checkACL(path, false, true); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("서비스 관리 키를 읽으려면 관리자 권한으로 실행하세요")
	}
	key := strings.TrimSpace(string(b))
	if !validKey(key) {
		return "", errors.New("invalid service key file")
	}
	return key, nil
}

func EnsureKey(configPath string) (string, error) {
	if err := ValidatePath(filepath.Dir(configPath)); err != nil {
		return "", err
	}
	if _, err := os.Lstat(KeyPath(configPath)); err == nil {
		return ReadKey(configPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	key := hex.EncodeToString(raw[:])
	// Set the private DACL at creation time, before writing any secret bytes.
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)")
	if err != nil {
		return "", err
	}
	name, err := windows.UTF16PtrFromString(KeyPath(configPath))
	if err != nil {
		return "", err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return "", err
	}
	f := os.NewFile(uintptr(h), KeyPath(configPath))
	_, writeErr := f.WriteString(key + "\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(KeyPath(configPath))
		return "", errors.Join(writeErr, closeErr)
	}
	return key, nil
}
