// Package archive reads the common Unix ar archive format used by static libraries.
package archive

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	regularMagic = "!<arch>\n"
	thinMagic    = "!<thin>\n"
	headerSize   = 60
)

type Member struct {
	Name string
	Path string
	Thin bool
	// Content is the member's bytes, for a regular archive. A thin archive
	// holds only references, so its members are files on disk and this is nil.
	// It is a slice of the archive already read, not a second copy.
	Content []byte
}

func ParseFile(path string) ([]Member, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Parse(file, path)
}

// Parse enumerates archive members. For thin archives, Path is resolved relative
// to the archive directory as required by the linker semantics.
func Parse(r io.Reader, archivePath string) ([]Member, error) {
	data, err := io.ReadAll(io.LimitReader(r, 64<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<20 {
		return nil, fmt.Errorf("archive exceeds 64 MiB")
	}
	thin := false
	switch {
	case bytes.HasPrefix(data, []byte(regularMagic)):
		data = data[len(regularMagic):]
	case bytes.HasPrefix(data, []byte(thinMagic)):
		thin = true
		data = data[len(thinMagic):]
	default:
		return nil, fmt.Errorf("invalid ar archive")
	}
	var longNames []byte
	var members []Member
	for len(data) > 0 {
		if len(data) < headerSize {
			return members, fmt.Errorf("truncated ar header")
		}
		header, body := data[:headerSize], data[headerSize:]
		if string(header[58:60]) != "`\n" {
			return members, fmt.Errorf("invalid ar header")
		}
		size, err := strconv.Atoi(strings.TrimSpace(string(header[48:58])))
		if err != nil || size < 0 {
			return members, fmt.Errorf("invalid ar member size")
		}
		if size > len(body) {
			return members, fmt.Errorf("truncated ar member")
		}
		name := strings.TrimSpace(string(header[:16]))
		content := body[:size]
		if name == "//" {
			longNames = append([]byte(nil), content...)
		} else if name != "/" && !strings.HasPrefix(name, "__.SYMDEF") {
			name, content = memberName(name, content, longNames)
			if name != "" {
				member := Member{Name: name, Thin: thin}
				if thin {
					member.Path = filepath.Join(filepath.Dir(archivePath), filepath.FromSlash(name))
				} else {
					member.Path = name
					member.Content = content
				}
				members = append(members, member)
			}
		}
		advance := size
		if advance%2 != 0 {
			advance++
		}
		if advance > len(body) {
			return members, fmt.Errorf("truncated ar padding")
		}
		data = body[advance:]
	}
	return members, nil
}

func memberName(name string, content, longNames []byte) (string, []byte) {
	if strings.HasPrefix(name, "#1/") {
		length, err := strconv.Atoi(strings.TrimPrefix(name, "#1/"))
		if err == nil && length >= 0 && length <= len(content) {
			return string(content[:length]), content[length:]
		}
		return "", content
	}
	if strings.HasPrefix(name, "/") && len(name) > 1 {
		offset, err := strconv.Atoi(strings.TrimPrefix(name, "/"))
		if err == nil && offset >= 0 && offset < len(longNames) {
			end := bytes.IndexByte(longNames[offset:], '\n')
			if end >= 0 {
				return strings.TrimSuffix(string(longNames[offset:offset+end]), "/"), content
			}
		}
	}
	return strings.TrimSuffix(name, "/"), content
}
