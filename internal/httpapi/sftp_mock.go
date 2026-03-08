package httpapi

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// MockSFTPFile is an in-memory file for MockSFTPClient.
type MockSFTPFile struct {
	*bytes.Reader
	info MockFileInfo
}

func (f *MockSFTPFile) Close() error   { return nil }
func (f *MockSFTPFile) Stat() (os.FileInfo, error) { return &f.info, nil }

// MockFileInfo implements os.FileInfo for the mock.
type MockFileInfo struct {
	Name_  string
	Size_  int64
	IsDir_ bool
	Mod_   time.Time
}

func (m *MockFileInfo) Name() string       { return m.Name_ }
func (m *MockFileInfo) Size() int64       { return m.Size_ }
func (m *MockFileInfo) Mode() os.FileMode { return 0 }
func (m *MockFileInfo) ModTime() time.Time { return m.Mod_ }
func (m *MockFileInfo) IsDir() bool       { return m.IsDir_ }
func (m *MockFileInfo) Sys() interface{}  { return nil }

// MockSFTPClient is an in-memory SFTP client for testing file transfer handlers.
// Keys are cleaned paths like "/" or "/dir/file.txt". Values are either:
// - nil for directories (list returns children by prefix)
// - []byte for file content
// - MockDirEntry for directory listing (Name, Size, IsDir, ModTime)
type MockSFTPClient struct {
	mu      sync.Mutex
	entries map[string]mockEntry
	// optional: if set, ReadDir returns this error
	ReadDirErr error
	OpenErr    error
	CreateErr  error
	RemoveErr  error
}

type mockEntry struct {
	content []byte
	dir     []MockFileInfo
}

// NewMockSFTPClient returns a new mock client with an empty root.
func NewMockSFTPClient() *MockSFTPClient {
	return &MockSFTPClient{
		entries: map[string]mockEntry{
			"/": {dir: []MockFileInfo{}},
		},
	}
}

// AddDir adds a directory and optional children for ReadDir. Path must be absolute and clean (e.g. "/" or "/foo").
// Open(path) will return a file that Stat().IsDir() is true. Pass nil for children to add an empty directory.
func (m *MockSFTPClient) AddDir(path string, children []MockFileInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = make(map[string]mockEntry)
	}
	dir := children
	if dir == nil {
		dir = []MockFileInfo{}
	}
	m.entries[path] = mockEntry{dir: dir}
}

// AddFile adds a file with the given content. Path must be absolute and clean.
func (m *MockSFTPClient) AddFile(path string, content []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = make(map[string]mockEntry)
	}
	m.entries[path] = mockEntry{content: content}
}

// Close implements FileTransferClient.
func (m *MockSFTPClient) Close() error { return nil }

// ReadDir implements FileTransferClient.
func (m *MockSFTPClient) ReadDir(path string) ([]os.FileInfo, error) {
	if m.ReadDirErr != nil {
		return nil, m.ReadDirErr
	}
	m.mu.Lock()
	e, ok := m.entries[path]
	m.mu.Unlock()
	if !ok {
		return nil, errors.New("no such directory")
	}
	if e.content != nil {
		return nil, errors.New("not a directory")
	}
	out := make([]os.FileInfo, len(e.dir))
	for i := range e.dir {
		out[i] = &e.dir[i]
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// Open implements FileTransferClient.
func (m *MockSFTPClient) Open(path string) (FileTransferFile, error) {
	if m.OpenErr != nil {
		return nil, m.OpenErr
	}
	m.mu.Lock()
	e, ok := m.entries[path]
	m.mu.Unlock()
	if !ok {
		return nil, errors.New("no such file")
	}
	if e.dir != nil {
		// directory: return a file that Stat() says IsDir
		return &MockSFTPFile{
			Reader: bytes.NewReader(nil),
			info:   MockFileInfo{Name_: filepath.Base(path), IsDir_: true, Mod_: time.Now()},
		}, nil
	}
	return &MockSFTPFile{
		Reader: bytes.NewReader(e.content),
		info:   MockFileInfo{Name_: filepath.Base(path), Size_: int64(len(e.content)), IsDir_: false, Mod_: time.Now()},
	}, nil
}

// Create implements FileTransferClient. It stores written content in the mock.
func (m *MockSFTPClient) Create(path string) (io.WriteCloser, error) {
	if m.CreateErr != nil {
		return nil, m.CreateErr
	}
	buf := &bytes.Buffer{}
	return &mockWriteCloser{buf: buf, client: m, path: path}, nil
}

type mockWriteCloser struct {
	buf    *bytes.Buffer
	client *MockSFTPClient
	path   string
}

func (w *mockWriteCloser) Write(p []byte) (n int, err error) { return w.buf.Write(p) }
func (w *mockWriteCloser) Close() error {
	w.client.mu.Lock()
	if w.client.entries == nil {
		w.client.entries = make(map[string]mockEntry)
	}
	w.client.entries[w.path] = mockEntry{content: append([]byte(nil), w.buf.Bytes()...)}
	w.client.mu.Unlock()
	return nil
}

// RemoveAll implements FileTransferClient.
func (m *MockSFTPClient) RemoveAll(path string) error {
	if m.RemoveErr != nil {
		return m.RemoveErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if path == "/" || path == "." {
		return errors.New("cannot remove root")
	}
	delete(m.entries, path)
	return nil
}
