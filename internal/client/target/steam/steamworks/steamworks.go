package steamworks

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Client is one connection to the running Steam client, speaking as one
// game through that game's own Steamworks library. Initializing marks the
// account as playing the game until Close, and a process can only ever
// speak as one game: the library caches its identity on first init, which
// is why reconciliation runs in a dedicated helper process rather than in
// the client that requested it.
type Client struct {
	shutdown func()
	storage  uintptr

	fileCount   func(uintptr) int32
	nameAndSize func(uintptr, int32, unsafe.Pointer) string
	fileWrite   func(uintptr, string, unsafe.Pointer, int32) bool
	fileRead    func(uintptr, string, unsafe.Pointer, int32) int32
	fileExists  func(uintptr, string) bool
	getFileSize func(uintptr, string) int32
}

// Connect loads the game's Steamworks library and initializes it as the
// given app. It fails when Steam is not running or the account is not
// signed in, which a caller reports rather than works around: without the
// store connected there is no registry to reconcile.
func Connect(libraryPath, appID string) (*Client, error) {
	// The library reads the app identity from the environment at init.
	// Process-wide, which is safe only because this runs in a helper
	// process dedicated to one game.
	if err := os.Setenv("SteamAppId", appID); err != nil {
		return nil, err
	}
	if err := os.Setenv("SteamGameId", appID); err != nil {
		return nil, err
	}
	handle, err := openLibrary(libraryPath)
	if err != nil {
		return nil, fmt.Errorf("load steamworks library: %w", err)
	}
	if err := initialize(handle); err != nil {
		return nil, err
	}
	client := &Client{}
	if err := register(&client.shutdown, handle, "SteamAPI_Shutdown"); err != nil {
		return nil, err
	}
	var storageAccessor func() uintptr
	if err := register(&storageAccessor, handle, "SteamAPI_SteamRemoteStorage_v016"); err != nil {
		client.shutdown()
		return nil, err
	}
	client.storage = storageAccessor()
	if client.storage == 0 {
		client.shutdown()
		return nil, fmt.Errorf("steam exposes no remote storage for app %s", appID)
	}
	for _, bind := range []struct {
		target any
		name   string
	}{
		{&client.fileCount, "SteamAPI_ISteamRemoteStorage_GetFileCount"},
		{&client.nameAndSize, "SteamAPI_ISteamRemoteStorage_GetFileNameAndSize"},
		{&client.fileWrite, "SteamAPI_ISteamRemoteStorage_FileWrite"},
		{&client.fileRead, "SteamAPI_ISteamRemoteStorage_FileRead"},
		{&client.fileExists, "SteamAPI_ISteamRemoteStorage_FileExists"},
		{&client.getFileSize, "SteamAPI_ISteamRemoteStorage_GetFileSize"},
	} {
		if err := register(bind.target, handle, bind.name); err != nil {
			client.shutdown()
			return nil, err
		}
	}
	return client, nil
}

// initialize brings the Steamworks connection up, preferring the current
// entry point and falling back to the one older libraries export.
func initialize(handle uintptr) error {
	if address, err := lookup(handle, "SteamAPI_InitFlat"); err == nil {
		var initFlat func(unsafe.Pointer) int32
		purego.RegisterFunc(&initFlat, address)
		var message [1024]byte
		if result := initFlat(unsafe.Pointer(&message[0])); result != 0 {
			return fmt.Errorf("steam init failed: %s", cString(message[:]))
		}
		return nil
	}
	var initClassic func() bool
	if err := register(&initClassic, handle, "SteamAPI_Init"); err != nil {
		return err
	}
	if !initClassic() {
		return fmt.Errorf("steam init failed; is Steam running and signed in?")
	}
	return nil
}

// Close disconnects from Steam, ending the "playing" presence the
// connection created.
func (c *Client) Close() {
	c.shutdown()
}

// Registry lists the store's cloud file registry for the connected game.
func (c *Client) Registry() []RegistryFile {
	total := c.fileCount(c.storage)
	files := make([]RegistryFile, 0, total)
	for index := int32(0); index < total; index++ {
		var size int32
		name := c.nameAndSize(c.storage, index, unsafe.Pointer(&size))
		if name == "" {
			continue
		}
		files = append(files, RegistryFile{Name: name, Size: int64(size)})
	}
	return files
}

// Holds reports whether the registry entry already carries exactly these
// bytes, so an idempotent reconciliation can skip the write.
func (c *Client) Holds(name string, content []byte) bool {
	if !c.fileExists(c.storage, name) {
		return false
	}
	if int(c.getFileSize(c.storage, name)) != len(content) {
		return false
	}
	if len(content) == 0 {
		return true
	}
	held := make([]byte, len(content))
	read := c.fileRead(c.storage, name, unsafe.Pointer(&held[0]), int32(len(held)))
	runtime.KeepAlive(held)
	return int(read) == len(content) && bytes.Equal(held, content)
}

// WriteFile registers content under name, exactly as the game would.
func (c *Client) WriteFile(name string, content []byte) error {
	payload := content
	if len(payload) == 0 {
		// The API distinguishes a null pointer from an empty write.
		payload = []byte{0}
	}
	ok := c.fileWrite(c.storage, name, unsafe.Pointer(&payload[0]), int32(len(content)))
	runtime.KeepAlive(payload)
	if !ok {
		return fmt.Errorf("steam refused the write (quota, size, or connectivity)")
	}
	if int(c.getFileSize(c.storage, name)) != len(content) {
		return fmt.Errorf("steam recorded a different size than was written")
	}
	return nil
}

// register binds one exported function, failing softly where RegisterLibFunc
// would panic: a library too old to export a symbol is a reportable answer.
func register(target any, handle uintptr, name string) error {
	address, err := lookup(handle, name)
	if err != nil || address == 0 {
		return fmt.Errorf("steamworks library does not export %s", name)
	}
	purego.RegisterFunc(target, address)
	return nil
}

func cString(raw []byte) string {
	if end := bytes.IndexByte(raw, 0); end >= 0 {
		raw = raw[:end]
	}
	return string(raw)
}
